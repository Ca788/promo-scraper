package application

import (
	"context"
	"log/slog"
	"sort"

	"github.com/shopspring/decimal"

	sources "promo-scraper/internal/modules/scraping/sources/domain"
)

type PromoItem struct {
	Loja          string
	Titulo        string
	Preco         decimal.Decimal
	PrecoAnterior *decimal.Decimal
	DescontoPct   int
	EmPromocao    bool
	Disponivel    bool
	Link          string
}

type OffersProvider interface {
	Offers(ctx context.Context, minDesconto, limit int) ([]PromoItem, error)
}

type NamedOffersProvider struct {
	Loja     string
	Provider OffersProvider
}

type CuratedTarget struct {
	Loja    string
	URL     string
	StoreID int64
}

type ListPromotionsInput struct {
	Loja        string
	MinDesconto int
	Limit       int
}

type ListPromotionsOutput struct {
	Itens []PromoItem
	Erros map[string]string
}

type ListPromotionsUseCase struct {
	providers []NamedOffersProvider
	collector Collector
	curated   []CuratedTarget
	logger    *slog.Logger
}

func NewListPromotionsUseCase(
	providers []NamedOffersProvider,
	collector Collector,
	curated []CuratedTarget,
	logger *slog.Logger,
) *ListPromotionsUseCase {
	if logger == nil {
		logger = slog.Default()
	}
	return &ListPromotionsUseCase{
		providers: providers,
		collector: collector,
		curated:   curated,
		logger:    logger,
	}
}

func (uc *ListPromotionsUseCase) Execute(ctx context.Context, in ListPromotionsInput) (ListPromotionsOutput, error) {
	out := ListPromotionsOutput{Erros: map[string]string{}}

	for _, p := range uc.providers {
		if p.Provider == nil {
			continue
		}
		if !wants(in.Loja, p.Loja) {
			continue
		}
		itens, err := p.Provider.Offers(ctx, in.MinDesconto, in.Limit)
		if err != nil {
			out.Erros[p.Loja] = err.Error()
			uc.logger.Warn("promotions provider_error",
				slog.String("loja", p.Loja),
				slog.String("error", err.Error()),
			)
			continue
		}
		out.Itens = append(out.Itens, itens...)
	}

	for _, target := range uc.curated {
		if !wants(in.Loja, target.Loja) {
			continue
		}
		item, err := uc.collectCurated(ctx, target)
		if err != nil {
			out.Erros[target.Loja] = err.Error()
			uc.logger.Warn("promotions curated_error",
				slog.String("loja", target.Loja),
				slog.String("url", target.URL),
				slog.String("error", err.Error()),
			)
			continue
		}
		if in.MinDesconto > 0 && item.DescontoPct < in.MinDesconto {
			continue
		}
		out.Itens = append(out.Itens, item)
	}

	sort.SliceStable(out.Itens, func(i, j int) bool {
		return out.Itens[i].DescontoPct > out.Itens[j].DescontoPct
	})

	if in.Limit > 0 && len(out.Itens) > in.Limit {
		out.Itens = out.Itens[:in.Limit]
	}

	return out, nil
}

func (uc *ListPromotionsUseCase) collectCurated(ctx context.Context, target CuratedTarget) (PromoItem, error) {
	snap, err := uc.collector.Collect(ctx, sources.Source{
		StoreID:  target.StoreID,
		URL:      target.URL,
		Strategy: sources.StrategyHeadless,
	})
	if err != nil {
		return PromoItem{}, err
	}
	return PromoItem{
		Loja:       target.Loja,
		Titulo:     snap.Titulo,
		Preco:      snap.Preco,
		EmPromocao: snap.BadgePromo,
		Disponivel: snap.EstoqueDisponivel,
		Link:       target.URL,
	}, nil
}

func wants(filter, loja string) bool {
	return filter == "" || filter == loja
}
