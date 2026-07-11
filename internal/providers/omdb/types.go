package omdb

type response struct {
	Title      string   `json:"Title"`
	Year       string   `json:"Year"`
	Rated      string   `json:"Rated"`
	Released   string   `json:"Released"`
	Runtime    string   `json:"Runtime"`
	Genre      string   `json:"Genre"`
	Plot       string   `json:"Plot"`
	Language   string   `json:"Language"`
	Country    string   `json:"Country"`
	Awards     string   `json:"Awards"`
	Poster     string   `json:"Poster"`
	Ratings    []rating `json:"Ratings"`
	Metascore  string   `json:"Metascore"`
	IMDBRating string   `json:"imdbRating"`
	IMDBVotes  string   `json:"imdbVotes"`
	IMDBID     string   `json:"imdbID"`
	Type       string   `json:"Type"`
	DVD        string   `json:"DVD"`
	BoxOffice  string   `json:"BoxOffice"`
	Website    string   `json:"Website"`
	Response   string   `json:"Response"`
	Error      string   `json:"Error"`
}

type rating struct {
	Source string `json:"Source"`
	Value  string `json:"Value"`
}
