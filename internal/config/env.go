package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

// LoadEnvFiles loads local development configuration without overwriting
// variables already provided by the process environment. .env.local is loaded
// first so its values take precedence over .env.
func LoadEnvFiles() error {
	for _, path := range []string{".env.local", ".env"} {
		if err := godotenv.Load(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("load %s: %w", path, err)
		}
	}
	return nil
}
