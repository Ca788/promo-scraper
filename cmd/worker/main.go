package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"golang.org/x/time/rate"

	"promo-scraper/internal/config"
	"promo-scraper/internal/modules/scraping/collection/application"
	collectinfra "promo-scraper/internal/modules/scraping/collection/infrastructure"
	eventsinfra "promo-scraper/internal/modules/scraping/promo_events/infrastructure"
	sourcesinfra "promo-scraper/internal/modules/scraping/sources/infrastructure"
	"promo-scraper/internal/shared/clock"
	"promo-scraper/internal/shared/observability"
)

const (
	shutdownTimeout      = 10 * time.Second
	metricsReadHeader    = 5 * time.Second
	defaultQueueWorkers  = 4
	defaultQueueName     = "default"
	pgxPoolDefaultMaxCon = 10
)

func main() {
	bootLogger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		bootLogger.Error("config_load_failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, cfg, logger); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("worker_exit", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	pool, err := newPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("pgxpool: %w", err)
	}
	defer pool.Close()

	reg := prometheus.NewRegistry()
	reg.MustRegister(prometheus.NewGoCollector())
	reg.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	metrics := observability.New(reg)

	bucket := collectinfra.NewTokenBucketRegistry(
		rate.Every(time.Minute/time.Duration(cfg.RateLimitPerMin)),
		cfg.RateLimitBurst,
	)
	httpClient := collectinfra.NewHTTPCollector(cfg.HTTPTimeout, bucket, logger)

	var meliClient *collectinfra.MercadoLivreAPICollector
	if cfg.MeliAccessToken != "" {
		meliClient = collectinfra.NewMercadoLivreAPICollector(cfg.MeliAccessToken, cfg.HTTPTimeout)
	} else {
		logger.Warn("mercadolivre_api_disabled", slog.String("reason", "MELI_ACCESS_TOKEN ausente"))
	}
	var headlessClient *collectinfra.HeadlessCollector
	if cfg.HeadlessEnabled {
		headlessClient = collectinfra.NewHeadlessCollector(cfg.ChromePath, cfg.HeadlessTimeout, bucket, logger)
	} else {
		logger.Warn("headless_disabled", slog.String("reason", "HEADLESS_ENABLED ausente — ML/Terabyte ficam indisponíveis"))
	}
	collector := collectinfra.NewRoutingCollector(httpClient, meliClient, headlessClient, logger)

	srcRepo := sourcesinfra.NewPGSourceRepository(pool)
	evtRepo := eventsinfra.NewPGPromoEventRepository(pool)

	uc := application.NewCollectSourceUseCase(srcRepo, evtRepo, collector, clock.SystemClock{}, logger)

	workers := river.NewWorkers()
	river.AddWorker(workers, collectinfra.NewCollectSourceWorker(uc, srcRepo, metrics, logger))

	rc, err := river.NewClient[pgx.Tx](riverpgxv5.New(pool), &river.Config{
		Logger:      logger,
		MaxAttempts: cfg.MaxAttempts,
		Queues: map[string]river.QueueConfig{
			defaultQueueName: {MaxWorkers: defaultQueueWorkers},
		},
		Workers: workers,
	})
	if err != nil {
		return fmt.Errorf("river client: %w", err)
	}

	if err := rc.Start(ctx); err != nil {
		return fmt.Errorf("river start: %w", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer stopCancel()
		if err := rc.Stop(stopCtx); err != nil {
			logger.Error("river_stop_failed", slog.String("error", err.Error()))
		}
	}()

	scheduler := collectinfra.NewScheduler(srcRepo, rc, cfg.SchedulerInterval, cfg.WorkerOrgIDs, logger)
	schedDone := make(chan struct{})
	go func() {
		defer close(schedDone)
		if err := scheduler.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("scheduler_stopped", slog.String("error", err.Error()))
		}
	}()

	srv := newMetricsServer(cfg.PortMetrics, reg)
	srvErr := make(chan error, 1)
	go func() {
		logger.Info("metrics_server_listening", slog.String("addr", cfg.PortMetrics))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			srvErr <- err
			return
		}
		srvErr <- nil
	}()

	logger.Info("worker_started",
		slog.Duration("scheduler_interval", cfg.SchedulerInterval),
		slog.Int("max_attempts", cfg.MaxAttempts),
		slog.Int("rate_limit_per_min", cfg.RateLimitPerMin),
	)

	select {
	case <-ctx.Done():
		logger.Info("shutdown_signal_received")
	case err := <-srvErr:
		if err != nil {
			logger.Error("metrics_server_error", slog.String("error", err.Error()))
		}
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("metrics_server_shutdown_failed", slog.String("error", err.Error()))
	}

	<-schedDone

	return ctx.Err()
}

func newPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	if cfg.MaxConns < pgxPoolDefaultMaxCon {
		cfg.MaxConns = pgxPoolDefaultMaxCon
	}

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return pgxpool.NewWithConfig(connectCtx, cfg)
}

func newMetricsServer(addr string, reg *prometheus.Registry) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	return &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: metricsReadHeader,
	}
}
