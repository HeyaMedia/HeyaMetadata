package fanart

type movieResponse struct {
	Name             string  `json:"name"`
	TMDBID           string  `json:"tmdb_id"`
	IMDBID           string  `json:"imdb_id"`
	MoviePosters     []image `json:"movieposter"`
	MovieBackgrounds []image `json:"moviebackground"`
	HDMovieLogos     []image `json:"hdmovielogo"`
	MovieLogos       []image `json:"movielogo"`
	MovieBanners     []image `json:"moviebanner"`
	HDMovieClearArts []image `json:"hdmovieclearart"`
	MovieArts        []image `json:"movieart"`
	MovieThumbs      []image `json:"moviethumb"`
	MovieDiscs       []image `json:"moviedisc"`
}

type tvResponse struct {
	Name            string        `json:"name"`
	TVDBID          string        `json:"thetvdb_id"`
	TVPosters       []image       `json:"tvposter"`
	ShowBackgrounds []image       `json:"showbackground"`
	HDTVLogos       []image       `json:"hdtvlogo"`
	ClearLogos      []image       `json:"clearlogo"`
	TVBanners       []image       `json:"tvbanner"`
	HDClearArts     []image       `json:"hdclearart"`
	ClearArts       []image       `json:"clearart"`
	TVThumbs        []image       `json:"tvthumb"`
	CharacterArts   []image       `json:"characterart"`
	SeasonPosters   []seasonImage `json:"seasonposter"`
	SeasonBanners   []seasonImage `json:"seasonbanner"`
	SeasonThumbs    []seasonImage `json:"seasonthumb"`
}

type seasonImage struct {
	image
	Season string `json:"season"`
}

type musicResponse struct {
	Name              string  `json:"name"`
	MBID              string  `json:"mbid_id"`
	ArtistBackgrounds []image `json:"artistbackground"`
	ArtistThumbs      []image `json:"artistthumb"`
	HDArtistLogos     []image `json:"hdartistlogo"`
	HDMusicLogos      []image `json:"hdmusiclogo"`
	MusicLogos        []image `json:"musiclogo"`
	MusicBanners      []image `json:"musicbanner"`
	Albums            []struct {
		ReleaseGroupID string  `json:"release_group_id"`
		AlbumCovers    []image `json:"albumcover"`
		CDArt          []image `json:"cdart"`
	} `json:"albums"`
}

type image struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Lang   string `json:"lang"`
	Likes  string `json:"likes"`
	Width  string `json:"width"`
	Height string `json:"height"`
}
