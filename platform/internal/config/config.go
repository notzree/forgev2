package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port       int
	TursoURL   string
	TursoToken string
}

func Load() *Config {
	port := 8080
	if p := os.Getenv("PORT"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil {
			port = parsed
		}
	}

	return &Config{
		Port:       port,
		TursoURL:   os.Getenv("TURSO_URL"),
		TursoToken: os.Getenv("TURSO_AUTH_TOKEN"),
	}
}
