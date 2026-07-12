package musicbrainz

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	releasedomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/release"
)

func NormalizeRecording(body []byte, observationID string, observedAt time.Time) (releasedomain.NormalizedRecording, error) {
	var source struct {
		ID             string     `json:"id"`
		Title          string     `json:"title"`
		Length         int64      `json:"length"`
		Disambiguation string     `json:"disambiguation"`
		Video          bool       `json:"video"`
		ISRCs          []string   `json:"isrcs"`
		ArtistCredit   []mbCredit `json:"artist-credit"`
		Genres         []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Count int    `json:"count"`
		} `json:"genres"`
		Tags []struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		} `json:"tags"`
		Rating *struct {
			Value float64 `json:"value"`
			Votes int     `json:"votes-count"`
		} `json:"rating"`
		Releases []struct {
			ID, Title, Status, Date, Country string
			ReleaseGroup                     *struct {
				ID    string `json:"id"`
				Title string `json:"title"`
			} `json:"release-group"`
		} `json:"releases"`
		Relations []struct {
			Type string `json:"type"`
			URL  *struct {
				Resource string `json:"resource"`
			} `json:"url"`
		} `json:"relations"`
	}
	if err := json.Unmarshal(body, &source); err != nil {
		return releasedomain.NormalizedRecording{}, fmt.Errorf("decode MusicBrainz recording: %w", err)
	}
	id, title := strings.ToLower(strings.TrimSpace(source.ID)), strings.TrimSpace(source.Title)
	if !mbidPattern.MatchString(id) || title == "" {
		return releasedomain.NormalizedRecording{}, fmt.Errorf("MusicBrainz recording is missing MBID or title")
	}
	recording := releasedomain.Recording{Provider: "musicbrainz", Namespace: "recording", ProviderID: id, Title: title, DurationMS: source.Length, Disambiguation: strings.TrimSpace(source.Disambiguation), Video: source.Video, ISRCs: uniqueUpper(source.ISRCs), ArtistCredits: releaseCredits(source.ArtistCredit)}
	for _, value := range source.Genres {
		if name := strings.TrimSpace(value.Name); name != "" {
			recording.Genres = append(recording.Genres, releasedomain.WeightedTerm{ProviderID: strings.ToLower(value.ID), Name: name, Count: value.Count})
		}
	}
	for _, value := range source.Tags {
		if name := strings.TrimSpace(value.Name); name != "" {
			recording.Tags = append(recording.Tags, releasedomain.WeightedTerm{Name: name, Count: value.Count})
		}
	}
	if source.Rating != nil && source.Rating.Votes > 0 {
		recording.Rating = &releasedomain.Rating{Value: source.Rating.Value, Votes: source.Rating.Votes}
	}
	seenReleases := map[string]bool{}
	for _, value := range source.Releases {
		releaseID := strings.ToLower(strings.TrimSpace(value.ID))
		if !mbidPattern.MatchString(releaseID) || seenReleases[releaseID] {
			continue
		}
		seenReleases[releaseID] = true
		release := releasedomain.RecordingRelease{ProviderID: releaseID, Title: strings.TrimSpace(value.Title), Status: normalizeToken(value.Status), Date: value.Date, Country: strings.ToUpper(value.Country)}
		if value.ReleaseGroup != nil {
			release.ReleaseGroupID = strings.ToLower(value.ReleaseGroup.ID)
			release.ReleaseGroupTitle = strings.TrimSpace(value.ReleaseGroup.Title)
		}
		recording.Releases = append(recording.Releases, release)
	}
	for _, value := range source.Relations {
		if value.URL != nil && strings.TrimSpace(value.URL.Resource) != "" {
			recording.Links = append(recording.Links, releasedomain.Link{Type: normalizeToken(value.Type), URL: strings.TrimSpace(value.URL.Resource)})
		}
	}
	sort.Slice(recording.Genres, func(i, j int) bool { return recording.Genres[i].Name < recording.Genres[j].Name })
	sort.Slice(recording.Tags, func(i, j int) bool { return recording.Tags[i].Name < recording.Tags[j].Name })
	sort.Slice(recording.Releases, func(i, j int) bool { return recording.Releases[i].ProviderID < recording.Releases[j].ProviderID })
	external := []releasedomain.ExternalID{{Provider: "musicbrainz", Namespace: "recording", Value: id, Evidence: "provider_record"}}
	for _, isrc := range recording.ISRCs {
		external = append(external, releasedomain.ExternalID{Provider: "isrc", Namespace: "recording", Value: isrc, Evidence: "provider_assertion"})
	}
	return releasedomain.NormalizedRecording{ProviderRecord: releasedomain.ProviderRecord{Provider: "musicbrainz", Namespace: "recording", Value: id, PrimaryObservationID: observationID, NormalizerVersion: releasedomain.RecordingNormalizerVersion, ObservedAt: observedAt, SchemaVersion: releasedomain.NormalizedSchemaVersion}, ExternalIDs: external, Recording: recording}, nil
}
