package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/accessstats"
	"github.com/HeyaMedia/HeyaMetadata/internal/fingerprintmatch"
	"github.com/HeyaMedia/HeyaMetadata/internal/jobs"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
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
type fingerprintMatchCreateInput struct {
	Prefer         string `header:"Prefer"`
	AcoustIDAPIKey string `header:"X-Heya-AcoustID-API-Key"`
	Body           struct {
		Encoding            string `json:"encoding" enum:"base64-uint32le,acoustid,base64-uint32le+acoustid"`
		RawFingerprint      string `json:"raw_fingerprint,omitempty"`
		AcoustIDFingerprint string `json:"acoustid_fingerprint,omitempty"`
		DurationMS          int64  `json:"duration_ms" minimum:"1000" maximum:"86400000"`
	}
}
type fingerprintMatchGetInput struct {
	ID string `path:"id" format:"uuid"`
}
type fingerprintMatchResource struct {
	ID        string                        `json:"id"`
	State     string                        `json:"state"`
	Result    *fingerprintmatch.MatchResult `json:"result,omitempty"`
	Job       *jobResource                  `json:"job,omitempty"`
	Error     string                        `json:"error,omitempty"`
	ExpiresAt time.Time                     `json:"expires_at"`
}
type fingerprintMatchOutput struct {
	Status int
	Body   fingerprintMatchResource
}

func registerReleases(api huma.API, runtime *platform.Runtime) {
	var jobClient *river.Client[pgx.Tx]
	if runtime != nil {
		var err error
		jobClient, err = jobs.NewClient(runtime, runtime.Config.Worker.MaxWorkers, false)
		if err != nil {
			panic(err)
		}
	}
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
			_ = accessstats.Track(ctx, runtime.Redis, input.ID)
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
		_ = accessstats.Track(ctx, runtime.Redis, input.ID)
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
		_ = accessstats.Track(ctx, runtime.Redis, input.ID)
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
	huma.Register(api, huma.Operation{OperationID: "match-recording-fingerprint", Method: http.MethodPost, Path: "/api/v2/fingerprint-matches", Summary: "Match a client Chromaprint against canonical recordings", Description: "Runs local preview matching and optional AcoustID lookup as one durable, short-lived job. Submitted fingerprints expire after one hour and are erased immediately after completion.", Tags: []string{"Music"}, DefaultStatus: http.StatusOK}, func(ctx context.Context, input *fingerprintMatchCreateInput) (*fingerprintMatchOutput, error) {
		if runtime == nil || jobClient == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		var raw []byte
		var err error
		if input.Body.RawFingerprint != "" {
			raw, err = base64.StdEncoding.DecodeString(input.Body.RawFingerprint)
			if err != nil || len(raw)%4 != 0 || len(raw) == 0 {
				return nil, huma.Error400BadRequest("raw_fingerprint must be non-empty base64 uint32le")
			}
			if len(raw) > 256*1024 {
				return nil, huma.Error400BadRequest("raw_fingerprint exceeds 256 KiB")
			}
		}
		compressed := strings.TrimSpace(input.Body.AcoustIDFingerprint)
		if len(compressed) > 128*1024 {
			return nil, huma.Error400BadRequest("acoustid_fingerprint exceeds 128 KiB")
		}
		if len(raw) == 0 && compressed == "" {
			return nil, huma.Error400BadRequest("a raw or AcoustID fingerprint is required")
		}
		switch input.Body.Encoding {
		case "base64-uint32le":
			if len(raw) == 0 || compressed != "" {
				return nil, huma.Error400BadRequest("encoding requires only raw_fingerprint")
			}
		case "acoustid":
			if compressed == "" || len(raw) > 0 {
				return nil, huma.Error400BadRequest("encoding requires only acoustid_fingerprint")
			}
		case "base64-uint32le+acoustid":
			if len(raw) == 0 || compressed == "" {
				return nil, huma.Error400BadRequest("combined encoding requires both fingerprints")
			}
		default:
			return nil, huma.Error400BadRequest("unsupported fingerprint encoding")
		}
		run, err := fingerprintmatch.EnsureRun(ctx, runtime, fingerprintmatch.Request{RawFingerprint: raw, AcoustIDFingerprint: compressed, DurationMS: input.Body.DurationMS})
		if err != nil {
			return nil, err
		}
		credentials := providercredentials.Credentials{}
		if strings.TrimSpace(input.AcoustIDAPIKey) != "" {
			credentials.APIKeys = map[string]string{"acoustid": input.AcoustIDAPIKey}
		}
		ref, err := providercredentials.Store(ctx, runtime.Redis, credentials)
		if err != nil {
			return nil, err
		}
		inserted, err := jobs.InsertFingerprintMatch(ctx, runtime, jobClient, jobs.FingerprintMatchArgs{RunID: run.ID, CredentialRef: ref})
		if err != nil {
			return nil, err
		}
		run.RiverJobID = inserted.Job.ID
		wait := 1200 * time.Millisecond
		if preferredWait(input.Prefer) > 0 {
			wait = preferredWait(input.Prefer)
		}
		if input.Prefer == "respond-async" {
			wait = 0
		}
		if wait > 0 {
			deadline := time.NewTimer(wait)
			defer deadline.Stop()
			ticker := time.NewTicker(40 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-deadline.C:
					goto accepted
				case <-ticker.C:
					current, getErr := fingerprintmatch.GetRun(ctx, runtime, run.ID)
					if getErr == nil && (current.State == "completed" || current.State == "failed") {
						status := http.StatusOK
						if current.State == "failed" {
							status = http.StatusUnprocessableEntity
						}
						return &fingerprintMatchOutput{Status: status, Body: fingerprintMatchRunResource(current)}, nil
					}
				}
			}
		}
	accepted:
		return &fingerprintMatchOutput{Status: http.StatusAccepted, Body: fingerprintMatchRunResource(run)}, nil
	})
	huma.Register(api, huma.Operation{OperationID: "get-fingerprint-match", Method: http.MethodGet, Path: "/api/v2/fingerprint-matches/{id}", Summary: "Get fingerprint match status", Tags: []string{"Music"}}, func(ctx context.Context, input *fingerprintMatchGetInput) (*fingerprintMatchOutput, error) {
		if runtime == nil {
			return nil, huma.Error503ServiceUnavailable("runtime is unavailable")
		}
		run, err := fingerprintmatch.GetRun(ctx, runtime, input.ID)
		if err == pgx.ErrNoRows {
			return nil, huma.Error404NotFound("fingerprint match not found")
		}
		if err != nil {
			return nil, err
		}
		return &fingerprintMatchOutput{Status: http.StatusOK, Body: fingerprintMatchRunResource(run)}, nil
	})
}

func fingerprintMatchRunResource(run fingerprintmatch.Run) fingerprintMatchResource {
	resource := fingerprintMatchResource{ID: run.ID, State: run.State, Result: run.Result, Error: run.Error, ExpiresAt: run.ExpiresAt}
	if run.RiverJobID > 0 {
		resource.Job = &jobResource{ID: run.RiverJobID, Kind: jobs.FingerprintMatchKind, State: run.State}
	}
	return resource
}
