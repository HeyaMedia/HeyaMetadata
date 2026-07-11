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

type image struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Lang   string `json:"lang"`
	Likes  string `json:"likes"`
	Width  string `json:"width"`
	Height string `json:"height"`
}
