package infrastructure

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"promo-scraper/internal/modules/scraping/collection/application"
)

func defaultSelectors() map[string]string {
	return map[string]string{
		"preco":   ".price",
		"titulo":  ".title",
		"sku":     ".sku",
		"estoque": ".stock",
		"badge":   ".badge",
	}
}

func TestParseProduct_HappyPath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		html      string
		wantPrice decimal.Decimal
		wantSKU   string
		wantTitle string
		wantStock bool
		wantBadge bool
		selectors map[string]string
	}{
		{
			name: "preco_milhar_BR",
			html: `<html><body>
				<span class="price">R$ 1.234,56</span>
				<h1 class="title">Placa de Video RTX 4070</h1>
				<span class="sku">SKU-001</span>
				<span class="stock">Em estoque</span>
				<div class="badge">Promo</div>
			</body></html>`,
			wantPrice: decimal.NewFromFloat(1234.56),
			wantSKU:   "SKU-001",
			wantTitle: "Placa de Video RTX 4070",
			wantStock: true,
			wantBadge: true,
			selectors: defaultSelectors(),
		},
		{
			name: "preco_pequeno_BR",
			html: `<html><body>
				<span class="price">R$ 99,90</span>
				<h1 class="title">Mouse Gamer</h1>
				<span class="sku">SKU-002</span>
				<span class="stock">Em estoque</span>
				<div class="badge">Oferta</div>
			</body></html>`,
			wantPrice: decimal.NewFromFloat(99.90),
			wantSKU:   "SKU-002",
			wantTitle: "Mouse Gamer",
			wantStock: true,
			wantBadge: true,
			selectors: defaultSelectors(),
		},
		{
			name: "preco_inteiro_milhar_BR",
			html: `<html><body>
				<span class="price">R$ 5.999,00</span>
				<h1 class="title">Notebook</h1>
				<span class="sku">SKU-003</span>
				<span class="stock">Disponivel</span>
				<div class="badge">Hot</div>
			</body></html>`,
			wantPrice: decimal.NewFromInt(5999),
			wantSKU:   "SKU-003",
			wantTitle: "Notebook",
			wantStock: true,
			wantBadge: true,
			selectors: defaultSelectors(),
		},
		{
			name: "sem_badge_e_sem_estoque",
			html: `<html><body>
				<span class="price">R$ 49,90</span>
				<h1 class="title">Cabo HDMI</h1>
				<span class="sku">SKU-004</span>
			</body></html>`,
			wantPrice: decimal.NewFromFloat(49.90),
			wantSKU:   "SKU-004",
			wantTitle: "Cabo HDMI",
			wantStock: false,
			wantBadge: false,
			selectors: defaultSelectors(),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			snap, err := ParseProduct([]byte(tc.html), tc.selectors)
			require.NoError(t, err)
			require.True(t, snap.Preco.Equal(tc.wantPrice), "preco esperado %s got %s", tc.wantPrice.String(), snap.Preco.String())
			require.Equal(t, tc.wantSKU, snap.SKU)
			require.Equal(t, tc.wantTitle, snap.Titulo)
			require.Equal(t, tc.wantStock, snap.EstoqueDisponivel)
			require.Equal(t, tc.wantBadge, snap.BadgePromo)
			require.False(t, snap.ColetadoEm.IsZero())
		})
	}
}

func TestParseProduct_ErrorCases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		html      string
		selectors map[string]string
		wantErr   error
	}{
		{
			name:      "seletor_preco_nao_casa",
			html:      `<html><body><h1 class="title">Sem preco aqui</h1></body></html>`,
			selectors: defaultSelectors(),
			wantErr:   application.ErrSelectorNotMatched,
		},
		{
			name:      "preco_texto_nao_numerico",
			html:      `<html><body><span class="price">R$ Esgotado</span></body></html>`,
			selectors: defaultSelectors(),
			wantErr:   application.ErrSelectorNotMatched,
		},
		{
			name:      "preco_vazio",
			html:      `<html><body><span class="price">   </span></body></html>`,
			selectors: defaultSelectors(),
			wantErr:   application.ErrSelectorNotMatched,
		},
		{
			name:      "preco_zero",
			html:      `<html><body><span class="price">R$ 0,00</span></body></html>`,
			selectors: defaultSelectors(),
			wantErr:   application.ErrInvalidPrice,
		},
		{
			name:      "preco_negativo",
			html:      `<html><body><span class="price">-R$ 10,00</span></body></html>`,
			selectors: defaultSelectors(),
			wantErr:   application.ErrInvalidPrice,
		},
		{
			name:      "selector_preco_ausente_no_map",
			html:      `<html><body><span class="price">R$ 99,90</span></body></html>`,
			selectors: map[string]string{"titulo": ".title"},
			wantErr:   application.ErrSelectorNotMatched,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := ParseProduct([]byte(tc.html), tc.selectors)
			require.Error(t, err)
			require.True(t, errors.Is(err, tc.wantErr), "expected %v got %v", tc.wantErr, err)
		})
	}
}

func TestNormalizeBRPrice(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  decimal.Decimal
	}{
		{"R$ 1.234,56", decimal.NewFromFloat(1234.56)},
		{"R$ 99,90", decimal.NewFromFloat(99.90)},
		{"R$ 5.999,00", decimal.NewFromInt(5999)},
		{"1234.56", decimal.NewFromFloat(1234.56)},
		{"R$1.000.000,00", decimal.NewFromInt(1000000)},
		{"R$ 10", decimal.NewFromInt(10)},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got, err := normalizeBRPrice(tc.input)
			require.NoError(t, err)
			require.True(t, got.Equal(tc.want), "input %q: want %s got %s", tc.input, tc.want.String(), got.String())
		})
	}
}
