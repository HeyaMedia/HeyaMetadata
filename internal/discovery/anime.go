package discovery

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/anidb"
	"github.com/jackc/pgx/v5"
)

type animeTitleCandidate struct {
	AID    string
	Titles []animeTitle
}
type animeTitle struct {
	Value    string
	Language string
	Type     string
}
type animeDetail struct {
	ID           string `xml:"id,attr"`
	Type         string `xml:"type"`
	EpisodeCount int    `xml:"episodecount"`
	StartDate    string `xml:"startdate"`
	EndDate      string `xml:"enddate"`
	Titles       []struct {
		Language string `xml:"lang,attr"`
		Type     string `xml:"type,attr"`
		Value    string `xml:",chardata"`
	} `xml:"titles>title"`
	Episodes []struct {
		Number struct {
			Type  int    `xml:"type,attr"`
			Value string `xml:",chardata"`
		} `xml:"epno"`
		Titles []struct {
			Language string `xml:"lang,attr"`
			Value    string `xml:",chardata"`
		} `xml:"title"`
	} `xml:"episodes>episode"`
}

func (s *Service) DiscoverAnime(ctx context.Context, request Request, jobID int64, apiKey string) (Result, error) {
	request = NormalizeRequest(request)
	if request.Kind != KindAnime || request.Query == "" {
		return Result{}, fmt.Errorf("Anime discovery requires kind anime and a query")
	}
	candidates, err := s.discoverTMDBEpisodicCandidates(ctx, request, jobID, apiKey, true)
	if err != nil {
		return Result{}, err
	}
	if hasPlausibleCandidate(candidates) {
		return episodicDiscoveryResult(request, candidates, []string{"tmdb"}), nil
	}
	return s.discoverAniDB(ctx, request, jobID)
}

func (s *Service) discoverAniDB(ctx context.Context, request Request, jobID int64) (Result, error) {
	base := anidb.New(s.runtime.Config.Providers.AniDB)
	resolver, err := providercache.New(s.runtime, "anidb-anime-discovery/v1", base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return Result{}, err
	}
	client := anidb.NewCached(s.runtime.Config.Providers.AniDB, resolver)
	dump, err := client.Titles(ctx)
	if err != nil {
		return Result{}, err
	}
	if dump.StatusCode != http.StatusOK {
		return Result{}, &providers.StatusError{Provider: "anidb", StatusCode: dump.StatusCode}
	}
	titles, err := parseAnimeTitleDump(dump.Body)
	if err != nil {
		return Result{}, err
	}
	q := normalizedText(request.Query)
	type scored struct {
		candidate animeTitleCandidate
		score     float64
	}
	scoredValues := make([]scored, 0, 64)
	for _, candidate := range titles {
		best := 0.0
		for _, title := range candidate.Titles {
			sim := similarity(q, normalizedText(title.Value))
			if normalizedText(title.Value) == q {
				sim = 1
			}
			if sim > best {
				best = sim
			}
		}
		if best >= .35 {
			scoredValues = append(scoredValues, scored{candidate, best})
		}
	}
	sort.SliceStable(scoredValues, func(i, j int) bool {
		if scoredValues[i].score != scoredValues[j].score {
			return scoredValues[i].score > scoredValues[j].score
		}
		return scoredValues[i].candidate.AID < scoredValues[j].candidate.AID
	})
	if len(scoredValues) > 25 {
		scoredValues = scoredValues[:25]
	}
	candidates := make([]Candidate, 0, len(scoredValues))
	for _, value := range scoredValues {
		mainTitle, aliases := selectAnimeTitles(value.candidate.Titles)
		candidate := Candidate{ProviderScore: int(math.Round(value.score * 100)), Identity: ExternalID{Provider: "anidb", Namespace: "anime", Value: value.candidate.AID}, Display: Display{Name: mainTitle, Type: "anime", Aliases: aliases}, Resolution: Resolution{Kind: KindAnime, Provider: "anidb", Namespace: "anime", Value: value.candidate.AID}}
		scoreAnimeCandidate(request, &candidate)
		var entityID string
		e := s.runtime.DB.QueryRow(ctx, `SELECT entity_id FROM external_id_claims WHERE entity_kind='anime' AND provider='anidb' AND namespace='anime' AND normalized_value=$1 AND state='accepted'`, value.candidate.AID).Scan(&entityID)
		if e == nil {
			candidate.ExistingEntityID = entityID
		} else if e != pgx.ErrNoRows {
			return Result{}, e
		}
		candidates = append(candidates, candidate)
	}
	return episodicDiscoveryResult(request, candidates, []string{"tmdb", "anidb"}), nil
}

func parseAnimeTitleDump(body []byte) ([]animeTitleCandidate, error) {
	reader := io.Reader(bytes.NewReader(body))
	if len(body) > 2 && body[0] == 0x1f && body[1] == 0x8b {
		gz, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("open AniDB title dump: %w", err)
		}
		defer gz.Close()
		reader = gz
	}
	decoder := xml.NewDecoder(reader)
	out := []animeTitleCandidate{}
	var current *animeTitleCandidate
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse AniDB title dump: %w", err)
		}
		switch value := token.(type) {
		case xml.StartElement:
			if value.Name.Local == "anime" {
				current = &animeTitleCandidate{}
				for _, a := range value.Attr {
					if a.Name.Local == "aid" {
						current.AID = a.Value
					}
				}
			} else if value.Name.Local == "title" && current != nil {
				var text string
				if err := decoder.DecodeElement(&text, &value); err != nil {
					return nil, err
				}
				title := animeTitle{Value: strings.TrimSpace(text)}
				for _, a := range value.Attr {
					if a.Name.Local == "lang" {
						title.Language = a.Value
					}
					if a.Name.Local == "type" {
						title.Type = a.Value
					}
				}
				if title.Value != "" {
					current.Titles = append(current.Titles, title)
				}
			}
		case xml.EndElement:
			if value.Name.Local == "anime" && current != nil {
				if current.AID != "" && len(current.Titles) > 0 {
					out = append(out, *current)
				}
				current = nil
			}
		}
	}
	return out, nil
}

func selectAnimeTitles(titles []animeTitle) (string, []string) {
	main := ""
	aliases := []string{}
	for _, t := range titles {
		if main == "" && (t.Type == "main" || t.Language == "x-jat") {
			main = t.Value
		}
		aliases = append(aliases, t.Value)
	}
	if main == "" && len(titles) > 0 {
		main = titles[0].Value
	}
	filtered := []string{}
	for _, a := range cleanSorted(aliases) {
		if normalizedText(a) != normalizedText(main) {
			filtered = append(filtered, a)
		}
	}
	return main, filtered
}
func matchAnimeEpisodes(hints []EpisodeHint, detail animeDetail) []EpisodeHint {
	out := []EpisodeHint{}
	for _, h := range hints {
		for _, e := range detail.Episodes {
			number, _ := strconv.Atoi(strings.TrimSpace(e.Number.Value))
			numberOK := h.Number == 0 || h.Number == number
			titleOK := h.Title == ""
			for _, t := range e.Titles {
				if normalizedText(h.Title) == normalizedText(t.Value) {
					titleOK = true
					break
				}
			}
			if numberOK && titleOK {
				out = append(out, h)
				break
			}
		}
	}
	return out
}

func scoreAnimeCandidate(request Request, c *Candidate) {
	score := float64(c.ProviderScore) / 100 * .2
	c.Evidence = append(c.Evidence, Evidence{Field: "provider_score", Outcome: "support", Weight: round(score), Detail: fmt.Sprintf("%s search score %d/100", strings.ToUpper(c.Identity.Provider), c.ProviderScore)})
	w, o := episodicTitleMatch(request, c.Display.Name, c.Display.Aliases, .42, .4)
	score += w
	c.Evidence = append(c.Evidence, Evidence{Field: "title", Outcome: o, Weight: round(w), Detail: c.Display.Name})
	if request.Hints.Year > 0 {
		w, o = -.06, "mismatch"
		d := abs(request.Hints.Year - c.Display.Year)
		if d == 0 {
			w, o = .14, "exact"
		} else if d == 1 {
			w, o = .06, "near"
		}
		score += w
		c.Evidence = append(c.Evidence, Evidence{Field: "year", Outcome: o, Weight: w, Detail: strconv.Itoa(c.Display.Year)})
	}
	if request.Hints.Type != "" {
		w, o = -.04, "mismatch"
		if request.Hints.Type == c.Display.Type {
			w, o = .1, "exact"
		}
		score += w
		c.Evidence = append(c.Evidence, Evidence{Field: "format", Outcome: o, Weight: w, Detail: c.Display.Type})
	}
	if request.Hints.EpisodeCount > 0 {
		w, o = -.04, "mismatch"
		if request.Hints.EpisodeCount == c.Display.EpisodeCount {
			w, o = .1, "exact"
		}
		score += w
		c.Evidence = append(c.Evidence, Evidence{Field: "episode_count", Outcome: o, Weight: w, Detail: strconv.Itoa(c.Display.EpisodeCount)})
	}
	if len(request.Hints.Episodes) > 0 {
		w = .16 * float64(len(c.MatchedEpisodes)) / float64(len(request.Hints.Episodes))
		score += w
		c.Evidence = append(c.Evidence, Evidence{Field: "episodes", Outcome: fmt.Sprintf("%d_of_%d", len(c.MatchedEpisodes), len(request.Hints.Episodes)), Weight: round(w)})
	}
	c.Confidence = round(math.Max(0, math.Min(.99, score)))
	setMatch(c)
}
