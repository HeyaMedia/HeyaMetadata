package tmdb

type named struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type image struct {
	FilePath    string  `json:"file_path"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	Language    *string `json:"iso_639_1"`
	VoteAverage float64 `json:"vote_average"`
	VoteCount   int     `json:"vote_count"`
}

type MovieDetail struct {
	ID                  int64    `json:"id"`
	Title               string   `json:"title"`
	OriginalTitle       string   `json:"original_title"`
	OriginalLanguage    string   `json:"original_language"`
	Overview            string   `json:"overview"`
	Tagline             string   `json:"tagline"`
	ReleaseDate         string   `json:"release_date"`
	Status              string   `json:"status"`
	Runtime             int      `json:"runtime"`
	Budget              int64    `json:"budget"`
	Revenue             int64    `json:"revenue"`
	Popularity          float64  `json:"popularity"`
	VoteAverage         float64  `json:"vote_average"`
	VoteCount           int      `json:"vote_count"`
	Homepage            string   `json:"homepage"`
	IMDbID              string   `json:"imdb_id"`
	Genres              []named  `json:"genres"`
	OriginCountry       []string `json:"origin_country"`
	ProductionCountries []struct {
		Code string `json:"iso_3166_1"`
		Name string `json:"name"`
	} `json:"production_countries"`
	SpokenLanguages []struct {
		Code string `json:"iso_639_1"`
		Name string `json:"english_name"`
	} `json:"spoken_languages"`
	ProductionCompanies []struct {
		ID            int64  `json:"id"`
		Name          string `json:"name"`
		OriginCountry string `json:"origin_country"`
		LogoPath      string `json:"logo_path"`
	} `json:"production_companies"`
	Collection *struct {
		ID           int64  `json:"id"`
		Name         string `json:"name"`
		PosterPath   string `json:"poster_path"`
		BackdropPath string `json:"backdrop_path"`
	} `json:"belongs_to_collection"`
	ExternalIDs struct {
		IMDbID      string `json:"imdb_id"`
		WikidataID  string `json:"wikidata_id"`
		FacebookID  string `json:"facebook_id"`
		InstagramID string `json:"instagram_id"`
		TwitterID   string `json:"twitter_id"`
	} `json:"external_ids"`
	Keywords struct {
		Keywords []named `json:"keywords"`
		Results  []named `json:"results"`
	} `json:"keywords"`
	AlternativeTitles struct {
		Titles []struct {
			Country string `json:"iso_3166_1"`
			Title   string `json:"title"`
			Type    string `json:"type"`
		} `json:"titles"`
	} `json:"alternative_titles"`
	Translations struct {
		Translations []struct {
			Language string `json:"iso_639_1"`
			Country  string `json:"iso_3166_1"`
			Data     struct {
				Title    string `json:"title"`
				Overview string `json:"overview"`
				Tagline  string `json:"tagline"`
			} `json:"data"`
		} `json:"translations"`
	} `json:"translations"`
	ReleaseDates struct {
		Results []struct {
			Country string `json:"iso_3166_1"`
			Dates   []struct {
				Certification string `json:"certification"`
				Date          string `json:"release_date"`
				Type          int    `json:"type"`
				Note          string `json:"note"`
			} `json:"release_dates"`
		} `json:"results"`
	} `json:"release_dates"`
	Videos struct {
		Results []struct {
			Site        string `json:"site"`
			Key         string `json:"key"`
			Type        string `json:"type"`
			Name        string `json:"name"`
			Language    string `json:"iso_639_1"`
			Country     string `json:"iso_3166_1"`
			Official    bool   `json:"official"`
			PublishedAt string `json:"published_at"`
		} `json:"results"`
	} `json:"videos"`
	Credits struct {
		Cast []struct {
			ID          int64  `json:"id"`
			Name        string `json:"name"`
			Character   string `json:"character"`
			Order       int    `json:"order"`
			ProfilePath string `json:"profile_path"`
		} `json:"cast"`
		Crew []struct {
			ID          int64  `json:"id"`
			Name        string `json:"name"`
			Department  string `json:"department"`
			Job         string `json:"job"`
			ProfilePath string `json:"profile_path"`
		} `json:"crew"`
	} `json:"credits"`
	Images struct {
		Posters   []image `json:"posters"`
		Backdrops []image `json:"backdrops"`
		Logos     []image `json:"logos"`
	} `json:"images"`
	Recommendations struct {
		Results []struct {
			ID          int64   `json:"id"`
			Title       string  `json:"title"`
			ReleaseDate string  `json:"release_date"`
			PosterPath  string  `json:"poster_path"`
			Popularity  float64 `json:"popularity"`
		} `json:"results"`
	} `json:"recommendations"`
}

type CollectionDetail struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Overview  string  `json:"overview"`
	Posters   []image `json:"posters"`
	Backdrops []image `json:"backdrops"`
	Parts     []struct {
		ID          int64  `json:"id"`
		Title       string `json:"title"`
		ReleaseDate string `json:"release_date"`
		PosterPath  string `json:"poster_path"`
	} `json:"parts"`
}
