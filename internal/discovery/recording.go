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

type mbRecordingDiscovery struct {
	Recordings []struct {
		ID             string   `json:"id"`
		Title          string   `json:"title"`
		Length         int64    `json:"length"`
		Disambiguation string   `json:"disambiguation"`
		Score          int      `json:"score"`
		ISRCs          []string `json:"isrcs"`
		ArtistCredit   []struct {
			Name       string                    `json:"name"`
			JoinPhrase string                    `json:"joinphrase"`
			Artist     struct{ ID, Name string } `json:"artist"`
		} `json:"artist-credit"`
		Releases []struct {
			Title string `json:"title"`
			Date  string `json:"date"`
		} `json:"releases"`
	} `json:"recordings"`
}

func (s *Service) DiscoverRecording(ctx context.Context, request Request, jobID int64) (Result, error) {
	request = NormalizeRequest(request)
	if request.Kind != KindRecording {
		return Result{}, fmt.Errorf("recording discovery requires kind recording")
	}
	if request.Query == "" {
		return Result{}, fmt.Errorf("discovery query is required")
	}
	base := musicbrainz.New(s.runtime.Config.Providers.MusicBrainz)
	resolver, err := providercache.New(s.runtime, "musicbrainz-recording-discovery/v1", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return Result{}, err
	}
	client := musicbrainz.NewCached(s.runtime.Config.Providers.MusicBrainz, resolver)
	query := `recording:"` + escapeLucene(request.Query) + `"`
	if len(request.Hints.ArtistIDs) == 1 {
		query += ` AND arid:` + escapeLucene(request.Hints.ArtistIDs[0])
	}
	payload, err := client.Search(ctx, "recording", query, min(100, max(25, request.Limit*4)), 0)
	if err != nil {
		return Result{}, err
	}
	if payload.StatusCode != http.StatusOK {
		return Result{}, &providers.StatusError{Provider: "musicbrainz", StatusCode: payload.StatusCode}
	}
	var source mbRecordingDiscovery
	if err := json.Unmarshal(payload.Body, &source); err != nil {
		return Result{}, fmt.Errorf("decode MusicBrainz recording search: %w", err)
	}
	candidates := make([]Candidate, 0, len(source.Recordings))
	for _, value := range source.Recordings {
		id, title := strings.ToLower(strings.TrimSpace(value.ID)), strings.TrimSpace(value.Title)
		if id == "" || title == "" {
			continue
		}
		artists := make([]ArtistDisplay, 0, len(value.ArtistCredit))
		for _, credit := range value.ArtistCredit {
			name := credit.Name
			if name == "" {
				name = credit.Artist.Name
			}
			artists = append(artists, ArtistDisplay{ID: strings.ToLower(credit.Artist.ID), Name: name, Join: credit.JoinPhrase})
		}
		releases := make([]ReleaseHint, 0, len(value.Releases))
		for _, release := range value.Releases {
			releases = appendUniqueReleaseHint(releases, ReleaseHint{Title: release.Title, Year: releaseYear(release.Date)})
		}
		candidate := Candidate{ProviderScore: value.Score, Identity: ExternalID{Provider: "musicbrainz", Namespace: "recording", Value: id}, Display: Display{Title: title, Disambiguation: strings.TrimSpace(value.Disambiguation), Artists: artists, DurationMS: value.Length, ISRCs: cleanSortedUpper(value.ISRCs), Releases: releases}, Resolution: Resolution{Kind: KindRecording, Provider: "musicbrainz", Namespace: "recording", Value: id}}
		scoreRecordingCandidate(request, &candidate)
		var entityID string
		err := s.runtime.DB.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind='recording' AND provider='musicbrainz' AND namespace='recording' AND normalized_value=$1 AND state='accepted'`, id).Scan(&entityID)
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
	return Result{SchemaVersion: SchemaVersion, Kind: KindRecording, Query: request.Query, Status: "completed", Recommendation: recommendation(candidates), Candidates: candidates, Providers: []string{"musicbrainz"}, ObservedAt: time.Now().UTC()}, nil
}

func scoreRecordingCandidate(request Request, candidate *Candidate) {
	score := float64(candidate.ProviderScore) / 100 * .18
	candidate.Evidence = append(candidate.Evidence, Evidence{Field: "provider_score", Outcome: "support", Weight: round(score), Detail: fmt.Sprintf("MusicBrainz score %d/100", candidate.ProviderScore)})
	query, title := normalizedText(request.Query), normalizedText(candidate.Display.Title)
	weight, outcome := similarity(query, title)*.34, "fuzzy"
	if query == title {
		weight, outcome = .34, "exact"
	}
	score += weight
	candidate.Evidence = append(candidate.Evidence, Evidence{Field: "title", Outcome: outcome, Weight: round(weight), Detail: candidate.Display.Title})
	if len(request.Hints.Artists) > 0 {
		matched := matchedArtists(request.Hints.Artists, nil, candidate.Display.Artists)
		weight = .16 * float64(matched) / float64(len(request.Hints.Artists))
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "artists", Outcome: fmt.Sprintf("%d_of_%d", matched, len(request.Hints.Artists)), Weight: round(weight)})
	}
	if len(request.Hints.ArtistIDs) > 0 {
		matched := matchedArtists(nil, request.Hints.ArtistIDs, candidate.Display.Artists)
		weight = .2 * float64(matched) / float64(len(request.Hints.ArtistIDs))
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "artist_ids", Outcome: fmt.Sprintf("%d_of_%d", matched, len(request.Hints.ArtistIDs)), Weight: round(weight)})
	}
	if request.Hints.DurationMS > 0 {
		delta := abs64(request.Hints.DurationMS - candidate.Display.DurationMS)
		weight, outcome = -.06, "mismatch"
		if delta <= 2000 {
			weight, outcome = .14, "near_exact"
		} else if delta <= 5000 {
			weight, outcome = .06, "near"
		}
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "duration_ms", Outcome: outcome, Weight: weight, Detail: strconv.FormatInt(candidate.Display.DurationMS, 10)})
	}
	if len(request.Hints.ISRCs) > 0 {
		matched := 0
		for _, hint := range request.Hints.ISRCs {
			for _, value := range candidate.Display.ISRCs {
				if hint == value {
					matched++
					break
				}
			}
		}
		weight = .35 * float64(matched) / float64(len(request.Hints.ISRCs))
		if matched == 0 {
			weight = -.08
		}
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "isrcs", Outcome: fmt.Sprintf("%d_of_%d", matched, len(request.Hints.ISRCs)), Weight: round(weight)})
	}
	if len(request.Hints.Releases) > 0 {
		matched := 0
		for _, hint := range request.Hints.Releases {
			for _, value := range candidate.Display.Releases {
				if normalizedText(hint.Title) == normalizedText(value.Title) && (hint.Year == 0 || value.Year == 0 || abs(hint.Year-value.Year) <= 1) {
					matched++
					break
				}
			}
		}
		weight = .14 * float64(matched) / float64(len(request.Hints.Releases))
		score += weight
		candidate.Evidence = append(candidate.Evidence, Evidence{Field: "releases", Outcome: fmt.Sprintf("%d_of_%d", matched, len(request.Hints.Releases)), Weight: round(weight)})
	}
	candidate.Confidence = round(math.Max(0, math.Min(.99, score)))
	setMatch(candidate)
}

func matchedArtists(names, ids []string, candidates []ArtistDisplay) int {
	matched := 0
	for _, candidate := range candidates {
		for _, name := range names {
			if normalizedText(name) == normalizedText(candidate.Name) {
				matched++
				break
			}
		}
		for _, id := range ids {
			if strings.EqualFold(id, candidate.ID) {
				matched++
				break
			}
		}
	}
	return min(matched, max(len(names), len(ids)))
}

func abs64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
