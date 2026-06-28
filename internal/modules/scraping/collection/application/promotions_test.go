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

func mlItem(pct int) PromoItem {
	return PromoItem{Loja: "mercado livre", Titulo: "Notebook", Preco: decimal.RequireFromString("2000"), DescontoPct: pct, EmPromocao: true}
}

func TestListPromotions_AggregatesAndSorts(t *testing.T) {
	providers := []NamedOffersProvider{
		{Loja: "kabum", Provider: fakeOffers{itens: []PromoItem{kabumItem(10), kabumItem(30)}}},
		{Loja: "mercado livre", Provider: fakeOffers{itens: []PromoItem{mlItem(25)}}},
	}

	uc := NewListPromotionsUseCase(providers, fakeCollector{}, nil, nil)
	out, err := uc.Execute(context.Background(), ListPromotionsInput{Limit: 10})
	require.NoError(t, err)
	require.Empty(t, out.Erros)
	require.Len(t, out.Itens, 3)
	require.Equal(t, 30, out.Itens[0].DescontoPct, "ordenado por desconto desc")
	require.Equal(t, 25, out.Itens[1].DescontoPct)
	require.Equal(t, 10, out.Itens[2].DescontoPct)
}

func TestListPromotions_FilterByLoja(t *testing.T) {
	providers := []NamedOffersProvider{
		{Loja: "kabum", Provider: fakeOffers{itens: []PromoItem{kabumItem(10)}}},
		{Loja: "mercado livre", Provider: fakeOffers{itens: []PromoItem{mlItem(25)}}},
	}

	uc := NewListPromotionsUseCase(providers, fakeCollector{}, nil, nil)
	out, err := uc.Execute(context.Background(), ListPromotionsInput{Loja: "mercado livre"})
	require.NoError(t, err)
	require.Len(t, out.Itens, 1)
	require.Equal(t, "mercado livre", out.Itens[0].Loja)
}

func TestListPromotions_PartialErrorDoesNotFail(t *testing.T) {
	providers := []NamedOffersProvider{
		{Loja: "kabum", Provider: fakeOffers{itens: []PromoItem{kabumItem(10)}}},
		{Loja: "terabyte", Provider: fakeOffers{err: errors.New("scrape timeout")}},
	}

	uc := NewListPromotionsUseCase(providers, fakeCollector{}, nil, nil)
	out, err := uc.Execute(context.Background(), ListPromotionsInput{})
	require.NoError(t, err, "erro de uma loja não derruba a rota")
	require.Len(t, out.Itens, 1, "kabum continua presente")
	require.Contains(t, out.Erros, "terabyte")
}

func TestListPromotions_Limit(t *testing.T) {
	providers := []NamedOffersProvider{
		{Loja: "kabum", Provider: fakeOffers{itens: []PromoItem{kabumItem(10), kabumItem(20), kabumItem(30)}}},
	}
	uc := NewListPromotionsUseCase(providers, fakeCollector{}, nil, nil)
	out, err := uc.Execute(context.Background(), ListPromotionsInput{Limit: 2})
	require.NoError(t, err)
	require.Len(t, out.Itens, 2)
}
