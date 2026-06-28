package domain

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	sources "promo-scraper/internal/modules/scraping/sources/domain"
	"promo-scraper/internal/shared/clock"
)

const DefaultCurrency = "BRL"

type PromoEvent struct {
	ID                int64
	OrgID             uuid.UUID
	SourceID          int64
	StoreID           int64
	SKU               string
	Titulo            string
	Preco             decimal.Decimal
	PrecoAnterior     *decimal.Decimal
	Moeda             string
	EstoqueDisponivel bool
	BadgePromo        bool
	URL               string
	DetectedAt        time.Time
}

func NewPromoEvent(src sources.Source, snap sources.Snapshot, clk clock.Clock) PromoEvent {
	var precoAnterior *decimal.Decimal
	if src.LastSnapshot != nil {
		anterior := src.LastSnapshot.Preco
		precoAnterior = &anterior
	}

	return PromoEvent{
		OrgID:             src.OrgID,
		SourceID:          src.ID,
		StoreID:           src.StoreID,
		SKU:               snap.SKU,
		Titulo:            snap.Titulo,
		Preco:             snap.Preco,
		PrecoAnterior:     precoAnterior,
		Moeda:             DefaultCurrency,
		EstoqueDisponivel: snap.EstoqueDisponivel,
		BadgePromo:        snap.BadgePromo,
		URL:               src.URL,
		DetectedAt:        clk.Now(),
	}
}
