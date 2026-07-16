package musicbrainz

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	releasedomain "github.com/HeyaMedia/HeyaMetadata/internal/domains/release"
)

// NormalizeRecording maps a standalone MusicBrainz recording lookup,
// including artist-targeted performance relations (producer, engineer,
// vocals, instruments) as role-bearing credits.
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
		Relations []mbRelation `json:"relations"`
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
	recording.Credits = performanceCredits(source.Relations)
	works := workRelations(source.Relations)
	sort.Slice(recording.Genres, func(i, j int) bool { return recording.Genres[i].Name < recording.Genres[j].Name })
	sort.Slice(recording.Tags, func(i, j int) bool { return recording.Tags[i].Name < recording.Tags[j].Name })
	sort.Slice(recording.Releases, func(i, j int) bool { return recording.Releases[i].ProviderID < recording.Releases[j].ProviderID })
	external := []releasedomain.ExternalID{{Provider: "musicbrainz", Namespace: "recording", Value: id, Evidence: "provider_record"}}
	for _, isrc := range recording.ISRCs {
		external = append(external, releasedomain.ExternalID{Provider: "isrc", Namespace: "recording", Value: isrc, Evidence: "provider_assertion"})
	}
	return releasedomain.NormalizedRecording{ProviderRecord: releasedomain.ProviderRecord{Provider: "musicbrainz", Namespace: "recording", Value: id, PrimaryObservationID: observationID, NormalizerVersion: releasedomain.RecordingNormalizerVersion, ObservedAt: observedAt, SchemaVersion: releasedomain.NormalizedSchemaVersion}, ExternalIDs: external, Recording: recording, WorkRelations: works}, nil
}

type mbRelation struct {
	Type       string   `json:"type"`
	TargetType string   `json:"target-type"`
	Attributes []string `json:"attributes"`
	URL        *struct {
		Resource string `json:"resource"`
	} `json:"url"`
	Work *struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Language string `json:"language"`
	} `json:"work"`
	Artist *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"artist"`
}

// performanceCredits maps artist-targeted recording relations (producer,
// engineer, vocal, instrument, ...) into role-bearing credits.
func performanceCredits(values []mbRelation) []releasedomain.PerformanceCredit {
	seen := map[string]bool{}
	var out []releasedomain.PerformanceCredit
	for _, value := range values {
		if value.Artist == nil {
			continue
		}
		role := normalizeToken(value.Type)
		id := strings.ToLower(strings.TrimSpace(value.Artist.ID))
		name := strings.TrimSpace(value.Artist.Name)
		if role == "" || !mbidPattern.MatchString(id) || name == "" {
			continue
		}
		attributes := append([]string(nil), value.Attributes...)
		for index := range attributes {
			attributes[index] = normalizeToken(attributes[index])
		}
		sort.Strings(attributes)
		key := role + "\x00" + id + "\x00" + strings.Join(attributes, ",")
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, releasedomain.PerformanceCredit{Role: role, Attributes: attributes, ArtistProvider: "musicbrainz", ArtistNamespace: "artist", ArtistID: id, ArtistName: name})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Role != out[j].Role {
			return out[i].Role < out[j].Role
		}
		return out[i].ArtistName < out[j].ArtistName
	})
	return out
}

func workRelations(values []mbRelation) []releasedomain.WorkRelation {
	seen := map[string]bool{}
	out := make([]releasedomain.WorkRelation, 0)
	for _, value := range values {
		if value.Work == nil || normalizeToken(value.Type) != "performance" {
			continue
		}
		id := strings.ToLower(strings.TrimSpace(value.Work.ID))
		if !mbidPattern.MatchString(id) || seen[id] {
			continue
		}
		seen[id] = true
		attributes := append([]string(nil), value.Attributes...)
		for index := range attributes {
			attributes[index] = normalizeToken(attributes[index])
		}
		sort.Strings(attributes)
		out = append(out, releasedomain.WorkRelation{
			ProviderID: id,
			Title:      strings.TrimSpace(value.Work.Title),
			Language:   strings.ToLower(strings.TrimSpace(value.Work.Language)),
			Type:       "performance",
			Attributes: attributes,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ProviderID < out[j].ProviderID })
	return out
}
