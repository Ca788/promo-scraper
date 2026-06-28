package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"promo-scraper/internal/modules/scraping/collection/application"
)

type stubOffers struct{ itens []application.PromoItem }

func (s stubOffers) Offers(_ context.Context, _, _ int) ([]application.PromoItem, error) {
	return s.itens, nil
}

type promoEnvelope struct {
	Data []promoJSON `json:"data"`
	Meta struct {
		Pagination struct {
			Page       int  `json:"page"`
			Limit      int  `json:"limit"`
			Total      int  `json:"total"`
			TotalPages int  `json:"total_pages"`
			HasNext    bool `json:"has_next"`
			HasPrev    bool `json:"has_prev"`
		} `json:"pagination"`
		ColetadoEm time.Time `json:"coletado_em"`
	} `json:"meta"`
	Errors map[string]string `json:"errors,omitempty"`
}

func newPromoItem(loja string, desconto int, preco, anterior string) application.PromoItem {
	prev := decimal.RequireFromString(anterior)
	return application.PromoItem{
		Loja:          loja,
		Titulo:        "Produto " + loja,
		Preco:         decimal.RequireFromString(preco),
		PrecoAnterior: &prev,
		DescontoPct:   desconto,
		EmPromocao:    true,
		Disponivel:    true,
		Link:          "https://example.com/" + loja,
	}
}

func TestPromocoesRoute_EnvelopeAndShape(t *testing.T) {
	offers := stubOffers{itens: []application.PromoItem{
		newPromoItem("kabum", 22, "1325.22", "1699.00"),
	}}
	uc := application.NewListPromotionsUseCase(
		[]application.NamedOffersProvider{{Loja: "kabum", Provider: offers}},
		nil, nil, nil,
	)

	srv := httptest.NewServer(newRouter(uc, nil))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/promocoes?loja=kabum")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))

	var body promoEnvelope
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Len(t, body.Data, 1)
	require.Equal(t, "kabum", body.Data[0].Loja)
	require.Equal(t, "1325.22", body.Data[0].Preco)
	require.Equal(t, 22, body.Data[0].DescontoPct)
	require.NotNil(t, body.Data[0].PrecoAnterior)
	require.Equal(t, "1699.00", *body.Data[0].PrecoAnterior)

	require.Equal(t, 1, body.Meta.Pagination.Page)
	require.Equal(t, 20, body.Meta.Pagination.Limit)
	require.Equal(t, 1, body.Meta.Pagination.Total)
	require.Equal(t, 1, body.Meta.Pagination.TotalPages)
	require.False(t, body.Meta.Pagination.HasNext)
	require.False(t, body.Meta.Pagination.HasPrev)
	require.False(t, body.Meta.ColetadoEm.IsZero())
}

func TestPromocoesRoute_Paginates(t *testing.T) {
	itens := make([]application.PromoItem, 0, 25)
	for i := 0; i < 25; i++ {
		itens = append(itens, newPromoItem("kabum", 30-i, "10.00", "20.00"))
	}
	offers := stubOffers{itens: itens}
	uc := application.NewListPromotionsUseCase(
		[]application.NamedOffersProvider{{Loja: "kabum", Provider: offers}},
		nil, nil, nil,
	)
	srv := httptest.NewServer(newRouter(uc, nil))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/promocoes?page=2&limit=10")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body promoEnvelope
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Len(t, body.Data, 10, "página 2 com limit=10 deve trazer 10 itens")
	require.Equal(t, 2, body.Meta.Pagination.Page)
	require.Equal(t, 10, body.Meta.Pagination.Limit)
	require.Equal(t, 25, body.Meta.Pagination.Total)
	require.Equal(t, 3, body.Meta.Pagination.TotalPages)
	require.True(t, body.Meta.Pagination.HasNext)
	require.True(t, body.Meta.Pagination.HasPrev)
}

func TestPromocoesRoute_PageBeyondLast(t *testing.T) {
	offers := stubOffers{itens: []application.PromoItem{
		newPromoItem("kabum", 20, "10.00", "20.00"),
	}}
	uc := application.NewListPromotionsUseCase(
		[]application.NamedOffersProvider{{Loja: "kabum", Provider: offers}},
		nil, nil, nil,
	)
	srv := httptest.NewServer(newRouter(uc, nil))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/promocoes?page=5&limit=10")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body promoEnvelope
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, []promoJSON{}, body.Data, "página além do total devolve coleção vazia, nunca null")
	require.Equal(t, 5, body.Meta.Pagination.Page)
	require.Equal(t, 1, body.Meta.Pagination.Total)
	require.Equal(t, 1, body.Meta.Pagination.TotalPages)
	require.False(t, body.Meta.Pagination.HasNext)
}

func TestHealthz(t *testing.T) {
	uc := application.NewListPromotionsUseCase(nil, nil, nil, nil)
	srv := httptest.NewServer(newRouter(uc, nil))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}
