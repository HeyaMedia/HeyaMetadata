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

// Adaptive artist refreshes rebuild the root identity and catalog that all
// release-group, release, and recording work hangs from. Keep them ahead of
// scheduled child refreshes without allowing them to overtake interactive or
// stale-on-read work.
const adaptiveArtistPriority = PriorityCatalog

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
		WITH candidates AS (
			SELECT refresh.provider,claims.normalized_value,refresh.entity_id,refresh.next_eligible_at,
			       CASE refresh.provider WHEN 'musicbrainz' THEN 1 WHEN 'apple' THEN 2 ELSE 3 END provider_priority,
			       COALESCE(stats.decayed_score * exp(-EXTRACT(EPOCH FROM (now() - stats.score_updated_at)) / 604800.0),0) access_score
			FROM provider_refresh_states refresh
			JOIN external_id_claims claims ON claims.entity_id=refresh.entity_id
			LEFT JOIN entity_access_stats stats ON stats.entity_id=refresh.entity_id
			WHERE refresh.provider IN('musicbrainz','apple','deezer') AND refresh.next_eligible_at<=now()
			  AND NOT EXISTS(SELECT 1 FROM river_job active WHERE active.id=refresh.current_job_id AND active.state IN('available','pending','retryable','running','scheduled'))
			  AND claims.entity_kind='artist' AND claims.provider=refresh.provider AND claims.namespace='artist' AND claims.state='accepted'
		), chosen AS (
			SELECT DISTINCT ON(entity_id) provider,normalized_value,entity_id,next_eligible_at,access_score
			FROM candidates
			ORDER BY entity_id,provider_priority
		)
		SELECT provider,normalized_value,entity_id FROM chosen
		ORDER BY access_score DESC,next_eligible_at
		LIMIT 500`)
	if err != nil {
		return fmt.Errorf("select adaptive artist refreshes: %w", err)
	}
	type dueArtist struct{ provider, providerID, entityID string }
	var artists []dueArtist
	for artistRows.Next() {
		var artist dueArtist
		if err := artistRows.Scan(&artist.provider, &artist.providerID, &artist.entityID); err != nil {
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
		inserted, err := InsertArtist(ctx, w.runtime, client, ArtistIngestArgs{Provider: artist.provider, ProviderID: artist.providerID, Reason: "adaptive_refresh"}, adaptiveArtistPriority)
		if err != nil {
			return fmt.Errorf("enqueue adaptive refresh for %s artist %s: %w", artist.provider, artist.providerID, err)
		}
		_, _ = w.runtime.DB.Exec(ctx, `UPDATE provider_refresh_states SET current_job_id=$3 WHERE entity_id=$1 AND provider=$2`, artist.entityID, artist.provider, inserted.Job.ID)
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
	issuedRows, err := w.runtime.DB.Query(ctx, `SELECT claims.normalized_value,refresh.entity_id FROM provider_refresh_states refresh JOIN external_id_claims claims ON claims.entity_id=refresh.entity_id LEFT JOIN entity_access_stats stats ON stats.entity_id=refresh.entity_id WHERE refresh.provider='musicbrainz' AND refresh.next_eligible_at<=now() AND NOT EXISTS(SELECT 1 FROM river_job active WHERE active.id=refresh.current_job_id AND active.state IN('available','pending','retryable','running','scheduled')) AND claims.entity_kind='release' AND claims.provider='musicbrainz' AND claims.namespace='release' AND claims.state='accepted' ORDER BY COALESCE(stats.decayed_score*exp(-EXTRACT(EPOCH FROM(now()-stats.score_updated_at))/604800.0),0)DESC,refresh.next_eligible_at LIMIT 500`)
	if err != nil {
		return fmt.Errorf("select adaptive release refreshes: %w", err)
	}
	var issued []dueReleaseGroup
	for issuedRows.Next() {
		var item dueReleaseGroup
		if err := issuedRows.Scan(&item.mbid, &item.entityID); err != nil {
			issuedRows.Close()
			return err
		}
		issued = append(issued, item)
	}
	if err := issuedRows.Err(); err != nil {
		issuedRows.Close()
		return err
	}
	issuedRows.Close()
	for _, item := range issued {
		inserted, err := InsertRelease(ctx, w.runtime, client, ReleaseIngestArgs{MusicBrainzID: item.mbid, Reason: "adaptive_refresh"}, PriorityScheduled)
		if err != nil {
			return err
		}
		_, _ = w.runtime.DB.Exec(ctx, `UPDATE provider_refresh_states SET current_job_id=$3 WHERE entity_id=$1 AND provider=$2`, item.entityID, "musicbrainz", inserted.Job.ID)
	}
	standaloneRecordingRows, err := w.runtime.DB.Query(ctx, `SELECT claims.normalized_value,refresh.entity_id FROM provider_refresh_states refresh JOIN external_id_claims claims ON claims.entity_id=refresh.entity_id LEFT JOIN entity_access_stats stats ON stats.entity_id=refresh.entity_id WHERE refresh.provider='musicbrainz' AND refresh.next_eligible_at<=now() AND NOT EXISTS(SELECT 1 FROM river_job active WHERE active.id=refresh.current_job_id AND active.state IN('available','pending','retryable','running','scheduled')) AND claims.entity_kind='recording' AND claims.provider='musicbrainz' AND claims.namespace='recording' AND claims.state='accepted' ORDER BY COALESCE(stats.decayed_score*exp(-EXTRACT(EPOCH FROM(now()-stats.score_updated_at))/604800.0),0) DESC,refresh.next_eligible_at LIMIT 500`)
	if err != nil {
		return fmt.Errorf("select adaptive recording refreshes: %w", err)
	}
	var standaloneRecordings []dueReleaseGroup
	for standaloneRecordingRows.Next() {
		var item dueReleaseGroup
		if err := standaloneRecordingRows.Scan(&item.mbid, &item.entityID); err != nil {
			standaloneRecordingRows.Close()
			return err
		}
		standaloneRecordings = append(standaloneRecordings, item)
	}
	if err := standaloneRecordingRows.Err(); err != nil {
		standaloneRecordingRows.Close()
		return err
	}
	standaloneRecordingRows.Close()
	for _, item := range standaloneRecordings {
		inserted, err := InsertRecording(ctx, w.runtime, client, RecordingIngestArgs{MusicBrainzID: item.mbid, Reason: "adaptive_refresh"}, PriorityScheduled)
		if err != nil {
			return err
		}
		_, _ = w.runtime.DB.Exec(ctx, `UPDATE provider_refresh_states SET current_job_id=$3 WHERE entity_id=$1 AND provider=$2`, item.entityID, "musicbrainz", inserted.Job.ID)
	}
	musicalWorkRows, err := w.runtime.DB.Query(ctx, `SELECT claims.normalized_value,refresh.entity_id FROM provider_refresh_states refresh JOIN external_id_claims claims ON claims.entity_id=refresh.entity_id LEFT JOIN entity_access_stats stats ON stats.entity_id=refresh.entity_id WHERE refresh.provider='openopus' AND refresh.next_eligible_at<=now() AND NOT EXISTS(SELECT 1 FROM river_job active WHERE active.id=refresh.current_job_id AND active.state IN('available','pending','retryable','running','scheduled')) AND claims.entity_kind='musical_work' AND claims.provider='openopus' AND claims.namespace='work' AND claims.state='accepted' ORDER BY COALESCE(stats.decayed_score*exp(-EXTRACT(EPOCH FROM(now()-stats.score_updated_at))/604800.0),0) DESC,refresh.next_eligible_at LIMIT 500`)
	if err != nil {
		return fmt.Errorf("select adaptive musical-work refreshes: %w", err)
	}
	var musicalWorks []dueReleaseGroup
	for musicalWorkRows.Next() {
		var item dueReleaseGroup
		if err := musicalWorkRows.Scan(&item.mbid, &item.entityID); err != nil {
			musicalWorkRows.Close()
			return err
		}
		musicalWorks = append(musicalWorks, item)
	}
	if err := musicalWorkRows.Err(); err != nil {
		musicalWorkRows.Close()
		return err
	}
	musicalWorkRows.Close()
	for _, item := range musicalWorks {
		inserted, err := InsertMusicalWork(ctx, w.runtime, client, MusicalWorkIngestArgs{OpenOpusWorkID: item.mbid, Reason: "adaptive_refresh"}, PriorityScheduled)
		if err != nil {
			return err
		}
		_, _ = w.runtime.DB.Exec(ctx, `UPDATE provider_refresh_states SET current_job_id=$3 WHERE entity_id=$1 AND provider=$2`, item.entityID, "openopus", inserted.Job.ID)
	}
	recordingRows, err := w.runtime.DB.Query(ctx, `SELECT refresh.entity_id FROM provider_refresh_states refresh LEFT JOIN entity_access_stats stats ON stats.entity_id=refresh.entity_id WHERE refresh.provider='lrclib' AND refresh.next_eligible_at<=now() AND NOT EXISTS(SELECT 1 FROM river_job active WHERE active.id=refresh.current_job_id AND active.state IN('available','pending','retryable','running','scheduled')) ORDER BY COALESCE(stats.decayed_score*exp(-EXTRACT(EPOCH FROM(now()-stats.score_updated_at))/604800.0),0) DESC,refresh.next_eligible_at LIMIT 200`)
	if err != nil {
		return fmt.Errorf("select recording evidence refreshes: %w", err)
	}
	var recordingIDs []string
	for recordingRows.Next() {
		var id string
		if err := recordingRows.Scan(&id); err != nil {
			recordingRows.Close()
			return err
		}
		recordingIDs = append(recordingIDs, id)
	}
	if err := recordingRows.Err(); err != nil {
		recordingRows.Close()
		return err
	}
	recordingRows.Close()
	for _, id := range recordingIDs {
		if _, err := InsertRecordingEvidenceRefresh(ctx, w.runtime, client, id); err != nil {
			return fmt.Errorf("enqueue recording evidence refresh %s: %w", id, err)
		}
	}
	tvRows, err := w.runtime.DB.Query(ctx, `
		WITH due AS (
		  SELECT DISTINCT ON (refresh.entity_id)
		    root.provider, root.normalized_value, refresh.entity_id, refresh.provider AS refresh_provider, refresh.next_eligible_at
		  FROM provider_refresh_states refresh
		  JOIN LATERAL (
		  SELECT claim.provider, claim.normalized_value
		  FROM external_id_claims claim
		  WHERE claim.entity_id=refresh.entity_id AND claim.entity_kind='tv_show' AND claim.state='accepted'
		    AND ((claim.provider='tmdb' AND claim.namespace='tv') OR (claim.provider='tvmaze' AND claim.namespace='show'))
		  ORDER BY CASE claim.provider WHEN 'tmdb' THEN 1 ELSE 2 END
		  LIMIT 1
		  ) root ON true
		  WHERE refresh.provider IN ('tmdb','tvmaze') AND refresh.next_eligible_at<=now()
		  AND NOT EXISTS(SELECT 1 FROM river_job active WHERE active.id=refresh.current_job_id AND active.state IN ('available','pending','retryable','running','scheduled'))
		  ORDER BY refresh.entity_id, CASE WHEN refresh.provider=root.provider THEN 1 ELSE 2 END, refresh.next_eligible_at
		)
		SELECT due.provider,due.normalized_value,due.entity_id,due.refresh_provider
		FROM due LEFT JOIN entity_access_stats stats ON stats.entity_id=due.entity_id
		ORDER BY COALESCE(stats.decayed_score * exp(-EXTRACT(EPOCH FROM (now()-stats.score_updated_at))/604800.0),0) DESC,
		  due.next_eligible_at
		LIMIT 500`)
	if err != nil {
		return err
	}
	type dueEpisodic struct{ provider, id, entityID, refreshProvider string }
	var tvShows []dueEpisodic
	for tvRows.Next() {
		var item dueEpisodic
		if err := tvRows.Scan(&item.provider, &item.id, &item.entityID, &item.refreshProvider); err != nil {
			tvRows.Close()
			return err
		}
		tvShows = append(tvShows, item)
	}
	tvRows.Close()
	for _, item := range tvShows {
		inserted, err := InsertTVShow(ctx, w.runtime, client, TVShowIngestArgs{Provider: item.provider, ProviderID: item.id, Reason: "adaptive_refresh"}, PriorityScheduled)
		if err != nil {
			return err
		}
		_, _ = w.runtime.DB.Exec(ctx, `UPDATE provider_refresh_states SET current_job_id=$3 WHERE entity_id=$1 AND provider=$2`, item.entityID, item.refreshProvider, inserted.Job.ID)
	}
	animeRows, err := w.runtime.DB.Query(ctx, `
		WITH due AS (
		  SELECT DISTINCT ON (refresh.entity_id)
		    root.provider, root.normalized_value, refresh.entity_id, refresh.provider AS refresh_provider, refresh.next_eligible_at
		  FROM provider_refresh_states refresh
		  JOIN LATERAL (
		  SELECT claim.provider, claim.normalized_value
		  FROM external_id_claims claim
		  WHERE claim.entity_id=refresh.entity_id AND claim.entity_kind='anime' AND claim.state='accepted'
		    AND ((claim.provider='tmdb' AND claim.namespace='tv') OR (claim.provider='tvmaze' AND claim.namespace='show') OR (claim.provider='anidb' AND claim.namespace='anime'))
		  ORDER BY CASE claim.provider WHEN 'tmdb' THEN 1 WHEN 'tvmaze' THEN 2 ELSE 3 END
		  LIMIT 1
		  ) root ON true
		  WHERE refresh.provider IN ('tmdb','tvmaze','anidb') AND refresh.next_eligible_at<=now()
		  AND NOT EXISTS(SELECT 1 FROM river_job active WHERE active.id=refresh.current_job_id AND active.state IN ('available','pending','retryable','running','scheduled'))
		  ORDER BY refresh.entity_id, CASE WHEN refresh.provider=root.provider THEN 1 ELSE 2 END, refresh.next_eligible_at
		)
		SELECT due.provider,due.normalized_value,due.entity_id,due.refresh_provider
		FROM due LEFT JOIN entity_access_stats stats ON stats.entity_id=due.entity_id
		ORDER BY COALESCE(stats.decayed_score * exp(-EXTRACT(EPOCH FROM (now()-stats.score_updated_at))/604800.0),0) DESC,
		  due.next_eligible_at
		LIMIT 500`)
	if err != nil {
		return err
	}
	var animeItems []dueEpisodic
	for animeRows.Next() {
		var item dueEpisodic
		if err := animeRows.Scan(&item.provider, &item.id, &item.entityID, &item.refreshProvider); err != nil {
			animeRows.Close()
			return err
		}
		animeItems = append(animeItems, item)
	}
	animeRows.Close()
	for _, item := range animeItems {
		inserted, err := InsertAnime(ctx, w.runtime, client, AnimeIngestArgs{Provider: item.provider, ProviderID: item.id, Reason: "adaptive_refresh"}, PriorityScheduled)
		if err != nil {
			return err
		}
		_, _ = w.runtime.DB.Exec(ctx, `UPDATE provider_refresh_states SET current_job_id=$3 WHERE entity_id=$1 AND provider=$2`, item.entityID, item.refreshProvider, inserted.Job.ID)
	}
	personRows, err := w.runtime.DB.Query(ctx, `SELECT refresh.provider,claims.normalized_value,refresh.entity_id FROM provider_refresh_states refresh JOIN external_id_claims claims ON claims.entity_id=refresh.entity_id AND claims.provider=refresh.provider LEFT JOIN entity_access_stats stats ON stats.entity_id=refresh.entity_id WHERE refresh.provider IN('tmdb','tvmaze','tvdb') AND refresh.next_eligible_at<=now() AND NOT EXISTS(SELECT 1 FROM river_job active WHERE active.id=refresh.current_job_id AND active.state IN('available','pending','retryable','running','scheduled')) AND claims.entity_kind='person' AND claims.namespace='person' AND claims.state='accepted' ORDER BY COALESCE(stats.decayed_score*exp(-EXTRACT(EPOCH FROM(now()-stats.score_updated_at))/604800.0),0)DESC,refresh.next_eligible_at LIMIT 600`)
	if err != nil {
		return err
	}
	type duePerson struct {
		entityID string
		ids      map[string]string
	}
	peopleByID := map[string]*duePerson{}
	var people []*duePerson
	for personRows.Next() {
		var provider, value, entityID string
		if err := personRows.Scan(&provider, &value, &entityID); err != nil {
			personRows.Close()
			return err
		}
		item := peopleByID[entityID]
		if item == nil {
			item = &duePerson{entityID: entityID, ids: map[string]string{}}
			peopleByID[entityID] = item
			people = append(people, item)
		}
		item.ids[provider] = value
	}
	if err := personRows.Err(); err != nil {
		personRows.Close()
		return err
	}
	personRows.Close()
	for _, item := range people {
		inserted, err := InsertPersonEnrich(ctx, w.runtime, client, PersonEnrichArgs{EntityID: item.entityID, TMDBID: item.ids["tmdb"], TVMazeID: item.ids["tvmaze"], TVDBID: item.ids["tvdb"], Reason: "adaptive_refresh"}, PriorityScheduled)
		if err != nil {
			return err
		}
		providers := make([]string, 0, len(item.ids))
		for provider := range item.ids {
			providers = append(providers, provider)
		}
		_, _ = w.runtime.DB.Exec(ctx, `UPDATE provider_refresh_states SET current_job_id=$2 WHERE entity_id=$1 AND provider=ANY($3)`, item.entityID, inserted.Job.ID, providers)
	}
	return nil
}
