package tvshows

import (
	"net/http"
	"testing"
	"time"

	"github.com/HeyaMedia/HeyaMetadata/internal/providers"
)

func TestNormalizeTMDBTVRetainsShowSeasonAndEpisodeProjection(t *testing.T) {
	detail := providers.Payload{ObservationID: "show-obs", ObservedAt: time.Unix(1, 0), StatusCode: http.StatusOK, Body: []byte(`{
		"id":1399,"name":"Game of Thrones","original_name":"Game of Thrones","original_language":"en","overview":"Kings compete.","homepage":"https://example.test/show","first_air_date":"2011-04-17","last_air_date":"2019-05-19","status":"Ended","type":"Scripted","number_of_episodes":73,"number_of_seasons":1,
		"networks":[{"id":49,"name":"HBO","origin_country":"US","logo_path":"/hbo.png"}],
		"production_companies":[{"id":76043,"name":"Revolution Sun Studios","origin_country":"US","logo_path":"/studio.png"}],
		"seasons":[{"id":0,"name":"Specials","season_number":0,"episode_count":299},{"id":3624,"name":"Season 1","air_date":"2011-04-17","season_number":1,"episode_count":10,"overview":"The first season.","poster_path":"/season.jpg"}],
		"content_ratings":{"results":[{"iso_3166_1":"US","rating":"TV-MA"}]},
		"videos":{"results":[{"key":"trailer","name":"Trailer","site":"YouTube","type":"Trailer","iso_639_1":"en","iso_3166_1":"US","official":true}]},
		"recommendations":{"results":[{"id":66732,"name":"Stranger Things","original_name":"Stranger Things","first_air_date":"2016-07-15","poster_path":"/recommendation.jpg","popularity":10}]},
		"translations":{"translations":[{"iso_639_1":"da","iso_3166_1":"DK","data":{"name":"Kampen om tronen","overview":"Konger kæmper."}}]}
	}`)}
	season := providers.Payload{ObservationID: "season-obs", ObservedAt: time.Unix(2, 0), StatusCode: http.StatusOK, Body: []byte(`{
		"id":3624,"name":"Season 1","season_number":1,"overview":"The first season.","poster_path":"/season.jpg",
		"images":{"posters":[{"file_path":"/season.jpg","iso_639_1":"en","width":1000,"height":1500,"vote_average":8},{"file_path":"/season-da.jpg","iso_639_1":"da","width":1000,"height":1500,"vote_average":7}]},
		"episodes":[{"id":63056,"name":"Winter Is Coming","overview":"The story begins.","air_date":"2011-04-17","still_path":"/still.jpg","episode_number":1,"season_number":1,"runtime":62,"vote_average":8.2,"vote_count":200}]
	}`)}
	record, err := NormalizeTMDBTV([]providers.Payload{detail, season}, "tv_show")
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Overviews) < 2 || len(record.Networks) != 1 || record.Networks[0].LogoURL == "" || len(record.Organizations) != 1 || record.Organizations[0].LogoURL == "" {
		t.Fatalf("show projection: %+v", record)
	}
	if len(record.Certifications) != 1 || len(record.Videos) != 1 || len(record.Recommendations) != 1 {
		t.Fatalf("show supplements: %+v", record)
	}
	if len(record.Seasons) != 1 || len(record.Seasons[0].ExternalIDs) != 1 || len(record.Seasons[0].Images) != 2 || record.Seasons[0].Images[1].Language != "da" || len(record.Seasons[0].Overviews) == 0 {
		t.Fatalf("season projection: %+v", record.Seasons)
	}
	if record.SeasonCount != 1 || record.Seasons[0].Number != 1 {
		t.Fatalf("season-zero shell leaked into canonical projection: %+v", record.Seasons)
	}
	if len(record.Episodes) != 1 || len(record.Episodes[0].ExternalIDs) != 1 || len(record.Episodes[0].Numbers) != 2 || len(record.Episodes[0].Ratings) != 1 || record.Episodes[0].Ratings[0].Votes != 200 || len(record.Episodes[0].Images) != 1 {
		t.Fatalf("episode projection: %+v", record.Episodes)
	}
}

func TestNormalizeTVDBSeriesRetainsSpecialsAbsoluteNumbersAndChildArtwork(t *testing.T) {
	payload := providers.Payload{ObservationID: "obs", ObservedAt: time.Unix(1, 0), StatusCode: http.StatusOK, Body: []byte(`{"data":{
		"id":121361,"name":"Game of Thrones","originalLanguage":"eng","originalCountry":"usa","firstAired":"2011-04-17","status":{"name":"Ended"},
		"companies":{"production":[{"id":1,"name":"HBO","country":"usa"}]},"contentRatings":[{"name":"TV-MA","country":"usa","contentType":"TV"}],
		"translations":{"nameTranslations":[{"language":"dan","name":"Kampen om tronen"}],"overviewTranslations":[{"language":"dan","overview":"Konger kæmper."}]},
		"seasons":[{"id":10,"number":0,"name":"Specials","image":"/specials.jpg","type":{"name":"Aired Order"}},{"id":11,"number":1,"name":"Season 1","image":"/season.jpg","type":{"name":"Aired Order"}}],
		"episodes":[{"id":20,"name":"Special","overview":"A special.","seasonNumber":0,"number":1,"absoluteNumber":0,"aired":"2010-01-01","runtime":10,"image":"/special.jpg"},{"id":21,"name":"Winter Is Coming","overview":"The story begins.","seasonNumber":1,"number":1,"absoluteNumber":1,"aired":"2011-04-17","runtime":62,"image":"/episode.jpg","translations":{"nameTranslations":[{"language":"dan","name":"Vinteren kommer"}],"overviewTranslations":[{"language":"dan","overview":"Historien begynder."}]}}]
	}}`)}
	record, err := NormalizeTVDBSeries(payload, "tv_show", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Seasons) != 2 || record.Seasons[0].Number != 0 || len(record.Seasons[0].Images) != 1 || len(record.Seasons[0].ExternalIDs) != 1 {
		t.Fatalf("seasons: %+v", record.Seasons)
	}
	if len(record.Episodes) != 2 || !record.Episodes[0].IsSpecial || record.Episodes[0].EpisodeType != "special" || len(record.Episodes[0].Images) != 1 {
		t.Fatalf("special: %+v", record.Episodes)
	}
	regular := record.Episodes[1]
	if len(regular.Numbers) != 3 || regular.Numbers[2].Scheme != "absolute" || len(regular.Titles) != 2 || len(regular.Overviews) != 2 || len(regular.ExternalIDs) != 1 {
		t.Fatalf("regular episode: %+v", regular)
	}
	if len(record.Organizations) != 1 || len(record.Certifications) != 1 || len(record.Overviews) != 1 {
		t.Fatalf("show supplements: %+v", record)
	}
}

func TestNormalizeTVDBSeriesRetainsYearNumberedSeason(t *testing.T) {
	payload := providers.Payload{ObservationID: "obs", ObservedAt: time.Unix(1, 0), StatusCode: http.StatusOK, Body: []byte(`{"data":{
		"id":73388,"name":"MythBusters","originalLanguage":"eng",
		"seasons":[{"id":2014,"number":2014,"name":"2014","type":{"name":"Aired Order"}}],
		"episodes":[{"id":5000001,"name":"Star Wars Special","seasonNumber":2014,"number":1,"aired":"2014-01-04"}]
	}}`)}
	record, err := NormalizeTVDBSeries(payload, "tv_show", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Seasons) != 1 || record.Seasons[0].Number != 2014 {
		t.Fatalf("seasons: %+v", record.Seasons)
	}
	if len(record.Episodes) != 1 {
		t.Fatalf("episodes: %+v", record.Episodes)
	}
	for _, number := range record.Episodes[0].Numbers {
		if number.Scheme == "tvdb" && number.Provider == "tvdb" && number.Season == 2014 && number.Number == 1 {
			return
		}
	}
	t.Fatalf("TVDB S2014E01 alias missing: %+v", record.Episodes[0].Numbers)
}

func TestNormalizeTVDBSeriesAddsEverySeasonArtworkClass(t *testing.T) {
	series := providers.Payload{ObservationID: "series", ObservedAt: time.Unix(1, 0), StatusCode: http.StatusOK, Body: []byte(`{"data":{"id":121361,"name":"Game of Thrones","seasons":[{"id":473271,"number":2,"name":"Season 2","image":"/primary.jpg","type":{"id":1,"name":"Aired Order"}},{"id":1713613,"number":2,"name":"DVD Season 2","type":{"id":2,"name":"DVD Order"}}]}}`)}
	season := providers.Payload{ObservationID: "season", ObservedAt: time.Unix(2, 0), StatusCode: http.StatusOK, Body: []byte(`{"data":{"id":473271,"number":2,"artwork":[{"id":1,"type":7,"image":"/poster.jpg","language":"eng","width":680,"height":1000,"score":10},{"id":2,"type":8,"image":"/background.jpg","width":1920,"height":1080},{"id":3,"type":6,"image":"/banner.jpg","width":758,"height":140},{"id":4,"type":10,"image":"/icon.png","width":1024,"height":1024}]}}`)}
	record, err := NormalizeTVDBSeries(series, "tv_show", nil, 0, season)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Seasons) != 1 || len(record.Seasons[0].Images) != 5 {
		t.Fatalf("seasons: %+v", record.Seasons)
	}
	classes := []string{"poster", "poster", "backdrop", "banner", "icon"}
	for i, class := range classes {
		if record.Seasons[0].Images[i].Class != class {
			t.Fatalf("image %d class = %q, want %q", i, record.Seasons[0].Images[i].Class, class)
		}
	}
}
