package infrastructure

import (
	"net/url"
	"strings"

	sources "promo-scraper/internal/modules/scraping/sources/domain"
)

type ExtractMode string

const (
	ExtractCSS     ExtractMode = "css"
	ExtractJSONLD  ExtractMode = "jsonld"
	ExtractAPI     ExtractMode = "api"
	ExtractMLAndes ExtractMode = "ml_andes"
)

type StoreProfile struct {
	Host         string
	Nome         string
	Strategy     sources.Strategy
	Extract      ExtractMode
	Selectors    map[string]string
	WaitSelector string
}

var storeProfiles = map[string]StoreProfile{
	"kabum.com.br": {
		Host:     "kabum.com.br",
		Nome:     "KaBuM!",
		Strategy: sources.StrategyHTTP,
		Extract:  ExtractJSONLD,
	},
	"mercadolivre.com.br": {
		Host:         "mercadolivre.com.br",
		Nome:         "Mercado Livre",
		Strategy:     sources.StrategyHeadless,
		Extract:      ExtractMLAndes,
		WaitSelector: ".ui-pdp-price__second-line",
	},
	"terabyteshop.com.br": {
		Host:         "terabyteshop.com.br",
		Nome:         "Terabyte",
		Strategy:     sources.StrategyHeadless,
		Extract:      ExtractCSS,
		Selectors:    map[string]string{"preco": "#valVista", "titulo": "h1"},
		WaitSelector: "#valVista",
	},
	"amazon.com.br": {
		Host:     "amazon.com.br",
		Nome:     "Amazon",
		Strategy: sources.StrategyHeadless,
		Extract:  ExtractCSS,
		Selectors: map[string]string{
			"preco":  ".a-price .a-offscreen",
			"titulo": "#productTitle",
		},
		WaitSelector: "#productTitle",
	},
	"shopee.com.br": {
		Host:     "shopee.com.br",
		Nome:     "Shopee",
		Strategy: sources.StrategyHeadless,
		Extract:  ExtractCSS,
	},
}

func ProfileForURL(rawURL string) (StoreProfile, bool) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return StoreProfile{}, false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return StoreProfile{}, false
	}

	for key, profile := range storeProfiles {
		if host == key || strings.HasSuffix(host, "."+key) {
			return profile, true
		}
	}
	return StoreProfile{}, false
}
