package infrastructure

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"promo-scraper/internal/modules/scraping/collection/application"
	sources "promo-scraper/internal/modules/scraping/sources/domain"
)

const (
	DefaultHTTPTimeout = 10 * time.Second
	maxResponseBytes   = 5 * 1024 * 1024
)

type HTTPCollector struct {
	client *http.Client
	bucket *TokenBucketRegistry
	logger *slog.Logger
}

func NewHTTPCollector(timeout time.Duration, bucket *TokenBucketRegistry, logger *slog.Logger) *HTTPCollector {
	if timeout <= 0 {
		timeout = DefaultHTTPTimeout
	}
	if logger == nil {
		logger = slog.Default()
	}
	if bucket == nil {
		bucket = NewTokenBucketRegistry(0, 1)
	}
	return &HTTPCollector{
		client: &http.Client{Timeout: timeout},
		bucket: bucket,
		logger: logger,
	}
}

func (c *HTTPCollector) Collect(ctx context.Context, src sources.Source) (sources.Snapshot, error) {
	if src.Strategy != sources.StrategyHTTP {
		return sources.Snapshot{}, fmt.Errorf("strategy %q not supported by http collector: %w", src.Strategy, application.ErrFetchFailed)
	}

	if err := c.bucket.Acquire(ctx, src.StoreID); err != nil {
		return sources.Snapshot{}, fmt.Errorf("acquire bucket for store %d: %w", src.StoreID, application.ErrFetchFailed)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.URL, nil)
	if err != nil {
		return sources.Snapshot{}, fmt.Errorf("build request for %s: %w", src.URL, application.ErrFetchFailed)
	}
	req.Header.Set("User-Agent", RandomUserAgent())
	req.Header.Set("Accept-Language", "pt-BR,pt;q=0.9,en;q=0.8")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Encoding", "gzip")

	start := time.Now()
	resp, err := c.client.Do(req)
	duration := time.Since(start)

	logger := c.logger.With(
		slog.Int64("source_id", src.ID),
		slog.Int64("store_id", src.StoreID),
		slog.String("strategy", string(src.Strategy)),
		slog.Int64("duration_ms", duration.Milliseconds()),
	)

	if err != nil {
		translated := translateHTTPError(err, nil, src.URL)
		logger.Warn("http_collector fetch_error", slog.String("error", err.Error()))
		return sources.Snapshot{}, translated
	}
	defer resp.Body.Close()

	if err := classifyStatus(resp.StatusCode, src.URL); err != nil {
		logger.Warn("http_collector status_error",
			slog.Int("status_code", resp.StatusCode),
		)
		return sources.Snapshot{}, err
	}

	body, err := readBody(resp)
	if err != nil {
		logger.Warn("http_collector read_body_error", slog.String("error", err.Error()))
		return sources.Snapshot{}, fmt.Errorf("read body from %s: %w", src.URL, application.ErrFetchFailed)
	}

	snap, err := ParseProduct(body, src.Selectors)
	if err != nil {
		logger.Warn("http_collector parse_error", slog.String("error", err.Error()))
		return sources.Snapshot{}, err
	}

	logger.Info("http_collector success",
		slog.Int("status_code", resp.StatusCode),
	)
	return snap, nil
}

func translateHTTPError(err error, resp *http.Response, url string) error {
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return fmt.Errorf("timeout fetching %s: %w", url, application.ErrFetchFailed)
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return fmt.Errorf("timeout fetching %s: %w", url, application.ErrFetchFailed)
		}
		return fmt.Errorf("fetch %s: %w", url, application.ErrFetchFailed)
	}
	if resp == nil {
		return nil
	}
	return classifyStatus(resp.StatusCode, url)
}

func classifyStatus(status int, url string) error {
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == http.StatusTooManyRequests:
		return fmt.Errorf("status %d from %s: %w", status, url, application.ErrFetchFailed)
	case status >= 500:
		return fmt.Errorf("status %d from %s: %w", status, url, application.ErrFetchFailed)
	case status == http.StatusNotFound:
		return fmt.Errorf("status %d from %s: %w", status, url, application.ErrFetchFailed)
	case status >= 400:
		return fmt.Errorf("status %d from %s: %w", status, url, application.ErrFetchFailed)
	default:
		return fmt.Errorf("unexpected status %d from %s: %w", status, url, application.ErrFetchFailed)
	}
}

func readBody(resp *http.Response) ([]byte, error) {
	var reader io.Reader = resp.Body
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("gzip reader: %w", err)
		}
		defer gz.Close()
		reader = gz
	}
	limited := io.LimitReader(reader, maxResponseBytes)
	return io.ReadAll(limited)
}
