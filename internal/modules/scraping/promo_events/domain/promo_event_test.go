package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	promoevents "promo-scraper/internal/modules/scraping/promo_events/domain"
	sources "promo-scraper/internal/modules/scraping/sources/domain"
	"promo-scraper/internal/shared/clock"
)

func newSource(t *testing.T, lastSnap *sources.Snapshot) sources.Source {
	t.Helper()
	return sources.Source{
		ID:           42,
		OrgID:        uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		StoreID:      7,
		URL:          "https://loja.example.com/p/produto-123",
		Strategy:     sources.StrategyHTTP,
		LastSnapshot: lastSnap,
	}
}

func snapshotAtPrice(t *testing.T, price string) sources.Snapshot {
	t.Helper()
	return sources.Snapshot{
		SKU:               "SKU-123",
		Titulo:            "Produto X",
		Preco:             decimal.RequireFromString(price),
		EstoqueDisponivel: true,
		BadgePromo:        true,
		ColetadoEm:        time.Now().UTC(),
	}
}

func TestNewPromoEvent_FillsPrecoAnteriorFromSourceSnapshot(t *testing.T) {
	prevSnap := snapshotAtPrice(t, "100.00")
	src := newSource(t, &prevSnap)
	newSnap := snapshotAtPrice(t, "90.00")
	clk := &clock.FakeClock{T: time.Date(2026, 6, 28, 17, 45, 0, 0, time.UTC)}

	event := promoevents.NewPromoEvent(src, newSnap, clk)

	require.NotNil(t, event.PrecoAnterior, "PrecoAnterior deve refletir o snapshot anterior")
	require.True(t, event.PrecoAnterior.Equal(decimal.RequireFromString("100.00")))
	require.True(t, event.Preco.Equal(decimal.RequireFromString("90.00")))
	require.Equal(t, src.OrgID, event.OrgID)
	require.Equal(t, src.ID, event.SourceID)
	require.Equal(t, src.StoreID, event.StoreID)
	require.Equal(t, src.URL, event.URL)
	require.Equal(t, newSnap.SKU, event.SKU)
	require.Equal(t, newSnap.Titulo, event.Titulo)
	require.Equal(t, newSnap.EstoqueDisponivel, event.EstoqueDisponivel)
	require.Equal(t, newSnap.BadgePromo, event.BadgePromo)
}

func TestNewPromoEvent_NilSourceSnapshot_LeavesPrecoAnteriorNil(t *testing.T) {
	src := newSource(t, nil)
	snap := snapshotAtPrice(t, "90.00")
	clk := &clock.FakeClock{T: time.Date(2026, 6, 28, 17, 45, 0, 0, time.UTC)}

	event := promoevents.NewPromoEvent(src, snap, clk)

	require.Nil(t, event.PrecoAnterior, "primeiro snapshot da fonte não tem PrecoAnterior")
	require.True(t, event.Preco.Equal(decimal.RequireFromString("90.00")))
}

func TestNewPromoEvent_UsesInjectedClock(t *testing.T) {
	src := newSource(t, nil)
	snap := snapshotAtPrice(t, "10.00")
	fixed := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	clk := &clock.FakeClock{T: fixed}

	event := promoevents.NewPromoEvent(src, snap, clk)

	require.True(t, event.DetectedAt.Equal(fixed), "DetectedAt deve vir do clock injetado")
}

func TestNewPromoEvent_DefaultsCurrencyToBRL(t *testing.T) {
	src := newSource(t, nil)
	snap := snapshotAtPrice(t, "10.00")
	clk := &clock.FakeClock{T: time.Now().UTC()}

	event := promoevents.NewPromoEvent(src, snap, clk)

	require.Equal(t, "BRL", event.Moeda)
}
