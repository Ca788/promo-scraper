package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"promo-scraper/internal/modules/scraping/collection/application"
)

type stubOffers struct{ itens []application.PromoItem }

func (s stubOffers) Offers(_ context.Context, _, _ int) ([]application.PromoItem, error) {
	return s.itens, nil
}

func TestPromocoesRoute(t *testing.T) {
	anterior := decimal.RequireFromString("1699.00")
	offers := stubOffers{itens: []application.PromoItem{
		{Loja: "kabum", Titulo: "Estabilizador", Preco: decimal.RequireFromString("1325.22"), PrecoAnterior: &anterior, DescontoPct: 22, EmPromocao: true, Disponivel: true, Link: "https://www.kabum.com.br/produto/986987"},
	}}
	uc := application.NewListPromotionsUseCase(offers, nil, nil, nil)

	srv := httptest.NewServer(newRouter(uc, nil))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/promocoes?loja=kabum&limit=10")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		Total     int         `json:"total"`
		Promocoes []promoJSON `json:"promocoes"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, 1, body.Total)
	require.Equal(t, "kabum", body.Promocoes[0].Loja)
	require.Equal(t, "1325.22", body.Promocoes[0].Preco)
	require.Equal(t, 22, body.Promocoes[0].DescontoPct)
	require.NotNil(t, body.Promocoes[0].PrecoAnterior)
	require.Equal(t, "1699.00", *body.Promocoes[0].PrecoAnterior)
}

func TestHealthz(t *testing.T) {
	srv := httptest.NewServer(newRouter(application.NewListPromotionsUseCase(stubOffers{}, nil, nil, nil), nil))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}
