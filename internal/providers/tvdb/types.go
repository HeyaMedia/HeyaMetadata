package tvdb

type envelope[T any] struct {
	Status string `json:"status"`
	Data   T      `json:"data"`
}

type remoteSearchResult struct {
	Movie *struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"movie"`
}

type movie struct {
	Aliases             []alias             `json:"aliases"`
	Artworks            []artwork           `json:"artworks"`
	AudioLanguages      []string            `json:"audioLanguages"`
	Characters          []character         `json:"characters"`
	Companies           companies           `json:"companies"`
	ContentRatings      []contentRating     `json:"contentRatings"`
	FirstRelease        release             `json:"first_release"`
	Genres              []namedRecord       `json:"genres"`
	ID                  int64               `json:"id"`
	Image               string              `json:"image"`
	Name                string              `json:"name"`
	OriginalCountry     string              `json:"originalCountry"`
	OriginalLanguage    string              `json:"originalLanguage"`
	ProductionCountries []productionCountry `json:"production_countries"`
	Releases            []release           `json:"releases"`
	RemoteIDs           []remoteID          `json:"remoteIds"`
	Runtime             *int                `json:"runtime"`
	Score               float64             `json:"score"`
	SpokenLanguages     []string            `json:"spoken_languages"`
	Status              status              `json:"status"`
	Studios             []namedRecord       `json:"studios"`
	TagOptions          []tagOption         `json:"tagOptions"`
	Trailers            []trailer           `json:"trailers"`
	Translations        translations        `json:"translations"`
	Year                string              `json:"year"`
}

type alias struct{ Language, Name string }
type artwork struct {
	ID       int64   `json:"id"`
	Image    string  `json:"image"`
	Language string  `json:"language"`
	Score    float64 `json:"score"`
	Type     int     `json:"type"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
}
type character struct {
	ID             int64  `json:"id"`
	Name           string `json:"name"`
	PeopleID       int64  `json:"peopleId"`
	PeopleType     string `json:"peopleType"`
	PersonName     string `json:"personName"`
	PersonImageURL string `json:"personImgURL"`
	Sort           int    `json:"sort"`
}
type companies struct {
	Studio         []company `json:"studio"`
	Production     []company `json:"production"`
	Distributor    []company `json:"distributor"`
	SpecialEffects []company `json:"special_effects"`
}
type company struct {
	ID            int64 `json:"id"`
	Name, Country string
}
type contentRating struct{ Name, Country, ContentType string }
type namedRecord struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}
type productionCountry struct {
	Country string `json:"country"`
}
type release struct{ Country, Date, Detail string }
type remoteID struct {
	ID         string `json:"id"`
	Type       int    `json:"type"`
	SourceName string `json:"sourceName"`
}
type status struct{ Name, RecordType string }
type tagOption struct {
	ID            int64 `json:"id"`
	Name, TagName string
}
type trailer struct {
	ID                  int64 `json:"id"`
	Language, Name, URL string
}
type translations struct {
	Name     []translation `json:"nameTranslations"`
	Overview []translation `json:"overviewTranslations"`
}
type translation struct {
	Aliases                  []string `json:"aliases"`
	IsAlias, IsPrimary       bool
	Language, Name, Overview string
}
