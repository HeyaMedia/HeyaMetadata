package anime

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/HeyaMedia/HeyaMetadata/internal/episodic"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/anidb"
)

var definition = episodic.Definition{Kind: "anime", Provider: "anidb", Namespace: "anime", NormalizerVersion: "anidb-anime/v1", MergeVersion: "anime-combiner/v1"}

type Service struct{ runtime *platform.Runtime }

func NewService(runtime *platform.Runtime) *Service { return &Service{runtime: runtime} }
func (s *Service) IngestAniDB(ctx context.Context, id string, jobID int64) (result episodic.Result, returnErr error) {
	if n, err := strconv.ParseInt(id, 10, 64); err != nil || n < 1 {
		return result, fmt.Errorf("invalid AniDB AID")
	}
	if err := episodic.StartRun(ctx, s.runtime, jobID, definition, id); err != nil {
		return result, err
	}
	defer func() {
		if returnErr != nil {
			episodic.FailRun(ctx, s.runtime, jobID, returnErr)
		}
	}()
	base := anidb.New(s.runtime.Config.Providers.AniDB)
	resolver, err := providercache.New(s.runtime, definition.NormalizerVersion, base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return result, err
	}
	payloads, err := anidb.NewCached(s.runtime.Config.Providers.AniDB, resolver).Collect(ctx, providers.Identifier{Provider: "anidb", Namespace: "anime", Value: id})
	if err != nil {
		return result, err
	}
	if len(payloads) == 0 {
		return result, fmt.Errorf("AniDB returned no anime detail")
	}
	payload := payloads[0]
	if payload.StatusCode != http.StatusOK {
		return result, &providers.StatusError{Provider: "anidb", StatusCode: payload.StatusCode}
	}
	record, err := normalize(payload)
	if err != nil {
		return result, err
	}
	return episodic.Persist(ctx, s.runtime, definition, record, jobID)
}
func (s *Service) Resolve(ctx context.Context, provider, namespace, value string) (string, error) {
	return episodic.Resolve(ctx, s.runtime, "anime", provider, namespace, value)
}
func (s *Service) Detail(ctx context.Context, id string) (episodic.Document, bool, error) {
	return episodic.Detail(ctx, s.runtime, "anime", id)
}

type detail struct {
	ID           string `xml:"id,attr"`
	Type         string `xml:"type"`
	EpisodeCount int    `xml:"episodecount"`
	StartDate    string `xml:"startdate"`
	EndDate      string `xml:"enddate"`
	Description  string `xml:"description"`
	Picture      string `xml:"picture"`
	Titles       []struct {
		Language string `xml:"lang,attr"`
		Type     string `xml:"type,attr"`
		Value    string `xml:",chardata"`
	} `xml:"titles>title"`
	Creators []struct {
		Type string `xml:"type,attr"`
		Name string `xml:",chardata"`
	} `xml:"creators>name"`
	Tags []struct {
		Weight        int    `xml:"weight,attr"`
		LocalSpoiler  string `xml:"localspoiler,attr"`
		GlobalSpoiler string `xml:"globalspoiler,attr"`
		Name          string `xml:"name"`
	} `xml:"tags>tag"`
	Episodes []struct {
		ID     string `xml:"id,attr"`
		Number struct {
			Type  int    `xml:"type,attr"`
			Value string `xml:",chardata"`
		} `xml:"epno"`
		Length  int    `xml:"length"`
		AirDate string `xml:"airdate"`
		Titles  []struct {
			Language string `xml:"lang,attr"`
			Value    string `xml:",chardata"`
		} `xml:"title"`
	} `xml:"episodes>episode"`
	Resources []struct {
		Type     int `xml:"type,attr"`
		External []struct {
			Identifier []string `xml:"identifier"`
		} `xml:"externalentity"`
	} `xml:"resources>resource"`
}

func normalize(payload providers.Payload) (episodic.NormalizedRecord, error) {
	var value detail
	if err := xml.Unmarshal(payload.Body, &value); err != nil {
		return episodic.NormalizedRecord{}, err
	}
	if value.ID == "" {
		return episodic.NormalizedRecord{}, fmt.Errorf("invalid AniDB anime detail")
	}
	record := episodic.NormalizedRecord{SchemaVersion: 1, Kind: "anime", Provider: "anidb", Namespace: "anime", ProviderID: value.ID, PrimaryObservationID: payload.ObservationID, ObservedAt: payload.ObservedAt, Overview: strings.TrimSpace(value.Description), Format: normalizeType(value.Type), StartDate: value.StartDate, EndDate: value.EndDate, EpisodeCount: value.EpisodeCount, ExternalIDs: []episodic.ExternalID{{Provider: "anidb", Namespace: "anime", Value: value.ID}}}
	for _, title := range value.Titles {
		kind := title.Type
		if kind == "main" {
			kind = "main"
		} else if title.Language == "ja" && title.Type == "official" {
			kind = "original"
		} else {
			kind = "alias"
		}
		record.Titles = append(record.Titles, episodic.Title{Value: strings.TrimSpace(title.Value), Language: title.Language, Type: kind})
	}
	for _, creator := range value.Creators {
		if strings.Contains(strings.ToLower(creator.Type), "animation work") {
			record.Studios = append(record.Studios, strings.TrimSpace(creator.Name))
		}
	}
	for _, tag := range value.Tags {
		if tag.Weight >= 500 && tag.LocalSpoiler != "true" && tag.GlobalSpoiler != "true" {
			record.Genres = append(record.Genres, tag.Name)
		}
	}
	for _, resource := range value.Resources {
		for _, external := range resource.External {
			if len(external.Identifier) == 0 {
				continue
			}
			switch resource.Type {
			case 2:
				record.ExternalIDs = append(record.ExternalIDs, episodic.ExternalID{Provider: "myanimelist", Namespace: "anime", Value: external.Identifier[0]})
			case 43:
				record.ExternalIDs = append(record.ExternalIDs, episodic.ExternalID{Provider: "imdb", Namespace: "title", Value: external.Identifier[0]})
			case 44:
				namespace := "tv"
				if len(external.Identifier) > 1 && external.Identifier[1] != "" {
					namespace = external.Identifier[1]
				}
				record.ExternalIDs = append(record.ExternalIDs, episodic.ExternalID{Provider: "tmdb", Namespace: namespace, Value: external.Identifier[0]})
			}
		}
	}
	record.Genres = episodic.SortStrings(record.Genres)
	record.Studios = episodic.SortStrings(record.Studios)
	for _, episode := range value.Episodes {
		titles := []episodic.Title{}
		for _, title := range episode.Titles {
			titles = append(titles, episodic.Title{Value: strings.TrimSpace(title.Value), Language: title.Language, Type: "main"})
		}
		scheme := "aired"
		if episode.Number.Type == 2 {
			scheme = "special"
		} else if episode.Number.Type == 3 {
			scheme = "credit"
		} else if episode.Number.Type == 4 {
			scheme = "trailer"
		} else if episode.Number.Type == 5 {
			scheme = "parody"
		}
		record.Episodes = append(record.Episodes, episodic.Episode{ProviderID: episode.ID, Titles: titles, Numbers: []episodic.EpisodeNumber{{Scheme: scheme, Number: animeEpisodeNumber(episode.Number.Value)}}, AirDate: episode.AirDate, RuntimeMinutes: episode.Length})
	}
	sort.SliceStable(record.Episodes, func(i, j int) bool {
		left, right := record.Episodes[i].Numbers[0], record.Episodes[j].Numbers[0]
		if animeSchemeOrder(left.Scheme) != animeSchemeOrder(right.Scheme) {
			return animeSchemeOrder(left.Scheme) < animeSchemeOrder(right.Scheme)
		}
		return left.Number < right.Number
	})
	if value.Picture != "" {
		record.Images = append(record.Images, episodic.Image{ProviderID: value.Picture, URL: "https://cdn-eu.anidb.net/images/main/" + value.Picture, Class: "poster"})
	}
	return record, nil
}
func normalizeType(value string) string {
	return strings.NewReplacer(" ", "_", "-", "_").Replace(strings.ToLower(strings.TrimSpace(value)))
}
func animeEpisodeNumber(value string) float64 {
	value = strings.TrimSpace(value)
	value = strings.TrimLeftFunc(value, func(r rune) bool { return (r < '0' || r > '9') && r != '.' })
	number, _ := strconv.ParseFloat(value, 64)
	return number
}
func animeSchemeOrder(value string) int {
	switch value {
	case "aired":
		return 0
	case "special":
		return 1
	case "credit":
		return 2
	case "trailer":
		return 3
	case "parody":
		return 4
	}
	return 5
}
