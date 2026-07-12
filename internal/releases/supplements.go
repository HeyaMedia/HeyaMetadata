package releases

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	releasedomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/release"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/apple"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/deezer"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/discogs"
)

func (s *Service) collectSupplements(ctx context.Context, spine releasedomain.NormalizedRecord, jobID int64, credentials providercredentials.Credentials) []releasedomain.NormalizedRecord {
	if strings.TrimSpace(spine.Barcode) == "" {
		return nil
	}
	out := []releasedomain.NormalizedRecord{}
	accept := func(record releasedomain.NormalizedRecord) {
		if releasedomain.Compatible(spine, record) {
			slog.Info("release supplement verified", "provider", record.ProviderRecord.Provider, "release", spine.ProviderRecord.Value)
			out = append(out, record)
			return
		}
		slog.Info("release supplement rejected by barcode/layout verification", "provider", record.ProviderRecord.Provider, "release", spine.ProviderRecord.Value)
	}

	if credentials.APIKey("apple") != "" || s.runtime.Config.Providers.Apple.DeveloperToken != "" {
		base := apple.New(s.runtime.Config.Providers.Apple)
		if resolver, err := providercache.New(s.runtime, "apple-release/v1", base.Capability().RawRetention, base.Capability().ResponseCache, jobID); err == nil {
			client := apple.NewCached(s.runtime.Config.Providers.Apple, resolver, credentials.APIKey("apple"))
			if payload, err := client.LookupAlbumByUPC(ctx, spine.Barcode); err == nil && payload.StatusCode == http.StatusOK {
				if id := appleAlbumID(payload.Body); id != "" {
					if normalized, err := apple.NormalizeAlbum(payload.Body, id, payload.ObservationID, payload.ObservedAt); err == nil {
						accept(releasedomain.FromReleaseGroup(normalized))
					}
				}
			}
		}
	}

	baseDeezer := deezer.New(s.runtime.Config.Providers.Deezer)
	if resolver, err := providercache.New(s.runtime, "deezer-release/v1", baseDeezer.Capability().RawRetention, baseDeezer.Capability().ResponseCache, jobID); err == nil {
		if payload, err := deezer.NewCached(s.runtime.Config.Providers.Deezer, resolver).LookupAlbumByUPC(ctx, spine.Barcode); err == nil && payload.StatusCode == http.StatusOK {
			if normalized, err := deezer.NormalizeAlbum(payload.Body, payload.ObservationID, payload.ObservedAt); err == nil {
				accept(releasedomain.FromReleaseGroup(normalized))
			} else {
				slog.Warn("normalize Deezer release supplement", "error", err)
			}
		} else if err != nil {
			slog.Warn("collect Deezer release supplement", "error", err)
		}
	}

	if credentials.APIKey("discogs") != "" || s.runtime.Config.Providers.Discogs.APIKey != "" {
		base := discogs.New(s.runtime.Config.Providers.Discogs)
		if resolver, err := providercache.New(s.runtime, "discogs-release/v1", base.Capability().RawRetention, base.Capability().ResponseCache, jobID); err == nil {
			client := discogs.NewCached(s.runtime.Config.Providers.Discogs, resolver, credentials.APIKey("discogs"))
			if search, err := client.SearchReleaseByBarcode(ctx, spine.Barcode, 5); err == nil && search.StatusCode == http.StatusOK {
				for _, id := range discogsReleaseIDs(search.Body) {
					payloads, err := client.Collect(ctx, providers.Identifier{Provider: "discogs", Namespace: "release", Value: id})
					if err != nil || len(payloads) == 0 || payloads[0].StatusCode != http.StatusOK {
						continue
					}
					normalized, err := discogs.NormalizeRelease(payloads[0].Body, payloads[0].ObservationID, payloads[0].ObservedAt)
					if err != nil {
						continue
					}
					candidate := releasedomain.FromReleaseGroup(normalized)
					if releasedomain.Compatible(spine, candidate) {
						out = append(out, candidate)
						break
					}
				}
			}
		}
	}
	return out
}

func appleAlbumID(body []byte) string {
	var e struct {
		Data []struct{ ID, Type string } `json:"data"`
	}
	if json.Unmarshal(body, &e) != nil {
		return ""
	}
	for _, v := range e.Data {
		if v.Type == "albums" {
			return v.ID
		}
	}
	return ""
}
func discogsReleaseIDs(body []byte) []string {
	var e struct {
		Results []struct {
			ID int64 `json:"id"`
		} `json:"results"`
	}
	if json.Unmarshal(body, &e) != nil {
		return nil
	}
	out := []string{}
	for _, v := range e.Results {
		if v.ID > 0 {
			out = append(out, strconv.FormatInt(v.ID, 10))
		}
	}
	return out
}
