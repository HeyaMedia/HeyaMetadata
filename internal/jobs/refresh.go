package jobs

import (
	"context"
	"fmt"

	"github.com/HeyaMedia/HeyaMetadata/internal/accessstats"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const RefreshSchedulerKind = "adaptive_refresh_scheduler_v1"

type RefreshSchedulerArgs struct{}

func (RefreshSchedulerArgs) Kind() string { return RefreshSchedulerKind }
func (RefreshSchedulerArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Priority: PriorityScheduled, MaxAttempts: 3,
		UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: activeJobStates()},
	}
}

func activeJobStates() []rivertype.JobState {
	return []rivertype.JobState{
		rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning,
		rivertype.JobStateRetryable, rivertype.JobStateScheduled,
	}
}

type RefreshSchedulerWorker struct {
	river.WorkerDefaults[RefreshSchedulerArgs]
	runtime *platform.Runtime
}

func NewRefreshSchedulerWorker(runtime *platform.Runtime) *RefreshSchedulerWorker {
	return &RefreshSchedulerWorker{runtime: runtime}
}

func (w *RefreshSchedulerWorker) Work(ctx context.Context, _ *river.Job[RefreshSchedulerArgs]) error {
	if _, err := accessstats.Flush(ctx, w.runtime, 1000); err != nil {
		return err
	}
	if err := accessstats.RecalculateRefreshCadence(ctx, w.runtime); err != nil {
		return err
	}
	rows, err := w.runtime.DB.Query(ctx, `
		SELECT claims.normalized_value::bigint, refresh.entity_id
		FROM provider_refresh_states refresh
		JOIN external_id_claims claims ON claims.entity_id = refresh.entity_id
		LEFT JOIN entity_access_stats stats ON stats.entity_id = refresh.entity_id
		WHERE refresh.provider = 'tmdb'
		  AND refresh.next_eligible_at <= now()
		  AND NOT EXISTS (
			SELECT 1 FROM river_job active
			WHERE active.id = refresh.current_job_id
			  AND active.state IN ('available', 'pending', 'retryable', 'running', 'scheduled')
		  )
		  AND claims.entity_kind = 'movie' AND claims.provider = 'tmdb'
		  AND claims.namespace = 'movie' AND claims.state = 'accepted'
		ORDER BY COALESCE(stats.decayed_score * exp(-EXTRACT(EPOCH FROM (now() - stats.score_updated_at)) / 604800.0), 0) DESC,
		         refresh.next_eligible_at
		LIMIT 500`)
	if err != nil {
		return fmt.Errorf("select adaptive movie refreshes: %w", err)
	}
	type dueMovie struct {
		tmdbID   int64
		entityID string
	}
	var due []dueMovie
	for rows.Next() {
		var movie dueMovie
		if err := rows.Scan(&movie.tmdbID, &movie.entityID); err != nil {
			rows.Close()
			return err
		}
		due = append(due, movie)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	client := river.ClientFromContext[pgx.Tx](ctx)
	for _, movie := range due {
		inserted, err := InsertMovie(ctx, w.runtime, client, MovieIngestArgs{
			TMDBID: movie.tmdbID, Reason: "adaptive_refresh",
		}, PriorityScheduled)
		if err != nil {
			return fmt.Errorf("enqueue adaptive refresh for TMDB movie %d: %w", movie.tmdbID, err)
		}
		_, _ = w.runtime.DB.Exec(ctx, `
			UPDATE provider_refresh_states
			SET current_job_id = $3
			WHERE entity_id = $1 AND provider = $2`, movie.entityID, "tmdb", inserted.Job.ID)
	}
	artistRows, err := w.runtime.DB.Query(ctx, `
		SELECT claims.normalized_value, refresh.entity_id
		FROM provider_refresh_states refresh
		JOIN external_id_claims claims ON claims.entity_id = refresh.entity_id
		LEFT JOIN entity_access_stats stats ON stats.entity_id = refresh.entity_id
		WHERE refresh.provider = 'musicbrainz' AND refresh.next_eligible_at <= now()
		  AND NOT EXISTS (SELECT 1 FROM river_job active WHERE active.id = refresh.current_job_id AND active.state IN ('available','pending','retryable','running','scheduled'))
		  AND claims.entity_kind='artist' AND claims.provider='musicbrainz' AND claims.namespace='artist' AND claims.state='accepted'
		ORDER BY COALESCE(stats.decayed_score * exp(-EXTRACT(EPOCH FROM (now() - stats.score_updated_at)) / 604800.0),0) DESC,refresh.next_eligible_at
		LIMIT 500`)
	if err != nil {
		return fmt.Errorf("select adaptive artist refreshes: %w", err)
	}
	type dueArtist struct{ mbid, entityID string }
	var artists []dueArtist
	for artistRows.Next() {
		var artist dueArtist
		if err := artistRows.Scan(&artist.mbid, &artist.entityID); err != nil {
			artistRows.Close()
			return err
		}
		artists = append(artists, artist)
	}
	if err := artistRows.Err(); err != nil {
		artistRows.Close()
		return err
	}
	artistRows.Close()
	for _, artist := range artists {
		inserted, err := InsertArtist(ctx, w.runtime, client, ArtistIngestArgs{MusicBrainzID: artist.mbid, Reason: "adaptive_refresh"}, PriorityScheduled)
		if err != nil {
			return fmt.Errorf("enqueue adaptive refresh for MusicBrainz artist %s: %w", artist.mbid, err)
		}
		_, _ = w.runtime.DB.Exec(ctx, `UPDATE provider_refresh_states SET current_job_id=$3 WHERE entity_id=$1 AND provider=$2`, artist.entityID, "musicbrainz", inserted.Job.ID)
	}
	releaseRows, err := w.runtime.DB.Query(ctx, `SELECT claims.normalized_value,refresh.entity_id FROM provider_refresh_states refresh JOIN external_id_claims claims ON claims.entity_id=refresh.entity_id LEFT JOIN entity_access_stats stats ON stats.entity_id=refresh.entity_id WHERE refresh.provider='musicbrainz' AND refresh.next_eligible_at<=now() AND NOT EXISTS(SELECT 1 FROM river_job active WHERE active.id=refresh.current_job_id AND active.state IN ('available','pending','retryable','running','scheduled')) AND claims.entity_kind='release_group' AND claims.provider='musicbrainz' AND claims.namespace='release_group' AND claims.state='accepted' ORDER BY COALESCE(stats.decayed_score * exp(-EXTRACT(EPOCH FROM (now()-stats.score_updated_at))/604800.0),0) DESC,refresh.next_eligible_at LIMIT 500`)
	if err != nil {
		return fmt.Errorf("select adaptive release-group refreshes: %w", err)
	}
	type dueReleaseGroup struct{ mbid, entityID string }
	var releaseGroups []dueReleaseGroup
	for releaseRows.Next() {
		var item dueReleaseGroup
		if err := releaseRows.Scan(&item.mbid, &item.entityID); err != nil {
			releaseRows.Close()
			return err
		}
		releaseGroups = append(releaseGroups, item)
	}
	if err := releaseRows.Err(); err != nil {
		releaseRows.Close()
		return err
	}
	releaseRows.Close()
	for _, item := range releaseGroups {
		inserted, err := InsertReleaseGroup(ctx, w.runtime, client, ReleaseGroupIngestArgs{MusicBrainzID: item.mbid, Reason: "adaptive_refresh"}, PriorityScheduled)
		if err != nil {
			return err
		}
		_, _ = w.runtime.DB.Exec(ctx, `UPDATE provider_refresh_states SET current_job_id=$3 WHERE entity_id=$1 AND provider=$2`, item.entityID, "musicbrainz", inserted.Job.ID)
	}
	return nil
}
