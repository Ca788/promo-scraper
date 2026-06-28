package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"promo-scraper/internal/modules/scraping/collection/application"
)

const (
	kabumCatalogURL  = "https://servicespub.prod.api.aws.grupokabum.com.br/catalog/v2/products"
	kabumProductBase = "https://www.kabum.com.br/produto/"
	kabumPageSize    = 100
	kabumMaxPages    = 10
)

type KabumOffersCollector struct {
	client  *http.Client
	baseURL string
}

func NewKabumOffersCollector(timeout time.Duration) *KabumOffersCollector {
	if timeout <= 0 {
		timeout = DefaultHTTPTimeout
	}
	return &KabumOffersCollector{
		client:  &http.Client{Timeout: timeout},
		baseURL: kabumCatalogURL,
	}
}

type kabumProduct struct {
	ID         int64 `json:"id"`
	Attributes struct {
		Title              string          `json:"title"`
		Price              decimal.Decimal `json:"price"`
		PriceWithDiscount  decimal.Decimal `json:"price_with_discount"`
		DiscountPercentage int             `json:"discount_percentage"`
		Available          bool            `json:"available"`
	} `json:"attributes"`
}

type kabumResponse struct {
	Data []kabumProduct `json:"data"`
}

func (c *KabumOffersCollector) Offers(ctx context.Context, minDesconto, limit int) ([]application.PromoItem, error) {
	if limit <= 0 || limit > kabumPageSize*kabumMaxPages {
		limit = kabumPageSize * kabumMaxPages
	}

	out := make([]application.PromoItem, 0, limit)
	for page := 1; page <= kabumMaxPages && len(out) < limit; page++ {
		batch, err := c.fetchPage(ctx, page)
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}
		for _, p := range batch {
			if p.Attributes.DiscountPercentage < minDesconto {
				continue
			}
			if p.Attributes.DiscountPercentage <= 0 {
				continue
			}
			anterior := p.Attributes.Price
			out = append(out, application.PromoItem{
				Loja:          "kabum",
				Titulo:        p.Attributes.Title,
				Preco:         p.Attributes.PriceWithDiscount,
				PrecoAnterior: &anterior,
				DescontoPct:   p.Attributes.DiscountPercentage,
				EmPromocao:    true,
				Disponivel:    p.Attributes.Available,
				Link:          fmt.Sprintf("%s%d", kabumProductBase, p.ID),
			})
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (c *KabumOffersCollector) fetchPage(ctx context.Context, page int) ([]kabumProduct, error) {
	url := fmt.Sprintf("%s?page_number=%d&page_size=%d&sort=offers", c.baseURL, page, kabumPageSize)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("kabum offers build request: %w", application.ErrFetchFailed)
	}
	req.Header.Set("User-Agent", RandomUserAgent())
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, translateHTTPError(err, nil, url)
	}
	defer resp.Body.Close()

	if err := classifyStatus(resp.StatusCode, url); err != nil {
		return nil, err
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("kabum offers read body: %w", application.ErrFetchFailed)
	}

	var parsed kabumResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("kabum offers decode: %w", application.ErrSelectorNotMatched)
	}
	return parsed.Data, nil
}

var _ application.OffersProvider = (*KabumOffersCollector)(nil)
