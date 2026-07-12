package releases

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	releasedomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/release"
	"github.com/HeyaMedia/HeyaMetadata/internal/fingerprint"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/deezer"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/lrclib"
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

type lyricsEvidence struct {
	RecordingProviderID string
	Evidence            lrclib.Evidence
}

type evidenceBundle struct {
	Fingerprints []fingerprintEvidence
	Lyrics       []lyricsEvidence
}

func (s *Service) collectRecordingEvidence(ctx context.Context, records []releasedomain.NormalizedRecord, jobID int64) evidenceBundle {
	if len(records) == 0 {
		return evidenceBundle{}
	}
	spine := records[0]
	bundle := evidenceBundle{}
	s.collectFingerprints(ctx, spine, records[1:], &bundle)
	s.collectLyrics(ctx, spine, jobID, &bundle)
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

func (s *Service) collectLyrics(ctx context.Context, spine releasedomain.NormalizedRecord, jobID int64, bundle *evidenceBundle) {
	base := lrclib.New(s.runtime.Config.Providers.LRCLIB)
	resolver, err := providercache.New(s.runtime, lrclib.NormalizerVersion, base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		slog.Warn("initialize LRCLIB cache", "error", err)
		return
	}
	client := lrclib.NewCached(s.runtime.Config.Providers.LRCLIB, resolver)
	// LRCLIB is beta and can be slow even on /get-cached. Bound concurrency so
	// one release gets useful partial evidence without monopolizing a worker.
	semaphore := make(chan struct{}, 4)
	var wait sync.WaitGroup
	var mutex sync.Mutex
	for _, medium := range spine.Media {
		for _, track := range medium.Tracks {
			if ctx.Err() != nil {
				break
			}
			duration := int((track.DurationMS + 500) / 1000)
			artist := trackArtistName(track, spine)
			if duration < 1 || artist == "" || spine.Title == "" {
				continue
			}
			signature := lrclib.Signature{TrackName: track.Title, ArtistName: artist, AlbumName: spine.Title, Duration: duration}
			recordingProviderID := track.Recording.ProviderID
			wait.Add(1)
			go func() {
				defer wait.Done()
				select {
				case semaphore <- struct{}{}:
					defer func() { <-semaphore }()
				case <-ctx.Done():
					return
				}
				payload, err := client.GetCached(ctx, signature)
				if err != nil {
					slog.Warn("fetch LRCLIB lyrics", "recording", recordingProviderID, "error", err)
					return
				}
				if payload.StatusCode == http.StatusNotFound {
					return
				}
				if payload.StatusCode != http.StatusOK {
					slog.Warn("LRCLIB lyrics lookup returned non-success", "status", payload.StatusCode, "recording", recordingProviderID)
					return
				}
				evidence, err := lrclib.Normalize(payload.Body, payload.ObservationID, payload.ObservedAt, signature)
				if err != nil {
					slog.Warn("normalize LRCLIB lyrics", "recording", recordingProviderID, "error", err)
					return
				}
				mutex.Lock()
				bundle.Lyrics = append(bundle.Lyrics, lyricsEvidence{RecordingProviderID: recordingProviderID, Evidence: evidence})
				mutex.Unlock()
			}()
		}
	}
	wait.Wait()
}

func trackArtistName(track releasedomain.Track, release releasedomain.NormalizedRecord) string {
	credits := track.ArtistCredits
	if len(credits) == 0 {
		credits = track.Recording.ArtistCredits
	}
	if len(credits) == 0 {
		credits = release.ArtistCredits
	}
	result := ""
	for _, credit := range credits {
		result += credit.Name + credit.JoinPhrase
	}
	return result
}
