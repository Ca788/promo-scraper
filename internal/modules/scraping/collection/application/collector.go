package application

import (
	"context"
	"errors"

	sources "promo-scraper/internal/modules/scraping/sources/domain"
)

var (
	ErrSelectorNotMatched = errors.New("selector did not match")
	ErrInvalidPrice       = errors.New("invalid price")
	ErrFetchFailed        = errors.New("fetch failed")
)

type Collector interface {
	Collect(ctx context.Context, src sources.Source) (sources.Snapshot, error)
}
