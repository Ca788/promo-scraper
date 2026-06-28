package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/robfig/cron/v3"
	"golang.org/x/time/rate"

	"promo-scraper/internal/config"
	"promo-scraper/internal/modules/scraping/collection/application"
	collectinfra "promo-scraper/internal/modules/scraping/collection/infrastructure"
)

const (
	readHeaderTimeout = 5 * time.Second
	shutdownTimeout   = 10 * time.Second
	cronSchedule      = "0 8,12,20 * * *"
	cronTimeZone      = "America/Sao_Paulo"
)

var curatedTargets = []application.CuratedTarget{
	{Loja: "mercado livre", StoreID: 101, URL: "https://www.mercadolivre.com.br/notebook-positivo-vision-i15m-com-minitela-intel-core-3-8gb-de-ram-ssd-de-256gb-e-tela-de-156-full-hd-ips-antirreflexo/p/MLB56961557"},
	{Loja: "terabyte", StoreID: 102, URL: "https://www.terabyteshop.com.br/produto/13953/ssd-patriot-p300-256gb-nvme-leitura-1700mbs-e-gravacao-1100mbs-p300p256gm28us"},
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		logger.Warn("config_load_warning", slog.String("error", err.Error()))
	}

	uc := buildUseCase(cfg, logger)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	scheduler := startCron(ctx, uc, logger)
	defer scheduler.Stop()

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           newRouter(uc, logger),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	logger.Info("server_listening", slog.String("addr", srv.Addr), slog.String("cron", cronSchedule))
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server_error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func buildUseCase(cfg config.Config, logger *slog.Logger) *application.ListPromotionsUseCase {
	bucket := collectinfra.NewTokenBucketRegistry(rate.Inf, 1)
	httpC := collectinfra.NewHTTPCollector(cfg.HTTPTimeout, bucket, logger)

	timeout := cfg.HeadlessTimeout
	if timeout <= 0 {
		timeout = collectinfra.DefaultHeadlessTimeout
	}
	headlessC := collectinfra.NewHeadlessCollector(cfg.ChromePath, timeout, bucket, logger)
	router := collectinfra.NewRoutingCollector(httpC, nil, headlessC, logger)

	kabum := collectinfra.NewKabumOffersCollector(cfg.HTTPTimeout)
	return application.NewListPromotionsUseCase(kabum, router, curatedTargets, logger)
}

func newRouter(uc *application.ListPromotionsUseCase, logger *slog.Logger) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(90 * time.Second))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	r.Get("/promocoes", promotionsHandler(uc, logger))
	return r
}

func promotionsHandler(uc *application.ListPromotionsUseCase, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		in := application.ListPromotionsInput{
			Loja:        req.URL.Query().Get("loja"),
			MinDesconto: atoiDefault(req.URL.Query().Get("min_desconto"), 0),
			Limit:       atoiDefault(req.URL.Query().Get("limit"), 50),
		}

		out, err := uc.Execute(req.Context(), in)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"total":       len(out.Itens),
			"coletado_em": time.Now().UTC().Format(time.RFC3339),
			"erros":       out.Erros,
			"promocoes":   toJSON(out.Itens),
		})
	}
}

type promoJSON struct {
	Loja          string  `json:"loja"`
	Titulo        string  `json:"titulo"`
	Preco         string  `json:"preco"`
	PrecoAnterior *string `json:"preco_anterior,omitempty"`
	DescontoPct   int     `json:"desconto_pct"`
	EmPromocao    bool    `json:"em_promocao"`
	Disponivel    bool    `json:"disponivel"`
	Link          string  `json:"link"`
}

func toJSON(itens []application.PromoItem) []promoJSON {
	out := make([]promoJSON, 0, len(itens))
	for _, it := range itens {
		j := promoJSON{
			Loja:        it.Loja,
			Titulo:      it.Titulo,
			Preco:       it.Preco.StringFixed(2),
			DescontoPct: it.DescontoPct,
			EmPromocao:  it.EmPromocao,
			Disponivel:  it.Disponivel,
			Link:        it.Link,
		}
		if it.PrecoAnterior != nil {
			s := it.PrecoAnterior.StringFixed(2)
			j.PrecoAnterior = &s
		}
		out = append(out, j)
	}
	return out
}

func startCron(ctx context.Context, uc *application.ListPromotionsUseCase, logger *slog.Logger) *cron.Cron {
	loc, err := time.LoadLocation(cronTimeZone)
	if err != nil {
		loc = time.UTC
	}
	c := cron.New(cron.WithLocation(loc))
	_, err = c.AddFunc(cronSchedule, func() {
		runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		out, err := uc.Execute(runCtx, application.ListPromotionsInput{Limit: 500})
		if err != nil {
			logger.Error("cron_promocoes_error", slog.String("error", err.Error()))
			return
		}
		logger.Info("cron_promocoes_done",
			slog.Int("total", len(out.Itens)),
			slog.Any("erros", out.Erros),
		)
	})
	if err != nil {
		logger.Error("cron_schedule_invalid", slog.String("error", err.Error()))
		return c
	}
	c.Start()
	return c
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return def
	}
	return n
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
