package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
)

type Config struct {
	Host        string
	Port        int
	LogLevel    string
	LogFormat   string
	DatabaseURL string
	RedisURL    string
	S3          S3Config
	Worker      WorkerConfig
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
	MaxWorkers int
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

	config := Config{
		Host:        env("HEYA_METADATA_HOST", "0.0.0.0"),
		Port:        port,
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
		Worker: WorkerConfig{MaxWorkers: maxWorkers},
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

func (c Config) Validate() error {
	if _, err := url.ParseRequestURI(c.DatabaseURL); err != nil {
		return fmt.Errorf("HEYA_METADATA_DATABASE_URL is invalid: %w", err)
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
	return nil
}
