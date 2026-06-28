package infrastructure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"promo-scraper/internal/modules/scraping/sources/domain"
	"promo-scraper/internal/modules/scraping/sources/infrastructure/sqlc"
)

const defaultEligibleLimit int32 = 100

type PGSourceRepository struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

func NewPGSourceRepository(pool *pgxpool.Pool) *PGSourceRepository {
	return &PGSourceRepository{
		pool: pool,
		q:    sqlc.New(pool),
	}
}

func (r *PGSourceRepository) GetEligible(ctx context.Context, orgID uuid.UUID, limit int32) ([]domain.Source, error) {
	if limit <= 0 {
		limit = defaultEligibleLimit
	}

	rows, err := r.q.GetEligibleSources(ctx, sqlc.GetEligibleSourcesParams{
		OrgID: toPgUUID(orgID),
		Limit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("sources.GetEligible: %w", err)
	}

	out := make([]domain.Source, 0, len(rows))
	for _, row := range rows {
		src, err := mapRowToDomain(row)
		if err != nil {
			return nil, fmt.Errorf("sources.GetEligible map: %w", err)
		}
		out = append(out, src)
	}
	return out, nil
}

func (r *PGSourceRepository) GetByID(ctx context.Context, orgID uuid.UUID, id int64) (domain.Source, error) {
	row, err := r.q.GetSourceByID(ctx, sqlc.GetSourceByIDParams{
		ID:    id,
		OrgID: toPgUUID(orgID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Source{}, domain.ErrSourceNotFound
		}
		return domain.Source{}, fmt.Errorf("sources.GetByID: %w", err)
	}
	return mapRowToDomain(row)
}

func (r *PGSourceRepository) UpdateAfterCollect(
	ctx context.Context,
	orgID uuid.UUID,
	id int64,
	version int,
	snapshot domain.Snapshot,
) error {
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("sources.UpdateAfterCollect marshal snapshot: %w", err)
	}

	affected, err := r.q.UpdateSourceAfterCollect(ctx, sqlc.UpdateSourceAfterCollectParams{
		LastSnapshot: payload,
		ID:           id,
		OrgID:        toPgUUID(orgID),
		Version:      int32(version),
	})
	if err != nil {
		return fmt.Errorf("sources.UpdateAfterCollect: %w", err)
	}
	if affected == 0 {
		return domain.ErrConcurrentUpdate
	}
	return nil
}

func (r *PGSourceRepository) MarkError(ctx context.Context, orgID uuid.UUID, id int64, msg string) error {
	m := msg
	err := r.q.MarkSourceError(ctx, sqlc.MarkSourceErrorParams{
		LastError: &m,
		ID:        id,
		OrgID:     toPgUUID(orgID),
	})
	if err != nil {
		return fmt.Errorf("sources.MarkError: %w", err)
	}
	return nil
}

func (r *PGSourceRepository) WithTx(ctx context.Context, fn func(domain.SourceRepository) error) error {
	if r.pool == nil {
		return errors.New("sources.WithTx: aninhamento de transação não suportado")
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("sources.WithTx begin: %w", err)
	}

	txRepo := &PGSourceRepository{
		pool: nil,
		q:    r.q.WithTx(tx),
	}

	if execErr := fn(txRepo); execErr != nil {
		_ = tx.Rollback(ctx)
		return execErr
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("sources.WithTx commit: %w", err)
	}
	return nil
}

func mapRowToDomain(row sqlc.Source) (domain.Source, error) {
	orgID, err := fromPgUUID(row.OrgID)
	if err != nil {
		return domain.Source{}, fmt.Errorf("org_id: %w", err)
	}

	src := domain.Source{
		ID:              row.ID,
		OrgID:           orgID,
		StoreID:         row.StoreID,
		URL:             row.Url,
		Strategy:        domain.Strategy(row.Strategy),
		IntervalSeconds: int(row.IntervalSeconds),
		Enabled:         row.Enabled,
		LastError:       row.LastError,
		Version:         int(row.Version),
	}

	if row.LastCollectedAt.Valid {
		t := row.LastCollectedAt.Time
		src.LastCollectedAt = &t
	}
	if row.CreatedAt.Valid {
		src.CreatedAt = row.CreatedAt.Time
	}
	if row.UpdatedAt.Valid {
		src.UpdatedAt = row.UpdatedAt.Time
	}

	if len(row.Selectors) > 0 {
		selectors := map[string]string{}
		if err := json.Unmarshal(row.Selectors, &selectors); err != nil {
			return domain.Source{}, fmt.Errorf("selectors: %w", err)
		}
		src.Selectors = selectors
	}

	if len(row.LastSnapshot) > 0 {
		var snap domain.Snapshot
		if err := json.Unmarshal(row.LastSnapshot, &snap); err != nil {
			return domain.Source{}, fmt.Errorf("last_snapshot: %w", err)
		}
		src.LastSnapshot = &snap
	}

	return src, nil
}

func toPgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func fromPgUUID(id pgtype.UUID) (uuid.UUID, error) {
	if !id.Valid {
		return uuid.Nil, errors.New("uuid inválido (NULL)")
	}
	return uuid.UUID(id.Bytes), nil
}
