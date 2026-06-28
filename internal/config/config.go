package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultPortMetrics       = ":9090"
	DefaultSchedulerInterval = 5 * time.Second
	DefaultHTTPTimeout       = 10 * time.Second
	DefaultRateLimitPerMin   = 30
	DefaultRateLimitBurst    = 1
	DefaultMaxAttempts       = 5
	DefaultHeadlessTimeout   = 30 * time.Second
)

var ErrDatabaseURLRequired = errors.New("DATABASE_URL is required")

type Config struct {
	DatabaseURL       string
	PortMetrics       string
	SchedulerInterval time.Duration
	WorkerOrgIDs      []uuid.UUID
	LogLevel          slog.Level
	HTTPTimeout       time.Duration
	RateLimitPerMin   int
	RateLimitBurst    int
	MaxAttempts       int
	MeliAccessToken   string
	HeadlessEnabled   bool
	ChromePath        string
	HeadlessTimeout   time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		DatabaseURL:       strings.TrimSpace(os.Getenv("DATABASE_URL")),
		PortMetrics:       DefaultPortMetrics,
		SchedulerInterval: DefaultSchedulerInterval,
		LogLevel:          slog.LevelInfo,
		HTTPTimeout:       DefaultHTTPTimeout,
		RateLimitPerMin:   DefaultRateLimitPerMin,
		RateLimitBurst:    DefaultRateLimitBurst,
		MaxAttempts:       DefaultMaxAttempts,
		MeliAccessToken:   strings.TrimSpace(os.Getenv("MELI_ACCESS_TOKEN")),
		ChromePath:        strings.TrimSpace(os.Getenv("CHROME_PATH")),
		HeadlessTimeout:   DefaultHeadlessTimeout,
	}

	if v := strings.TrimSpace(os.Getenv("HEADLESS_ENABLED")); v != "" {
		cfg.HeadlessEnabled = v == "1" || strings.EqualFold(v, "true")
	}

	if v := strings.TrimSpace(os.Getenv("HEADLESS_TIMEOUT")); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("HEADLESS_TIMEOUT inválido %q: %w", v, err)
		}
		if d <= 0 {
			return Config{}, fmt.Errorf("HEADLESS_TIMEOUT deve ser positivo, recebido %q", v)
		}
		cfg.HeadlessTimeout = d
	}

	if cfg.DatabaseURL == "" {
		return Config{}, ErrDatabaseURLRequired
	}

	if v := strings.TrimSpace(os.Getenv("PORT_METRICS")); v != "" {
		cfg.PortMetrics = normalizePort(v)
	}

	if v := strings.TrimSpace(os.Getenv("SCHEDULER_INTERVAL")); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("SCHEDULER_INTERVAL inválido %q: %w", v, err)
		}
		if d <= 0 {
			return Config{}, fmt.Errorf("SCHEDULER_INTERVAL deve ser positivo, recebido %q", v)
		}
		cfg.SchedulerInterval = d
	}

	if v := strings.TrimSpace(os.Getenv("HTTP_TIMEOUT")); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("HTTP_TIMEOUT inválido %q: %w", v, err)
		}
		if d <= 0 {
			return Config{}, fmt.Errorf("HTTP_TIMEOUT deve ser positivo, recebido %q", v)
		}
		cfg.HTTPTimeout = d
	}

	if v := strings.TrimSpace(os.Getenv("RATE_LIMIT_PER_MIN")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("RATE_LIMIT_PER_MIN inválido %q: %w", v, err)
		}
		if n <= 0 {
			return Config{}, fmt.Errorf("RATE_LIMIT_PER_MIN deve ser positivo, recebido %d", n)
		}
		cfg.RateLimitPerMin = n
	}

	if v := strings.TrimSpace(os.Getenv("RATE_LIMIT_BURST")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("RATE_LIMIT_BURST inválido %q: %w", v, err)
		}
		if n <= 0 {
			return Config{}, fmt.Errorf("RATE_LIMIT_BURST deve ser positivo, recebido %d", n)
		}
		cfg.RateLimitBurst = n
	}

	if v := strings.TrimSpace(os.Getenv("MAX_ATTEMPTS")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("MAX_ATTEMPTS inválido %q: %w", v, err)
		}
		if n <= 0 {
			return Config{}, fmt.Errorf("MAX_ATTEMPTS deve ser positivo, recebido %d", n)
		}
		cfg.MaxAttempts = n
	}

	if v := strings.TrimSpace(os.Getenv("LOG_LEVEL")); v != "" {
		level, err := parseLogLevel(v)
		if err != nil {
			return Config{}, err
		}
		cfg.LogLevel = level
	}

	if v := strings.TrimSpace(os.Getenv("WORKER_ORG_IDS")); v != "" {
		ids, err := parseOrgIDs(v)
		if err != nil {
			return Config{}, err
		}
		cfg.WorkerOrgIDs = ids
	}

	return cfg, nil
}

func normalizePort(v string) string {
	if strings.HasPrefix(v, ":") {
		return v
	}
	if _, err := strconv.Atoi(v); err == nil {
		return ":" + v
	}
	return v
}

func parseLogLevel(v string) (slog.Level, error) {
	switch strings.ToLower(v) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("LOG_LEVEL inválido %q (use debug|info|warn|error)", v)
	}
}

func parseOrgIDs(v string) ([]uuid.UUID, error) {
	parts := strings.Split(v, ",")
	out := make([]uuid.UUID, 0, len(parts))
	for _, raw := range parts {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("WORKER_ORG_IDS contém UUID inválido %q: %w", raw, err)
		}
		out = append(out, id)
	}
	return out, nil
}
