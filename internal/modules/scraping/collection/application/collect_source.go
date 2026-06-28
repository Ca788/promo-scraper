package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	promoevents "promo-scraper/internal/modules/scraping/promo_events/domain"
	sources "promo-scraper/internal/modules/scraping/sources/domain"
	"promo-scraper/internal/shared/clock"
)

type CollectSourceKind string

const (
	KindSuccess           CollectSourceKind = "success"
	KindDedup             CollectSourceKind = "dedup"
	KindNoPriceDrop       CollectSourceKind = "no_price_drop"
	KindParseSelector     CollectSourceKind = "parse_error_selector"
	KindParseInvalidPrice CollectSourceKind = "parse_error_invalid_price"
	KindFetchFailed       CollectSourceKind = "fetch_failed"
	KindConcurrentUpdate  CollectSourceKind = "concurrent_update"
	KindInternal          CollectSourceKind = "internal"
)

type CollectSourceInput struct {
	OrgID    uuid.UUID
	SourceID int64
}

type CollectSourceOutput struct {
	Kind    CollectSourceKind
	StoreID int64
}

type CollectSourceUseCase struct {
	sources   sources.SourceRepository
	events    promoevents.PromoEventRepository
	collector Collector
	clock     clock.Clock
	logger    *slog.Logger
}

func NewCollectSourceUseCase(
	sourceRepo sources.SourceRepository,
	eventRepo promoevents.PromoEventRepository,
	collector Collector,
	clk clock.Clock,
	logger *slog.Logger,
) *CollectSourceUseCase {
	if logger == nil {
		logger = slog.Default()
	}
	return &CollectSourceUseCase{
		sources:   sourceRepo,
		events:    eventRepo,
		collector: collector,
		clock:     clk,
		logger:    logger,
	}
}

func (uc *CollectSourceUseCase) Execute(ctx context.Context, in CollectSourceInput) (CollectSourceOutput, error) {
	logger := uc.logger.With(
		slog.String("org_id", in.OrgID.String()),
		slog.Int64("source_id", in.SourceID),
	)

	src, err := uc.sources.GetByID(ctx, in.OrgID, in.SourceID)
	if err != nil {
		return CollectSourceOutput{Kind: KindInternal}, err
	}

	out := CollectSourceOutput{StoreID: src.StoreID}

	snap, err := uc.collector.Collect(ctx, src)
	if err != nil {
		switch {
		case errors.Is(err, ErrSelectorNotMatched), errors.Is(err, ErrInvalidPrice):
			kind := KindParseSelector
			if errors.Is(err, ErrInvalidPrice) {
				kind = KindParseInvalidPrice
			}
			out.Kind = kind
			if markErr := uc.sources.MarkError(ctx, in.OrgID, in.SourceID, err.Error()); markErr != nil {
				logger.Error("collect parse_error_mark_failed",
					slog.String("result", "parse_error"),
					slog.String("error", markErr.Error()),
				)
				return out, markErr
			}
			logger.Warn("collect parse_error",
				slog.String("result", "parse_error"),
				slog.String("error", err.Error()),
			)
			return out, nil
		case errors.Is(err, ErrFetchFailed):
			out.Kind = KindFetchFailed
			logger.Warn("collect fetch_error",
				slog.String("result", "fetch_error"),
				slog.String("error", err.Error()),
			)
			return out, fmt.Errorf("collect source %d: %w", in.SourceID, err)
		default:
			out.Kind = KindInternal
			logger.Error("collect unknown_error",
				slog.String("result", "unknown_error"),
				slog.String("error", err.Error()),
			)
			return out, fmt.Errorf("collect source %d: %w", in.SourceID, err)
		}
	}

	out.Kind = KindNoPriceDrop

	if src.LastSnapshot != nil && src.LastSnapshot.HasPriceDrop(snap.Preco) {
		event := promoevents.NewPromoEvent(src, snap, uc.clock)
		inserted, insertErr := uc.events.Insert(ctx, event)
		if insertErr != nil {
			out.Kind = KindInternal
			return out, fmt.Errorf("collect source %d insert event: %w", in.SourceID, insertErr)
		}
		if inserted {
			out.Kind = KindSuccess
			logger.Info("collect promo_event_materialized",
				slog.String("result", "event_inserted"),
			)
		} else {
			out.Kind = KindDedup
			logger.Info("collect promo_event_dedup",
				slog.String("result", "event_dedup"),
			)
		}
	}

	if err := uc.sources.UpdateAfterCollect(ctx, in.OrgID, in.SourceID, src.Version, snap); err != nil {
		if errors.Is(err, sources.ErrConcurrentUpdate) {
			out.Kind = KindConcurrentUpdate
			logger.Warn("collect concurrent_update",
				slog.String("result", "concurrent_update"),
			)
		} else {
			out.Kind = KindInternal
		}
		return out, err
	}

	logger.Info("collect success",
		slog.String("result", string(out.Kind)),
	)
	return out, nil
}
