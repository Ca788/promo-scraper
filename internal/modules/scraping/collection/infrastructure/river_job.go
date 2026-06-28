package infrastructure

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"promo-scraper/internal/modules/scraping/collection/application"
	sources "promo-scraper/internal/modules/scraping/sources/domain"
	"promo-scraper/internal/shared/observability"
)

const CollectSourceKind = "collect_source"

type CollectSourceArgs struct {
	OrgID    uuid.UUID `json:"org_id"`
	SourceID int64     `json:"source_id"`
	Version  int       `json:"version"`
}

func (CollectSourceArgs) Kind() string { return CollectSourceKind }

type CollectSourceWorker struct {
	river.WorkerDefaults[CollectSourceArgs]
	uc      *application.CollectSourceUseCase
	sources sources.SourceRepository
	metrics *observability.Metrics
	logger  *slog.Logger
}

func NewCollectSourceWorker(
	uc *application.CollectSourceUseCase,
	sourceRepo sources.SourceRepository,
	metrics *observability.Metrics,
	logger *slog.Logger,
) *CollectSourceWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &CollectSourceWorker{
		uc:      uc,
		sources: sourceRepo,
		metrics: metrics,
		logger:  logger,
	}
}

func (w *CollectSourceWorker) Work(ctx context.Context, job *river.Job[CollectSourceArgs]) error {
	start := time.Now()
	out, err := w.uc.Execute(ctx, application.CollectSourceInput{
		OrgID:    job.Args.OrgID,
		SourceID: job.Args.SourceID,
	})
	duration := time.Since(start)

	storeLabel := strconv.FormatInt(out.StoreID, 10)
	strategyLabel := string(sources.StrategyHTTP)
	resultLabel := classifyResult(out.Kind, err)

	if w.metrics != nil {
		w.metrics.CollectionDuration.WithLabelValues(storeLabel, strategyLabel, resultLabel).Observe(duration.Seconds())
		if kind := classifyErrorKind(out.Kind, err); kind != "" {
			w.metrics.CollectionErrors.WithLabelValues(storeLabel, kind).Inc()
		}
	}

	logger := w.logger.With(
		slog.String("org_id", job.Args.OrgID.String()),
		slog.Int64("source_id", job.Args.SourceID),
		slog.Int64("store_id", out.StoreID),
		slog.Int("attempt", job.Attempt),
		slog.Int("max_attempts", job.MaxAttempts),
		slog.Int64("duration_ms", duration.Milliseconds()),
		slog.String("result", resultLabel),
		slog.String("kind", string(out.Kind)),
	)

	if err != nil {
		if errors.Is(err, application.ErrFetchFailed) && job.Attempt >= job.MaxAttempts {
			if markErr := w.sources.MarkError(ctx, job.Args.OrgID, job.Args.SourceID, err.Error()); markErr != nil {
				logger.Error("collect_source mark_error_failed", slog.String("error", markErr.Error()))
			} else {
				logger.Warn("collect_source fetch_failed_marked", slog.String("error", err.Error()))
			}
		} else {
			logger.Warn("collect_source error", slog.String("error", err.Error()))
		}
		return err
	}

	logger.Info("collect_source done")
	return nil
}

func classifyResult(kind application.CollectSourceKind, err error) string {
	if err != nil {
		if errors.Is(err, application.ErrFetchFailed) {
			return observability.ResultFetchFailed
		}
		return observability.ResultInternal
	}

	switch kind {
	case application.KindSuccess, application.KindNoPriceDrop:
		return observability.ResultSuccess
	case application.KindDedup:
		return observability.ResultDedup
	case application.KindParseSelector, application.KindParseInvalidPrice:
		return observability.ResultParseError
	case application.KindFetchFailed:
		return observability.ResultFetchFailed
	case application.KindConcurrentUpdate:
		return observability.ResultInternal
	case application.KindInternal:
		return observability.ResultInternal
	default:
		return observability.ResultInternal
	}
}

func classifyErrorKind(kind application.CollectSourceKind, err error) string {
	if err != nil {
		if errors.Is(err, application.ErrFetchFailed) {
			return observability.ErrorKindFetchFailed
		}
		return observability.ErrorKindInternal
	}

	switch kind {
	case application.KindParseSelector:
		return observability.ErrorKindSelectorNotMatched
	case application.KindParseInvalidPrice:
		return observability.ErrorKindInvalidPrice
	case application.KindConcurrentUpdate:
		return observability.ErrorKindConcurrentUpdate
	default:
		return ""
	}
}
