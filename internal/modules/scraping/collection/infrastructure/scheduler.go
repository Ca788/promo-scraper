package infrastructure

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	sources "promo-scraper/internal/modules/scraping/sources/domain"
)

const defaultSchedulerLimit int32 = 100

const defaultUniquePeriod = 30 * time.Second

type Scheduler struct {
	sources      sources.SourceRepository
	client       *river.Client[pgx.Tx]
	interval     time.Duration
	orgIDs       []uuid.UUID
	logger       *slog.Logger
	uniquePeriod time.Duration
	limit        int32
}

func NewScheduler(
	sourceRepo sources.SourceRepository,
	client *river.Client[pgx.Tx],
	interval time.Duration,
	orgIDs []uuid.UUID,
	logger *slog.Logger,
) *Scheduler {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &Scheduler{
		sources:      sourceRepo,
		client:       client,
		interval:     interval,
		orgIDs:       orgIDs,
		logger:       logger,
		uniquePeriod: defaultUniquePeriod,
		limit:        defaultSchedulerLimit,
	}
}

func (s *Scheduler) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	if err := s.runOnce(ctx); err != nil {
		s.logger.Warn("scheduler initial run error", slog.String("error", err.Error()))
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := s.runOnce(ctx); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return err
				}
				s.logger.Warn("scheduler tick error", slog.String("error", err.Error()))
			}
		}
	}
}

func (s *Scheduler) RunOnce(ctx context.Context) error {
	return s.runOnce(ctx)
}

func (s *Scheduler) runOnce(ctx context.Context) error {
	if len(s.orgIDs) == 0 {
		return nil
	}

	insertOpts := &river.InsertOpts{
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: s.uniquePeriod,
		},
	}

	for _, orgID := range s.orgIDs {
		eligible, err := s.sources.GetEligible(ctx, orgID, s.limit)
		if err != nil {
			s.logger.Warn("scheduler get_eligible_error",
				slog.String("org_id", orgID.String()),
				slog.String("error", err.Error()),
			)
			continue
		}

		for _, src := range eligible {
			args := CollectSourceArgs{
				OrgID:    src.OrgID,
				SourceID: src.ID,
				Version:  src.Version,
			}
			if _, err := s.client.Insert(ctx, args, insertOpts); err != nil {
				s.logger.Warn("scheduler enqueue_error",
					slog.String("org_id", orgID.String()),
					slog.Int64("source_id", src.ID),
					slog.String("error", err.Error()),
				)
				continue
			}
		}
	}

	return nil
}
