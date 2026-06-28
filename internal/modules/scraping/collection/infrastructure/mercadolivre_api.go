package infrastructure

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"promo-scraper/internal/modules/scraping/collection/application"
	sources "promo-scraper/internal/modules/scraping/sources/domain"
)

const defaultMeliBaseURL = "https://api.mercadolibre.com"

var mlItemIDPattern = regexp.MustCompile(`MLB-?(\d+)`)

type MercadoLivreAPICollector struct {
	client  *http.Client
	baseURL string
	token   string
}

func NewMercadoLivreAPICollector(token string, timeout time.Duration) *MercadoLivreAPICollector {
	if timeout <= 0 {
		timeout = DefaultHTTPTimeout
	}
	return &MercadoLivreAPICollector{
		client:  &http.Client{Timeout: timeout},
		baseURL: defaultMeliBaseURL,
		token:   strings.TrimSpace(token),
	}
}

type meliItem struct {
	ID                string           `json:"id"`
	Title             string           `json:"title"`
	Price             decimal.Decimal  `json:"price"`
	OriginalPrice     *decimal.Decimal `json:"original_price"`
	AvailableQuantity int              `json:"available_quantity"`
	Status            string           `json:"status"`
}

func (c *MercadoLivreAPICollector) Collect(ctx context.Context, src sources.Source) (sources.Snapshot, error) {
	if c.token == "" {
		return sources.Snapshot{}, fmt.Errorf("mercadolivre: MELI_ACCESS_TOKEN ausente: %w", application.ErrFetchFailed)
	}

	itemID, ok := extractMeliItemID(src.URL)
	if !ok {
		return sources.Snapshot{}, fmt.Errorf("mercadolivre: id do item não encontrado em %q: %w", src.URL, application.ErrSelectorNotMatched)
	}

	endpoint := fmt.Sprintf("%s/items/%s", c.baseURL, itemID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return sources.Snapshot{}, fmt.Errorf("mercadolivre: build request: %w", application.ErrFetchFailed)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return sources.Snapshot{}, translateHTTPError(err, nil, endpoint)
	}
	defer resp.Body.Close()

	if err := classifyStatus(resp.StatusCode, endpoint); err != nil {
		return sources.Snapshot{}, err
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return sources.Snapshot{}, fmt.Errorf("mercadolivre: read body: %w", application.ErrFetchFailed)
	}

	var item meliItem
	if err := json.Unmarshal(body, &item); err != nil {
		return sources.Snapshot{}, fmt.Errorf("mercadolivre: decode item: %w", application.ErrSelectorNotMatched)
	}

	if !item.Price.IsPositive() {
		return sources.Snapshot{}, fmt.Errorf("mercadolivre: price %s não é positivo: %w", item.Price.String(), application.ErrInvalidPrice)
	}

	badge := item.OriginalPrice != nil && item.OriginalPrice.GreaterThan(item.Price)

	return sources.Snapshot{
		SKU:               item.ID,
		Titulo:            strings.TrimSpace(item.Title),
		Preco:             item.Price,
		EstoqueDisponivel: item.AvailableQuantity > 0 && !strings.EqualFold(item.Status, "paused"),
		BadgePromo:        badge,
		ColetadoEm:        time.Now().UTC(),
	}, nil
}

func extractMeliItemID(rawURL string) (string, bool) {
	m := mlItemIDPattern.FindStringSubmatch(rawURL)
	if len(m) < 2 {
		return "", false
	}
	return "MLB" + m[1], true
}

var _ application.Collector = (*MercadoLivreAPICollector)(nil)
