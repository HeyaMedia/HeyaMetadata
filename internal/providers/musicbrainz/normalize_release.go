package musicbrainz

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	releasedomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/release"
)

type releaseResponse struct {
	ID, Title, Disambiguation, Status, Quality, Packaging, Date, Country, Barcode string
	ArtistCredit                                                                  []mbCredit `json:"artist-credit"`
	ReleaseGroup                                                                  struct {
		ID string `json:"id"`
	} `json:"release-group"`
	LabelInfo []struct {
		CatalogNumber string                    `json:"catalog-number"`
		Label         struct{ ID, Name string } `json:"label"`
	} `json:"label-info"`
	Media []struct {
		Position      int
		TrackCount    int `json:"track-count"`
		Title, Format string
		Discs         []struct {
			ID string `json:"id"`
		} `json:"discs"`
		Tracks []struct {
			ID           string
			Position     flexibleString
			Number       flexibleString
			Title        string
			Length       int64
			ArtistCredit []mbCredit `json:"artist-credit"`
			Recording    struct {
				ID, Title    string
				Length       int64
				ISRCs        []string
				ArtistCredit []mbCredit `json:"artist-credit"`
			} `json:"recording"`
		} `json:"tracks"`
	} `json:"media"`
}
type mbCredit struct {
	Name, JoinPhrase string
	Artist           struct{ ID, Name string } `json:"artist"`
}
type flexibleString string

func (v *flexibleString) UnmarshalJSON(body []byte) error {
	var text string
	if len(body) > 0 && body[0] == '"' {
		if err := json.Unmarshal(body, &text); err != nil {
			return err
		}
		*v = flexibleString(text)
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(body, &number); err != nil {
		return err
	}
	*v = flexibleString(number.String())
	return nil
}

func NormalizeRelease(body []byte, observationID string, observedAt time.Time) (releasedomain.NormalizedRecord, error) {
	var source releaseResponse
	if err := json.Unmarshal(body, &source); err != nil {
		return releasedomain.NormalizedRecord{}, fmt.Errorf("decode MusicBrainz release: %w", err)
	}
	id := strings.ToLower(strings.TrimSpace(source.ID))
	if !mbidPattern.MatchString(id) || strings.TrimSpace(source.Title) == "" {
		return releasedomain.NormalizedRecord{}, fmt.Errorf("MusicBrainz release is missing MBID or title")
	}
	r := releasedomain.NormalizedRecord{ProviderRecord: releasedomain.ProviderRecord{Provider: "musicbrainz", Namespace: "release", Value: id, PrimaryObservationID: observationID, ObservedAt: observedAt, NormalizerVersion: releasedomain.NormalizerVersion, SchemaVersion: releasedomain.NormalizedSchemaVersion}, Title: strings.TrimSpace(source.Title), Disambiguation: strings.TrimSpace(source.Disambiguation), Status: normalizeToken(source.Status), Quality: normalizeToken(source.Quality), Packaging: normalizeToken(source.Packaging), Date: source.Date, Country: strings.ToUpper(source.Country), Barcode: strings.TrimSpace(source.Barcode), ExternalIDs: []releasedomain.ExternalID{{Provider: "musicbrainz", Namespace: "release", Value: id, Evidence: "provider_record"}}}
	if rg := strings.ToLower(source.ReleaseGroup.ID); mbidPattern.MatchString(rg) {
		r.ExternalIDs = append(r.ExternalIDs, releasedomain.ExternalID{Provider: "musicbrainz", Namespace: "release_group", Value: rg, Evidence: "release_group_relationship"})
	}
	r.ArtistCredits = releaseCredits(source.ArtistCredit)
	for _, item := range source.LabelInfo {
		if item.Label.ID != "" || item.Label.Name != "" {
			r.Labels = append(r.Labels, releasedomain.Label{ProviderID: strings.ToLower(item.Label.ID), Name: item.Label.Name, CatalogNumber: item.CatalogNumber})
		}
	}
	for _, medium := range source.Media {
		m := releasedomain.Medium{Position: medium.Position, Title: medium.Title, Format: medium.Format, TrackCount: medium.TrackCount}
		for _, disc := range medium.Discs {
			if disc.ID != "" {
				m.DiscIDs = append(m.DiscIDs, disc.ID)
			}
		}
		for i, item := range medium.Tracks {
			t := releasedomain.Track{ProviderID: strings.ToLower(item.ID), Position: string(item.Position), Number: string(item.Number), Sequence: i + 1, Title: item.Title, DurationMS: item.Length, ArtistCredits: releaseCredits(item.ArtistCredit)}
			recID := strings.ToLower(item.Recording.ID)
			if mbidPattern.MatchString(recID) {
				t.Recording = releasedomain.Recording{Provider: "musicbrainz", Namespace: "recording", ProviderID: recID, Title: item.Recording.Title, DurationMS: item.Recording.Length, ISRCs: uniqueUpper(item.Recording.ISRCs), ArtistCredits: releaseCredits(item.Recording.ArtistCredit)}
			}
			if t.DurationMS == 0 {
				t.DurationMS = t.Recording.DurationMS
			}
			if len(t.ArtistCredits) == 0 {
				t.ArtistCredits = t.Recording.ArtistCredits
			}
			m.Tracks = append(m.Tracks, t)
		}
		if m.TrackCount == 0 {
			m.TrackCount = len(m.Tracks)
		}
		r.Media = append(r.Media, m)
	}
	sort.SliceStable(r.Media, func(i, j int) bool { return r.Media[i].Position < r.Media[j].Position })
	return r, nil
}
func releaseCredits(values []mbCredit) []releasedomain.ArtistCredit {
	out := make([]releasedomain.ArtistCredit, 0, len(values))
	for i, v := range values {
		id := strings.ToLower(v.Artist.ID)
		if !mbidPattern.MatchString(id) {
			continue
		}
		out = append(out, releasedomain.ArtistCredit{Position: i, Name: v.Name, JoinPhrase: v.JoinPhrase, ArtistProvider: "musicbrainz", ArtistNamespace: "artist", ArtistID: id, ArtistName: v.Artist.Name})
	}
	return out
}
func uniqueUpper(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range values {
		v = strings.ToUpper(strings.TrimSpace(v))
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}
