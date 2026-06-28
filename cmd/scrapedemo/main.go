package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"golang.org/x/time/rate"

	"promo-scraper/internal/modules/scraping/collection/application"
	"promo-scraper/internal/modules/scraping/collection/infrastructure"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	bucket := infrastructure.NewTokenBucketRegistry(rate.Inf, 1)
	headless := infrastructure.NewHeadlessCollector(os.Getenv("CHROME_PATH"), 45*time.Second, bucket, logger)

	providers := []struct {
		nome string
		p    interface {
			Offers(ctx context.Context, minDesconto, limit int) ([]application.PromoItem, error)
		}
	}{
		{"KaBuM!", infrastructure.NewKabumOffersCollector(15 * time.Second)},
		{"Mercado Livre", infrastructure.NewMercadoLivreOffersCollector(15 * time.Second)},
		{"Terabyte", infrastructure.NewTerabyteOffersCollector(headless)},
	}

	for _, pv := range providers {
		fmt.Printf("\n========== %s ==========\n", pv.nome)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		itens, err := pv.p.Offers(ctx, 0, 5)
		cancel()
		if err != nil {
			fmt.Printf("  ERRO -> %v\n", err)
			continue
		}
		fmt.Printf("  total: %d\n", len(itens))
		for i, it := range itens {
			pretty, _ := json.Marshal(map[string]any{
				"loja":   it.Loja,
				"titulo": it.Titulo,
				"preco":  it.Preco.StringFixed(2),
				"prev":   it.PrecoAnterior,
				"off":    it.DescontoPct,
				"link":   it.Link,
			})
			fmt.Printf("  [%d] %s\n", i+1, pretty)
		}
	}
}
