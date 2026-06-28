package infrastructure_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	domain "promo-scraper/internal/modules/scraping/promo_events/domain"
	"promo-scraper/internal/modules/scraping/promo_events/infrastructure"
)

const defaultTestDSN = "postgres://app:app@localhost:55432/app?sslmode=disable"

func setupTestDB(t *testing.T) (*pgxpool.Pool, func()) {
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

	truncate(t, pool)

	cleanup := func() {
		truncate(t, pool)
		pool.Close()
	}
	return pool, cleanup
}

func truncate(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := pool.Exec(ctx, "TRUNCATE promo_events, sources, stores RESTART IDENTITY CASCADE")
	require.NoError(t, err)
}

func insertStore(t *testing.T, pool *pgxpool.Pool, orgID uuid.UUID, host string) int64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var id int64
	err := pool.QueryRow(
		ctx,
		`INSERT INTO stores (org_id, nome, host) VALUES ($1, $2, $3) RETURNING id`,
		orgID, host, host,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func insertSource(t *testing.T, pool *pgxpool.Pool, orgID uuid.UUID, storeID int64, url string) int64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var id int64
	err := pool.QueryRow(
		ctx,
		`INSERT INTO sources (org_id, store_id, url, strategy, interval_seconds, selectors, enabled)
		 VALUES ($1, $2, $3, 'http', 60, $4::jsonb, true)
		 RETURNING id`,
		orgID, storeID, url,
		`{"preco":".preco","estoque":".estoque","badge":".badge"}`,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func newEvent(orgID uuid.UUID, sourceID, storeID int64, price string, detectedAt time.Time) domain.PromoEvent {
	return domain.PromoEvent{
		OrgID:             orgID,
		SourceID:          sourceID,
		StoreID:           storeID,
		SKU:               "SKU-TEST",
		Titulo:            "Produto Teste",
		Preco:             decimal.RequireFromString(price),
		PrecoAnterior:     nil,
		Moeda:             "BRL",
		EstoqueDisponivel: true,
		BadgePromo:        true,
		URL:               "https://loja.example.com/p/teste",
		DetectedAt:        detectedAt,
	}
}

func TestInsert_NewEvent_ReturnsInsertedTrue(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	repo := infrastructure.NewPGPromoEventRepository(pool)

	orgID := uuid.New()
	storeID := insertStore(t, pool, orgID, "loja-promo-a.example.com")
	sourceID := insertSource(t, pool, orgID, storeID, "https://loja-promo-a.example.com/p/1")

	ev := newEvent(orgID, sourceID, storeID, "90.00", time.Now().UTC())
	inserted, err := repo.Insert(ctx, ev)
	require.NoError(t, err)
	require.True(t, inserted, "evento novo deve ser inserido")

	var rowCount int
	require.NoError(t,
		pool.QueryRow(ctx, `SELECT COUNT(*) FROM promo_events WHERE org_id = $1`, orgID).Scan(&rowCount),
	)
	require.Equal(t, 1, rowCount)
}

func TestInsert_SameBucket_ReturnsInsertedFalse(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	repo := infrastructure.NewPGPromoEventRepository(pool)

	orgID := uuid.New()
	storeID := insertStore(t, pool, orgID, "loja-promo-b.example.com")
	sourceID := insertSource(t, pool, orgID, storeID, "https://loja-promo-b.example.com/p/2")

	baseTime := time.Date(2026, 6, 28, 12, 10, 0, 0, time.UTC)
	first := newEvent(orgID, sourceID, storeID, "199.99", baseTime)
	sameBucket := newEvent(orgID, sourceID, storeID, "199.99", baseTime.Add(15*time.Minute))

	inserted, err := repo.Insert(ctx, first)
	require.NoError(t, err)
	require.True(t, inserted)

	inserted, err = repo.Insert(ctx, sameBucket)
	require.NoError(t, err)
	require.False(t, inserted, "segundo evento no mesmo bucket deve ser dedupado (inserted=false)")

	var rowCount int
	require.NoError(t,
		pool.QueryRow(ctx, `SELECT COUNT(*) FROM promo_events WHERE org_id = $1`, orgID).Scan(&rowCount),
	)
	require.Equal(t, 1, rowCount)
}

func TestInsert_DifferentBucket_ReturnsInsertedTrue(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	repo := infrastructure.NewPGPromoEventRepository(pool)

	orgID := uuid.New()
	storeID := insertStore(t, pool, orgID, "loja-promo-c.example.com")
	sourceID := insertSource(t, pool, orgID, storeID, "https://loja-promo-c.example.com/p/3")

	baseTime := time.Date(2026, 6, 28, 12, 5, 0, 0, time.UTC)
	first := newEvent(orgID, sourceID, storeID, "49.90", baseTime)
	differentBucket := newEvent(orgID, sourceID, storeID, "49.90", baseTime.Add(31*time.Minute))

	inserted, err := repo.Insert(ctx, first)
	require.NoError(t, err)
	require.True(t, inserted)

	inserted, err = repo.Insert(ctx, differentBucket)
	require.NoError(t, err)
	require.True(t, inserted, "evento em bucket distinto deve ser inserido")

	var rowCount int
	require.NoError(t,
		pool.QueryRow(ctx, `SELECT COUNT(*) FROM promo_events WHERE org_id = $1`, orgID).Scan(&rowCount),
	)
	require.Equal(t, 2, rowCount)
}
