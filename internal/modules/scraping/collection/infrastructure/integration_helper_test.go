package infrastructure_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const defaultTestDSN = "postgres://app:app@localhost:55432/app?sslmode=disable"

func setupIntegrationDB(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = defaultTestDSN
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("DATABASE_URL inválido ou Postgres indisponível: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Postgres não respondeu ping: %v", err)
	}

	truncateAll(t, pool)

	cleanup := func() {
		truncateAll(t, pool)
		pool.Close()
	}
	return pool, cleanup
}

func truncateAll(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := pool.Exec(ctx, "TRUNCATE river_job, promo_events, sources, stores RESTART IDENTITY CASCADE")
	require.NoError(t, err)
}

func insertStoreRow(t *testing.T, pool *pgxpool.Pool, orgID uuid.UUID, nome, host string) int64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var id int64
	err := pool.QueryRow(
		ctx,
		`INSERT INTO stores (org_id, nome, host) VALUES ($1, $2, $3) RETURNING id`,
		orgID, nome, host,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

type sourceFixture struct {
	OrgID           uuid.UUID
	StoreID         int64
	URL             string
	Selectors       string
	Enabled         bool
	IntervalSeconds int
	LastCollectedAt *time.Time
	LastSnapshot    *string
}

func insertSourceRow(t *testing.T, pool *pgxpool.Pool, fx sourceFixture) int64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	selectors := fx.Selectors
	if selectors == "" {
		selectors = `{"preco":".preco"}`
	}
	interval := fx.IntervalSeconds
	if interval == 0 {
		interval = 60
	}

	var id int64
	err := pool.QueryRow(
		ctx,
		`INSERT INTO sources
		   (org_id, store_id, url, strategy, interval_seconds, selectors, enabled, last_collected_at, last_snapshot)
		 VALUES ($1, $2, $3, 'http', $4, $5::jsonb, $6, $7, $8::jsonb)
		 RETURNING id`,
		fx.OrgID, fx.StoreID, fx.URL, interval, selectors, fx.Enabled, fx.LastCollectedAt, fx.LastSnapshot,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func countRiverJobs(t *testing.T, pool *pgxpool.Pool, kind string) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var n int
	err := pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind = $1`, kind).Scan(&n)
	require.NoError(t, err)
	return n
}
