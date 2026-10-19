// Package config читает настройки сервиса из переменных окружения.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Addr string

	ClickHouseAddr     string
	ClickHouseDatabase string
	ClickHouseUser     string
	ClickHousePassword string

	// OIDCIssuer — ожидаемый claim iss (адрес Keycloak, как его видит браузер).
	OIDCIssuer string
	// OIDCJWKSURL — откуда сервис скачивает ключи подписи (адрес Keycloak внутри docker-сети).
	OIDCJWKSURL  string
	AllowedAZP   []string
	RequiredRole string

	CORSAllowedOrigins []string

	DefaultPeriodDays int
	MaxPeriodDays     int
}

func Load() (Config, error) {
	cfg := Config{
		Addr:               env("ADDR", ":8000"),
		ClickHouseAddr:     env("CLICKHOUSE_ADDR", "localhost:9000"),
		ClickHouseDatabase: env("CLICKHOUSE_DATABASE", "reports"),
		ClickHouseUser:     env("CLICKHOUSE_USER", "reports_api"),
		ClickHousePassword: os.Getenv("CLICKHOUSE_PASSWORD"),
		OIDCIssuer:         os.Getenv("OIDC_ISSUER"),
		OIDCJWKSURL:        os.Getenv("OIDC_JWKS_URL"),
		AllowedAZP:         list(env("OIDC_ALLOWED_AZP", "reports-frontend")),
		RequiredRole:       env("REQUIRED_ROLE", "prothetic_user"),
		CORSAllowedOrigins: list(env("CORS_ALLOWED_ORIGINS", "http://localhost:3000")),
	}

	var err error
	if cfg.DefaultPeriodDays, err = intEnv("DEFAULT_PERIOD_DAYS", 30); err != nil {
		return Config{}, err
	}
	if cfg.MaxPeriodDays, err = intEnv("MAX_PERIOD_DAYS", 366); err != nil {
		return Config{}, err
	}

	if cfg.OIDCIssuer == "" || cfg.OIDCJWKSURL == "" {
		return Config{}, fmt.Errorf("OIDC_ISSUER и OIDC_JWKS_URL обязательны")
	}
	if len(cfg.AllowedAZP) == 0 {
		return Config{}, fmt.Errorf("OIDC_ALLOWED_AZP не может быть пустым")
	}
	return cfg, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func intEnv(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s: ожидается положительное целое, получено %q", key, v)
	}
	return n, nil
}

func list(v string) []string {
	var out []string
	for _, item := range strings.Split(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
