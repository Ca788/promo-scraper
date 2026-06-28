package infrastructure

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"promo-scraper/internal/modules/scraping/promo_events/domain"
	"promo-scraper/internal/modules/scraping/promo_events/infrastructure/sqlc"
)

type PGPromoEventRepository struct {
	q *sqlc.Queries
}

func NewPGPromoEventRepository(pool *pgxpool.Pool) *PGPromoEventRepository {
	return &PGPromoEventRepository{q: sqlc.New(pool)}
}

func (r *PGPromoEventRepository) Insert(ctx context.Context, e domain.PromoEvent) (bool, error) {
	params := sqlc.InsertPromoEventParams{
		OrgID:             toPgUUID(e.OrgID),
		SourceID:          e.SourceID,
		StoreID:           e.StoreID,
		Sku:               e.SKU,
		Titulo:            e.Titulo,
		Preco:             decimalToPgNumeric(e.Preco),
		PrecoAnterior:     optionalDecimalToPgNumeric(e.PrecoAnterior),
		Moeda:             e.Moeda,
		EstoqueDisponivel: e.EstoqueDisponivel,
		BadgePromo:        e.BadgePromo,
		Url:               e.URL,
		DetectedAt:        pgtype.Timestamptz{Time: e.DetectedAt, Valid: true},
	}

	affected, err := r.q.InsertPromoEvent(ctx, params)
	if err != nil {
		return false, fmt.Errorf("promo_events.Insert: %w", err)
	}
	return affected == 1, nil
}

func toPgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func decimalToPgNumeric(d decimal.Decimal) pgtype.Numeric {
	return pgtype.Numeric{
		Int:   d.Coefficient(),
		Exp:   d.Exponent(),
		Valid: true,
	}
}

func optionalDecimalToPgNumeric(d *decimal.Decimal) pgtype.Numeric {
	if d == nil {
		return pgtype.Numeric{Valid: false}
	}
	return decimalToPgNumeric(*d)
}
