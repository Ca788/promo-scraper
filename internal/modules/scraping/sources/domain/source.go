package domain

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Strategy string

const (
	StrategyHTTP     Strategy = "http"
	StrategyHeadless Strategy = "headless"
	StrategyAPI      Strategy = "api"
)

type Source struct {
	ID              int64
	OrgID           uuid.UUID
	StoreID         int64
	URL             string
	Strategy        Strategy
	IntervalSeconds int
	Selectors       map[string]string
	Enabled         bool
	LastCollectedAt *time.Time
	LastSnapshot    *Snapshot
	LastError       *string
	Version         int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Snapshot struct {
	SKU               string          `json:"sku"`
	Titulo            string          `json:"titulo"`
	Preco             decimal.Decimal `json:"preco"`
	EstoqueDisponivel bool            `json:"estoque_disponivel"`
	BadgePromo        bool            `json:"badge_promo"`
	ColetadoEm        time.Time       `json:"coletado_em"`
}

func (s *Snapshot) HasPriceDrop(novo decimal.Decimal) bool {
	if s == nil {
		return false
	}
	return novo.LessThan(s.Preco)
}
