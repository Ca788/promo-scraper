package infrastructure

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"promo-scraper/internal/modules/scraping/collection/application"
)

const kabumLikeHTML = `<!doctype html><html><head>
<script type="application/ld+json">
[{"@type":"BreadcrumbList","itemListElement":[]},
 {"@type":"Product","name":"Controle Sony DualSense Edge PS5","sku":"401293",
  "offers":{"@type":"Offer","price":1199,"priceCurrency":"BRL","availability":"https://schema.org/InStock"}}]
</script></head><body><h1>produto</h1></body></html>`

func TestParseProductJSONLD_HappyPath(t *testing.T) {
	t.Parallel()

	snap, err := ParseProductJSONLD([]byte(kabumLikeHTML))
	require.NoError(t, err)
	require.Equal(t, "Controle Sony DualSense Edge PS5", snap.Titulo)
	require.Equal(t, "401293", snap.SKU, "sku real do ld+json, não o nome")
	require.True(t, snap.Preco.Equal(decimal.RequireFromString("1199")), "preço numérico do ld+json")
	require.True(t, snap.EstoqueDisponivel, "availability InStock")
	require.False(t, snap.ColetadoEm.IsZero())
}

func TestParseProductJSONLD_Variants(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		html      string
		wantPrice string
		wantStock bool
	}{
		{
			name:      "graph wrapper + price string",
			html:      `<script type="application/ld+json">{"@graph":[{"@type":"Product","name":"X","offers":{"price":"2.50","availability":"InStock"}}]}</script>`,
			wantPrice: "2.50",
			wantStock: true,
		},
		{
			name:      "type array + offers array + out of stock",
			html:      `<script type="application/ld+json">{"@type":["Product","Thing"],"name":"Y","offers":[{"price":99.9,"availability":"https://schema.org/OutOfStock"}]}</script>`,
			wantPrice: "99.9",
			wantStock: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snap, err := ParseProductJSONLD([]byte(tc.html))
			require.NoError(t, err)
			require.True(t, snap.Preco.Equal(decimal.RequireFromString(tc.wantPrice)))
			require.Equal(t, tc.wantStock, snap.EstoqueDisponivel)
		})
	}
}

func TestParseProductJSONLD_Errors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		html    string
		wantErr error
	}{
		{"sem product", `<script type="application/ld+json">{"@type":"WebPage"}</script>`, application.ErrSelectorNotMatched},
		{"sem ld+json", `<html><body>nada</body></html>`, application.ErrSelectorNotMatched},
		{"price ausente", `<script type="application/ld+json">{"@type":"Product","name":"X","offers":{"availability":"InStock"}}</script>`, application.ErrSelectorNotMatched},
		{"price zero", `<script type="application/ld+json">{"@type":"Product","name":"X","offers":{"price":0}}</script>`, application.ErrInvalidPrice},
		{"price texto", `<script type="application/ld+json">{"@type":"Product","name":"X","offers":{"price":"grátis"}}</script>`, application.ErrSelectorNotMatched},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseProductJSONLD([]byte(tc.html))
			require.Error(t, err)
			require.True(t, errors.Is(err, tc.wantErr), "esperava %v, got %v", tc.wantErr, err)
		})
	}
}
