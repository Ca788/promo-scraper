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

	"promo-scraper/internal/modules/scraping/sources/domain"
	"promo-scraper/internal/modules/scraping/sources/infrastructure"
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
	_, err := pool.Exec(ctx, "TRUNCATE sources, stores RESTART IDENTITY CASCADE")
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

type insertSourceArgs struct {
	OrgID           uuid.UUID
	StoreID         int64
	URL             string
	Enabled         bool
	IntervalSeconds int32
	LastCollectedAt *time.Time
}

func insertSource(t *testing.T, pool *pgxpool.Pool, args insertSourceArgs) int64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var id int64
	err := pool.QueryRow(
		ctx,
		`INSERT INTO sources (org_id, store_id, url, strategy, interval_seconds, selectors, enabled, last_collected_at)
		 VALUES ($1, $2, $3, 'http', $4, $5::jsonb, $6, $7)
		 RETURNING id`,
		args.OrgID,
		args.StoreID,
		args.URL,
		args.IntervalSeconds,
		`{"preco":".preco","estoque":".estoque","badge":".badge"}`,
		args.Enabled,
		args.LastCollectedAt,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

func TestGetEligibleSources_ReturnsOnlyEligible(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	repo := infrastructure.NewPGSourceRepository(pool)

	orgID := uuid.New()
	otherOrgID := uuid.New()
	storeID := insertStore(t, pool, orgID, "loja-a.example.com")
	otherStoreID := insertStore(t, pool, otherOrgID, "loja-a.example.com")

	now := time.Now().UTC()
	tenMinAgo := now.Add(-10 * time.Minute)
	thirtySecAgo := now.Add(-30 * time.Second)

	eligibleID := insertSource(t, pool, insertSourceArgs{
		OrgID:           orgID,
		StoreID:         storeID,
		URL:             "https://loja-a.example.com/p/eligivel",
		Enabled:         true,
		IntervalSeconds: 60,
		LastCollectedAt: &tenMinAgo,
	})

	insertSource(t, pool, insertSourceArgs{
		OrgID:           orgID,
		StoreID:         storeID,
		URL:             "https://loja-a.example.com/p/desabilitada",
		Enabled:         false,
		IntervalSeconds: 60,
		LastCollectedAt: &tenMinAgo,
	})

	insertSource(t, pool, insertSourceArgs{
		OrgID:           orgID,
		StoreID:         storeID,
		URL:             "https://loja-a.example.com/p/dentro-do-intervalo",
		Enabled:         true,
		IntervalSeconds: 60,
		LastCollectedAt: &thirtySecAgo,
	})

	insertSource(t, pool, insertSourceArgs{
		OrgID:           otherOrgID,
		StoreID:         otherStoreID,
		URL:             "https://loja-a.example.com/p/outro-tenant",
		Enabled:         true,
		IntervalSeconds: 60,
		LastCollectedAt: &tenMinAgo,
	})

	got, err := repo.GetEligible(ctx, orgID, 10)
	require.NoError(t, err)
	require.Len(t, got, 1, "apenas a fonte habilitada + fora do intervalo + mesmo org deve voltar")
	require.Equal(t, eligibleID, got[0].ID)
	require.Equal(t, orgID, got[0].OrgID)
	require.Equal(t, domain.StrategyHTTP, got[0].Strategy)
	require.True(t, got[0].Enabled)
}

func TestGetEligibleSources_NeverCollected(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	repo := infrastructure.NewPGSourceRepository(pool)

	orgID := uuid.New()
	storeID := insertStore(t, pool, orgID, "loja-b.example.com")

	insertSource(t, pool, insertSourceArgs{
		OrgID:           orgID,
		StoreID:         storeID,
		URL:             "https://loja-b.example.com/p/nunca-coletada",
		Enabled:         true,
		IntervalSeconds: 60,
		LastCollectedAt: nil,
	})

	got, err := repo.GetEligible(ctx, orgID, 10)
	require.NoError(t, err)
	require.Len(t, got, 1, "fonte nunca coletada (last_collected_at NULL) é elegível")
}

func TestUpdateSourceAfterCollect_OptimisticLocking(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	repo := infrastructure.NewPGSourceRepository(pool)

	orgID := uuid.New()
	storeID := insertStore(t, pool, orgID, "loja-c.example.com")
	tenMinAgo := time.Now().UTC().Add(-10 * time.Minute)
	srcID := insertSource(t, pool, insertSourceArgs{
		OrgID:           orgID,
		StoreID:         storeID,
		URL:             "https://loja-c.example.com/p/concorrencia",
		Enabled:         true,
		IntervalSeconds: 60,
		LastCollectedAt: &tenMinAgo,
	})

	snap1 := domain.Snapshot{
		SKU:               "SKU-1",
		Titulo:            "Produto",
		Preco:             decimal.RequireFromString("99.90"),
		EstoqueDisponivel: true,
		BadgePromo:        true,
		ColetadoEm:        time.Now().UTC(),
	}

	require.NoError(t, repo.UpdateAfterCollect(ctx, orgID, srcID, 1, snap1))

	snap2 := snap1
	snap2.Preco = decimal.RequireFromString("89.90")
	err := repo.UpdateAfterCollect(ctx, orgID, srcID, 1, snap2)
	require.ErrorIs(t, err, domain.ErrConcurrentUpdate)

	got, err := repo.GetByID(ctx, orgID, srcID)
	require.NoError(t, err)
	require.Equal(t, 2, got.Version)
	require.NotNil(t, got.LastSnapshot)
	require.True(t, got.LastSnapshot.Preco.Equal(decimal.RequireFromString("99.90")))
	require.Nil(t, got.LastError)
}

func TestUpdateSourceAfterCollect_NotFoundOtherOrg(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	repo := infrastructure.NewPGSourceRepository(pool)

	orgID := uuid.New()
	otherOrgID := uuid.New()
	storeID := insertStore(t, pool, orgID, "loja-d.example.com")
	srcID := insertSource(t, pool, insertSourceArgs{
		OrgID:           orgID,
		StoreID:         storeID,
		URL:             "https://loja-d.example.com/p/multi-tenant",
		Enabled:         true,
		IntervalSeconds: 60,
		LastCollectedAt: nil,
	})

	snap := domain.Snapshot{
		Preco:      decimal.RequireFromString("10.00"),
		ColetadoEm: time.Now().UTC(),
	}
	err := repo.UpdateAfterCollect(ctx, otherOrgID, srcID, 1, snap)
	require.ErrorIs(t, err, domain.ErrConcurrentUpdate, "tenant alheio não pode atualizar")
}

func TestMarkError_SetsErrorAndBumpsVersion(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	repo := infrastructure.NewPGSourceRepository(pool)

	orgID := uuid.New()
	storeID := insertStore(t, pool, orgID, "loja-e.example.com")
	srcID := insertSource(t, pool, insertSourceArgs{
		OrgID:           orgID,
		StoreID:         storeID,
		URL:             "https://loja-e.example.com/p/falha",
		Enabled:         true,
		IntervalSeconds: 60,
		LastCollectedAt: nil,
	})

	require.NoError(t, repo.MarkError(ctx, orgID, srcID, "timeout fetch"))

	got, err := repo.GetByID(ctx, orgID, srcID)
	require.NoError(t, err)
	require.NotNil(t, got.LastError)
	require.Equal(t, "timeout fetch", *got.LastError)
	require.Equal(t, 2, got.Version)
}

func TestGetByID_NotFound(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	repo := infrastructure.NewPGSourceRepository(pool)

	orgID := uuid.New()
	_, err := repo.GetByID(ctx, orgID, 999_999)
	require.ErrorIs(t, err, domain.ErrSourceNotFound)
}

func TestWithTx_RollbackOnError(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()
	repo := infrastructure.NewPGSourceRepository(pool)

	orgID := uuid.New()
	storeID := insertStore(t, pool, orgID, "loja-f.example.com")
	srcID := insertSource(t, pool, insertSourceArgs{
		OrgID:           orgID,
		StoreID:         storeID,
		URL:             "https://loja-f.example.com/p/tx",
		Enabled:         true,
		IntervalSeconds: 60,
		LastCollectedAt: nil,
	})

	snap := domain.Snapshot{
		Preco:      decimal.RequireFromString("100.00"),
		ColetadoEm: time.Now().UTC(),
	}

	wantErr := domain.ErrConcurrentUpdate
	err := repo.WithTx(ctx, func(txRepo domain.SourceRepository) error {
		if e := txRepo.UpdateAfterCollect(ctx, orgID, srcID, 1, snap); e != nil {
			return e
		}
		return wantErr
	})
	require.ErrorIs(t, err, wantErr)

	got, err := repo.GetByID(ctx, orgID, srcID)
	require.NoError(t, err)
	require.Equal(t, 1, got.Version, "rollback deve preservar version original")
	require.Nil(t, got.LastSnapshot)
}
