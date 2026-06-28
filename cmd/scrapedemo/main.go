package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"
	"golang.org/x/time/rate"

	"promo-scraper/internal/modules/scraping/collection/infrastructure"
	sources "promo-scraper/internal/modules/scraping/sources/domain"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	bucket := infrastructure.NewTokenBucketRegistry(rate.Inf, 1)
	httpC := infrastructure.NewHTTPCollector(15*time.Second, bucket, logger)

	var meliC *infrastructure.MercadoLivreAPICollector
	if tok := os.Getenv("MELI_ACCESS_TOKEN"); tok != "" {
		meliC = infrastructure.NewMercadoLivreAPICollector(tok, 15*time.Second)
	}

	headlessC := infrastructure.NewHeadlessCollector(os.Getenv("CHROME_PATH"), 40*time.Second, bucket, logger)
	router := infrastructure.NewRoutingCollector(httpC, meliC, headlessC, logger)

	targets := []struct {
		nome string
		url  string
	}{
		{"KaBuM!", "https://www.kabum.com.br/produto/401293/controle-sony-dualsense-edge-ps5-sem-fio-preto-e-branco-cfi-zcp1wy"},
		{"Mercado Livre", "https://www.mercadolivre.com.br/notebook-positivo-vision-i15m-com-minitela-intel-core-3-8gb-de-ram-ssd-de-256gb-e-tela-de-156-full-hd-ips-antirreflexo/p/MLB56961557"},
		{"Terabyte", "https://www.terabyteshop.com.br/produto/13953/ssd-patriot-p300-256gb-nvme-leitura-1700mbs-e-gravacao-1100mbs-p300p256gm28us"},
		{"Amazon", "https://www.amazon.com.br/dp/B0CHX1W1XY"},
		{"Shopee", "https://shopee.com.br/"},
	}

	for _, tg := range targets {
		profile, _ := infrastructure.ProfileForURL(tg.url)
		fmt.Printf("\n========== %s ==========\n", tg.nome)
		fmt.Printf("  perfil    : strategy=%s extract=%s\n", profile.Strategy, profile.Extract)
		fmt.Printf("  link      : %s\n", tg.url)

		src := sources.Source{ID: 1, OrgID: uuid.New(), StoreID: 1, URL: tg.url, Strategy: profile.Strategy}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		snap, err := router.Collect(ctx, src)
		cancel()

		if err != nil {
			fmt.Printf("  resultado : ERRO -> %v\n", err)
			continue
		}
		pretty, _ := json.MarshalIndent(snap, "  ", "  ")
		fmt.Printf("  resultado : SUCESSO\n  %s\n", pretty)
	}
}
