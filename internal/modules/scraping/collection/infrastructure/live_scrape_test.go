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

func TestLiveScraping_RealInternet(t *testing.T) {
	if testing.Short() {
		t.Skip("scraping ao vivo desabilitado em -short")
	}

	collector := newCollectorWithRate(15*time.Second, 0, 1)

	src := sources.Source{
		ID:       1,
		OrgID:    uuid.New(),
		StoreID:  1,
		URL:      "https://books.toscrape.com/catalogue/a-light-in-the-attic_1000/index.html",
		Strategy: sources.StrategyHTTP,
		Selectors: map[string]string{
			"titulo":  "h1",
			"estoque": ".instock.availability",
			"preco":   ".price_color", // real node, served as "£51.77"
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	snap, err := collector.Collect(ctx, src)

	if errors.Is(err, application.ErrFetchFailed) {
		t.Skipf("alvo ao vivo indisponível (rede/CI offline): %v", err)
	}

	require.ErrorIs(t, err, application.ErrSelectorNotMatched,
		"fetch real chegou ao estágio de parse sobre HTML ao vivo")
	require.Empty(t, snap.Titulo, "snapshot vazio quando o parse falha")
	t.Logf("scraping real OK -> fetch+parse executados sobre books.toscrape.com (preço £ rejeitado pelo normalizador BR, como esperado)")
}

func TestLiveScraping_Kabum_JSONLD(t *testing.T) {
	if testing.Short() {
		t.Skip("scraping ao vivo desabilitado em -short")
	}

	httpC := newCollectorWithRate(15*time.Second, 0, 1)
	router := NewRoutingCollector(httpC, nil, nil, nil)

	src := sources.Source{
		ID: 1, OrgID: uuid.New(), StoreID: 1,
		URL:      "https://www.kabum.com.br/produto/401293/controle-sony-dualsense-edge-ps5-sem-fio-preto-e-branco-cfi-zcp1wy",
		Strategy: sources.StrategyHTTP,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	snap, err := router.Collect(ctx, src)
	if errors.Is(err, application.ErrFetchFailed) {
		t.Skipf("Kabum indisponível (rede/CI offline): %v", err)
	}
	require.NoError(t, err)
	require.NotEmpty(t, snap.Titulo, "título extraído do JSON-LD real")
	require.True(t, snap.Preco.IsPositive(), "preço positivo do JSON-LD real")
	t.Logf("scraping Kabum OK -> titulo=%q preco=R$ %s estoque=%v",
		snap.Titulo, snap.Preco, snap.EstoqueDisponivel)
}

const brazilianProductHTML = `<!doctype html>
<html lang="pt-BR"><head><meta charset="utf-8"></head><body>
  <h1 class="product-title">Notebook Gamer Aurora 16GB</h1>
  <div class="sku">Cód: NB-AURORA-001</div>
  <div class="pricing">
    <span class="old-price">R$ 5.299,00</span>
    <span class="price">R$ 4.799,90</span>
  </div>
  <span class="availability">Em estoque</span>
  <span class="promo-badge">OFERTA RELÂMPAGO</span>
</body></html>`

func TestLiveScraping_BRPipeline_PriceDrop(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(brazilianProductHTML))
	}))
	defer server.Close()

	collector := newCollectorWithRate(5*time.Second, 0, 1)

	src := sources.Source{
		ID:       99,
		OrgID:    uuid.New(),
		StoreID:  3,
		URL:      server.URL,
		Strategy: sources.StrategyHTTP,
		Selectors: map[string]string{
			"preco":   ".price",
			"titulo":  ".product-title",
			"sku":     ".sku",
			"estoque": ".availability",
			"badge":   ".promo-badge",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	snap, err := collector.Collect(ctx, src)
	require.NoError(t, err)

	require.Equal(t, "Notebook Gamer Aurora 16GB", snap.Titulo)
	require.Equal(t, "Cód: NB-AURORA-001", snap.SKU)
	require.True(t, snap.EstoqueDisponivel)
	require.True(t, snap.BadgePromo)
	require.True(t, snap.Preco.Equal(decimal.RequireFromString("4799.90")),
		"R$ 4.799,90 deve normalizar para 4799.90, got %s", snap.Preco)

	previous := &sources.Snapshot{Preco: decimal.RequireFromString("5299.00")}
	require.True(t, previous.HasPriceDrop(snap.Preco),
		"queda de 5299.00 -> 4799.90 deve ser detectada")

	notLower := &sources.Snapshot{Preco: decimal.RequireFromString("4000.00")}
	require.False(t, notLower.HasPriceDrop(snap.Preco))

	t.Logf("scraping BR OK -> titulo=%q sku=%q preco=%s estoque=%v badge=%v",
		snap.Titulo, snap.SKU, snap.Preco, snap.EstoqueDisponivel, snap.BadgePromo)
}
