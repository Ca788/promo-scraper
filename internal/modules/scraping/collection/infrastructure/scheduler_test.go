package infrastructure_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/stretchr/testify/require"

	"promo-scraper/internal/modules/scraping/collection/infrastructure"
	sourcesinfra "promo-scraper/internal/modules/scraping/sources/infrastructure"
)

func TestScheduler_RunOnce_EnqueuesOnlyEligibleSources(t *testing.T) {
	pool, cleanup := setupIntegrationDB(t)
	defer cleanup()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	rc, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger: logger,
	})
	require.NoError(t, err)

	repo := sourcesinfra.NewPGSourceRepository(pool)

	orgID := uuid.New()
	storeID := insertStoreRow(t, pool, orgID, "Kabum", "kabum.example.com")

	tenMinAgo := time.Now().UTC().Add(-10 * time.Minute)

	insertSourceRow(t, pool, sourceFixture{
		OrgID:           orgID,
		StoreID:         storeID,
		URL:             "https://kabum.example.com/p/a",
		Enabled:         true,
		IntervalSeconds: 60,
		LastCollectedAt: &tenMinAgo,
	})

	insertSourceRow(t, pool, sourceFixture{
		OrgID:           orgID,
		StoreID:         storeID,
		URL:             "https://kabum.example.com/p/b",
		Enabled:         true,
		IntervalSeconds: 60,
		LastCollectedAt: nil,
	})

	insertSourceRow(t, pool, sourceFixture{
		OrgID:           orgID,
		StoreID:         storeID,
		URL:             "https://kabum.example.com/p/c",
		Enabled:         false,
		IntervalSeconds: 60,
		LastCollectedAt: &tenMinAgo,
	})

	scheduler := infrastructure.NewScheduler(
		repo,
		rc,
		200*time.Millisecond,
		[]uuid.UUID{orgID},
		logger,
	)

	require.NoError(t, scheduler.RunOnce(context.Background()))

	require.Equal(t, 2, countRiverJobs(t, pool, infrastructure.CollectSourceKind))
}
