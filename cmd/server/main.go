package main

import (
	"context"
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
	"promo-scraper/internal/shared/envelope"
	"promo-scraper/internal/shared/pagination"
)

// useCaseHardCap limita o total de itens coletados em memória para que a
// paginação in-memory tenha material suficiente sem inflar latência/RAM.
const useCaseHardCap = 500

const (
	readHeaderTimeout = 5 * time.Second
	shutdownTimeout   = 10 * time.Second
	cronSchedule      = "0 8,12,20 * * *"
	cronTimeZone      = "America/Sao_Paulo"
)

// curatedTargets mantém alvos pontuais para inspeção via Collector (não
// integrados ao ranking principal por padrão; cada loja com listagem própria
// é coletada pelos OffersProviders abaixo).
var curatedTargets []application.CuratedTarget

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

	providers := []application.NamedOffersProvider{
		{Loja: "kabum", Provider: collectinfra.NewKabumOffersCollector(cfg.HTTPTimeout)},
		{Loja: "mercado livre", Provider: collectinfra.NewMercadoLivreOffersCollector(cfg.HTTPTimeout)},
		{Loja: "terabyte", Provider: collectinfra.NewTerabyteOffersCollector(headlessC)},
	}
	return application.NewListPromotionsUseCase(providers, router, curatedTargets, logger)
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
		q := req.URL.Query()
		page := pagination.FromQuery(q)

		in := application.ListPromotionsInput{
			Loja:        q.Get("loja"),
			MinDesconto: atoiDefault(q.Get("min_desconto"), 0),
			Limit:       useCaseHardCap,
		}

		out, err := uc.Execute(req.Context(), in)
		if err != nil {
			envelope.WriteError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}

		dtos := toJSON(out.Itens)
		window, meta := pagination.Slice(dtos, page)
		envelope.WriteJSON(w, http.StatusOK, envelope.NewCollection(window, meta, out.Erros))
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
