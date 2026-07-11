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
		"Redis": func(config *Config) { config.RedisURL = "http://127.0.0.1:6380" },
		"S3":    func(config *Config) { config.S3.Endpoint = "s3.karbowiak.dk" },
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
		Providers: ProvidersConfig{TMDB: TMDBConfig{
			BaseURL: "https://api.themoviedb.org/3", Language: "en-US",
		}},
	}
}
