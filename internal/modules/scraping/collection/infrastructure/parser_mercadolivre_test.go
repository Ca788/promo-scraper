package infrastructure

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"promo-scraper/internal/modules/scraping/collection/application"
)

const mlPDPHTML = `<!doctype html><html><body>
<h1 class="ui-pdp-title">Console PlayStation 5 Slim</h1>
<div class="ui-pdp-price__second-line">
  <span class="andes-money-amount andes-money-amount__discount" aria-label="20% OFF">20% OFF</span>
  <span class="andes-money-amount" aria-label="3499 reais com 90 centavos">
    <span class="andes-money-amount__fraction">3.499</span>
    <span class="andes-money-amount__cents">90</span>
  </span>
</div>
<div class="ui-pdp-price__other"><span class="andes-money-amount" aria-label="50 reais">outro</span></div>
</body></html>`

func TestParseMercadoLivre_HappyPath(t *testing.T) {
	t.Parallel()
	snap, err := ParseMercadoLivre([]byte(mlPDPHTML))
	require.NoError(t, err)
	require.Equal(t, "Console PlayStation 5 Slim", snap.Titulo)
	require.True(t, snap.Preco.Equal(decimal.RequireFromString("3499.90")), "preço do aria-label, got %s", snap.Preco)
	require.True(t, snap.EstoqueDisponivel)
	require.True(t, snap.BadgePromo, "discount presente -> badge")
}

func TestParseMercadoLivre_NoCents(t *testing.T) {
	t.Parallel()
	html := `<div class="ui-pdp-price__second-line"><span class="andes-money-amount" aria-label="1200 reais">x</span></div>`
	snap, err := ParseMercadoLivre([]byte(html))
	require.NoError(t, err)
	require.True(t, snap.Preco.Equal(decimal.RequireFromString("1200")), "got %s", snap.Preco)
	require.False(t, snap.BadgePromo)
}

func TestParseMercadoLivre_Errors(t *testing.T) {
	t.Parallel()
	_, err := ParseMercadoLivre([]byte(`<body><h1 class="ui-pdp-title">Sem preço</h1></body>`))
	require.True(t, errors.Is(err, application.ErrSelectorNotMatched))
}
