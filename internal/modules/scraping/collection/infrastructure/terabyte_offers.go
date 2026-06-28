package infrastructure

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/shopspring/decimal"

	"promo-scraper/internal/modules/scraping/collection/application"
	sources "promo-scraper/internal/modules/scraping/sources/domain"
)

const (
	terabyteOffersDefaultURL = "https://www.terabyteshop.com.br/hardware/ofertas"
	terabyteWaitSelector     = ".product-item__name"
)

var terabyteDiscountRe = regexp.MustCompile(`(\d{1,3})`)

type HTMLRenderer interface {
	Render(ctx context.Context, src sources.Source, waitSelector string) ([]byte, error)
}

type TerabyteOffersCollector struct {
	renderer HTMLRenderer
	baseURL  string
	storeID  int64
}

func NewTerabyteOffersCollector(renderer HTMLRenderer) *TerabyteOffersCollector {
	return &TerabyteOffersCollector{
		renderer: renderer,
		baseURL:  terabyteOffersDefaultURL,
		storeID:  102,
	}
}

func (c *TerabyteOffersCollector) Offers(ctx context.Context, minDesconto, limit int) ([]application.PromoItem, error) {
	if c.renderer == nil {
		return nil, fmt.Errorf("terabyte offers requer headless habilitado: %w", application.ErrFetchFailed)
	}

	body, err := c.renderer.Render(ctx, sources.Source{
		StoreID:  c.storeID,
		URL:      c.baseURL,
		Strategy: sources.StrategyHeadless,
	}, terabyteWaitSelector)
	if err != nil {
		return nil, err
	}

	all, err := ParseTerabyteOffers(body)
	if err != nil {
		return nil, err
	}

	out := make([]application.PromoItem, 0, len(all))
	for _, it := range all {
		if it.DescontoPct < minDesconto {
			continue
		}
		out = append(out, it)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func ParseTerabyteOffers(html []byte) ([]application.PromoItem, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("terabyte parse html: %w", application.ErrSelectorNotMatched)
	}

	cards := doc.Find("div.product-item")
	if cards.Length() == 0 {
		return nil, fmt.Errorf("terabyte offers cards não encontrados: %w", application.ErrSelectorNotMatched)
	}

	out := make([]application.PromoItem, 0, cards.Length())
	cards.Each(func(_ int, s *goquery.Selection) {
		item, ok := parseTerabyteCard(s)
		if !ok {
			return
		}
		out = append(out, item)
	})

	if len(out) == 0 {
		return nil, fmt.Errorf("terabyte offers nenhum card válido: %w", application.ErrSelectorNotMatched)
	}
	return out, nil
}

func parseTerabyteCard(s *goquery.Selection) (application.PromoItem, bool) {
	link, _ := s.Find("a.product-item__name").First().Attr("href")
	titulo := strings.TrimSpace(s.Find("a.product-item__name h2").First().Text())
	if titulo == "" || link == "" {
		return application.PromoItem{}, false
	}

	priceTxt := strings.TrimSpace(s.Find("div.product-item__new-price span").First().Text())
	preco, ok := parseBRPrice(priceTxt)
	if !ok || !preco.IsPositive() {
		return application.PromoItem{}, false
	}

	var precoAnterior *decimal.Decimal
	if oldTxt := strings.TrimSpace(s.Find("div.product-item__old-price del span").First().Text()); oldTxt != "" {
		if prev, ok := parseBRPrice(oldTxt); ok && prev.IsPositive() {
			precoAnterior = &prev
		}
	}

	desconto := 0
	if pctTxt := strings.TrimSpace(s.Find("div.product-promo-bar__percent .number").First().Text()); pctTxt != "" {
		if m := terabyteDiscountRe.FindString(pctTxt); m != "" {
			_, _ = fmt.Sscanf(m, "%d", &desconto)
		}
	}
	if desconto == 0 && precoAnterior != nil && precoAnterior.GreaterThan(preco) {
		diff := precoAnterior.Sub(preco)
		pct := diff.Div(*precoAnterior).Mul(decimal.NewFromInt(100))
		desconto = int(pct.IntPart())
	}

	disponivel := s.Find(".tss-card-badge-esgotado, .esgotado").Length() == 0

	return application.PromoItem{
		Loja:          "terabyte",
		Titulo:        titulo,
		Preco:         preco,
		PrecoAnterior: precoAnterior,
		DescontoPct:   desconto,
		EmPromocao:    desconto > 0 || precoAnterior != nil,
		Disponivel:    disponivel,
		Link:          strings.TrimSpace(link),
	}, true
}

func parseBRPrice(raw string) (decimal.Decimal, bool) {
	cleaned := strings.NewReplacer(
		"R$", "",
		"\u00a0", "",
		" ", "",
	).Replace(raw)
	cleaned = strings.TrimSpace(cleaned)
	cleaned = strings.ReplaceAll(cleaned, ".", "")
	cleaned = strings.Replace(cleaned, ",", ".", 1)
	if cleaned == "" {
		return decimal.Zero, false
	}
	v, err := decimal.NewFromString(cleaned)
	if err != nil {
		return decimal.Zero, false
	}
	return v, true
}

var _ application.OffersProvider = (*TerabyteOffersCollector)(nil)
