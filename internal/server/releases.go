package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
)

type musicEntityInput struct {
	ID string `path:"id" format:"uuid"`
}

type recordingFingerprint struct {
	ID               string    `json:"id"`
	Algorithm        string    `json:"algorithm"`
	AlgorithmVersion string    `json:"algorithm_version"`
	GeneratorVersion string    `json:"generator_version"`
	Encoding         string    `json:"encoding"`
	Fingerprint      string    `json:"fingerprint"`
	DurationMS       int64     `json:"duration_ms,omitempty"`
	HashCount        int       `json:"hash_count"`
	SourceProvider   string    `json:"source_provider"`
	SourceTrackID    string    `json:"source_track_id"`
	SourceChecksum   string    `json:"source_checksum"`
	GeneratedAt      time.Time `json:"generated_at"`
}

type recordingFingerprintsOutput struct {
	Body struct {
		RecordingID string                 `json:"recording_id"`
		Items       []recordingFingerprint `json:"items"`
	}
}

type recordingLyrics struct {
	ID                  string    `json:"id"`
	Provider            string    `json:"provider"`
	ProviderRecordID    string    `json:"provider_record_id"`
	TrackName           string    `json:"track_name"`
	ArtistName          string    `json:"artist_name"`
	AlbumName           string    `json:"album_name,omitempty"`
	DurationMS          int64     `json:"duration_ms,omitempty"`
	Instrumental        bool      `json:"instrumental"`
	PlainLyrics         string    `json:"plain_lyrics,omitempty"`
	SyncedLyrics        string    `json:"synced_lyrics,omitempty"`
	ContentChecksum     string    `json:"content_checksum"`
	SourceObservationID string    `json:"source_observation_id"`
	ObservedAt          time.Time `json:"observed_at"`
}

type recordingLyricsOutput struct {
	Body struct {
		RecordingID string            `json:"recording_id"`
		Items       []recordingLyrics `json:"items"`
	}
}

func registerReleases(api huma.API, runtime *platform.Runtime) {
	read := func(kind string) func(context.Context, *musicEntityInput) (*entityOutput, error) {
		return func(ctx context.Context, input *musicEntityInput) (*entityOutput, error) {
			if runtime == nil {
				return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
			}
			var body []byte
			if err := runtime.DB.QueryRow(ctx, `SELECT d.document FROM api_documents d JOIN entities e ON e.id=d.entity_id WHERE d.entity_id=$1 AND d.document_kind='detail' AND e.kind=$2`, input.ID, kind).Scan(&body); err == pgx.ErrNoRows {
				return nil, huma.Error404NotFound(kind + " not found")
			} else if err != nil {
				return nil, err
			}
			var document any
			if err := json.Unmarshal(body, &document); err != nil {
				return nil, err
			}
			return &entityOutput{Body: document}, nil
		}
	}
	huma.Register(api, huma.Operation{OperationID: "release-detail", Method: http.MethodGet, Path: "/api/v2/releases/{id}", Summary: "Get a canonical issued music release", Tags: []string{"Music"}}, read("release"))
	huma.Register(api, huma.Operation{OperationID: "recording-detail", Method: http.MethodGet, Path: "/api/v2/recordings/{id}", Summary: "Get a canonical music recording", Tags: []string{"Music"}}, read("recording"))
	huma.Register(api, huma.Operation{OperationID: "recording-fingerprints", Method: http.MethodGet, Path: "/api/v2/recordings/{id}/fingerprints", Summary: "Get derived Chromaprint evidence for a recording", Tags: []string{"Music"}}, func(ctx context.Context, input *musicEntityInput) (*recordingFingerprintsOutput, error) {
		if runtime == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		var exists bool
		if err := runtime.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM entities WHERE id=$1 AND kind='recording')`, input.ID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, huma.Error404NotFound("recording not found")
		}
		rows, err := runtime.DB.Query(ctx, `SELECT id,algorithm,algorithm_version,generator_version,fingerprint,COALESCE(duration_ms,0),hash_count,source_provider,source_track_id,source_checksum,generated_at FROM recording_fingerprints WHERE recording_entity_id=$1 AND state='ready' ORDER BY generated_at DESC,id`, input.ID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := &recordingFingerprintsOutput{}
		out.Body.RecordingID, out.Body.Items = input.ID, []recordingFingerprint{}
		for rows.Next() {
			var item recordingFingerprint
			var packed []byte
			item.Encoding = "base64-uint32le"
			if err := rows.Scan(&item.ID, &item.Algorithm, &item.AlgorithmVersion, &item.GeneratorVersion, &packed, &item.DurationMS, &item.HashCount, &item.SourceProvider, &item.SourceTrackID, &item.SourceChecksum, &item.GeneratedAt); err != nil {
				return nil, err
			}
			item.Fingerprint = base64.StdEncoding.EncodeToString(packed)
			out.Body.Items = append(out.Body.Items, item)
		}
		return out, rows.Err()
	})
	huma.Register(api, huma.Operation{OperationID: "recording-lyrics", Method: http.MethodGet, Path: "/api/v2/recordings/{id}/lyrics", Summary: "Get plain and synchronized lyrics evidence for a recording", Tags: []string{"Music"}}, func(ctx context.Context, input *musicEntityInput) (*recordingLyricsOutput, error) {
		if runtime == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		var exists bool
		if err := runtime.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM entities WHERE id=$1 AND kind='recording')`, input.ID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, huma.Error404NotFound("recording not found")
		}
		rows, err := runtime.DB.Query(ctx, `SELECT id,provider,provider_record_id,track_name,artist_name,album_name,COALESCE(duration_ms,0),instrumental,COALESCE(plain_lyrics,''),COALESCE(synced_lyrics,''),content_checksum,source_observation_id,observed_at FROM recording_lyrics WHERE recording_entity_id=$1 ORDER BY observed_at DESC,id`, input.ID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := &recordingLyricsOutput{}
		out.Body.RecordingID, out.Body.Items = input.ID, []recordingLyrics{}
		for rows.Next() {
			var item recordingLyrics
			if err := rows.Scan(&item.ID, &item.Provider, &item.ProviderRecordID, &item.TrackName, &item.ArtistName, &item.AlbumName, &item.DurationMS, &item.Instrumental, &item.PlainLyrics, &item.SyncedLyrics, &item.ContentChecksum, &item.SourceObservationID, &item.ObservedAt); err != nil {
				return nil, err
			}
			out.Body.Items = append(out.Body.Items, item)
		}
		return out, rows.Err()
	})
}
