package infrastructure

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/shopspring/decimal"

	"promo-scraper/internal/modules/scraping/collection/application"
	sources "promo-scraper/internal/modules/scraping/sources/domain"
)

const (
	selectorPreco   = "preco"
	selectorTitulo  = "titulo"
	selectorSKU     = "sku"
	selectorEstoque = "estoque"
	selectorBadge   = "badge"
)

func ParseProduct(html []byte, selectors map[string]string) (sources.Snapshot, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(html))
	if err != nil {
		return sources.Snapshot{}, fmt.Errorf("parse html: %w", application.ErrSelectorNotMatched)
	}

	priceSel := selectors[selectorPreco]
	if priceSel == "" {
		return sources.Snapshot{}, fmt.Errorf("missing price selector: %w", application.ErrSelectorNotMatched)
	}

	priceNodes := doc.Find(priceSel)
	if priceNodes.Length() == 0 {
		return sources.Snapshot{}, fmt.Errorf("price selector %q matched no nodes: %w", priceSel, application.ErrSelectorNotMatched)
	}

	priceText := strings.TrimSpace(priceNodes.First().Text())
	if priceText == "" {
		return sources.Snapshot{}, fmt.Errorf("price selector %q matched empty text: %w", priceSel, application.ErrSelectorNotMatched)
	}

	price, err := normalizeBRPrice(priceText)
	if err != nil {
		return sources.Snapshot{}, fmt.Errorf("normalize price %q: %w", priceText, application.ErrSelectorNotMatched)
	}

	if !price.IsPositive() {
		return sources.Snapshot{}, fmt.Errorf("price %s is not positive: %w", price.String(), application.ErrInvalidPrice)
	}

	titulo := extractText(doc, selectors[selectorTitulo])
	sku := extractText(doc, selectors[selectorSKU])
	estoque := hasNonEmptyMatch(doc, selectors[selectorEstoque])
	badge := hasNonEmptyMatch(doc, selectors[selectorBadge])

	return sources.Snapshot{
		SKU:               sku,
		Titulo:            titulo,
		Preco:             price,
		EstoqueDisponivel: estoque,
		BadgePromo:        badge,
		ColetadoEm:        time.Now().UTC(),
	}, nil
}

func extractText(doc *goquery.Document, selector string) string {
	if selector == "" {
		return ""
	}
	node := doc.Find(selector).First()
	if node.Length() == 0 {
		return ""
	}
	return strings.TrimSpace(node.Text())
}

func hasNonEmptyMatch(doc *goquery.Document, selector string) bool {
	if selector == "" {
		return false
	}
	node := doc.Find(selector).First()
	if node.Length() == 0 {
		return false
	}
	return strings.TrimSpace(node.Text()) != ""
}

func normalizeBRPrice(s string) (decimal.Decimal, error) {
	cleaned := strings.TrimSpace(s)
	cleaned = strings.ReplaceAll(cleaned, "R$", "")
	cleaned = strings.ReplaceAll(cleaned, "\u00a0", "")
	cleaned = strings.ReplaceAll(cleaned, " ", "")
	cleaned = strings.ReplaceAll(cleaned, "\t", "")
	cleaned = strings.ReplaceAll(cleaned, "\n", "")
	cleaned = strings.ReplaceAll(cleaned, "\r", "")

	if cleaned == "" {
		return decimal.Zero, fmt.Errorf("empty price string")
	}

	negative := false
	if strings.HasPrefix(cleaned, "-") {
		negative = true
		cleaned = strings.TrimPrefix(cleaned, "-")
	}

	if strings.ContainsAny(cleaned, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz") {
		return decimal.Zero, fmt.Errorf("price %q contains letters", s)
	}

	lastComma := strings.LastIndex(cleaned, ",")
	lastDot := strings.LastIndex(cleaned, ".")

	var normalized string
	switch {
	case lastComma == -1 && lastDot == -1:
		normalized = cleaned
	case lastComma > lastDot:
		withoutThousands := strings.ReplaceAll(cleaned, ".", "")
		normalized = strings.Replace(withoutThousands, ",", ".", 1)
	case lastDot > lastComma:
		normalized = strings.ReplaceAll(cleaned, ",", "")
	default:
		normalized = cleaned
	}

	value, err := decimal.NewFromString(normalized)
	if err != nil {
		return decimal.Zero, fmt.Errorf("parse decimal %q: %w", normalized, err)
	}

	if negative {
		value = value.Neg()
	}
	return value, nil
}
