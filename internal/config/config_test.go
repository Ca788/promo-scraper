package config_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"promo-scraper/internal/config"
)

func clearEnv(t *testing.T) {
	t.Helper()
	envs := []string{
		"DATABASE_URL",
		"PORT_METRICS",
		"SCHEDULER_INTERVAL",
		"WORKER_ORG_IDS",
		"LOG_LEVEL",
		"HTTP_TIMEOUT",
		"RATE_LIMIT_PER_MIN",
		"RATE_LIMIT_BURST",
		"MAX_ATTEMPTS",
	}
	for _, name := range envs {
		t.Setenv(name, "")
	}
}

func TestLoad_FailsWhenDatabaseURLMissing(t *testing.T) {
	clearEnv(t)

	_, err := config.Load()
	require.ErrorIs(t, err, config.ErrDatabaseURLRequired)
}

func TestLoad_AppliesDefaults(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/app")

	cfg, err := config.Load()
	require.NoError(t, err)
	require.Equal(t, "postgres://user:pass@localhost:5432/app", cfg.DatabaseURL)
	require.Equal(t, config.DefaultPortMetrics, cfg.PortMetrics)
	require.Equal(t, config.DefaultSchedulerInterval, cfg.SchedulerInterval)
	require.Equal(t, slog.LevelInfo, cfg.LogLevel)
	require.Equal(t, config.DefaultHTTPTimeout, cfg.HTTPTimeout)
	require.Equal(t, config.DefaultRateLimitPerMin, cfg.RateLimitPerMin)
	require.Equal(t, config.DefaultRateLimitBurst, cfg.RateLimitBurst)
	require.Equal(t, config.DefaultMaxAttempts, cfg.MaxAttempts)
	require.Empty(t, cfg.WorkerOrgIDs)
}

func TestLoad_ParsesSchedulerInterval(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://x/y")
	t.Setenv("SCHEDULER_INTERVAL", "15s")

	cfg, err := config.Load()
	require.NoError(t, err)
	require.Equal(t, 15*time.Second, cfg.SchedulerInterval)
}

func TestLoad_ParsesWorkerOrgIDsCSV(t *testing.T) {
	clearEnv(t)
	id1 := uuid.New()
	id2 := uuid.New()
	t.Setenv("DATABASE_URL", "postgres://x/y")
	t.Setenv("WORKER_ORG_IDS", id1.String()+", "+id2.String())

	cfg, err := config.Load()
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{id1, id2}, cfg.WorkerOrgIDs)
}

func TestLoad_RejectsInvalidSchedulerInterval(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://x/y")
	t.Setenv("SCHEDULER_INTERVAL", "abc")

	_, err := config.Load()
	require.Error(t, err)
}

func TestLoad_RejectsInvalidWorkerOrgID(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://x/y")
	t.Setenv("WORKER_ORG_IDS", "not-a-uuid")

	_, err := config.Load()
	require.Error(t, err)
}

func TestLoad_NormalizesPortMetrics(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://x/y")
	t.Setenv("PORT_METRICS", "8080")

	cfg, err := config.Load()
	require.NoError(t, err)
	require.Equal(t, ":8080", cfg.PortMetrics)
}

func TestLoad_ParsesLogLevel(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://x/y")
	t.Setenv("LOG_LEVEL", "debug")

	cfg, err := config.Load()
	require.NoError(t, err)
	require.Equal(t, slog.LevelDebug, cfg.LogLevel)
}
