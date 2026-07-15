package config

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Host         string
	Port         int
	WebRoot      string
	SiteURL      string
	LogLevel     string
	LogFormat    string
	DatabaseURL  string
	RedisURL     string
	S3           S3Config
	Worker       WorkerConfig
	Chromaprint  ChromaprintConfig
	Providers    ProvidersConfig
	Captcha      CaptchaConfig
	Connectivity ConnectivityConfig
}

// ConnectivityConfig defines which immediate reverse proxies may supply the
// caller address used by the outside-in connectivity probe. Header values are
// ignored unless request.RemoteAddr belongs to one of these networks.
type ConnectivityConfig struct {
	TrustedProxyCIDRs []string
	PublicIPEchoURL   string
}

// CaptchaConfig enables the self-hosted proof-of-work captcha on register/login
// when Secret is set. Left empty (the default) the captcha is disabled.
type CaptchaConfig struct {
	Secret string
}

type S3Config struct {
	Endpoint         string
	Region           string
	Bucket           string
	Prefix           string
	AccessKeyID      string
	SecretAccessKey  string
	PathStyle        bool
	AutoCreateBucket bool
}

type WorkerConfig struct {
	MaxWorkers      int
	ImageMaxWorkers int
}

type ChromaprintConfig struct {
	FPCalcPath    string
	MaxPerRelease int
}

type ProvidersConfig struct {
	TMDB        TMDBConfig
	OMDB        OMDBConfig
	TVDB        TVDBConfig
	Fanart      FanartConfig
	MusicBrainz MusicBrainzConfig
	Apple       AppleConfig
	Deezer      DeezerConfig
	Discogs     DiscogsConfig
	LastFM      LastFMConfig
	AniDB       AniDBConfig
	AnimeLists  AnimeListsConfig
	TheXEM      TheXEMConfig
	TVMaze      TVMazeConfig
	Wikidata    WikidataConfig
	OpenOpus    OpenOpusConfig
	LRCLIB      LRCLIBConfig
	OpenLibrary OpenLibraryConfig
	GoogleBooks GoogleBooksConfig
	CoverArt    CoverArtArchiveConfig
	AcoustID    AcoustIDConfig
	Kitsu       KitsuConfig
	MyAnimeList MyAnimeListConfig
}

type CoverArtArchiveConfig struct {
	BaseURL           string
	RequestsPerSecond float64
	UserAgent         string
}

type KitsuConfig struct {
	BaseURL           string
	RequestsPerSecond float64
}

type MyAnimeListConfig struct {
	BaseURL           string
	ClientID          string
	RequestsPerSecond float64
}

type AcoustIDConfig struct {
	APIKey            string
	BaseURL           string
	RequestsPerSecond float64
}

type OpenLibraryConfig struct {
	BaseURL           string
	CoversBaseURL     string
	RequestsPerSecond float64
	UserAgent         string
}

type GoogleBooksConfig struct {
	APIKey            string
	BaseURL           string
	RequestsPerSecond float64
}

type LRCLIBConfig struct {
	BaseURL           string
	RequestsPerSecond float64
	UserAgent         string
}

type AnimeListsConfig struct {
	URL       string
	UserAgent string
}

type TheXEMConfig struct {
	BaseURL           string
	RequestsPerSecond float64
	UserAgent         string
}

type WikidataConfig struct {
	BaseURL           string
	RequestsPerSecond float64
	UserAgent         string
}

type OpenOpusConfig struct {
	BaseURL           string
	RequestsPerSecond float64
}

type AniDBConfig struct {
	Client        string
	ClientVersion int
	BaseURL       string
	TitlesURL     string
	UserAgent     string
}

type TVMazeConfig struct {
	BaseURL           string
	RequestsPerSecond float64
}

type AppleConfig struct {
	BaseURL           string
	MusicBaseURL      string
	DeveloperToken    string
	Country           string
	RequestsPerSecond float64
}

type DeezerConfig struct {
	BaseURL           string
	RequestsPerSecond float64
}

type DiscogsConfig struct {
	APIKey            string
	BaseURL           string
	RequestsPerSecond float64
	UserAgent         string
}

type LastFMConfig struct {
	APIKey            string
	BaseURL           string
	RequestsPerSecond float64
}

type MusicBrainzConfig struct {
	BaseURL           string
	RequestsPerSecond float64
	UserAgent         string
}

type FanartConfig struct {
	APIKey  string
	BaseURL string
}

type TVDBConfig struct {
	APIKey  string
	BaseURL string
}

type OMDBConfig struct {
	APIKey  string
	BaseURL string
}

type TMDBConfig struct {
	Token    string
	BaseURL  string
	Language string
}

func Load() (Config, error) {
	port, err := envInt("HEYA_METADATA_PORT", 3030)
	if err != nil {
		return Config{}, err
	}
	if port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("HEYA_METADATA_PORT must be between 1 and 65535")
	}

	pathStyle, err := envBool("HEYA_METADATA_S3_PATH_STYLE", true)
	if err != nil {
		return Config{}, err
	}
	autoCreateBucket, err := envBool("HEYA_METADATA_S3_AUTO_CREATE_BUCKET", false)
	if err != nil {
		return Config{}, err
	}
	maxWorkers, err := envInt("HEYA_METADATA_WORKER_MAX_WORKERS", 8)
	if err != nil {
		return Config{}, err
	}
	if maxWorkers < 1 || maxWorkers > 1000 {
		return Config{}, fmt.Errorf("HEYA_METADATA_WORKER_MAX_WORKERS must be between 1 and 1000")
	}
	imageMaxWorkers, err := envInt("HEYA_METADATA_IMAGE_MAX_WORKERS", 12)
	if err != nil {
		return Config{}, err
	}
	if imageMaxWorkers < 1 || imageMaxWorkers > 100 {
		return Config{}, fmt.Errorf("HEYA_METADATA_IMAGE_MAX_WORKERS must be between 1 and 100")
	}
	musicBrainzRate, err := envFloat("HEYA_METADATA_MUSICBRAINZ_REQUESTS_PER_SECOND", 1)
	if err != nil {
		return Config{}, err
	}
	if musicBrainzRate <= 0 || musicBrainzRate > 1000 {
		return Config{}, fmt.Errorf("HEYA_METADATA_MUSICBRAINZ_REQUESTS_PER_SECOND must be greater than 0 and at most 1000")
	}
	appleRate, err := envFloat("HEYA_METADATA_APPLE_REQUESTS_PER_SECOND", 1.0/3.0)
	if err != nil {
		return Config{}, err
	}
	deezerRate, err := envFloat("HEYA_METADATA_DEEZER_REQUESTS_PER_SECOND", 5)
	if err != nil {
		return Config{}, err
	}
	discogsRate, err := envFloat("HEYA_METADATA_DISCOGS_REQUESTS_PER_SECOND", 1)
	if err != nil {
		return Config{}, err
	}
	lastFMRate, err := envFloat("HEYA_METADATA_LASTFM_REQUESTS_PER_SECOND", 5)
	if err != nil {
		return Config{}, err
	}
	anidbClientVersion, err := envInt("HEYA_METADATA_ANIDB_CLIENT_VERSION", 1)
	if err != nil {
		return Config{}, err
	}
	tvMazeRate, err := envFloat("HEYA_METADATA_TVMAZE_REQUESTS_PER_SECOND", 2)
	if err != nil {
		return Config{}, err
	}
	theXEMRate, err := envFloat("HEYA_METADATA_THEXEM_REQUESTS_PER_SECOND", 2)
	if err != nil {
		return Config{}, err
	}
	wikidataRate, err := envFloat("HEYA_METADATA_WIKIDATA_REQUESTS_PER_SECOND", 1)
	if err != nil {
		return Config{}, err
	}
	openOpusRate, err := envFloat("HEYA_METADATA_OPENOPUS_REQUESTS_PER_SECOND", 2)
	if err != nil {
		return Config{}, err
	}
	lrclibRate, err := envFloat("HEYA_METADATA_LRCLIB_REQUESTS_PER_SECOND", 2)
	if err != nil {
		return Config{}, err
	}
	openLibraryRate, err := envFloat("HEYA_METADATA_OPENLIBRARY_REQUESTS_PER_SECOND", 3)
	if err != nil {
		return Config{}, err
	}
	googleBooksRate, err := envFloat("HEYA_METADATA_GOOGLE_BOOKS_REQUESTS_PER_SECOND", 5)
	if err != nil {
		return Config{}, err
	}
	acoustIDRate, err := envFloat("HEYA_METADATA_ACOUSTID_REQUESTS_PER_SECOND", 3)
	if err != nil {
		return Config{}, err
	}
	kitsuRate, err := envFloat("HEYA_METADATA_KITSU_REQUESTS_PER_SECOND", 2)
	if err != nil {
		return Config{}, err
	}
	malRate, err := envFloat("HEYA_METADATA_MAL_REQUESTS_PER_SECOND", 2)
	if err != nil {
		return Config{}, err
	}
	chromaprintMax, err := envInt("HEYA_METADATA_CHROMAPRINT_MAX_PER_RELEASE", 100)
	if err != nil {
		return Config{}, err
	}

	config := Config{
		Host:        env("HEYA_METADATA_HOST", "0.0.0.0"),
		Port:        port,
		WebRoot:     env("HEYA_METADATA_WEB_ROOT", ""),
		SiteURL:     strings.TrimRight(env("HEYA_METADATA_SITE_URL", "https://heya.media"), "/"),
		LogLevel:    env("HEYA_METADATA_LOG_LEVEL", "info"),
		LogFormat:   env("HEYA_METADATA_LOG_FORMAT", "text"),
		DatabaseURL: env("HEYA_METADATA_DATABASE_URL", "postgres://heya_metadata:heya_metadata_dev@127.0.0.1:5441/heya_metadata?sslmode=disable"),
		RedisURL:    env("HEYA_METADATA_REDIS_URL", "redis://127.0.0.1:6380/0"),
		S3: S3Config{
			Endpoint:         env("HEYA_METADATA_S3_ENDPOINT", "https://s3-api.karbowiak.dk"),
			Region:           env("HEYA_METADATA_S3_REGION", "us-east-1"),
			Bucket:           env("HEYA_METADATA_S3_BUCKET", "heyamedia"),
			Prefix:           env("HEYA_METADATA_S3_PREFIX", "data"),
			AccessKeyID:      env("HEYA_METADATA_S3_ACCESS_KEY_ID", ""),
			SecretAccessKey:  env("HEYA_METADATA_S3_SECRET_ACCESS_KEY", ""),
			PathStyle:        pathStyle,
			AutoCreateBucket: autoCreateBucket,
		},
		Worker: WorkerConfig{
			MaxWorkers:      maxWorkers,
			ImageMaxWorkers: imageMaxWorkers,
		},
		Captcha: CaptchaConfig{Secret: env("HEYA_METADATA_CAPTCHA_SECRET", "")},
		Connectivity: ConnectivityConfig{
			TrustedProxyCIDRs: envList(
				"HEYA_METADATA_CONNECTIVITY_TRUSTED_PROXIES",
				"127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16",
			),
			PublicIPEchoURL: env("HEYA_METADATA_CONNECTIVITY_PUBLIC_IP_ECHO_URL", "https://api.ipify.org"),
		},
		Chromaprint: ChromaprintConfig{
			FPCalcPath: env("HEYA_METADATA_FPCALC_PATH", ""), MaxPerRelease: chromaprintMax,
		},
		Providers: ProvidersConfig{TMDB: TMDBConfig{
			Token:    env("HEYA_METADATA_TMDB_TOKEN", ""),
			BaseURL:  env("HEYA_METADATA_TMDB_BASE_URL", "https://api.themoviedb.org/3"),
			Language: env("HEYA_METADATA_TMDB_LANGUAGE", "en-US"),
		}, OMDB: OMDBConfig{
			APIKey:  env("HEYA_METADATA_OMDB_API_KEY", ""),
			BaseURL: env("HEYA_METADATA_OMDB_BASE_URL", "https://www.omdbapi.com/"),
		}, TVDB: TVDBConfig{
			APIKey:  env("HEYA_METADATA_TVDB_API_KEY", ""),
			BaseURL: env("HEYA_METADATA_TVDB_BASE_URL", "https://api4.thetvdb.com/v4"),
		}, Fanart: FanartConfig{
			APIKey:  env("HEYA_METADATA_FANART_API_KEY", ""),
			BaseURL: env("HEYA_METADATA_FANART_BASE_URL", "https://webservice.fanart.tv/v3.2"),
		}, MusicBrainz: MusicBrainzConfig{
			BaseURL:           env("HEYA_METADATA_MUSICBRAINZ_BASE_URL", "https://musicbrainz.org/ws/2"),
			RequestsPerSecond: musicBrainzRate,
			UserAgent:         env("HEYA_METADATA_MUSICBRAINZ_USER_AGENT", "HeyaMetadata/dev (https://github.com/HeyaMedia/HeyaMetadata)"),
		}, CoverArt: CoverArtArchiveConfig{
			BaseURL:           env("HEYA_METADATA_COVER_ART_ARCHIVE_BASE_URL", "https://coverartarchive.org"),
			RequestsPerSecond: musicBrainzRate,
			UserAgent:         env("HEYA_METADATA_MUSICBRAINZ_USER_AGENT", "HeyaMetadata/dev (https://github.com/HeyaMedia/HeyaMetadata)"),
		}, Apple: AppleConfig{
			BaseURL: env("HEYA_METADATA_APPLE_BASE_URL", "https://itunes.apple.com"), MusicBaseURL: env("HEYA_METADATA_APPLE_MUSIC_BASE_URL", "https://api.music.apple.com/v1"),
			DeveloperToken: env("HEYA_METADATA_APPLE_DEVELOPER_TOKEN", ""), Country: env("HEYA_METADATA_APPLE_COUNTRY", "US"), RequestsPerSecond: appleRate,
		}, Deezer: DeezerConfig{
			BaseURL: env("HEYA_METADATA_DEEZER_BASE_URL", "https://api.deezer.com"), RequestsPerSecond: deezerRate,
		}, Discogs: DiscogsConfig{
			APIKey: env("HEYA_METADATA_DISCOGS_API_KEY", ""), BaseURL: env("HEYA_METADATA_DISCOGS_BASE_URL", "https://api.discogs.com"),
			RequestsPerSecond: discogsRate, UserAgent: env("HEYA_METADATA_DISCOGS_USER_AGENT", "HeyaMetadata/dev +https://github.com/HeyaMedia/HeyaMetadata"),
		}, LastFM: LastFMConfig{
			APIKey: env("HEYA_METADATA_LASTFM_API_KEY", ""), BaseURL: env("HEYA_METADATA_LASTFM_BASE_URL", "https://ws.audioscrobbler.com/2.0/"), RequestsPerSecond: lastFMRate,
		}, AniDB: AniDBConfig{
			Client: env("HEYA_METADATA_ANIDB_CLIENT", ""), ClientVersion: anidbClientVersion, BaseURL: env("HEYA_METADATA_ANIDB_BASE_URL", "http://api.anidb.net:9001/httpapi"),
			TitlesURL: env("HEYA_METADATA_ANIDB_TITLES_URL", "https://anidb.net/api/anime-titles.xml.gz"),
			UserAgent: env("HEYA_METADATA_ANIDB_USER_AGENT", "heya-media/1.0 anidb-titles-sync"),
		}, AnimeLists: AnimeListsConfig{
			URL:       env("HEYA_METADATA_ANIME_LISTS_URL", "https://raw.githubusercontent.com/Fribb/anime-lists/master/anime-list-mini.json"),
			UserAgent: env("HEYA_METADATA_ANIME_LISTS_USER_AGENT", "heya-media/1.0 anime-lists-sync"),
		}, TheXEM: TheXEMConfig{
			BaseURL: env("HEYA_METADATA_THEXEM_BASE_URL", "https://thexem.info"), RequestsPerSecond: theXEMRate,
			UserAgent: env("HEYA_METADATA_THEXEM_USER_AGENT", "HeyaMetadata/dev (+https://github.com/HeyaMedia/HeyaMetadata)"),
		}, TVMaze: TVMazeConfig{
			BaseURL: env("HEYA_METADATA_TVMAZE_BASE_URL", "https://api.tvmaze.com"), RequestsPerSecond: tvMazeRate,
		}, Wikidata: WikidataConfig{
			BaseURL: env("HEYA_METADATA_WIKIDATA_BASE_URL", "https://www.wikidata.org"), RequestsPerSecond: wikidataRate,
			UserAgent: env("HEYA_METADATA_WIKIDATA_USER_AGENT", "HeyaMetadata/dev (https://github.com/HeyaMedia/HeyaMetadata)"),
		}, OpenOpus: OpenOpusConfig{
			BaseURL: env("HEYA_METADATA_OPENOPUS_BASE_URL", "https://api.openopus.org"), RequestsPerSecond: openOpusRate,
		}, LRCLIB: LRCLIBConfig{
			BaseURL: env("HEYA_METADATA_LRCLIB_BASE_URL", "https://lrclib.net"), RequestsPerSecond: lrclibRate,
			UserAgent: env("HEYA_METADATA_LRCLIB_USER_AGENT", "HeyaMetadata/dev (https://github.com/HeyaMedia/HeyaMetadata)"),
		}, OpenLibrary: OpenLibraryConfig{
			BaseURL: env("HEYA_METADATA_OPENLIBRARY_BASE_URL", "https://openlibrary.org"), CoversBaseURL: env("HEYA_METADATA_OPENLIBRARY_COVERS_BASE_URL", "https://covers.openlibrary.org"),
			RequestsPerSecond: openLibraryRate, UserAgent: env("HEYA_METADATA_OPENLIBRARY_USER_AGENT", "HeyaMetadata/dev (https://github.com/HeyaMedia/HeyaMetadata; metadata@heya.media)"),
		}, GoogleBooks: GoogleBooksConfig{
			APIKey: env("HEYA_METADATA_GOOGLE_BOOKS_API_KEY", ""), BaseURL: env("HEYA_METADATA_GOOGLE_BOOKS_BASE_URL", "https://www.googleapis.com/books/v1"), RequestsPerSecond: googleBooksRate,
		}, AcoustID: AcoustIDConfig{
			APIKey: env("HEYA_METADATA_ACOUSTID_API_KEY", ""), BaseURL: env("HEYA_METADATA_ACOUSTID_BASE_URL", "https://api.acoustid.org"), RequestsPerSecond: acoustIDRate,
		}, Kitsu: KitsuConfig{
			BaseURL: env("HEYA_METADATA_KITSU_BASE_URL", "https://kitsu.io/api/edge"), RequestsPerSecond: kitsuRate,
		}, MyAnimeList: MyAnimeListConfig{
			BaseURL: env("HEYA_METADATA_MAL_BASE_URL", "https://api.myanimelist.net/v2"), ClientID: env("HEYA_METADATA_MAL_CLIENT_ID", ""), RequestsPerSecond: malRate,
		}},
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c Config) Address() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) (int, error) {
	value, ok := os.LookupEnv(key)
	if !ok || value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return parsed, nil
}

func envBool(key string, fallback bool) (bool, error) {
	value, ok := os.LookupEnv(key)
	if !ok || value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", key, err)
	}
	return parsed, nil
}

func envList(key, fallback string) []string {
	value := env(key, fallback)
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func (c Config) Validate() error {
	for _, rawPrefix := range c.Connectivity.TrustedProxyCIDRs {
		if _, err := netip.ParsePrefix(rawPrefix); err != nil {
			return fmt.Errorf("HEYA_METADATA_CONNECTIVITY_TRUSTED_PROXIES contains invalid CIDR %q: %w", rawPrefix, err)
		}
	}
	if c.Connectivity.PublicIPEchoURL != "" {
		echoURL, err := url.Parse(c.Connectivity.PublicIPEchoURL)
		if err != nil || echoURL.Scheme != "https" || echoURL.Host == "" {
			return fmt.Errorf("HEYA_METADATA_CONNECTIVITY_PUBLIC_IP_ECHO_URL must be an absolute HTTPS URL")
		}
	}
	if _, err := url.ParseRequestURI(c.DatabaseURL); err != nil {
		return fmt.Errorf("HEYA_METADATA_DATABASE_URL is invalid: %w", err)
	}
	if c.SiteURL != "" {
		siteURL, err := url.Parse(c.SiteURL)
		if err != nil || (siteURL.Scheme != "http" && siteURL.Scheme != "https") || siteURL.Host == "" {
			return fmt.Errorf("HEYA_METADATA_SITE_URL must be an absolute HTTP(S) URL")
		}
	}
	redisURL, err := url.Parse(c.RedisURL)
	if err != nil || (redisURL.Scheme != "redis" && redisURL.Scheme != "rediss") || redisURL.Host == "" {
		return fmt.Errorf("HEYA_METADATA_REDIS_URL must be an absolute redis:// or rediss:// URL")
	}
	endpoint, err := url.Parse(c.S3.Endpoint)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" {
		return fmt.Errorf("HEYA_METADATA_S3_ENDPOINT must be an absolute HTTP(S) URL")
	}
	if c.S3.Region == "" || c.S3.Bucket == "" || c.S3.Prefix == "" {
		return fmt.Errorf("HEYA_METADATA_S3_REGION, HEYA_METADATA_S3_BUCKET, and HEYA_METADATA_S3_PREFIX are required")
	}
	if (c.S3.AccessKeyID == "") != (c.S3.SecretAccessKey == "") {
		return fmt.Errorf("HEYA_METADATA_S3_ACCESS_KEY_ID and HEYA_METADATA_S3_SECRET_ACCESS_KEY must be set together")
	}
	tmdbURL, err := url.Parse(c.Providers.TMDB.BaseURL)
	if err != nil || tmdbURL.Scheme != "https" || tmdbURL.Host == "" {
		return fmt.Errorf("HEYA_METADATA_TMDB_BASE_URL must be an absolute HTTPS URL")
	}
	if len(c.Providers.TMDB.Language) < 2 {
		return fmt.Errorf("HEYA_METADATA_TMDB_LANGUAGE must begin with an ISO 639-1 language code")
	}
	omdbURL, err := url.Parse(c.Providers.OMDB.BaseURL)
	if err != nil || omdbURL.Scheme != "https" || omdbURL.Host == "" {
		return fmt.Errorf("HEYA_METADATA_OMDB_BASE_URL must be an absolute HTTPS URL")
	}
	tvdbURL, err := url.Parse(c.Providers.TVDB.BaseURL)
	if err != nil || tvdbURL.Scheme != "https" || tvdbURL.Host == "" {
		return fmt.Errorf("HEYA_METADATA_TVDB_BASE_URL must be an absolute HTTPS URL")
	}
	fanartURL, err := url.Parse(c.Providers.Fanart.BaseURL)
	if err != nil || fanartURL.Scheme != "https" || fanartURL.Host == "" {
		return fmt.Errorf("HEYA_METADATA_FANART_BASE_URL must be an absolute HTTPS URL")
	}
	musicBrainzURL, err := url.Parse(c.Providers.MusicBrainz.BaseURL)
	if err != nil || musicBrainzURL.Scheme != "https" || musicBrainzURL.Host == "" {
		return fmt.Errorf("HEYA_METADATA_MUSICBRAINZ_BASE_URL must be an absolute HTTPS URL")
	}
	if strings.TrimSpace(c.Providers.MusicBrainz.UserAgent) == "" {
		return fmt.Errorf("HEYA_METADATA_MUSICBRAINZ_USER_AGENT must not be empty")
	}
	if c.Providers.MusicBrainz.RequestsPerSecond <= 0 || c.Providers.MusicBrainz.RequestsPerSecond > 1000 {
		return fmt.Errorf("HEYA_METADATA_MUSICBRAINZ_REQUESTS_PER_SECOND must be greater than 0 and at most 1000")
	}
	coverArtURL, err := url.Parse(c.Providers.CoverArt.BaseURL)
	if err != nil || coverArtURL.Scheme != "https" || coverArtURL.Host == "" {
		return fmt.Errorf("HEYA_METADATA_COVER_ART_ARCHIVE_BASE_URL must be an absolute HTTPS URL")
	}
	if c.Providers.CoverArt.RequestsPerSecond <= 0 || c.Providers.CoverArt.RequestsPerSecond > 1000 {
		return fmt.Errorf("Cover Art Archive requests per second must be greater than 0 and at most 1000")
	}
	for name, rawURL := range map[string]string{
		"HEYA_METADATA_APPLE_BASE_URL":       c.Providers.Apple.BaseURL,
		"HEYA_METADATA_APPLE_MUSIC_BASE_URL": c.Providers.Apple.MusicBaseURL,
		"HEYA_METADATA_DEEZER_BASE_URL":      c.Providers.Deezer.BaseURL,
		"HEYA_METADATA_DISCOGS_BASE_URL":     c.Providers.Discogs.BaseURL,
		"HEYA_METADATA_LASTFM_BASE_URL":      c.Providers.LastFM.BaseURL,
	} {
		parsed, parseErr := url.Parse(rawURL)
		if parseErr != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return fmt.Errorf("%s must be an absolute HTTPS URL", name)
		}
	}
	if len(strings.TrimSpace(c.Providers.Apple.Country)) != 2 {
		return fmt.Errorf("HEYA_METADATA_APPLE_COUNTRY must be a two-letter storefront code")
	}
	for name, rate := range map[string]float64{
		"HEYA_METADATA_APPLE_REQUESTS_PER_SECOND":   c.Providers.Apple.RequestsPerSecond,
		"HEYA_METADATA_DEEZER_REQUESTS_PER_SECOND":  c.Providers.Deezer.RequestsPerSecond,
		"HEYA_METADATA_DISCOGS_REQUESTS_PER_SECOND": c.Providers.Discogs.RequestsPerSecond,
		"HEYA_METADATA_LASTFM_REQUESTS_PER_SECOND":  c.Providers.LastFM.RequestsPerSecond,
	} {
		if rate <= 0 || rate > 1000 {
			return fmt.Errorf("%s must be greater than 0 and at most 1000", name)
		}
	}
	if strings.TrimSpace(c.Providers.Discogs.UserAgent) == "" {
		return fmt.Errorf("HEYA_METADATA_DISCOGS_USER_AGENT must not be empty")
	}
	anidbURL, err := url.Parse(c.Providers.AniDB.BaseURL)
	if err != nil || anidbURL.Scheme != "http" || anidbURL.Hostname() != "api.anidb.net" || anidbURL.Port() != "9001" {
		return fmt.Errorf("HEYA_METADATA_ANIDB_BASE_URL must be AniDB's official HTTP API endpoint")
	}
	if c.Providers.AniDB.ClientVersion < 1 {
		return fmt.Errorf("HEYA_METADATA_ANIDB_CLIENT_VERSION must be positive")
	}
	anidbTitlesURL, err := url.Parse(c.Providers.AniDB.TitlesURL)
	if err != nil || anidbTitlesURL.Scheme != "https" || anidbTitlesURL.Hostname() != "anidb.net" || anidbTitlesURL.Path != "/api/anime-titles.xml.gz" {
		return fmt.Errorf("HEYA_METADATA_ANIDB_TITLES_URL must be AniDB's official HTTPS title dump")
	}
	if strings.TrimSpace(c.Providers.AniDB.UserAgent) == "" {
		return fmt.Errorf("HEYA_METADATA_ANIDB_USER_AGENT is required")
	}
	animeListsURL, err := url.Parse(c.Providers.AnimeLists.URL)
	if err != nil || animeListsURL.Scheme != "https" || animeListsURL.Host == "" {
		return fmt.Errorf("HEYA_METADATA_ANIME_LISTS_URL must be an absolute HTTPS URL")
	}
	if strings.TrimSpace(c.Providers.AnimeLists.UserAgent) == "" {
		return fmt.Errorf("HEYA_METADATA_ANIME_LISTS_USER_AGENT is required")
	}
	theXEMURL, err := url.Parse(c.Providers.TheXEM.BaseURL)
	if err != nil || theXEMURL.Scheme != "https" || theXEMURL.Host == "" {
		return fmt.Errorf("HEYA_METADATA_THEXEM_BASE_URL must be an absolute HTTPS URL")
	}
	if strings.TrimSpace(c.Providers.TheXEM.UserAgent) == "" {
		return fmt.Errorf("HEYA_METADATA_THEXEM_USER_AGENT is required")
	}
	if c.Providers.TheXEM.RequestsPerSecond <= 0 || c.Providers.TheXEM.RequestsPerSecond > 1000 {
		return fmt.Errorf("HEYA_METADATA_THEXEM_REQUESTS_PER_SECOND must be greater than 0 and at most 1000")
	}
	tvMazeURL, err := url.Parse(c.Providers.TVMaze.BaseURL)
	if err != nil || tvMazeURL.Scheme != "https" || tvMazeURL.Host == "" {
		return fmt.Errorf("HEYA_METADATA_TVMAZE_BASE_URL must be an absolute HTTPS URL")
	}
	if c.Providers.TVMaze.RequestsPerSecond <= 0 || c.Providers.TVMaze.RequestsPerSecond > 1000 {
		return fmt.Errorf("HEYA_METADATA_TVMAZE_REQUESTS_PER_SECOND must be greater than 0 and at most 1000")
	}
	for name, rawURL := range map[string]string{
		"HEYA_METADATA_WIKIDATA_BASE_URL":           c.Providers.Wikidata.BaseURL,
		"HEYA_METADATA_OPENOPUS_BASE_URL":           c.Providers.OpenOpus.BaseURL,
		"HEYA_METADATA_LRCLIB_BASE_URL":             c.Providers.LRCLIB.BaseURL,
		"HEYA_METADATA_OPENLIBRARY_BASE_URL":        c.Providers.OpenLibrary.BaseURL,
		"HEYA_METADATA_OPENLIBRARY_COVERS_BASE_URL": c.Providers.OpenLibrary.CoversBaseURL,
		"HEYA_METADATA_GOOGLE_BOOKS_BASE_URL":       c.Providers.GoogleBooks.BaseURL,
		"HEYA_METADATA_ACOUSTID_BASE_URL":           c.Providers.AcoustID.BaseURL,
		"HEYA_METADATA_KITSU_BASE_URL":              c.Providers.Kitsu.BaseURL,
		"HEYA_METADATA_MAL_BASE_URL":                c.Providers.MyAnimeList.BaseURL,
	} {
		parsed, parseErr := url.Parse(rawURL)
		if parseErr != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return fmt.Errorf("%s must be an absolute HTTPS URL", name)
		}
	}
	if strings.TrimSpace(c.Providers.Wikidata.UserAgent) == "" {
		return fmt.Errorf("HEYA_METADATA_WIKIDATA_USER_AGENT must not be empty")
	}
	if strings.TrimSpace(c.Providers.LRCLIB.UserAgent) == "" {
		return fmt.Errorf("HEYA_METADATA_LRCLIB_USER_AGENT must not be empty")
	}
	if strings.TrimSpace(c.Providers.OpenLibrary.UserAgent) == "" {
		return fmt.Errorf("HEYA_METADATA_OPENLIBRARY_USER_AGENT must not be empty")
	}
	for name, rate := range map[string]float64{
		"HEYA_METADATA_WIKIDATA_REQUESTS_PER_SECOND":     c.Providers.Wikidata.RequestsPerSecond,
		"HEYA_METADATA_OPENOPUS_REQUESTS_PER_SECOND":     c.Providers.OpenOpus.RequestsPerSecond,
		"HEYA_METADATA_LRCLIB_REQUESTS_PER_SECOND":       c.Providers.LRCLIB.RequestsPerSecond,
		"HEYA_METADATA_OPENLIBRARY_REQUESTS_PER_SECOND":  c.Providers.OpenLibrary.RequestsPerSecond,
		"HEYA_METADATA_GOOGLE_BOOKS_REQUESTS_PER_SECOND": c.Providers.GoogleBooks.RequestsPerSecond,
		"HEYA_METADATA_ACOUSTID_REQUESTS_PER_SECOND":     c.Providers.AcoustID.RequestsPerSecond,
		"HEYA_METADATA_KITSU_REQUESTS_PER_SECOND":        c.Providers.Kitsu.RequestsPerSecond,
		"HEYA_METADATA_MAL_REQUESTS_PER_SECOND":          c.Providers.MyAnimeList.RequestsPerSecond,
	} {
		if rate <= 0 || rate > 1000 {
			return fmt.Errorf("%s must be greater than 0 and at most 1000", name)
		}
	}
	if c.Chromaprint.MaxPerRelease < 0 || c.Chromaprint.MaxPerRelease > 1000 {
		return fmt.Errorf("HEYA_METADATA_CHROMAPRINT_MAX_PER_RELEASE must be between 0 and 1000")
	}
	return nil
}

func envFloat(key string, fallback float64) (float64, error) {
	value, ok := os.LookupEnv(key)
	if !ok || value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number: %w", key, err)
	}
	return parsed, nil
}
