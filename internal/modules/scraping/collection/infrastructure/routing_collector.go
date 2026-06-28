package infrastructure

import (
	"context"
	"fmt"
	"log/slog"

	"promo-scraper/internal/modules/scraping/collection/application"
	sources "promo-scraper/internal/modules/scraping/sources/domain"
)

type RoutingCollector struct {
	httpC     *HTTPCollector
	meliC     *MercadoLivreAPICollector
	headlessC *HeadlessCollector
	logger    *slog.Logger
}

func NewRoutingCollector(httpC *HTTPCollector, meliC *MercadoLivreAPICollector, headlessC *HeadlessCollector, logger *slog.Logger) *RoutingCollector {
	if logger == nil {
		logger = slog.Default()
	}
	return &RoutingCollector{httpC: httpC, meliC: meliC, headlessC: headlessC, logger: logger}
}

func (r *RoutingCollector) Collect(ctx context.Context, src sources.Source) (sources.Snapshot, error) {
	profile, ok := ProfileForURL(src.URL)
	if !ok {
		return r.httpC.Collect(ctx, src)
	}

	switch profile.Strategy {
	case sources.StrategyAPI:
		if r.meliC == nil {
			return sources.Snapshot{}, fmt.Errorf("loja %s requer API configurada: %w", profile.Nome, application.ErrFetchFailed)
		}
		return r.meliC.Collect(ctx, src)

	case sources.StrategyHeadless:
		if r.headlessC == nil {
			return sources.Snapshot{}, fmt.Errorf("loja %s requer headless habilitado (HEADLESS_ENABLED): %w", profile.Nome, application.ErrStrategyUnsupported)
		}
		body, err := r.headlessC.Render(ctx, src, profile.WaitSelector)
		if err != nil {
			return sources.Snapshot{}, err
		}
		return extractByMode(body, src, profile.Extract, profile.Selectors)

	case sources.StrategyHTTP:
		body, err := r.httpC.Fetch(ctx, src)
		if err != nil {
			return sources.Snapshot{}, err
		}
		return extractByMode(body, src, profile.Extract, profile.Selectors)

	default:
		return sources.Snapshot{}, fmt.Errorf("estratégia %q desconhecida para %s: %w", profile.Strategy, profile.Nome, application.ErrStrategyUnsupported)
	}
}

func extractByMode(body []byte, src sources.Source, mode ExtractMode, selectors map[string]string) (sources.Snapshot, error) {
	switch mode {
	case ExtractJSONLD:
		return ParseProductJSONLD(body)
	case ExtractMLAndes:
		return ParseMercadoLivre(body)
	case ExtractCSS:
		if len(selectors) == 0 {
			selectors = src.Selectors
		}
		return ParseProduct(body, selectors)
	default:
		return ParseProduct(body, src.Selectors)
	}
}

var _ application.Collector = (*RoutingCollector)(nil)
