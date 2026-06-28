package infrastructure

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/chromedp/chromedp"

	"promo-scraper/internal/modules/scraping/collection/application"
	sources "promo-scraper/internal/modules/scraping/sources/domain"
)

const (
	DefaultHeadlessTimeout = 30 * time.Second
	settleDelay            = 1500 * time.Millisecond
)

type HeadlessCollector struct {
	execPath string
	timeout  time.Duration
	bucket   *TokenBucketRegistry
	logger   *slog.Logger
}

func NewHeadlessCollector(execPath string, timeout time.Duration, bucket *TokenBucketRegistry, logger *slog.Logger) *HeadlessCollector {
	if timeout <= 0 {
		timeout = DefaultHeadlessTimeout
	}
	if logger == nil {
		logger = slog.Default()
	}
	if bucket == nil {
		bucket = NewTokenBucketRegistry(0, 1)
	}
	return &HeadlessCollector{execPath: execPath, timeout: timeout, bucket: bucket, logger: logger}
}

func (c *HeadlessCollector) Render(ctx context.Context, src sources.Source, waitSelector string) ([]byte, error) {
	if err := c.bucket.Acquire(ctx, src.StoreID); err != nil {
		return nil, fmt.Errorf("acquire bucket for store %d: %w", src.StoreID, application.ErrFetchFailed)
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Headless,
		chromedp.NoSandbox,
		chromedp.DisableGPU,
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("lang", "pt-BR"),
		chromedp.UserAgent(RandomUserAgent()),
		chromedp.WindowSize(1920, 1080),
	)
	if c.execPath != "" {
		opts = append(opts, chromedp.ExecPath(c.execPath))
	}

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	defer cancelAlloc()
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()
	runCtx, cancelRun := context.WithTimeout(browserCtx, c.timeout)
	defer cancelRun()

	start := time.Now()
	tasks := chromedp.Tasks{
		chromedp.Navigate(src.URL),
		chromedp.WaitReady("body", chromedp.ByQuery),
	}
	if waitSelector != "" {
		tasks = append(tasks,
			chromedp.WaitVisible(waitSelector, chromedp.ByQuery),
			chromedp.Sleep(settleDelay),
		)
	}

	var html string
	tasks = append(tasks, chromedp.OuterHTML("html", &html, chromedp.ByQuery))

	logger := c.logger.With(
		slog.Int64("source_id", src.ID),
		slog.Int64("store_id", src.StoreID),
		slog.String("strategy", string(sources.StrategyHeadless)),
	)

	if err := chromedp.Run(runCtx, tasks); err != nil {
		logger.Warn("headless_collector render_error",
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("render %s: %w", src.URL, application.ErrFetchFailed)
	}

	logger.Info("headless_collector success",
		slog.Int64("duration_ms", time.Since(start).Milliseconds()),
		slog.Int("html_bytes", len(html)),
	)
	return []byte(html), nil
}
