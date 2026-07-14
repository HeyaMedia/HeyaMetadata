package releases

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	releasedomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/release"
	"github.com/HeyaMedia/HeyaMetadata/internal/fingerprint"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/deezer"
)

type fingerprintEvidence struct {
	RecordingProviderID string
	SourceProvider      string
	SourceTrackID       string
	SourceChecksum      string
	GeneratorVersion    string
	Fingerprint         []byte
	DurationMS          int64
	HashCount           int
	State               string
	FailureClass        string
	FailureMessage      string
	RetryAfter          *time.Time
}

type evidenceBundle struct {
	Fingerprints []fingerprintEvidence
}

func (s *Service) collectRecordingEvidence(ctx context.Context, records []releasedomain.NormalizedRecord) evidenceBundle {
	if len(records) == 0 {
		return evidenceBundle{}
	}
	spine := records[0]
	bundle := evidenceBundle{}
	s.collectFingerprints(ctx, spine, records[1:], &bundle)
	return bundle
}

func (s *Service) collectFingerprints(ctx context.Context, spine releasedomain.NormalizedRecord, supplements []releasedomain.NormalizedRecord, bundle *evidenceBundle) {
	calculator := fingerprint.NewCalculator(s.runtime.Config.Chromaprint.FPCalcPath)
	limit := s.runtime.Config.Chromaprint.MaxPerRelease
	if limit == 0 || !calculator.Available() {
		if limit > 0 {
			slog.Warn("preview fingerprint generation skipped: fpcalc is unavailable")
		}
		return
	}
	processed := 0
	generatorVersion := calculator.Version(ctx)
	for _, medium := range spine.Media {
		for _, track := range medium.Tracks {
			for _, source := range supplements {
				if processed >= limit || ctx.Err() != nil {
					return
				}
				match := releasedomain.MatchTrack(track, source, medium.Position)
				if match == nil || match.PreviewURL == "" || match.ProviderID == "" {
					continue
				}
				var state string
				var retryAfter *time.Time
				err := s.runtime.DB.QueryRow(ctx, `SELECT state,retry_after FROM recording_fingerprints WHERE source_provider=$1 AND source_track_id=$2 AND algorithm_version=$3`, source.ProviderRecord.Provider, match.ProviderID, fingerprint.AlgorithmVersion).Scan(&state, &retryAfter)
				if err == nil && (state == "ready" || retryAfter != nil && retryAfter.After(time.Now())) {
					continue
				}
				processed++
				previewURL := match.PreviewURL
				// Album responses are cacheable metadata, but Deezer preview query
				// tokens are not. Renew the matched track URL immediately before use.
				if source.ProviderRecord.Provider == "deezer" {
					payloads, renewErr := deezer.New(s.runtime.Config.Providers.Deezer).Collect(ctx, providers.Identifier{Provider: "deezer", Namespace: "track", Value: match.ProviderID})
					if renewErr != nil || len(payloads) == 0 || payloads[0].StatusCode != http.StatusOK {
						slog.Warn("renew Deezer preview URL", "track_id", match.ProviderID, "error", renewErr)
						continue
					}
					previewURL, renewErr = deezer.TrackPreviewURL(payloads[0].Body, match.ProviderID)
					if renewErr != nil {
						slog.Warn("decode renewed Deezer preview URL", "track_id", match.ProviderID, "error", renewErr)
						continue
					}
				}
				generated, generateErr := calculator.FromURL(ctx, previewURL)
				evidence := fingerprintEvidence{
					RecordingProviderID: track.Recording.ProviderID,
					SourceProvider:      source.ProviderRecord.Provider, SourceTrackID: match.ProviderID,
					SourceChecksum:   fingerprint.SourceChecksum(source.ProviderRecord.Provider, match.ProviderID, previewURL),
					GeneratorVersion: generatorVersion,
				}
				if generateErr == nil {
					evidence.State = "ready"
					evidence.Fingerprint = fingerprint.Pack(generated.Hashes)
					evidence.DurationMS = int64(generated.Duration * 1000)
					evidence.HashCount = len(generated.Hashes)
					bundle.Fingerprints = append(bundle.Fingerprints, evidence)
					continue
				}
				var permanent *fingerprint.PermanentError
				if errors.As(generateErr, &permanent) {
					next := time.Now().UTC().Add(time.Hour)
					evidence.State, evidence.FailureClass, evidence.FailureMessage, evidence.RetryAfter = "failed", "permanent_preview_error", generateErr.Error(), &next
					bundle.Fingerprints = append(bundle.Fingerprints, evidence)
				} else {
					slog.Warn("generate preview fingerprint", "provider", source.ProviderRecord.Provider, "track_id", match.ProviderID, "error", generateErr)
				}
			}
		}
	}
}
