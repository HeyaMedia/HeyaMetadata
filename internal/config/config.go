package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
)

type Config struct {
	Host      string
	Port      int
	LogLevel  string
	LogFormat string
}

func Load() (Config, error) {
	port, err := envInt("HEYA_METADATA_PORT", 3030)
	if err != nil {
		return Config{}, err
	}
	if port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("HEYA_METADATA_PORT must be between 1 and 65535")
	}

	return Config{
		Host:      env("HEYA_METADATA_HOST", "0.0.0.0"),
		Port:      port,
		LogLevel:  env("HEYA_METADATA_LOG_LEVEL", "info"),
		LogFormat: env("HEYA_METADATA_LOG_FORMAT", "text"),
	}, nil
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
