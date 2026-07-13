package tvshows

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/HeyaMedia/HeyaMetadata/internal/episodic"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
	"github.com/HeyaMedia/HeyaMetadata/internal/providers/tvdb"
)

const tvdbSeriesNormalizerVersion = "tvdb-series/v4"

type tvdbEnvelope struct {
	Data tvdbSeries `json:"data"`
}

type tvdbCompany struct {
	ID            int64 `json:"id"`
	Name, Country string
	CompanyType   struct {
		Name string `json:"companyTypeName"`
	} `json:"companyType"`
}

type tvdbCompanies struct {
	Studio, Production, Distributor, SpecialEffects, Network []tvdbCompany
}

func (companies *tvdbCompanies) UnmarshalJSON(body []byte) error {
	var grouped struct {
		Studio, Production, Distributor, SpecialEffects, Network []tvdbCompany
	}
	if len(body) > 0 && body[0] == '{' {
		if err := json.Unmarshal(body, &grouped); err != nil {
			return err
		}
		companies.Studio = grouped.Studio
		companies.Production = grouped.Production
		companies.Distributor = grouped.Distributor
		companies.SpecialEffects = grouped.SpecialEffects
		companies.Network = grouped.Network
		return nil
	}
	var values []tvdbCompany
	if err := json.Unmarshal(body, &values); err != nil {
		return err
	}
	for _, company := range values {
		switch normalizeType(company.CompanyType.Name) {
		case "network":
			companies.Network = append(companies.Network, company)
		case "studio":
			companies.Studio = append(companies.Studio, company)
		case "distributor":
			companies.Distributor = append(companies.Distributor, company)
		case "special_effects":
			companies.SpecialEffects = append(companies.SpecialEffects, company)
		default:
			companies.Production = append(companies.Production, company)
		}
	}
	return nil
}

type tvdbSeries struct {
	ID                                                                    int64 `json:"id"`
	Name, FirstAired, LastAired, Image, OriginalCountry, OriginalLanguage string
	AverageRuntime                                                        int `json:"averageRuntime"`
	Status                                                                struct {
		Name string `json:"name"`
	} `json:"status"`
	Aliases         []struct{ Language, Name string } `json:"aliases"`
	Genres, Studios []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	Companies      tvdbCompanies `json:"companies"`
	ContentRatings []struct {
		Name        string `json:"name"`
		Country     string `json:"country"`
		ContentType string `json:"contentType"`
	} `json:"contentRatings"`
	Translations struct {
		Names []struct {
			Aliases                  []string `json:"aliases"`
			IsAlias, IsPrimary       bool
			Language, Name, Overview string
		} `json:"nameTranslations"`
		Overviews []struct {
			Aliases                  []string `json:"aliases"`
			IsAlias, IsPrimary       bool
			Language, Name, Overview string
		} `json:"overviewTranslations"`
	} `json:"translations"`
	RemoteIDs []struct {
		ID         string `json:"id"`
		Type       int    `json:"type"`
		SourceName string `json:"sourceName"`
	} `json:"remoteIds"`
	Artworks []struct {
		ID                  int64 `json:"id"`
		Image, Language     string
		Type, Width, Height int
		Score               float64
	} `json:"artworks"`
	Seasons []struct {
		ID     int64  `json:"id"`
		Number int    `json:"number"`
		Name   string `json:"name"`
		Image  string `json:"image"`
		Type   struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"type"`
	} `json:"seasons"`
	Episodes []struct {
		ID                                            int64 `json:"id"`
		Name, Overview, Aired, Image                  string
		SeasonNumber, Number, AbsoluteNumber, Runtime int
		Translations                                  struct {
			Names []struct {
				Aliases                  []string `json:"aliases"`
				IsAlias, IsPrimary       bool
				Language, Name, Overview string
			} `json:"nameTranslations"`
			Overviews []struct {
				Aliases                  []string `json:"aliases"`
				IsAlias, IsPrimary       bool
				Language, Name, Overview string
			} `json:"overviewTranslations"`
		} `json:"translations"`
	} `json:"episodes"`
	Characters []struct {
		ID             int64  `json:"id"`
		PeopleID       int64  `json:"peopleId"`
		PersonName     string `json:"personName"`
		PeopleType     string `json:"peopleType"`
		Name           string `json:"name"`
		Sort           int    `json:"sort"`
		PersonImageURL string `json:"personImgURL"`
	} `json:"characters"`
	Score float64 `json:"score"`
}

func normalizeTVDBSeries(payload providers.Payload, kind string, seasonFilter *int, episodeOffset int, seasonPayloads ...providers.Payload) (episodic.NormalizedRecord, error) {
	var wrapper tvdbEnvelope
	if err := json.Unmarshal(payload.Body, &wrapper); err != nil {
		return episodic.NormalizedRecord{}, err
	}
	v := wrapper.Data
	if v.ID < 1 || strings.TrimSpace(v.Name) == "" {
		return episodic.NormalizedRecord{}, fmt.Errorf("invalid TVDB series detail")
	}
	r := episodic.NormalizedRecord{SchemaVersion: 1, Kind: kind, Provider: "tvdb", Namespace: "series", ProviderID: strconv.FormatInt(v.ID, 10), PrimaryObservationID: payload.ObservationID, ObservedAt: payload.ObservedAt, NormalizerVersion: tvdbSeriesNormalizerVersion, Titles: []episodic.Title{{Value: v.Name, Language: v.OriginalLanguage, Type: "main"}}, Status: normalizeType(v.Status.Name), Language: v.OriginalLanguage, Countries: []string{v.OriginalCountry}, StartDate: v.FirstAired, EndDate: v.LastAired, RuntimeMinutes: v.AverageRuntime, ExternalIDs: []episodic.ExternalID{{Provider: "tvdb", Namespace: "series", Value: strconv.FormatInt(v.ID, 10)}}}
	if v.Score > 0 && v.Score <= 10 {
		r.Ratings = append(r.Ratings, episodic.Rating{System: "tvdb", Value: v.Score, ScaleMin: 0, ScaleMax: 10})
	}
	for _, character := range v.Characters {
		creditType := "crew"
		if strings.EqualFold(character.PeopleType, "actor") {
			creditType = "cast"
		}
		personID := character.PeopleID
		if personID == 0 {
			personID = character.ID
		}
		r.Credits = append(r.Credits, episodic.Credit{Provider: "tvdb", ProviderPersonID: strconv.FormatInt(personID, 10), DisplayName: character.PersonName, CreditType: creditType, Character: character.Name, Job: character.PeopleType, Order: character.Sort, ProfileURL: tvdbArtworkURL(character.PersonImageURL)})
	}
	for _, x := range v.Aliases {
		r.Titles = append(r.Titles, episodic.Title{Value: x.Name, Language: x.Language, Type: "alias"})
	}
	for _, x := range v.Translations.Names {
		if strings.TrimSpace(x.Name) != "" {
			r.Titles = append(r.Titles, episodic.Title{Value: strings.TrimSpace(x.Name), Language: x.Language, Type: "translated"})
		}
		for _, alias := range x.Aliases {
			if strings.TrimSpace(alias) != "" {
				r.Titles = append(r.Titles, episodic.Title{Value: strings.TrimSpace(alias), Language: x.Language, Type: "alias"})
			}
		}
	}
	for _, x := range v.Translations.Overviews {
		if strings.TrimSpace(x.Overview) != "" {
			r.Overviews = append(r.Overviews, episodic.Text{Value: strings.TrimSpace(x.Overview), Language: x.Language, Type: "overview"})
		}
	}
	if len(r.Overviews) > 0 {
		r.Overview = r.Overviews[0].Value
	}
	for _, x := range v.Genres {
		r.Genres = append(r.Genres, x.Name)
	}
	for _, x := range v.Studios {
		r.Studios = append(r.Studios, x.Name)
		r.Organizations = append(r.Organizations, episodic.Organization{Name: x.Name, Type: "studio", ExternalIDs: []episodic.ExternalID{{Provider: "tvdb", Namespace: "company", Value: strconv.FormatInt(x.ID, 10)}}})
	}
	appendCompanies := func(values []tvdbCompany, kind string) {
		for _, company := range values {
			if strings.TrimSpace(company.Name) == "" {
				continue
			}
			organization := episodic.Organization{Name: company.Name, Country: company.Country, Type: kind}
			if company.ID > 0 {
				organization.ExternalIDs = []episodic.ExternalID{{Provider: "tvdb", Namespace: "company", Value: strconv.FormatInt(company.ID, 10)}}
			}
			r.Organizations = append(r.Organizations, organization)
		}
	}
	appendCompanies(v.Companies.Studio, "studio")
	appendCompanies(v.Companies.Production, "production_company")
	appendCompanies(v.Companies.Distributor, "distributor")
	appendCompanies(v.Companies.SpecialEffects, "special_effects")
	for _, company := range v.Companies.Network {
		if strings.TrimSpace(company.Name) == "" {
			continue
		}
		network := episodic.Network{Name: company.Name, Country: company.Country, Type: "network"}
		if company.ID > 0 {
			network.ExternalIDs = []episodic.ExternalID{{Provider: "tvdb", Namespace: "company", Value: strconv.FormatInt(company.ID, 10)}}
		}
		r.Networks = append(r.Networks, network)
	}
	for _, rating := range v.ContentRatings {
		if strings.TrimSpace(rating.Name) != "" {
			r.Certifications = append(r.Certifications, episodic.Certification{System: "tvdb", Country: rating.Country, Rating: rating.Name, Description: rating.ContentType})
		}
	}
	for _, x := range v.RemoteIDs {
		switch x.Type {
		case 2:
			r.ExternalIDs = append(r.ExternalIDs, episodic.ExternalID{Provider: "imdb", Namespace: "title", Value: x.ID})
		case 12:
			r.ExternalIDs = append(r.ExternalIDs, episodic.ExternalID{Provider: "tmdb", Namespace: "tv", Value: x.ID})
		case 18:
			r.ExternalIDs = append(r.ExternalIDs, episodic.ExternalID{Provider: "wikidata", Namespace: "item", Value: strings.ToUpper(x.ID)})
		case 23:
			r.ExternalIDs = append(r.ExternalIDs, episodic.ExternalID{Provider: "anidb", Namespace: "anime", Value: x.ID})
		}
	}
	if v.Image != "" {
		r.Images = append(r.Images, episodic.Image{Provider: "tvdb", ProviderID: "primary", URL: tvdbArtworkURL(v.Image), Class: "poster", Language: v.OriginalLanguage})
	}
	for _, x := range v.Artworks {
		class := tvdb.ArtworkClass(x.Type)
		if class != "" {
			r.Images = append(r.Images, episodic.Image{Provider: "tvdb", ProviderID: strconv.FormatInt(x.ID, 10), URL: tvdbArtworkURL(x.Image), Class: class, Language: x.Language, Width: x.Width, Height: x.Height, ProviderScore: x.Score})
		}
	}
	for _, x := range v.Seasons {
		if x.Type.ID != 0 && x.Type.ID != 1 {
			continue
		}
		if seasonFilter == nil || x.Number == *seasonFilter {
			providerID := strconv.FormatInt(x.ID, 10)
			season := episodic.Season{ProviderID: providerID, Number: x.Number, Name: x.Name, Status: normalizeType(x.Type.Name), ExternalIDs: []episodic.ExternalID{{Provider: "tvdb", Namespace: "season", Value: providerID}}}
			if x.Name != "" {
				season.Titles = []episodic.Title{{Value: x.Name, Language: v.OriginalLanguage, Type: "display"}}
			}
			if x.Image != "" {
				season.Images = []episodic.Image{{Provider: "tvdb", ProviderID: "season:" + providerID + ":poster", URL: tvdbArtworkURL(x.Image), Class: "poster", Language: v.OriginalLanguage}}
			}
			r.Seasons = append(r.Seasons, season)
		}
	}
	for _, seasonPayload := range seasonPayloads {
		appendTVDBSeasonArtwork(&r, seasonPayload, seasonFilter)
	}
	for _, x := range v.Episodes {
		if seasonFilter != nil && x.SeasonNumber != *seasonFilter {
			continue
		}
		number := x.Number - episodeOffset
		if number < 1 {
			continue
		}
		providerID := strconv.FormatInt(x.ID, 10)
		item := episodic.Episode{ProviderID: providerID, ExternalIDs: []episodic.ExternalID{{Provider: "tvdb", Namespace: "episode", Value: providerID}}, Titles: []episodic.Title{{Value: x.Name, Language: v.OriginalLanguage, Type: "main"}}, Numbers: []episodic.EpisodeNumber{{Scheme: "aired", Season: x.SeasonNumber, Number: float64(number), Provider: "tvdb"}, {Scheme: "tvdb", Season: x.SeasonNumber, Number: float64(x.Number), Provider: "tvdb"}}, IsSpecial: x.SeasonNumber == 0, EpisodeType: "regular", AirDate: x.Aired, RuntimeMinutes: x.Runtime, Summary: strings.TrimSpace(x.Overview)}
		if item.IsSpecial {
			item.EpisodeType = "special"
		}
		if x.AbsoluteNumber > 0 {
			item.Numbers = append(item.Numbers, episodic.EpisodeNumber{Scheme: "absolute", Number: float64(x.AbsoluteNumber), Provider: "tvdb"})
		}
		if item.Summary != "" {
			item.Overviews = append(item.Overviews, episodic.Text{Value: item.Summary, Language: v.OriginalLanguage, Type: "overview"})
		}
		for _, translation := range x.Translations.Names {
			if strings.TrimSpace(translation.Name) != "" {
				item.Titles = append(item.Titles, episodic.Title{Value: strings.TrimSpace(translation.Name), Language: translation.Language, Type: "translated"})
			}
		}
		for _, translation := range x.Translations.Overviews {
			if strings.TrimSpace(translation.Overview) != "" {
				item.Overviews = append(item.Overviews, episodic.Text{Value: strings.TrimSpace(translation.Overview), Language: translation.Language, Type: "overview"})
			}
		}
		if x.Image != "" {
			item.Images = []episodic.Image{{Provider: "tvdb", ProviderID: "episode:" + providerID + ":still", URL: tvdbArtworkURL(x.Image), Class: "still"}}
		}
		r.Episodes = append(r.Episodes, item)
	}
	r.EpisodeCount = len(r.Episodes)
	r.SeasonCount = len(r.Seasons)
	return r, nil
}

func appendTVDBSeasonArtwork(record *episodic.NormalizedRecord, payload providers.Payload, seasonFilter *int) {
	if payload.StatusCode != 0 && payload.StatusCode != http.StatusOK {
		return
	}
	var wrapper struct {
		Data struct {
			Number  int `json:"number"`
			Artwork []struct {
				ID                  int64 `json:"id"`
				Image, Language     string
				Type, Width, Height int
				Score               float64
			} `json:"artwork"`
		} `json:"data"`
	}
	if json.Unmarshal(payload.Body, &wrapper) != nil || (seasonFilter != nil && wrapper.Data.Number != *seasonFilter) {
		return
	}
	for i := range record.Seasons {
		if record.Seasons[i].Number != wrapper.Data.Number {
			continue
		}
		for _, artwork := range wrapper.Data.Artwork {
			class := tvdb.ArtworkClass(artwork.Type)
			url := tvdbArtworkURL(artwork.Image)
			if class == "" || artwork.ID < 1 || url == "" {
				continue
			}
			candidate := episodic.Image{Provider: "tvdb", ProviderID: strconv.FormatInt(artwork.ID, 10), URL: url, Class: class, Language: artwork.Language, Width: artwork.Width, Height: artwork.Height, ProviderScore: artwork.Score}
			if !containsTVDBImage(record.Seasons[i].Images, candidate) {
				record.Seasons[i].Images = append(record.Seasons[i].Images, candidate)
			}
		}
		return
	}
}

func containsTVDBImage(values []episodic.Image, candidate episodic.Image) bool {
	for _, value := range values {
		if value.Provider == candidate.Provider && value.URL == candidate.URL {
			return true
		}
	}
	return false
}

func tvdbArtworkURL(value string) string {
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	if value == "" {
		return ""
	}
	return "https://artworks.thetvdb.com/" + strings.TrimLeft(value, "/")
}
