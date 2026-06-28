package infrastructure

import (
	"testing"

	"github.com/stretchr/testify/require"

	sources "promo-scraper/internal/modules/scraping/sources/domain"
)

func TestProfileForURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		url          string
		wantFound    bool
		wantStrategy sources.Strategy
		wantExtract  ExtractMode
	}{
		{"kabum www", "https://www.kabum.com.br/produto/401293/x", true, sources.StrategyHTTP, ExtractJSONLD},
		{"mercadolivre", "https://www.mercadolivre.com.br/x/p/MLB123", true, sources.StrategyHeadless, ExtractMLAndes},
		{"mercadolivre subdominio", "https://produto.mercadolivre.com.br/MLB-123-x", true, sources.StrategyHeadless, ExtractMLAndes},
		{"amazon headless", "https://www.amazon.com.br/dp/B0CHX1W1XY", true, sources.StrategyHeadless, ExtractCSS},
		{"terabyte headless", "https://www.terabyteshop.com.br/produto/26233/x", true, sources.StrategyHeadless, ExtractCSS},
		{"shopee headless", "https://shopee.com.br/produto-i.1.2", true, sources.StrategyHeadless, ExtractCSS},
		{"host desconhecido", "https://loja-qualquer.example.com/p/1", false, "", ""},
		{"url invalida", "://sem-esquema", false, "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			profile, ok := ProfileForURL(tc.url)
			require.Equal(t, tc.wantFound, ok)
			if !tc.wantFound {
				return
			}
			require.Equal(t, tc.wantStrategy, profile.Strategy)
			require.Equal(t, tc.wantExtract, profile.Extract)
		})
	}
}
