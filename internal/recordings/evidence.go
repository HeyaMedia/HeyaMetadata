package recordings

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	releasedomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/release"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/lrclib"
	"github.com/jackc/pgx/v5"
)

func (s *Service) RefreshLyricsEvidence(ctx context.Context, entityID string, jobID int64) error {
	doc, _, err := s.Detail(ctx, entityID)
	if err != nil {
		return err
	}
	signatures, err := s.lyricsSignatures(ctx, doc)
	if err != nil {
		return err
	}
	if len(signatures) == 0 {
		_, _ = s.runtime.DB.Exec(ctx, `UPDATE provider_refresh_states SET last_attempt_at=now(),current_job_id=NULL,next_eligible_at=now()+interval '30 days' WHERE entity_id=$1 AND provider='lrclib'`, entityID)
		return nil
	}
	base := lrclib.New(s.runtime.Config.Providers.LRCLIB)
	resolver, err := providercache.New(s.runtime, lrclib.NormalizerVersion, base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return err
	}
	client := lrclib.NewBackgroundCached(s.runtime.Config.Providers.LRCLIB, resolver)
	var lastErr error
	for _, signature := range signatures {
		payload, fetchErr := client.Get(ctx, signature)
		if fetchErr != nil {
			lastErr = fetchErr
			continue
		}
		if payload.StatusCode == http.StatusNotFound {
			continue
		}
		if payload.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("LRCLIB returned HTTP %d", payload.StatusCode)
			continue
		}
		evidence, normalizeErr := lrclib.Normalize(payload.Body, payload.ObservationID, payload.ObservedAt, signature)
		if normalizeErr != nil {
			lastErr = normalizeErr
			continue
		}
		_, err = s.runtime.DB.Exec(ctx, `INSERT INTO recording_lyrics(recording_entity_id,provider,provider_record_id,track_name,artist_name,album_name,duration_ms,instrumental,plain_lyrics,synced_lyrics,content_checksum,source_observation_id,observed_at)VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,0),$8,NULLIF($9,''),NULLIF($10,''),$11,$12,$13)ON CONFLICT(recording_entity_id,provider,provider_record_id)DO UPDATE SET track_name=EXCLUDED.track_name,artist_name=EXCLUDED.artist_name,album_name=EXCLUDED.album_name,duration_ms=EXCLUDED.duration_ms,instrumental=EXCLUDED.instrumental,plain_lyrics=EXCLUDED.plain_lyrics,synced_lyrics=EXCLUDED.synced_lyrics,content_checksum=EXCLUDED.content_checksum,source_observation_id=EXCLUDED.source_observation_id,observed_at=EXCLUDED.observed_at,updated_at=now()`, entityID, evidence.Provider, evidence.ProviderRecordID, evidence.TrackName, evidence.ArtistName, evidence.AlbumName, evidence.DurationMS, evidence.Instrumental, evidence.PlainLyrics, evidence.SyncedLyrics, evidence.ContentChecksum, evidence.PrimaryObservationID, evidence.ObservedAt)
		if err != nil {
			return err
		}
		_, err = s.runtime.DB.Exec(ctx, `UPDATE provider_refresh_states SET last_attempt_at=now(),last_success_at=now(),last_observation_id=$2,current_job_id=NULL,next_eligible_at=now()+interval '30 days',failure_class=NULL,failure_message=NULL WHERE entity_id=$1 AND provider='lrclib'`, entityID, evidence.PrimaryObservationID)
		return err
	}
	if lastErr != nil {
		_, _ = s.runtime.DB.Exec(context.WithoutCancel(ctx), `UPDATE provider_refresh_states SET last_attempt_at=now(),next_eligible_at=now()+interval '6 hours',failure_class='transient',failure_message=$2 WHERE entity_id=$1 AND provider='lrclib'`, entityID, lastErr.Error())
		return lastErr
	}
	_, err = s.runtime.DB.Exec(ctx, `UPDATE provider_refresh_states SET last_attempt_at=now(),current_job_id=NULL,next_eligible_at=now()+interval '30 days',failure_class='not_found',failure_message=NULL WHERE entity_id=$1 AND provider='lrclib'`, entityID)
	return err
}

func (s *Service) lyricsSignatures(ctx context.Context, doc releasedomain.RecordingDocument) ([]lrclib.Signature, error) {
	artist := recordingArtistName(doc.Data)
	duration := int((doc.Data.DurationMS + 500) / 1000)
	if doc.Data.Title == "" || artist == "" || duration < 1 {
		return nil, nil
	}
	albums := []string{}
	for _, release := range doc.Data.Releases {
		albums = appendUnique(albums, release.Title)
	}
	rows, err := s.runtime.DB.Query(ctx, `SELECT DISTINCT cr.document#>>'{data,title}' FROM release_tracks rt JOIN canonical_releases cr ON cr.entity_id=rt.release_entity_id WHERE rt.recording_entity_id=$1 ORDER BY 1 LIMIT 10`, doc.ID)
	if err != nil && err != pgx.ErrNoRows {
		return nil, err
	}
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var album string
			if scanErr := rows.Scan(&album); scanErr != nil {
				return nil, scanErr
			}
			albums = appendUnique(albums, album)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	if len(albums) > 3 {
		albums = albums[:3]
	}
	out := make([]lrclib.Signature, 0, len(albums))
	for _, album := range albums {
		out = append(out, lrclib.Signature{TrackName: doc.Data.Title, ArtistName: artist, AlbumName: album, Duration: duration})
	}
	return out, nil
}

func recordingArtistName(recording releasedomain.Recording) string {
	var value strings.Builder
	for _, credit := range recording.ArtistCredits {
		value.WriteString(credit.Name)
		value.WriteString(credit.JoinPhrase)
	}
	return strings.TrimSpace(value.String())
}

func appendUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if strings.EqualFold(existing, value) {
			return values
		}
	}
	return append(values, value)
}
