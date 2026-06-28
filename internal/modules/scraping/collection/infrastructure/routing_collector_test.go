package infrastructure

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"promo-scraper/internal/modules/scraping/collection/application"
	sources "promo-scraper/internal/modules/scraping/sources/domain"
)

func newRouter(t *testing.T) *RoutingCollector {
	t.Helper()
	httpC := newCollectorWithRate(5*time.Second, 0, 1)
	return NewRoutingCollector(httpC, nil, nil, nil)
}

func TestRouting_HeadlessNotImplemented(t *testing.T) {
	t.Parallel()
	r := newRouter(t)
	src := sources.Source{ID: 1, OrgID: uuid.New(), StoreID: 1, URL: "https://www.amazon.com.br/dp/B0CHX1W1XY"}
	_, err := r.Collect(context.Background(), src)
	require.True(t, errors.Is(err, application.ErrStrategyUnsupported), "amazon -> headless fase 2")
}

func TestRouting_MLNeedsHeadless(t *testing.T) {
	t.Parallel()
	r := newRouter(t)
	src := sources.Source{ID: 1, OrgID: uuid.New(), StoreID: 1, URL: "https://www.mercadolivre.com.br/x/p/MLB123"}
	_, err := r.Collect(context.Background(), src)
	require.True(t, errors.Is(err, application.ErrStrategyUnsupported), "ML sem headless habilitado")
}

func TestRouting_UnknownHostFallsBackToCSS(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, validProductHTML)
	}))
	defer server.Close()

	r := newRouter(t)
	src := newSource(t, server.URL, sources.StrategyHTTP)
	snap, err := r.Collect(context.Background(), src)
	require.NoError(t, err)
	require.Equal(t, "Placa Mae X", snap.Titulo)
}

func TestExtractByMode_JSONLD(t *testing.T) {
	t.Parallel()
	snap, err := extractByMode([]byte(kabumLikeHTML), sources.Source{}, ExtractJSONLD, nil)
	require.NoError(t, err)
	require.Equal(t, "Controle Sony DualSense Edge PS5", snap.Titulo)
}

func TestExtractByMode_CSSDefault(t *testing.T) {
	t.Parallel()
	src := newSource(t, "http://x", sources.StrategyHTTP)
	snap, err := extractByMode([]byte(validProductHTML), src, ExtractCSS, src.Selectors)
	require.NoError(t, err)
	require.Equal(t, "Placa Mae X", snap.Titulo)
	require.True(t, strings.HasPrefix(snap.SKU, "SKU"))
}
