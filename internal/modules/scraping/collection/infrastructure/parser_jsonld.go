package infrastructure

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/shopspring/decimal"

	"promo-scraper/internal/modules/scraping/collection/application"
	sources "promo-scraper/internal/modules/scraping/sources/domain"
)

type jsonLDPrice string

func (p *jsonLDPrice) UnmarshalJSON(data []byte) error {
	*p = jsonLDPrice(strings.Trim(string(data), `"`))
	return nil
}

type jsonLDOffer struct {
	Price         jsonLDPrice `json:"price"`
	PriceCurrency string      `json:"priceCurrency"`
	Availability  string      `json:"availability"`
}

type jsonLDProduct struct {
	Type   json.RawMessage `json:"@type"`
	Name   string          `json:"name"`
	SKU    string          `json:"sku"`
	Offers json.RawMessage `json:"offers"`
}

func ParseProductJSONLD(html []byte) (sources.Snapshot, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(html))
	if err != nil {
		return sources.Snapshot{}, fmt.Errorf("parse html: %w", application.ErrSelectorNotMatched)
	}

	product, offer, found := findProductOffer(doc)
	if !found {
		return sources.Snapshot{}, fmt.Errorf("nenhum Product em ld+json: %w", application.ErrSelectorNotMatched)
	}

	priceText := strings.TrimSpace(string(offer.Price))
	if priceText == "" {
		return sources.Snapshot{}, fmt.Errorf("offer sem price: %w", application.ErrSelectorNotMatched)
	}

	price, err := decimal.NewFromString(priceText)
	if err != nil {
		return sources.Snapshot{}, fmt.Errorf("price %q inválido: %w", priceText, application.ErrSelectorNotMatched)
	}
	if !price.IsPositive() {
		return sources.Snapshot{}, fmt.Errorf("price %s não é positivo: %w", price.String(), application.ErrInvalidPrice)
	}

	return sources.Snapshot{
		SKU:               strings.TrimSpace(product.SKU),
		Titulo:            strings.TrimSpace(product.Name),
		Preco:             price,
		EstoqueDisponivel: strings.Contains(strings.ToLower(offer.Availability), "instock"),
		BadgePromo:        false,
		ColetadoEm:        time.Now().UTC(),
	}, nil
}

func findProductOffer(doc *goquery.Document) (jsonLDProduct, jsonLDOffer, bool) {
	var result jsonLDProduct
	var offer jsonLDOffer
	found := false

	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		for _, node := range flattenLDNodes([]byte(s.Text())) {
			var prod jsonLDProduct
			if err := json.Unmarshal(node, &prod); err != nil {
				continue
			}
			if !isProductType(prod.Type) || len(prod.Offers) == 0 {
				continue
			}
			off, ok := firstOffer(prod.Offers)
			if !ok {
				continue
			}
			result, offer, found = prod, off, true
			return false
		}
		return true
	})

	return result, offer, found
}

func flattenLDNodes(raw []byte) []json.RawMessage {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil
	}

	if raw[0] == '[' {
		var arr []json.RawMessage
		if err := json.Unmarshal(raw, &arr); err != nil {
			return nil
		}
		return arr
	}

	var wrapper struct {
		Graph []json.RawMessage `json:"@graph"`
	}
	if err := json.Unmarshal(raw, &wrapper); err == nil && len(wrapper.Graph) > 0 {
		return wrapper.Graph
	}

	return []json.RawMessage{raw}
}

func isProductType(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return strings.EqualFold(single, "Product")
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err == nil {
		for _, t := range many {
			if strings.EqualFold(t, "Product") {
				return true
			}
		}
	}
	return false
}

func firstOffer(raw json.RawMessage) (jsonLDOffer, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return jsonLDOffer{}, false
	}
	if raw[0] == '[' {
		var arr []jsonLDOffer
		if err := json.Unmarshal(raw, &arr); err != nil || len(arr) == 0 {
			return jsonLDOffer{}, false
		}
		return arr[0], true
	}
	var single jsonLDOffer
	if err := json.Unmarshal(raw, &single); err != nil {
		return jsonLDOffer{}, false
	}
	return single, true
}
