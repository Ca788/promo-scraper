package infrastructure

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

const kabumAPIResponse = `{"data":[
  {"id":986987,"attributes":{"title":"Estabilizador DJI","price":1699.0,"price_with_discount":1325.22,"discount_percentage":22,"available":true}},
  {"id":201027,"attributes":{"title":"PC Gamer","price":1249.95,"price_with_discount":1062.46,"discount_percentage":15,"available":true}},
  {"id":393548,"attributes":{"title":"Escrivaninha sem desconto","price":489.9,"price_with_discount":489.9,"discount_percentage":0,"available":true}}
]}`

func TestKabumOffers_FiltersAndMaps(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page_number") != "1" {
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		_, _ = w.Write([]byte(kabumAPIResponse))
	}))
	defer server.Close()

	c := NewKabumOffersCollector(5 * time.Second)
	c.baseURL = server.URL

	itens, err := c.Offers(context.Background(), 0, 50)
	require.NoError(t, err)
	require.Len(t, itens, 2, "produto com desconto 0 deve ser descartado")

	first := itens[0]
	require.Equal(t, "kabum", first.Loja)
	require.Equal(t, "Estabilizador DJI", first.Titulo)
	require.Equal(t, 22, first.DescontoPct)
	require.True(t, first.EmPromocao)
	require.True(t, first.Preco.Equal(decimal.RequireFromString("1325.22")))
	require.NotNil(t, first.PrecoAnterior)
	require.True(t, first.PrecoAnterior.Equal(decimal.RequireFromString("1699")))
	require.Equal(t, "https://www.kabum.com.br/produto/986987", first.Link)
}

func TestKabumOffers_MinDesconto(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page_number") != "1" {
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		_, _ = w.Write([]byte(kabumAPIResponse))
	}))
	defer server.Close()

	c := NewKabumOffersCollector(5 * time.Second)
	c.baseURL = server.URL

	itens, err := c.Offers(context.Background(), 20, 50)
	require.NoError(t, err)
	require.Len(t, itens, 1, "apenas desconto >= 20%")
	require.Equal(t, 22, itens[0].DescontoPct)
}
