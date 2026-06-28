package infrastructure

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"

	"promo-scraper/internal/modules/scraping/collection/application"
	sources "promo-scraper/internal/modules/scraping/sources/domain"
)

const validProductHTML = `<html><body>
<span class="price">R$ 90,00</span>
<h1 class="title">Placa Mae X</h1>
<span class="sku">SKU-X1</span>
<span class="stock">Em estoque</span>
<div class="badge">Promo</div>
</body></html>`

func newSource(t *testing.T, url string, strategy sources.Strategy) sources.Source {
	t.Helper()
	return sources.Source{
		ID:       42,
		OrgID:    uuid.New(),
		StoreID:  7,
		URL:      url,
		Strategy: strategy,
		Selectors: map[string]string{
			"preco":   ".price",
			"titulo":  ".title",
			"sku":     ".sku",
			"estoque": ".stock",
			"badge":   ".badge",
		},
	}
}

func newCollectorWithRate(timeout time.Duration, r rate.Limit, burst int) *HTTPCollector {
	bucket := NewTokenBucketRegistry(r, burst)
	return NewHTTPCollector(timeout, bucket, nil)
}

func TestCollect_HappyPath(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, validProductHTML)
	}))
	t.Cleanup(server.Close)

	collector := newCollectorWithRate(2*time.Second, rate.Inf, 1)
	src := newSource(t, server.URL, sources.StrategyHTTP)

	snap, err := collector.Collect(context.Background(), src)
	require.NoError(t, err)
	require.True(t, snap.Preco.Equal(decimal.NewFromInt(90)), "preco esperado 90 got %s", snap.Preco.String())
	require.Equal(t, "Placa Mae X", snap.Titulo)
	require.Equal(t, "SKU-X1", snap.SKU)
	require.True(t, snap.EstoqueDisponivel)
	require.True(t, snap.BadgePromo)
}

func TestCollect_SelectorNotMatched(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><body><h1 class="title">Sem preco</h1></body></html>`)
	}))
	t.Cleanup(server.Close)

	collector := newCollectorWithRate(2*time.Second, rate.Inf, 1)
	src := newSource(t, server.URL, sources.StrategyHTTP)

	_, err := collector.Collect(context.Background(), src)
	require.Error(t, err)
	require.True(t, errors.Is(err, application.ErrSelectorNotMatched), "esperava ErrSelectorNotMatched got %v", err)
}

func TestCollect_InvalidPrice(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<html><body><span class="price">R$ 0,00</span></body></html>`)
	}))
	t.Cleanup(server.Close)

	collector := newCollectorWithRate(2*time.Second, rate.Inf, 1)
	src := newSource(t, server.URL, sources.StrategyHTTP)

	_, err := collector.Collect(context.Background(), src)
	require.Error(t, err)
	require.True(t, errors.Is(err, application.ErrInvalidPrice), "esperava ErrInvalidPrice got %v", err)
}

func TestCollect_ServerError5xx(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	collector := newCollectorWithRate(2*time.Second, rate.Inf, 1)
	src := newSource(t, server.URL, sources.StrategyHTTP)

	_, err := collector.Collect(context.Background(), src)
	require.Error(t, err)
	require.True(t, errors.Is(err, application.ErrFetchFailed), "esperava ErrFetchFailed got %v", err)
}

func TestCollect_StatusTooManyRequests(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	t.Cleanup(server.Close)

	collector := newCollectorWithRate(2*time.Second, rate.Inf, 1)
	src := newSource(t, server.URL, sources.StrategyHTTP)

	_, err := collector.Collect(context.Background(), src)
	require.Error(t, err)
	require.True(t, errors.Is(err, application.ErrFetchFailed))
}

func TestCollect_Timeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = io.WriteString(w, validProductHTML)
	}))
	t.Cleanup(server.Close)

	collector := newCollectorWithRate(50*time.Millisecond, rate.Inf, 1)
	src := newSource(t, server.URL, sources.StrategyHTTP)

	_, err := collector.Collect(context.Background(), src)
	require.Error(t, err)
	require.True(t, errors.Is(err, application.ErrFetchFailed), "esperava ErrFetchFailed got %v", err)
}

func TestCollect_SendsRealisticUserAgent(t *testing.T) {
	t.Parallel()

	var received atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Store(r.Header.Get("User-Agent"))
		require.Contains(t, r.Header.Get("Accept-Language"), "pt-BR")
		_, _ = io.WriteString(w, validProductHTML)
	}))
	t.Cleanup(server.Close)

	collector := newCollectorWithRate(2*time.Second, rate.Inf, 1)
	src := newSource(t, server.URL, sources.StrategyHTTP)

	_, err := collector.Collect(context.Background(), src)
	require.NoError(t, err)

	got, ok := received.Load().(string)
	require.True(t, ok)
	require.NotEmpty(t, got)

	pool := userAgentPool()
	inPool := false
	for _, ua := range pool {
		if ua == got {
			inPool = true
			break
		}
	}
	require.True(t, inPool, "user agent %q deveria estar no pool", got)
}

func TestCollect_RespectsTokenBucket(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, validProductHTML)
	}))
	t.Cleanup(server.Close)

	collector := newCollectorWithRate(2*time.Second, rate.Every(80*time.Millisecond), 1)
	src := newSource(t, server.URL, sources.StrategyHTTP)
	ctx := context.Background()

	_, err := collector.Collect(ctx, src)
	require.NoError(t, err)

	start := time.Now()
	_, err = collector.Collect(ctx, src)
	require.NoError(t, err)
	elapsed := time.Since(start)

	require.GreaterOrEqual(t, elapsed, 40*time.Millisecond, "segunda chamada deveria aguardar pelo token bucket")
}

func TestCollect_UnsupportedStrategy(t *testing.T) {
	t.Parallel()

	collector := newCollectorWithRate(2*time.Second, rate.Inf, 1)
	src := newSource(t, "http://does-not-matter.invalid", sources.StrategyHeadless)

	_, err := collector.Collect(context.Background(), src)
	require.Error(t, err)
	require.True(t, errors.Is(err, application.ErrFetchFailed))
	require.Contains(t, err.Error(), "headless")
}

func TestCollect_ContextCancelled(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(500 * time.Millisecond)
		_, _ = io.WriteString(w, validProductHTML)
	}))
	t.Cleanup(server.Close)

	collector := newCollectorWithRate(2*time.Second, rate.Inf, 1)
	src := newSource(t, server.URL, sources.StrategyHTTP)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	var collectErr error
	go func() {
		defer wg.Done()
		_, collectErr = collector.Collect(ctx, src)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	wg.Wait()

	require.Error(t, collectErr)
	require.True(t, errors.Is(collectErr, application.ErrFetchFailed))
}

func TestCollect_GzipResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Contains(t, r.Header.Get("Accept-Encoding"), "gzip")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(mustGzipBytes(t, []byte(validProductHTML)))
	}))
	t.Cleanup(server.Close)

	collector := newCollectorWithRate(2*time.Second, rate.Inf, 1)
	src := newSource(t, server.URL, sources.StrategyHTTP)

	snap, err := collector.Collect(context.Background(), src)
	require.NoError(t, err)
	require.True(t, snap.Preco.Equal(decimal.NewFromInt(90)))
}

func mustGzipBytes(t *testing.T, raw []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, err := gz.Write(raw)
	require.NoError(t, err)
	require.NoError(t, gz.Close())
	return buf.Bytes()
}
