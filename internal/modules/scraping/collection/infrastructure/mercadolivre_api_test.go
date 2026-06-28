package infrastructure

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"promo-scraper/internal/modules/scraping/collection/application"
	sources "promo-scraper/internal/modules/scraping/sources/domain"
)

func meliSource(url string) sources.Source {
	return sources.Source{
		ID: 1, OrgID: uuid.New(), StoreID: 5, URL: url, Strategy: sources.StrategyAPI,
	}
}

const itemsResponse = `{
  "id": "MLB3771434588",
  "title": "Console PlayStation 5 Slim 1TB",
  "price": 3499.90,
  "original_price": 3999.00,
  "available_quantity": 12,
  "status": "active"
}`

func TestMercadoLivreAPI_HappyPath(t *testing.T) {
	t.Parallel()

	var gotAuth, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(itemsResponse))
	}))
	defer server.Close()

	c := NewMercadoLivreAPICollector("token-xyz", 5*time.Second)
	c.baseURL = server.URL

	snap, err := c.Collect(context.Background(), meliSource("https://www.mercadolivre.com.br/console-ps5/p/MLB3771434588"))
	require.NoError(t, err)

	require.Equal(t, "Bearer token-xyz", gotAuth)
	require.Equal(t, "/items/MLB3771434588", gotPath)
	require.Equal(t, "Console PlayStation 5 Slim 1TB", snap.Titulo)
	require.True(t, snap.Preco.Equal(decimal.RequireFromString("3499.90")))
	require.True(t, snap.EstoqueDisponivel)
	require.True(t, snap.BadgePromo, "original_price > price deve marcar promo")
}

func TestMercadoLivreAPI_ItemIDFromDashedURL(t *testing.T) {
	t.Parallel()

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(itemsResponse))
	}))
	defer server.Close()

	c := NewMercadoLivreAPICollector("t", 5*time.Second)
	c.baseURL = server.URL

	_, err := c.Collect(context.Background(), meliSource("https://produto.mercadolivre.com.br/MLB-3771434588-console-ps5"))
	require.NoError(t, err)
	require.Equal(t, "/items/MLB3771434588", gotPath, "MLB-123 deve normalizar para MLB123")
}

func TestMercadoLivreAPI_Errors(t *testing.T) {
	t.Parallel()

	t.Run("token ausente", func(t *testing.T) {
		c := NewMercadoLivreAPICollector("", 5*time.Second)
		_, err := c.Collect(context.Background(), meliSource("https://www.mercadolivre.com.br/x/p/MLB1"))
		require.True(t, errors.Is(err, application.ErrFetchFailed))
	})

	t.Run("url sem id", func(t *testing.T) {
		c := NewMercadoLivreAPICollector("t", 5*time.Second)
		_, err := c.Collect(context.Background(), meliSource("https://www.mercadolivre.com.br/ofertas"))
		require.True(t, errors.Is(err, application.ErrSelectorNotMatched))
	})

	t.Run("403 da api", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()
		c := NewMercadoLivreAPICollector("t", 5*time.Second)
		c.baseURL = server.URL
		_, err := c.Collect(context.Background(), meliSource("https://www.mercadolivre.com.br/x/p/MLB1"))
		require.True(t, errors.Is(err, application.ErrFetchFailed))
	})
}
