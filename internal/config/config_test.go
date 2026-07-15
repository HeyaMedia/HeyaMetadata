package config

import "testing"

func TestConfigValidateAcceptsDevelopmentDefaults(t *testing.T) {
	t.Parallel()
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("valid development configuration: %v", err)
	}
}

func TestConfigValidateRequiresS3CredentialPair(t *testing.T) {
	t.Parallel()
	config := validConfig()
	config.S3.AccessKeyID = "access-key"
	if err := config.Validate(); err == nil {
		t.Fatal("expected partial S3 credentials to be rejected")
	}
}

func TestConfigValidateRejectsInvalidDependencyURLs(t *testing.T) {
	t.Parallel()
	tests := map[string]func(*Config){
		"Redis":        func(config *Config) { config.RedisURL = "http://127.0.0.1:6380" },
		"S3":           func(config *Config) { config.S3.Endpoint = "s3.karbowiak.dk" },
		"OMDb":         func(config *Config) { config.Providers.OMDB.BaseURL = "omdbapi.com" },
		"TVDB":         func(config *Config) { config.Providers.TVDB.BaseURL = "api4.thetvdb.com" },
		"Fanart":       func(config *Config) { config.Providers.Fanart.BaseURL = "webservice.fanart.tv" },
		"MusicBrainz":  func(config *Config) { config.Providers.MusicBrainz.BaseURL = "musicbrainz.org/ws/2" },
		"Apple":        func(config *Config) { config.Providers.Apple.BaseURL = "itunes.apple.com" },
		"Deezer":       func(config *Config) { config.Providers.Deezer.BaseURL = "api.deezer.com" },
		"Discogs":      func(config *Config) { config.Providers.Discogs.BaseURL = "api.discogs.com" },
		"LastFM":       func(config *Config) { config.Providers.LastFM.BaseURL = "ws.audioscrobbler.com" },
		"AniDB":        func(config *Config) { config.Providers.AniDB.BaseURL = "https://api.anidb.net/httpapi" },
		"AniDB titles": func(config *Config) { config.Providers.AniDB.TitlesURL = "https://example.com/anime-titles.xml.gz" },
		"Anime lists":  func(config *Config) { config.Providers.AnimeLists.URL = "raw.githubusercontent.com/list.json" },
		"TheXEM":       func(config *Config) { config.Providers.TheXEM.BaseURL = "thexem.info" },
		"TVMaze":       func(config *Config) { config.Providers.TVMaze.BaseURL = "api.tvmaze.com" },
		"Wikidata":     func(config *Config) { config.Providers.Wikidata.BaseURL = "wikidata.org" },
		"OpenOpus":     func(config *Config) { config.Providers.OpenOpus.BaseURL = "api.openopus.org" },
		"LRCLIB":       func(config *Config) { config.Providers.LRCLIB.BaseURL = "lrclib.net" },
		"OpenLibrary":  func(config *Config) { config.Providers.OpenLibrary.BaseURL = "openlibrary.org" },
		"Google Books": func(config *Config) { config.Providers.GoogleBooks.BaseURL = "googleapis.com/books/v1" },
		"AcoustID":     func(config *Config) { config.Providers.AcoustID.BaseURL = "api.acoustid.org" },
		"Kitsu":        func(config *Config) { config.Providers.Kitsu.BaseURL = "kitsu.io/api/edge" },
		"MyAnimeList":  func(config *Config) { config.Providers.MyAnimeList.BaseURL = "api.myanimelist.net/v2" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			config := validConfig()
			mutate(&config)
			if err := config.Validate(); err == nil {
				t.Fatal("expected invalid dependency URL to be rejected")
			}
		})
	}
}

func TestConfigValidateRejectsInvalidConnectivityProxyCIDR(t *testing.T) {
	t.Parallel()
	config := validConfig()
	config.Connectivity.TrustedProxyCIDRs = []string{"not-a-network"}
	if err := config.Validate(); err == nil {
		t.Fatal("expected invalid trusted proxy CIDR to be rejected")
	}
}

func TestConfigValidateRejectsInsecureConnectivityIPEchoURL(t *testing.T) {
	t.Parallel()
	config := validConfig()
	config.Connectivity.PublicIPEchoURL = "http://api.ipify.org"
	if err := config.Validate(); err == nil {
		t.Fatal("expected insecure public IP echo URL to be rejected")
	}
}

func TestConfigValidateRejectsInvalidMusicBrainzPolicy(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*Config){
		"rate":       func(config *Config) { config.Providers.MusicBrainz.RequestsPerSecond = 0 },
		"user_agent": func(config *Config) { config.Providers.MusicBrainz.UserAgent = " " },
	} {
		t.Run(name, func(t *testing.T) {
			config := validConfig()
			mutate(&config)
			if err := config.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func validConfig() Config {
	return Config{
		DatabaseURL: "postgres://heya_metadata:password@127.0.0.1:5441/heya_metadata",
		RedisURL:    "redis://127.0.0.1:6380/0",
		S3: S3Config{
			Endpoint: "https://s3-api.karbowiak.dk",
			Region:   "us-east-1",
			Bucket:   "heyamedia",
			Prefix:   "data",
		},
		Providers: ProvidersConfig{
			TMDB:        TMDBConfig{BaseURL: "https://api.themoviedb.org/3", Language: "en-US"},
			OMDB:        OMDBConfig{BaseURL: "https://www.omdbapi.com/"},
			TVDB:        TVDBConfig{BaseURL: "https://api4.thetvdb.com/v4"},
			Fanart:      FanartConfig{BaseURL: "https://webservice.fanart.tv/v3.2"},
			MusicBrainz: MusicBrainzConfig{BaseURL: "https://musicbrainz.org/ws/2", RequestsPerSecond: 1, UserAgent: "HeyaMetadata/test (test@example.com)"},
			CoverArt:    CoverArtArchiveConfig{BaseURL: "https://coverartarchive.org", RequestsPerSecond: 1, UserAgent: "HeyaMetadata/test (test@example.com)"},
			Apple:       AppleConfig{BaseURL: "https://itunes.apple.com", MusicBaseURL: "https://api.music.apple.com/v1", Country: "US", RequestsPerSecond: 1},
			Deezer:      DeezerConfig{BaseURL: "https://api.deezer.com", RequestsPerSecond: 1},
			Discogs:     DiscogsConfig{BaseURL: "https://api.discogs.com", RequestsPerSecond: 1, UserAgent: "HeyaMetadata/test"},
			LastFM:      LastFMConfig{BaseURL: "https://ws.audioscrobbler.com/2.0/", RequestsPerSecond: 1},
			AniDB:       AniDBConfig{BaseURL: "http://api.anidb.net:9001/httpapi", TitlesURL: "https://anidb.net/api/anime-titles.xml.gz", UserAgent: "heya-media/1.0 anidb-titles-sync", ClientVersion: 1},
			AnimeLists:  AnimeListsConfig{URL: "https://raw.githubusercontent.com/Fribb/anime-lists/master/anime-list-mini.json", UserAgent: "heya-media/1.0 anime-lists-sync"},
			TheXEM:      TheXEMConfig{BaseURL: "https://thexem.info", RequestsPerSecond: 2, UserAgent: "HeyaMetadata/test"},
			TVMaze:      TVMazeConfig{BaseURL: "https://api.tvmaze.com", RequestsPerSecond: 2},
			Wikidata:    WikidataConfig{BaseURL: "https://www.wikidata.org", RequestsPerSecond: 1, UserAgent: "HeyaMetadata/test"},
			OpenOpus:    OpenOpusConfig{BaseURL: "https://api.openopus.org", RequestsPerSecond: 2},
			LRCLIB:      LRCLIBConfig{BaseURL: "https://lrclib.net", RequestsPerSecond: 2, UserAgent: "HeyaMetadata/test"},
			OpenLibrary: OpenLibraryConfig{BaseURL: "https://openlibrary.org", CoversBaseURL: "https://covers.openlibrary.org", RequestsPerSecond: 3, UserAgent: "HeyaMetadata/test (test@example.com)"},
			GoogleBooks: GoogleBooksConfig{BaseURL: "https://www.googleapis.com/books/v1", RequestsPerSecond: 5},
			AcoustID:    AcoustIDConfig{BaseURL: "https://api.acoustid.org", RequestsPerSecond: 3},
			Kitsu:       KitsuConfig{BaseURL: "https://kitsu.io/api/edge", RequestsPerSecond: 2},
			MyAnimeList: MyAnimeListConfig{BaseURL: "https://api.myanimelist.net/v2", RequestsPerSecond: 2},
		},
		Chromaprint: ChromaprintConfig{MaxPerRelease: 100},
		Connectivity: ConnectivityConfig{
			PublicIPEchoURL: "https://api.ipify.org",
		},
	}
}
