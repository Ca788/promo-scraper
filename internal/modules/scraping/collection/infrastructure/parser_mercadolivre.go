package infrastructure

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/shopspring/decimal"

	"promo-scraper/internal/modules/scraping/collection/application"
	sources "promo-scraper/internal/modules/scraping/sources/domain"
)

var mlPriceLabel = regexp.MustCompile(`^(\d+) reais(?: com (\d+) centavos)?`)

const (
	mlMainPriceContainer = ".ui-pdp-price__second-line"
	mlTitleSelector      = ".ui-pdp-title"
	mlDiscountSelector   = ".ui-pdp-price__second-line .andes-money-amount__discount"
)

func ParseMercadoLivre(html []byte) (sources.Snapshot, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(html))
	if err != nil {
		return sources.Snapshot{}, fmt.Errorf("parse html: %w", application.ErrSelectorNotMatched)
	}

	label, ok := firstPriceLabel(doc)
	if !ok {
		return sources.Snapshot{}, fmt.Errorf("preço andes não encontrado: %w", application.ErrSelectorNotMatched)
	}

	price, err := priceFromLabel(label)
	if err != nil {
		return sources.Snapshot{}, fmt.Errorf("aria-label %q: %w", label, application.ErrSelectorNotMatched)
	}
	if !price.IsPositive() {
		return sources.Snapshot{}, fmt.Errorf("preço %s não é positivo: %w", price.String(), application.ErrInvalidPrice)
	}

	titulo := strings.TrimSpace(doc.Find(mlTitleSelector).First().Text())
	badge := doc.Find(mlDiscountSelector).Length() > 0

	return sources.Snapshot{
		SKU:               "",
		Titulo:            titulo,
		Preco:             price,
		EstoqueDisponivel: true,
		BadgePromo:        badge,
		ColetadoEm:        time.Now().UTC(),
	}, nil
}

func firstPriceLabel(doc *goquery.Document) (string, bool) {
	if label, ok := priceLabelWithin(doc.Find(mlMainPriceContainer)); ok {
		return label, true
	}
	if label, ok := priceLabelWithin(doc.Selection); ok {
		return label, true
	}
	return "", false
}

func priceLabelWithin(sel *goquery.Selection) (string, bool) {
	var found string
	sel.Find(".andes-money-amount[aria-label]").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		label, _ := s.Attr("aria-label")
		if mlPriceLabel.MatchString(strings.TrimSpace(label)) {
			found = strings.TrimSpace(label)
			return false
		}
		return true
	})
	return found, found != ""
}

func priceFromLabel(label string) (decimal.Decimal, error) {
	m := mlPriceLabel.FindStringSubmatch(label)
	if m == nil {
		return decimal.Zero, fmt.Errorf("formato inesperado")
	}
	cents := m[2]
	if cents == "" {
		cents = "00"
	}
	if len(cents) == 1 {
		cents = "0" + cents
	}
	return decimal.NewFromString(m[1] + "." + cents)
}
