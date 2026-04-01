package config

import (
	"fmt"
	"os"
)

type Config struct {
	ListenAddr  string
	APIKey      string
	Backend     string // "postgres" or "redis"
	PostgresDSN string
	RedisAddr   string
}

func Load() (*Config, error) {
	c := &Config{
		ListenAddr:  envOrDefault("QUEUEKIT_LISTEN_ADDR", ":8080"),
		APIKey:      os.Getenv("QUEUEKIT_API_KEY"),
		Backend:     envOrDefault("QUEUEKIT_BACKEND", "postgres"),
		PostgresDSN: os.Getenv("QUEUEKIT_POSTGRES_DSN"),
		RedisAddr:   envOrDefault("QUEUEKIT_REDIS_ADDR", "localhost:6379"),
	}

	switch c.Backend {
	case "postgres":
		if c.PostgresDSN == "" {
			return nil, fmt.Errorf("QUEUEKIT_POSTGRES_DSN is required when backend is postgres")
		}
	case "redis":
		// RedisAddr has a default, so no hard requirement
	default:
		return nil, fmt.Errorf("unsupported backend %q: must be postgres or redis", c.Backend)
	}

	return c, nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
