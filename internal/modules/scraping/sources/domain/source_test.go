package domain_test

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"promo-scraper/internal/modules/scraping/sources/domain"
)

func TestSnapshot_HasPriceDrop(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		snapshot    *domain.Snapshot
		novoPreco   string
		expectDrop  bool
		description string
	}{
		{
			name:        "queda relevante de 1 centavo (100,00 -> 99,99)",
			snapshot:    &domain.Snapshot{Preco: decimal.RequireFromString("100.00")},
			novoPreco:   "99.99",
			expectDrop:  true,
			description: "qualquer queda >= 1 centavo dispara evento",
		},
		{
			name:        "preço estável (100,00 -> 100,00)",
			snapshot:    &domain.Snapshot{Preco: decimal.RequireFromString("100.00")},
			novoPreco:   "100.00",
			expectDrop:  false,
			description: "preço igual não é queda",
		},
		{
			name:        "alta de preço (100,00 -> 100,01)",
			snapshot:    &domain.Snapshot{Preco: decimal.RequireFromString("100.00")},
			novoPreco:   "100.01",
			expectDrop:  false,
			description: "subida de preço não é queda",
		},
		{
			name:        "snapshot nil (primeiro poll)",
			snapshot:    nil,
			novoPreco:   "50.00",
			expectDrop:  false,
			description: "primeiro poll não tem baseline e não dispara evento",
		},
		{
			name:        "queda grande (199,90 -> 99,90)",
			snapshot:    &domain.Snapshot{Preco: decimal.RequireFromString("199.90")},
			novoPreco:   "99.90",
			expectDrop:  true,
			description: "quedas relevantes são detectadas",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			novo := decimal.RequireFromString(tc.novoPreco)
			got := tc.snapshot.HasPriceDrop(novo)
			require.Equal(t, tc.expectDrop, got, tc.description)
		})
	}
}
