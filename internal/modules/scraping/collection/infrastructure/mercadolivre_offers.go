package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"promo-scraper/internal/modules/scraping/collection/application"
)

const (
	mlOffersDefaultURL = "https://www.mercadolivre.com.br/ofertas"
	mlOffersPageSize   = 48
	mlOffersMaxPages   = 6
)

var mlDiscountRe = regexp.MustCompile(`(\d{1,3})\s*%`)

type MercadoLivreOffersCollector struct {
	client  *http.Client
	baseURL string
}

func NewMercadoLivreOffersCollector(timeout time.Duration) *MercadoLivreOffersCollector {
	if timeout <= 0 {
		timeout = DefaultHTTPTimeout
	}
	return &MercadoLivreOffersCollector{
		client:  &http.Client{Timeout: timeout},
		baseURL: mlOffersDefaultURL,
	}
}

func (c *MercadoLivreOffersCollector) Offers(ctx context.Context, minDesconto, limit int) ([]application.PromoItem, error) {
	if limit <= 0 || limit > mlOffersPageSize*mlOffersMaxPages {
		limit = mlOffersPageSize * mlOffersMaxPages
	}

	out := make([]application.PromoItem, 0, limit)
	for page := 1; page <= mlOffersMaxPages && len(out) < limit; page++ {
		batch, err := c.fetchPage(ctx, page)
		if err != nil {
			if page == 1 {
				return nil, err
			}
			break
		}
		if len(batch) == 0 {
			break
		}
		for _, item := range batch {
			if item.DescontoPct < minDesconto {
				continue
			}
			out = append(out, item)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (c *MercadoLivreOffersCollector) fetchPage(ctx context.Context, page int) ([]application.PromoItem, error) {
	pageURL := c.baseURL
	if page > 1 {
		pageURL = fmt.Sprintf("%s?page=%d", c.baseURL, page)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("mercadolivre offers build request: %w", application.ErrFetchFailed)
	}
	req.Header.Set("User-Agent", RandomUserAgent())
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "pt-BR,pt;q=0.9,en;q=0.8")
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, translateHTTPError(err, nil, pageURL)
	}
	defer resp.Body.Close()

	if err := classifyStatus(resp.StatusCode, pageURL); err != nil {
		return nil, err
	}

	body, err := readBody(resp)
	if err != nil {
		return nil, fmt.Errorf("mercadolivre offers read body: %w", application.ErrFetchFailed)
	}

	items, err := ParseMercadoLivreOffers(body)
	if err != nil {
		return nil, err
	}
	return items, nil
}

func ParseMercadoLivreOffers(html []byte) ([]application.PromoItem, error) {
	raw, ok := extractJSONArray(string(html), `"items":`)
	if !ok {
		return nil, fmt.Errorf("mercadolivre items JSON não encontrado: %w", application.ErrSelectorNotMatched)
	}

	var items []mlOfferItem
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("mercadolivre items decode: %w", application.ErrSelectorNotMatched)
	}

	out := make([]application.PromoItem, 0, len(items))
	for _, it := range items {
		promo, ok := it.toPromo()
		if !ok {
			continue
		}
		out = append(out, promo)
	}
	return out, nil
}

type mlOfferItem struct {
	Position int    `json:"position"`
	Type     string `json:"type"`
	Card     struct {
		Metadata struct {
			URL       string `json:"url"`
			URLParams string `json:"url_params"`
		} `json:"metadata"`
		Components []mlComponent `json:"components"`
	} `json:"card"`
}

type mlComponent struct {
	Type  string         `json:"type"`
	ID    string         `json:"id"`
	Title *mlTitle       `json:"title,omitempty"`
	Price *mlPriceComp   `json:"price,omitempty"`
	Extra map[string]any `json:"-"`
}

type mlTitle struct {
	Text string `json:"text"`
}

type mlPriceComp struct {
	PreviousPrice *mlPriceValue `json:"previous_price,omitempty"`
	CurrentPrice  *mlPriceValue `json:"current_price,omitempty"`
	DiscountLabel *mlLabel      `json:"discount_label,omitempty"`
}

type mlPriceValue struct {
	Value    json.Number `json:"value"`
	Currency string      `json:"currency"`
}

type mlLabel struct {
	Text string `json:"text"`
}

func (it mlOfferItem) toPromo() (application.PromoItem, bool) {
	var titulo string
	var priceComp *mlPriceComp
	for i := range it.Card.Components {
		c := &it.Card.Components[i]
		switch c.Type {
		case "title":
			if c.Title != nil {
				titulo = strings.TrimSpace(c.Title.Text)
			}
		case "price":
			priceComp = c.Price
		}
	}
	if priceComp == nil || priceComp.CurrentPrice == nil {
		return application.PromoItem{}, false
	}

	preco, err := decimal.NewFromString(string(priceComp.CurrentPrice.Value))
	if err != nil || !preco.IsPositive() {
		return application.PromoItem{}, false
	}

	var precoAnterior *decimal.Decimal
	if priceComp.PreviousPrice != nil {
		if prev, err := decimal.NewFromString(string(priceComp.PreviousPrice.Value)); err == nil && prev.IsPositive() {
			precoAnterior = &prev
		}
	}

	desconto := 0
	if priceComp.DiscountLabel != nil {
		if m := mlDiscountRe.FindStringSubmatch(priceComp.DiscountLabel.Text); len(m) == 2 {
			_, _ = fmt.Sscanf(m[1], "%d", &desconto)
		}
	}
	if desconto == 0 && precoAnterior != nil && precoAnterior.GreaterThan(preco) {
		diff := precoAnterior.Sub(preco)
		pct := diff.Div(*precoAnterior).Mul(decimal.NewFromInt(100))
		desconto = int(pct.IntPart())
	}

	link := buildMLLink(it.Card.Metadata.URL, it.Card.Metadata.URLParams)

	return application.PromoItem{
		Loja:          "mercado livre",
		Titulo:        titulo,
		Preco:         preco,
		PrecoAnterior: precoAnterior,
		DescontoPct:   desconto,
		EmPromocao:    desconto > 0 || precoAnterior != nil,
		Disponivel:    true,
		Link:          link,
	}, true
}

func buildMLLink(rawURL, params string) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return ""
	}
	link := rawURL
	if !strings.HasPrefix(link, "http://") && !strings.HasPrefix(link, "https://") {
		link = "https://" + link
	}
	params = strings.TrimSpace(params)
	if params != "" && !strings.Contains(link, "?") {
		link += params
	}
	if u, err := url.Parse(link); err == nil {
		u.Fragment = ""
		return u.String()
	}
	return link
}

func extractJSONArray(s, marker string) (string, bool) {
	idx := strings.Index(s, marker)
	if idx < 0 {
		return "", false
	}
	i := idx + len(marker)
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n') {
		i++
	}
	if i >= len(s) || s[i] != '[' {
		return "", false
	}
	depth := 0
	inStr := false
	esc := false
	start := i
	for ; i < len(s); i++ {
		c := s[i]
		if esc {
			esc = false
			continue
		}
		if inStr {
			switch c {
			case '\\':
				esc = true
			case '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return s[start : i+1], true
			}
		}
	}
	return "", false
}

var _ application.OffersProvider = (*MercadoLivreOffersCollector)(nil)
