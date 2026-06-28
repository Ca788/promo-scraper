package application

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	sources "promo-scraper/internal/modules/scraping/sources/domain"
)

type fakeOffers struct {
	itens []PromoItem
	err   error
}

func (f fakeOffers) Offers(_ context.Context, _, _ int) ([]PromoItem, error) {
	return f.itens, f.err
}

type fakeCollector struct {
	byURL map[string]sources.Snapshot
	err   error
}

func (f fakeCollector) Collect(_ context.Context, src sources.Source) (sources.Snapshot, error) {
	if f.err != nil {
		return sources.Snapshot{}, f.err
	}
	return f.byURL[src.URL], nil
}

func kabumItem(pct int) PromoItem {
	return PromoItem{Loja: "kabum", Titulo: "X", Preco: decimal.RequireFromString("10"), DescontoPct: pct, EmPromocao: true}
}

func TestListPromotions_AggregatesAndSorts(t *testing.T) {
	offers := fakeOffers{itens: []PromoItem{kabumItem(10), kabumItem(30)}}
	collector := fakeCollector{byURL: map[string]sources.Snapshot{
		"http://ml/x": {Titulo: "Notebook", Preco: decimal.RequireFromString("2000"), BadgePromo: true, EstoqueDisponivel: true},
	}}
	curated := []CuratedTarget{{Loja: "mercado livre", URL: "http://ml/x"}}

	uc := NewListPromotionsUseCase(offers, collector, curated, nil)
	out, err := uc.Execute(context.Background(), ListPromotionsInput{Limit: 10})
	require.NoError(t, err)
	require.Empty(t, out.Erros)
	require.Len(t, out.Itens, 3)
	require.Equal(t, 30, out.Itens[0].DescontoPct, "ordenado por desconto desc")

	var ml PromoItem
	for _, it := range out.Itens {
		if it.Loja == "mercado livre" {
			ml = it
		}
	}
	require.Equal(t, "Notebook", ml.Titulo)
	require.True(t, ml.EmPromocao)
}

func TestListPromotions_FilterByLoja(t *testing.T) {
	offers := fakeOffers{itens: []PromoItem{kabumItem(10)}}
	collector := fakeCollector{byURL: map[string]sources.Snapshot{"http://ml/x": {Titulo: "N", Preco: decimal.RequireFromString("1")}}}
	curated := []CuratedTarget{{Loja: "mercado livre", URL: "http://ml/x"}}

	uc := NewListPromotionsUseCase(offers, collector, curated, nil)
	out, err := uc.Execute(context.Background(), ListPromotionsInput{Loja: "mercado livre"})
	require.NoError(t, err)
	require.Len(t, out.Itens, 1)
	require.Equal(t, "mercado livre", out.Itens[0].Loja)
}

func TestListPromotions_PartialErrorDoesNotFail(t *testing.T) {
	offers := fakeOffers{itens: []PromoItem{kabumItem(10)}}
	collector := fakeCollector{err: errors.New("headless timeout")}
	curated := []CuratedTarget{{Loja: "terabyte", URL: "http://tb/x"}}

	uc := NewListPromotionsUseCase(offers, collector, curated, nil)
	out, err := uc.Execute(context.Background(), ListPromotionsInput{})
	require.NoError(t, err, "erro de uma loja não derruba a rota")
	require.Len(t, out.Itens, 1, "kabum continua presente")
	require.Contains(t, out.Erros, "terabyte")
}

func TestListPromotions_Limit(t *testing.T) {
	offers := fakeOffers{itens: []PromoItem{kabumItem(10), kabumItem(20), kabumItem(30)}}
	uc := NewListPromotionsUseCase(offers, fakeCollector{}, nil, nil)
	out, err := uc.Execute(context.Background(), ListPromotionsInput{Limit: 2})
	require.NoError(t, err)
	require.Len(t, out.Itens, 2)
}
