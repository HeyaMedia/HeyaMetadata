package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/musicbrainz"
	"github.com/jackc/pgx/v5"
)

type mbReleaseGroupSearch struct {
	ReleaseGroups []struct {
		ID               string   `json:"id"`
		Title            string   `json:"title"`
		FirstReleaseDate string   `json:"first-release-date"`
		PrimaryType      string   `json:"primary-type"`
		SecondaryTypes   []string `json:"secondary-types"`
		Score            int      `json:"score"`
		Aliases          []struct {
			Name string `json:"name"`
		} `json:"aliases"`
		ArtistCredit []struct {
			Name       string `json:"name"`
			JoinPhrase string `json:"joinphrase"`
			Artist     struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"artist"`
		} `json:"artist-credit"`
	} `json:"release-groups"`
}

type mbRecordingSearch struct {
	Recordings []struct {
		Title    string `json:"title"`
		Releases []struct {
			ReleaseGroup *struct {
				ID string `json:"id"`
			} `json:"release-group"`
		} `json:"releases"`
	} `json:"recordings"`
}

func (s *Service) DiscoverReleaseGroup(ctx context.Context, request Request, jobID int64) (Result, error) {
	request = NormalizeRequest(request)
	if request.Kind != KindReleaseGroup {
		return Result{}, fmt.Errorf("release-group discovery requires kind release_group")
	}
	if request.Query == "" {
		return Result{}, fmt.Errorf("discovery query is required")
	}
	base := musicbrainz.New(s.runtime.Config.Providers.MusicBrainz)
	resolver, err := providercache.New(s.runtime, "musicbrainz-release-group-discovery/v1", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return Result{}, err
	}
	client := musicbrainz.NewCached(s.runtime.Config.Providers.MusicBrainz, resolver)
	query := `(releasegroup:"` + escapeLucene(request.Query) + `" OR alias:"` + escapeLucene(request.Query) + `")`
	if len(request.Hints.ArtistIDs) == 1 {
		query += ` AND arid:` + escapeLucene(request.Hints.ArtistIDs[0])
	}
	payload, err := client.Search(ctx, "release_group", query, min(100, max(25, request.Limit*4)), 0)
	if err != nil {
		return Result{}, err
	}
	if payload.StatusCode != http.StatusOK {
		return Result{}, &providers.StatusError{Provider: "musicbrainz", StatusCode: payload.StatusCode}
	}
	var source mbReleaseGroupSearch
	if err := json.Unmarshal(payload.Body, &source); err != nil {
		return Result{}, fmt.Errorf("decode MusicBrainz release-group search: %w", err)
	}
	trackMatches := map[string][]string{}
	for _, track := range request.Hints.Tracks {
		trackPayload, searchErr := client.Search(ctx, "recording", `recording:"`+escapeLucene(track)+`"`, 50, 0)
		if searchErr != nil {
			return Result{}, searchErr
		}
		if trackPayload.StatusCode != http.StatusOK {
			return Result{}, &providers.StatusError{Provider: "musicbrainz", StatusCode: trackPayload.StatusCode}
		}
		var recordings mbRecordingSearch
		if err := json.Unmarshal(trackPayload.Body, &recordings); err != nil {
			return Result{}, fmt.Errorf("decode MusicBrainz recording search: %w", err)
		}
		for _, recording := range recordings.Recordings {
			if normalizedText(recording.Title) != normalizedText(track) {
				continue
			}
			for _, release := range recording.Releases {
				if release.ReleaseGroup != nil && release.ReleaseGroup.ID != "" {
					id := strings.ToLower(release.ReleaseGroup.ID)
					trackMatches[id] = appendUniqueString(trackMatches[id], track)
				}
			}
		}
	}
	candidates := make([]Candidate, 0, len(source.ReleaseGroups))
	for _, value := range source.ReleaseGroups {
		id := strings.ToLower(value.ID)
		if id == "" || strings.TrimSpace(value.Title) == "" {
			continue
		}
		aliases := make([]string, 0, len(value.Aliases))
		for _, alias := range value.Aliases {
			aliases = append(aliases, alias.Name)
		}
		artists := make([]ArtistDisplay, 0, len(value.ArtistCredit))
		for _, credit := range value.ArtistCredit {
			name := credit.Name
			if name == "" {
				name = credit.Artist.Name
			}
			artists = append(artists, ArtistDisplay{ID: strings.ToLower(credit.Artist.ID), Name: name, Join: credit.JoinPhrase})
		}
		candidate := Candidate{
			ProviderScore: value.Score,
			Identity:      ExternalID{Provider: "musicbrainz", Namespace: "release_group", Value: id},
			Display:       Display{Title: value.Title, Type: normalizeType(value.PrimaryType), Year: releaseYear(value.FirstReleaseDate), Date: value.FirstReleaseDate, Aliases: cleanSorted(aliases), Artists: artists, SecondaryTypes: cleanSortedTypes(value.SecondaryTypes)},
			MatchedTracks: trackMatches[id],
			Resolution:    Resolution{Kind: KindReleaseGroup, Provider: "musicbrainz", Namespace: "release_group", Value: id},
		}
		scoreReleaseGroupCandidate(request, &candidate)
		var entityID string
		err := s.runtime.DB.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind='release_group' AND provider='musicbrainz' AND namespace='release_group' AND normalized_value=$1 AND state='accepted'`, id).Scan(&entityID)
		if err == nil {
			candidate.ExistingEntityID = entityID
		} else if err != pgx.ErrNoRows {
			return Result{}, err
		}
		candidates = append(candidates, candidate)
	}
	sortCandidates(candidates)
	if len(candidates) > request.Limit {
		candidates = candidates[:request.Limit]
	}
	for i := range candidates {
		candidates[i].Rank = i + 1
	}
	return Result{SchemaVersion: SchemaVersion, Kind: KindReleaseGroup, Query: request.Query, Status: "completed", Recommendation: recommendation(candidates), Candidates: candidates, Providers: []string{"musicbrainz"}, ObservedAt: time.Now().UTC()}, nil
}

func scoreReleaseGroupCandidate(request Request, candidate *Candidate) {
	score := float64(candidate.ProviderScore) / 100 * .2
	candidate.Evidence = append(candidate.Evidence, Evidence{Field: "provider_score", Outcome: "support", Weight: round(float64(candidate.ProviderScore) / 100 * .2), Detail: fmt.Sprintf("MusicBrainz score %d/100", candidate.ProviderScore)})
	query := normalizedText(request.Query)
	title := normalizedText(candidate.Display.Title)
	weight, outcome := similarity(query, title)*.4, "fuzzy"
	if query == title {
		weight, outcome = .4, "exact"
	} else {
		for _, alias := range candidate.Display.Aliases {
			if query == normalizedText(alias) {
				weight, outcome = .37, "exact_alias"
				break
			}
		}
	}
	score += weight
	candidate.Evidence = append(candidate.Evidence, Evidence{Field: "title", Outcome: outcome, Weight: round(weight), Detail: candidate.Display.Title})
	if request.Hints.Year > 0 {
		weight, outcome = -.06, "mismatch"
		delta := abs(request.Hints.Year - candidate.Display.Year)
		if delta == 0 {
			weight, outcome = .14, "exact"
		} else if delta == 1 {
			weight, outcome = .06, "near"
		}
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "year", Outcome: outcome, Weight: weight, Detail: strconv.Itoa(candidate.Display.Year)})
	}
	if request.Hints.Date != "" {
		weight, outcome = -.04, "mismatch"
		if request.Hints.Date == candidate.Display.Date {
			weight, outcome = .14, "exact"
		} else if releaseYear(request.Hints.Date) == candidate.Display.Year {
			weight, outcome = .06, "year_match"
		}
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "date", Outcome: outcome, Weight: weight, Detail: candidate.Display.Date})
	}
	if request.Hints.Type != "" {
		weight, outcome = -.04, "mismatch"
		if request.Hints.Type == candidate.Display.Type || contains(candidate.Display.SecondaryTypes, request.Hints.Type) {
			weight, outcome = .1, "exact"
		}
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "type", Outcome: outcome, Weight: weight, Detail: candidate.Display.Type})
	}
	if len(request.Hints.Artists) > 0 {
		matched := 0
		for _, hint := range request.Hints.Artists {
			for _, artist := range candidate.Display.Artists {
				if normalizedText(hint) == normalizedText(artist.Name) {
					matched++
					break
				}
			}
		}
		weight = .18 * float64(matched) / float64(len(request.Hints.Artists))
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "artists", Outcome: fmt.Sprintf("%d_of_%d", matched, len(request.Hints.Artists)), Weight: round(weight)})
	}
	if len(request.Hints.ArtistIDs) > 0 {
		matched := 0
		for _, hint := range request.Hints.ArtistIDs {
			for _, artist := range candidate.Display.Artists {
				if hint == artist.ID {
					matched++
					break
				}
			}
		}
		weight = .22 * float64(matched) / float64(len(request.Hints.ArtistIDs))
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "artist_ids", Outcome: fmt.Sprintf("%d_of_%d", matched, len(request.Hints.ArtistIDs)), Weight: round(weight)})
	}
	if len(request.Hints.Tracks) > 0 {
		weight = .18 * float64(len(candidate.MatchedTracks)) / float64(len(request.Hints.Tracks))
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "tracks", Outcome: fmt.Sprintf("%d_of_%d", len(candidate.MatchedTracks), len(request.Hints.Tracks)), Weight: round(weight)})
	}
	candidate.Confidence = round(math.Max(0, math.Min(.99, score)))
	setMatch(candidate)
}

func cleanSortedTypes(values []string) []string {
	for i := range values {
		values[i] = normalizeType(values[i])
	}
	return cleanSorted(values)
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if normalizedText(existing) == normalizedText(value) {
			return values
		}
	}
	return append(values, value)
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
