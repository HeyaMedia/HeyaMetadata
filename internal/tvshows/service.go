package tvshows

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/HeyaMedia/HeyaMetadata/internal/episodic"
	"github.com/HeyaMedia/HeyaMetadata/internal/platform"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercache"
	"github.com/HeyaMedia/HeyaMetadata/internal/providercredentials"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/tmdb"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/tvdb"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/tvmaze"
)

var definition = episodic.Definition{Kind: "tv_show", Provider: "tvmaze", Namespace: "show", NormalizerVersion: "tvmaze-tv-show/v2", MergeVersion: "tv-show-combiner/v3"}
var htmlTags = regexp.MustCompile(`<[^>]+>`)

type Service struct{ runtime *platform.Runtime }

func NewService(runtime *platform.Runtime) *Service { return &Service{runtime: runtime} }
func (s *Service) IngestTVMaze(ctx context.Context, id string, jobID int64) (result episodic.Result, returnErr error) {
	return s.IngestTVMazeWithCredentials(ctx, id, jobID, providercredentials.Credentials{})
}
func (s *Service) IngestTVMazeWithCredentials(ctx context.Context, id string, jobID int64, credentials providercredentials.Credentials) (result episodic.Result, returnErr error) {
	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		return result, fmt.Errorf("invalid TVMaze show ID")
	}
	if err := episodic.StartRun(ctx, s.runtime, jobID, definition, id); err != nil {
		return result, err
	}
	defer func() {
		if returnErr != nil {
			episodic.FailRun(ctx, s.runtime, jobID, returnErr)
		}
	}()
	base := tvmaze.New(s.runtime.Config.Providers.TVMaze)
	resolver, err := providercache.New(s.runtime, definition.NormalizerVersion, base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
	if err != nil {
		return result, err
	}
	payloads, err := tvmaze.NewCached(s.runtime.Config.Providers.TVMaze, resolver).Collect(ctx, providers.Identifier{Provider: "tvmaze", Namespace: "show", Value: id})
	if err != nil {
		return result, err
	}
	if len(payloads) == 0 {
		return result, fmt.Errorf("TVMaze returned no show detail")
	}
	payload := payloads[len(payloads)-1]
	if payload.StatusCode != http.StatusOK {
		return result, &providers.StatusError{Provider: "tvmaze", StatusCode: payload.StatusCode}
	}
	record, err := normalize(payload)
	if err != nil {
		return result, err
	}
	record.NormalizerVersion = definition.NormalizerVersion
	records := []episodic.NormalizedRecord{record}

	if tvdbID := externalID(record, "tvdb", "series"); tvdbID != "" && (credentials.APIKey("tvdb") != "" || s.runtime.Config.Providers.TVDB.APIKey != "") {
		base := tvdb.New(s.runtime.Config.Providers.TVDB)
		cache, cacheErr := providercache.New(s.runtime, tvdbSeriesNormalizerVersion, base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
		if cacheErr != nil {
			return result, cacheErr
		}
		payloads, collectErr := tvdb.NewCached(s.runtime.Config.Providers.TVDB, cache, credentials.APIKey("tvdb"), s.runtime.Redis).CollectSeries(ctx, providers.Identifier{Provider: "tvdb", Namespace: "series", Value: tvdbID})
		if collectErr == nil && len(payloads) > 0 && payloads[0].StatusCode == http.StatusOK {
			if supplemental, normalizeErr := normalizeTVDBSeries(payloads[0], "tv_show", nil, 0); normalizeErr == nil {
				records = append(records, supplemental)
			}
		}
	}

	if imdbID := externalID(record, "imdb", "title"); imdbID != "" && (credentials.APIKey("tmdb") != "" || s.runtime.Config.Providers.TMDB.Token != "") {
		base := tmdb.New(s.runtime.Config.Providers.TMDB)
		cache, cacheErr := providercache.New(s.runtime, tmdbTVNormalizerVersion, base.Capability().RawRetention, base.Capability().ResponseCache, jobID)
		if cacheErr != nil {
			return result, cacheErr
		}
		client := tmdb.NewCached(s.runtime.Config.Providers.TMDB, cache, credentials.APIKey("tmdb"))
		lookup, lookupErr := client.FindTVByIMDb(ctx, imdbID)
		if lookupErr == nil && lookup.StatusCode == http.StatusOK {
			if tmdbID := tmdbTVID(lookup.Body); tmdbID != "" {
				payloads, collectErr := client.CollectTV(ctx, providers.Identifier{Provider: "tmdb", Namespace: "tv", Value: tmdbID})
				if collectErr == nil {
					if supplemental, normalizeErr := normalizeTMDBTV(payloads); normalizeErr == nil {
						records = append(records, supplemental)
					}
				}
			}
		}
	}
	return episodic.PersistMany(ctx, s.runtime, definition, records, jobID)
}
func (s *Service) Resolve(ctx context.Context, provider, namespace, value string) (string, error) {
	return episodic.Resolve(ctx, s.runtime, "tv_show", provider, namespace, value)
}
func (s *Service) Detail(ctx context.Context, id string) (episodic.Document, bool, error) {
	return episodic.Detail(ctx, s.runtime, "tv_show", id)
}

type show struct {
	ID                                                      int64 `json:"id"`
	Name, Type, Language, Status, Premiered, Ended, Summary string
	OfficialSite                                            string   `json:"officialSite"`
	Runtime                                                 int      `json:"runtime"`
	AverageRuntime                                          int      `json:"averageRuntime"`
	Genres                                                  []string `json:"genres"`
	Rating                                                  struct {
		Average float64 `json:"average"`
	} `json:"rating"`
	Image      struct{ Medium, Original string } `json:"image"`
	Network    *network                          `json:"network"`
	WebChannel *network                          `json:"webChannel"`
	Externals  struct {
		TVDB   int64  `json:"thetvdb"`
		IMDb   string `json:"imdb"`
		TVRage int64  `json:"tvrage"`
	} `json:"externals"`
	Embedded struct {
		AKAs []struct {
			Name    string `json:"name"`
			Country *struct {
				Code string `json:"code"`
			} `json:"country"`
		} `json:"akas"`
		Seasons []struct {
			ID           int64  `json:"id"`
			Number       int    `json:"number"`
			Name         string `json:"name"`
			EpisodeOrder int    `json:"episodeOrder"`
			PremiereDate string `json:"premiereDate"`
			EndDate      string `json:"endDate"`
			Summary      string `json:"summary"`
			Image        struct {
				Original string `json:"original"`
			} `json:"image"`
		} `json:"seasons"`
		Episodes []struct {
			ID      int64   `json:"id"`
			Name    string  `json:"name"`
			Season  int     `json:"season"`
			Number  float64 `json:"number"`
			Airdate string  `json:"airdate"`
			Runtime int     `json:"runtime"`
			Summary string  `json:"summary"`
			Type    string  `json:"type"`
			Rating  struct {
				Average float64 `json:"average"`
			} `json:"rating"`
			Image struct {
				Original string `json:"original"`
			} `json:"image"`
		} `json:"episodes"`
		Images []struct {
			ID          int64  `json:"id"`
			Type        string `json:"type"`
			Main        bool   `json:"main"`
			Resolutions map[string]struct {
				URL    string `json:"url"`
				Width  int    `json:"width"`
				Height int    `json:"height"`
			} `json:"resolutions"`
		} `json:"images"`
		Cast []struct {
			Person struct {
				ID    int64  `json:"id"`
				Name  string `json:"name"`
				Image struct {
					Original string `json:"original"`
				} `json:"image"`
			} `json:"person"`
			Character struct {
				Name string `json:"name"`
			} `json:"character"`
		} `json:"cast"`
		Crew []struct {
			Type   string `json:"type"`
			Person struct {
				ID    int64  `json:"id"`
				Name  string `json:"name"`
				Image struct {
					Original string `json:"original"`
				} `json:"image"`
			} `json:"person"`
		} `json:"crew"`
	} `json:"_embedded"`
}
type network struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Country *struct {
		Code string `json:"code"`
	} `json:"country"`
}

func normalize(payload providers.Payload) (episodic.NormalizedRecord, error) {
	var value show
	if err := json.Unmarshal(payload.Body, &value); err != nil {
		return episodic.NormalizedRecord{}, err
	}
	if value.ID < 1 || value.Name == "" {
		return episodic.NormalizedRecord{}, fmt.Errorf("invalid TVMaze show detail")
	}
	record := episodic.NormalizedRecord{SchemaVersion: 1, Kind: "tv_show", Provider: "tvmaze", Namespace: "show", ProviderID: strconv.FormatInt(value.ID, 10), PrimaryObservationID: payload.ObservationID, ObservedAt: payload.ObservedAt, Titles: []episodic.Title{{Value: value.Name, Language: languageCode(value.Language), Type: "main"}}, Overview: cleanHTML(value.Summary), Format: normalizeType(value.Type), Status: normalizeType(value.Status), Language: languageCode(value.Language), Genres: episodic.SortStrings(value.Genres), StartDate: value.Premiered, EndDate: value.Ended, RuntimeMinutes: value.Runtime, ExternalIDs: []episodic.ExternalID{{Provider: "tvmaze", Namespace: "show", Value: strconv.FormatInt(value.ID, 10)}}}
	if record.Overview != "" {
		record.Overviews = []episodic.Text{{Value: record.Overview, Language: record.Language, Type: "overview"}}
	}
	if value.OfficialSite != "" {
		record.Links = append(record.Links, episodic.Link{Type: "official", URL: value.OfficialSite})
	}
	if record.RuntimeMinutes == 0 {
		record.RuntimeMinutes = value.AverageRuntime
	}
	if value.Rating.Average > 0 {
		record.Ratings = append(record.Ratings, episodic.Rating{System: "tvmaze", Value: value.Rating.Average, ScaleMin: 0, ScaleMax: 10})
	}
	for i, cast := range value.Embedded.Cast {
		record.Credits = append(record.Credits, episodic.Credit{Provider: "tvmaze", ProviderPersonID: strconv.FormatInt(cast.Person.ID, 10), DisplayName: cast.Person.Name, CreditType: "cast", Character: cast.Character.Name, Order: i, ProfileURL: cast.Person.Image.Original})
	}
	for _, crew := range value.Embedded.Crew {
		record.Credits = append(record.Credits, episodic.Credit{Provider: "tvmaze", ProviderPersonID: strconv.FormatInt(crew.Person.ID, 10), DisplayName: crew.Person.Name, CreditType: "crew", Job: crew.Type, ProfileURL: crew.Person.Image.Original})
	}
	if value.Externals.TVDB > 0 {
		record.ExternalIDs = append(record.ExternalIDs, episodic.ExternalID{Provider: "tvdb", Namespace: "series", Value: strconv.FormatInt(value.Externals.TVDB, 10)})
	}
	if value.Externals.IMDb != "" {
		record.ExternalIDs = append(record.ExternalIDs, episodic.ExternalID{Provider: "imdb", Namespace: "title", Value: value.Externals.IMDb})
	}
	if value.Externals.TVRage > 0 {
		record.ExternalIDs = append(record.ExternalIDs, episodic.ExternalID{Provider: "tvrage", Namespace: "show", Value: strconv.FormatInt(value.Externals.TVRage, 10)})
	}
	for _, aka := range value.Embedded.AKAs {
		country := ""
		if aka.Country != nil {
			country = aka.Country.Code
		}
		record.Titles = append(record.Titles, episodic.Title{Value: aka.Name, Country: country, Type: "alias"})
	}
	if value.Network != nil {
		record.Networks = append(record.Networks, toNetwork(value.Network, "broadcast"))
	} else if value.WebChannel != nil {
		record.Networks = append(record.Networks, toNetwork(value.WebChannel, "streaming"))
	}
	if len(record.Networks) > 0 && record.Networks[0].Country != "" {
		record.Countries = []string{record.Networks[0].Country}
	}
	for _, season := range value.Embedded.Seasons {
		providerID := strconv.FormatInt(season.ID, 10)
		item := episodic.Season{ProviderID: providerID, Number: season.Number, Name: season.Name, EpisodeOrder: season.EpisodeOrder, EpisodeCount: season.EpisodeOrder, PremiereDate: season.PremiereDate, EndDate: season.EndDate, ExternalIDs: []episodic.ExternalID{{Provider: "tvmaze", Namespace: "season", Value: providerID}}}
		if season.Name != "" {
			item.Titles = []episodic.Title{{Value: season.Name, Language: record.Language, Type: "display"}}
		}
		if overview := cleanHTML(season.Summary); overview != "" {
			item.Overviews = []episodic.Text{{Value: overview, Language: record.Language, Type: "overview"}}
		}
		if season.EndDate != "" {
			item.Status = "ended"
		}
		if season.Image.Original != "" {
			item.Images = []episodic.Image{{Provider: "tvmaze", ProviderID: "season:" + providerID + ":poster", URL: season.Image.Original, Class: "poster"}}
		}
		record.Seasons = append(record.Seasons, item)
	}
	for _, episode := range value.Embedded.Episodes {
		providerID := strconv.FormatInt(episode.ID, 10)
		summary := cleanHTML(episode.Summary)
		item := episodic.Episode{ProviderID: providerID, ExternalIDs: []episodic.ExternalID{{Provider: "tvmaze", Namespace: "episode", Value: providerID}}, Titles: []episodic.Title{{Value: episode.Name, Language: record.Language, Type: "main"}}, Numbers: []episodic.EpisodeNumber{{Scheme: "aired", Season: episode.Season, Number: episode.Number, Provider: "tvmaze"}, {Scheme: "tvmaze", Season: episode.Season, Number: episode.Number, Provider: "tvmaze"}}, IsSpecial: episode.Season == 0, EpisodeType: normalizeType(episode.Type), AirDate: episode.Airdate, RuntimeMinutes: episode.Runtime, Summary: summary}
		if item.EpisodeType == "" {
			item.EpisodeType = "regular"
		}
		if summary != "" {
			item.Overviews = []episodic.Text{{Value: summary, Language: record.Language, Type: "overview"}}
		}
		if episode.Rating.Average > 0 {
			item.Ratings = []episodic.Rating{{System: "tvmaze", Value: episode.Rating.Average, ScaleMin: 0, ScaleMax: 10}}
		}
		if episode.Image.Original != "" {
			item.Images = []episodic.Image{{Provider: "tvmaze", ProviderID: "episode:" + providerID + ":still", URL: episode.Image.Original, Class: "still"}}
		}
		record.Episodes = append(record.Episodes, item)
	}
	record.EpisodeCount = len(record.Episodes)
	record.SeasonCount = len(record.Seasons)
	if value.Image.Original != "" {
		record.Images = append(record.Images, episodic.Image{Provider: "tvmaze", ProviderID: "show-original", URL: value.Image.Original, Class: "poster"})
	}
	for _, image := range value.Embedded.Images[:min(len(value.Embedded.Images), 50)] {
		for resolution, item := range image.Resolutions {
			if item.URL != "" {
				record.Images = append(record.Images, episodic.Image{Provider: "tvmaze", ProviderID: fmt.Sprintf("%d-%s", image.ID, resolution), URL: item.URL, Class: normalizeType(image.Type), Width: item.Width, Height: item.Height})
			}
		}
	}
	return record, nil
}
func toNetwork(value *network, kind string) episodic.Network {
	country := ""
	if value.Country != nil {
		country = value.Country.Code
	}
	result := episodic.Network{Name: value.Name, Country: country, Type: kind}
	if value.ID > 0 {
		result.ExternalIDs = []episodic.ExternalID{{Provider: "tvmaze", Namespace: "network", Value: strconv.FormatInt(value.ID, 10)}}
	}
	return result
}
func cleanHTML(value string) string {
	return strings.TrimSpace(html.UnescapeString(htmlTags.ReplaceAllString(value, " ")))
}
func normalizeType(value string) string {
	return strings.NewReplacer(" ", "_", "-", "_").Replace(strings.ToLower(strings.TrimSpace(value)))
}
func languageCode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if code := map[string]string{"english": "en", "japanese": "ja", "korean": "ko", "chinese": "zh", "french": "fr", "german": "de", "spanish": "es", "danish": "da", "swedish": "sv", "norwegian": "no"}[value]; code != "" {
		return code
	}
	return value
}

func externalID(record episodic.NormalizedRecord, provider, namespace string) string {
	for _, id := range record.ExternalIDs {
		if id.Provider == provider && id.Namespace == namespace {
			return id.Value
		}
	}
	return ""
}
func tmdbTVID(body []byte) string {
	var result struct {
		TVResults []struct {
			ID int64 `json:"id"`
		} `json:"tv_results"`
	}
	if json.Unmarshal(body, &result) != nil || len(result.TVResults) == 0 || result.TVResults[0].ID < 1 {
		return ""
	}
	return strconv.FormatInt(result.TVResults[0].ID, 10)
}
