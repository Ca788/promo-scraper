package infrastructure_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"

	"promo-scraper/internal/modules/scraping/collection/application"
	"promo-scraper/internal/modules/scraping/collection/infrastructure"
	eventsinfra "promo-scraper/internal/modules/scraping/promo_events/infrastructure"
	sourcesinfra "promo-scraper/internal/modules/scraping/sources/infrastructure"
	"promo-scraper/internal/shared/clock"
	"promo-scraper/internal/shared/observability"
)

type e2eSetup struct {
	pool    *pgxpool.Pool
	server  *httptest.Server
	client  *river.Client[pgx.Tx]
	metrics *observability.Metrics
	reg     *prometheus.Registry
	logger  *slog.Logger
	orgID   uuid.UUID
	storeID int64
	source  int64
}

func setupE2E(t *testing.T, handler http.HandlerFunc, maxAttempts int, lastSnapshot string) *e2eSetup {
	t.Helper()

	pool, cleanup := setupIntegrationDB(t)
	t.Cleanup(cleanup)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	orgID := uuid.New()
	storeID := insertStoreRow(t, pool, orgID, "Kabum", "kabum.example.com")

	tenMinAgo := time.Now().UTC().Add(-10 * time.Minute)
	srcID := insertSourceRow(t, pool, sourceFixture{
		OrgID:           orgID,
		StoreID:         storeID,
		URL:             server.URL + "/produto",
		Selectors:       `{"preco":".preco","titulo":".titulo","sku":".sku","estoque":".estoque","badge":".badge"}`,
		Enabled:         true,
		IntervalSeconds: 5,
		LastCollectedAt: &tenMinAgo,
		LastSnapshot:    &lastSnapshot,
	})

	srcRepo := sourcesinfra.NewPGSourceRepository(pool)
	evtRepo := eventsinfra.NewPGPromoEventRepository(pool)

	bucket := infrastructure.NewTokenBucketRegistry(rate.Inf, 100)
	httpCol := infrastructure.NewHTTPCollector(5*time.Second, bucket, logger)
	uc := application.NewCollectSourceUseCase(srcRepo, evtRepo, httpCol, clock.SystemClock{}, logger)

	reg := prometheus.NewRegistry()
	metrics := observability.New(reg)

	workers := river.NewWorkers()
	river.AddWorker(workers, infrastructure.NewCollectSourceWorker(uc, srcRepo, metrics, logger))

	rc, err := river.NewClient[pgx.Tx](riverpgxv5.New(pool), &river.Config{
		Logger:      logger,
		MaxAttempts: maxAttempts,
		Queues: map[string]river.QueueConfig{
			"default": {MaxWorkers: 1},
		},
		Workers:           workers,
		FetchCooldown:     100 * time.Millisecond,
		FetchPollInterval: 250 * time.Millisecond,
	})
	require.NoError(t, err)

	return &e2eSetup{
		pool:    pool,
		server:  server,
		client:  rc,
		metrics: metrics,
		reg:     reg,
		logger:  logger,
		orgID:   orgID,
		storeID: storeID,
		source:  srcID,
	}
}

func (s *e2eSetup) start(t *testing.T) func() {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, s.client.Start(ctx))
	return func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.client.Stop(stopCtx)
	}
}

func (s *e2eSetup) waitForJob(t *testing.T, sub <-chan *river.Event, want rivertype.JobState, timeout time.Duration) *river.Event {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		select {
		case ev := <-sub:
			if ev == nil || ev.Job == nil {
				continue
			}
			if ev.Job.State == want {
				return ev
			}
		case <-deadline.C:
			t.Fatalf("timeout esperando job state=%s", want)
			return nil
		}
	}
}

func happyPathHTML() string {
	return `<!doctype html><html><body>
<div class="titulo">Mouse Gamer XPTO</div>
<div class="sku">SKU-XPTO-1</div>
<div class="preco">R$ 90,00</div>
<div class="estoque">em estoque</div>
<div class="badge">promo</div>
</body></html>`
}

func selectorMissingHTML() string {
	return `<!doctype html><html><body>
<div class="titulo">Mouse Gamer XPTO</div>
<div class="sku">SKU-XPTO-1</div>
</body></html>`
}

func snapshotJSON(preco string) string {
	snap := map[string]interface{}{
		"sku":                "SKU-XPTO-1",
		"titulo":             "Mouse Gamer XPTO",
		"preco":              json.RawMessage(preco),
		"estoque_disponivel": true,
		"badge_promo":        false,
		"coletado_em":        time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano),
	}
	b, _ := json.Marshal(snap)
	return string(b)
}

func TestEndToEnd_HappyPath_HTTPDropDetected(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(happyPathHTML()))
	}

	setup := setupE2E(t, handler, 5, snapshotJSON("100.00"))

	sub, unsub := setup.client.Subscribe(river.EventKindJobCompleted, river.EventKindJobFailed)
	defer unsub()

	stop := setup.start(t)
	defer stop()

	_, err := setup.client.Insert(context.Background(), infrastructure.CollectSourceArgs{
		OrgID:    setup.orgID,
		SourceID: setup.source,
		Version:  1,
	}, nil)
	require.NoError(t, err)

	setup.waitForJob(t, sub, rivertype.JobStateCompleted, 10*time.Second)

	var (
		preco         decimal.Decimal
		precoAnterior decimal.Decimal
		count         int
	)
	row := setup.pool.QueryRow(
		context.Background(),
		`SELECT preco, preco_anterior FROM promo_events WHERE org_id = $1 AND source_id = $2`,
		setup.orgID, setup.source,
	)
	require.NoError(t, row.Scan(&preco, &precoAnterior))

	err = setup.pool.QueryRow(
		context.Background(),
		`SELECT count(*) FROM promo_events WHERE org_id = $1`,
		setup.orgID,
	).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.True(t, preco.Equal(decimal.RequireFromString("90.00")), "preço atual=%s", preco)
	require.True(t, precoAnterior.Equal(decimal.RequireFromString("100.00")), "preço anterior=%s", precoAnterior)

	var (
		lastError  *string
		lastCollAt *time.Time
		snapPreco  string
	)
	require.NoError(t, setup.pool.QueryRow(
		context.Background(),
		`SELECT last_snapshot->>'preco', last_error, last_collected_at FROM sources WHERE id = $1 AND org_id = $2`,
		setup.source, setup.orgID,
	).Scan(&snapPreco, &lastError, &lastCollAt))

	require.NotNil(t, lastCollAt)
	require.WithinDuration(t, time.Now().UTC(), *lastCollAt, 10*time.Second)
	require.Nil(t, lastError, "last_error deve ser NULL após sucesso")
	snapDecimal, err := decimal.NewFromString(snapPreco)
	require.NoError(t, err)
	require.True(t, snapDecimal.Equal(decimal.RequireFromString("90.00")),
		"snapshot.preco esperado=90.00 obtido=%s", snapPreco)

	require.True(t, histogramHasSample(t, setup.reg, "collection_duration_seconds", map[string]string{
		"store_id": strconv.FormatInt(setup.storeID, 10),
		"strategy": "http",
		"result":   observability.ResultSuccess,
	}))
}

func TestEndToEnd_FetchFailed_RetriesThenMarksError(t *testing.T) {
	var hits atomic.Int64
	handler := func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}

	setup := setupE2E(t, handler, 2, snapshotJSON("100.00"))

	sub, unsub := setup.client.Subscribe(river.EventKindJobCompleted, river.EventKindJobFailed)
	defer unsub()

	stop := setup.start(t)
	defer stop()

	_, err := setup.client.Insert(context.Background(), infrastructure.CollectSourceArgs{
		OrgID:    setup.orgID,
		SourceID: setup.source,
		Version:  1,
	}, nil)
	require.NoError(t, err)

	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	discarded := false
	for !discarded {
		select {
		case ev := <-sub:
			if ev == nil || ev.Job == nil {
				continue
			}
			if ev.Job.State == rivertype.JobStateDiscarded {
				discarded = true
			}
		case <-deadline.C:
			t.Fatal("timeout esperando job entrar em state=discarded")
		}
	}

	var count int
	require.NoError(t, setup.pool.QueryRow(
		context.Background(),
		`SELECT count(*) FROM promo_events WHERE org_id = $1`,
		setup.orgID,
	).Scan(&count))
	require.Zero(t, count, "fetch falhado não deve materializar evento")

	var lastError *string
	require.NoError(t, setup.pool.QueryRow(
		context.Background(),
		`SELECT last_error FROM sources WHERE id = $1 AND org_id = $2`,
		setup.source, setup.orgID,
	).Scan(&lastError))
	require.NotNil(t, lastError, "last_error deve ser populado após esgotar tentativas")

	require.True(t, counterGreaterThan(t, setup.reg, "collection_errors_total", map[string]string{
		"store_id": strconv.FormatInt(setup.storeID, 10),
		"kind":     observability.ErrorKindFetchFailed,
	}, 0))

	require.GreaterOrEqual(t, int(hits.Load()), 2)
}

func TestEndToEnd_SelectorNotMatched_MarksErrorImmediately(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(selectorMissingHTML()))
	}

	setup := setupE2E(t, handler, 5, snapshotJSON("100.00"))

	sub, unsub := setup.client.Subscribe(river.EventKindJobCompleted, river.EventKindJobFailed)
	defer unsub()

	stop := setup.start(t)
	defer stop()

	_, err := setup.client.Insert(context.Background(), infrastructure.CollectSourceArgs{
		OrgID:    setup.orgID,
		SourceID: setup.source,
		Version:  1,
	}, nil)
	require.NoError(t, err)

	setup.waitForJob(t, sub, rivertype.JobStateCompleted, 10*time.Second)

	var count int
	require.NoError(t, setup.pool.QueryRow(
		context.Background(),
		`SELECT count(*) FROM promo_events WHERE org_id = $1`,
		setup.orgID,
	).Scan(&count))
	require.Zero(t, count, "selector não casado não materializa evento")

	var (
		lastError *string
		snapPreco string
	)
	require.NoError(t, setup.pool.QueryRow(
		context.Background(),
		`SELECT last_error, last_snapshot->>'preco' FROM sources WHERE id = $1 AND org_id = $2`,
		setup.source, setup.orgID,
	).Scan(&lastError, &snapPreco))
	require.NotNil(t, lastError, "last_error deve estar populado em selector miss")
	snapDecimal, err := decimal.NewFromString(snapPreco)
	require.NoError(t, err)
	require.True(t, snapDecimal.Equal(decimal.RequireFromString("100.00")),
		"snapshot anterior deve permanecer inalterado (preço 100), obtido=%s", snapPreco)

	require.True(t, counterGreaterThan(t, setup.reg, "collection_errors_total", map[string]string{
		"store_id": strconv.FormatInt(setup.storeID, 10),
		"kind":     observability.ErrorKindSelectorNotMatched,
	}, 0))
}

func histogramHasSample(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string) bool {
	t.Helper()
	mfs, err := reg.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			if matchesLabels(m, labels) {
				if h := m.GetHistogram(); h != nil && h.GetSampleCount() > 0 {
					return true
				}
			}
		}
	}
	return false
}

func counterGreaterThan(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string, min float64) bool {
	t.Helper()
	mfs, err := reg.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			if matchesLabels(m, labels) {
				if c := m.GetCounter(); c != nil && c.GetValue() > min {
					return true
				}
			}
		}
	}
	return false
}

func matchesLabels(m *dto.Metric, want map[string]string) bool {
	got := map[string]string{}
	for _, l := range m.GetLabel() {
		got[l.GetName()] = l.GetValue()
	}
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}
