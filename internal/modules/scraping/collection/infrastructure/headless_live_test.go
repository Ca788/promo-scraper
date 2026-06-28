package infrastructure

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"promo-scraper/internal/modules/scraping/collection/application"
	sources "promo-scraper/internal/modules/scraping/sources/domain"
)

func chromeAvailable() bool {
	for _, bin := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if _, err := exec.LookPath(bin); err == nil {
			return true
		}
	}
	return false
}

func TestHeadless_LiveTier1(t *testing.T) {
	if testing.Short() {
		t.Skip("headless ao vivo desabilitado em -short")
	}
	if !chromeAvailable() {
		t.Skip("Chrome/Chromium não encontrado no PATH")
	}

	headless := NewHeadlessCollector("", 40*time.Second, NewTokenBucketRegistry(0, 1), nil)
	router := NewRoutingCollector(nil, nil, headless, nil)

	cases := []struct {
		nome string
		url  string
	}{
		{"mercadolivre", "https://www.mercadolivre.com.br/notebook-positivo-vision-i15m-com-minitela-intel-core-3-8gb-de-ram-ssd-de-256gb-e-tela-de-156-full-hd-ips-antirreflexo/p/MLB56961557"},
		{"terabyte", "https://www.terabyteshop.com.br/produto/13953/ssd-patriot-p300-256gb-nvme-leitura-1700mbs-e-gravacao-1100mbs-p300p256gm28us"},
	}

	for _, tc := range cases {
		t.Run(tc.nome, func(t *testing.T) {
			src := sources.Source{ID: 1, OrgID: uuid.New(), StoreID: 1, URL: tc.url}
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
			defer cancel()

			snap, err := router.Collect(ctx, src)
			if errors.Is(err, application.ErrFetchFailed) {
				t.Skipf("%s indisponível/bloqueado: %v", tc.nome, err)
			}
			require.NoError(t, err)
			require.True(t, snap.Preco.IsPositive(), "preço positivo")
			t.Logf("%s OK -> titulo=%q preco=R$ %s", tc.nome, snap.Titulo, snap.Preco)
		})
	}
}
