package application_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"promo-scraper/internal/modules/scraping/collection/application"
	promoevents "promo-scraper/internal/modules/scraping/promo_events/domain"
	sources "promo-scraper/internal/modules/scraping/sources/domain"
	"promo-scraper/internal/shared/clock"
)

type fakeCollector struct {
	snapshot sources.Snapshot
	err      error
	calls    int
}

func (f *fakeCollector) Collect(ctx context.Context, src sources.Source) (sources.Snapshot, error) {
	f.calls++
	if f.err != nil {
		return sources.Snapshot{}, f.err
	}
	return f.snapshot, nil
}

type inMemorySourceRepo struct {
	source           sources.Source
	updateCalls      int
	updatedSnapshot  sources.Snapshot
	updatedVersion   int
	markErrorCalls   int
	markErrorMessage string
	getByIDErr       error
	updateErr        error
	markErrorErr     error
}

func (r *inMemorySourceRepo) GetEligible(ctx context.Context, orgID uuid.UUID, limit int32) ([]sources.Source, error) {
	return nil, errors.New("not used")
}

func (r *inMemorySourceRepo) GetByID(ctx context.Context, orgID uuid.UUID, id int64) (sources.Source, error) {
	if r.getByIDErr != nil {
		return sources.Source{}, r.getByIDErr
	}
	if r.source.OrgID != orgID || r.source.ID != id {
		return sources.Source{}, sources.ErrSourceNotFound
	}
	return r.source, nil
}

func (r *inMemorySourceRepo) UpdateAfterCollect(ctx context.Context, orgID uuid.UUID, id int64, version int, snapshot sources.Snapshot) error {
	r.updateCalls++
	if r.updateErr != nil {
		return r.updateErr
	}
	r.updatedSnapshot = snapshot
	r.updatedVersion = version
	return nil
}

func (r *inMemorySourceRepo) MarkError(ctx context.Context, orgID uuid.UUID, id int64, msg string) error {
	r.markErrorCalls++
	r.markErrorMessage = msg
	return r.markErrorErr
}

func (r *inMemorySourceRepo) WithTx(ctx context.Context, fn func(sources.SourceRepository) error) error {
	return fn(r)
}

type inMemoryPromoEventRepo struct {
	inserted []promoevents.PromoEvent
	result   bool
	err      error
}

func (r *inMemoryPromoEventRepo) Insert(ctx context.Context, e promoevents.PromoEvent) (bool, error) {
	if r.err != nil {
		return false, r.err
	}
	r.inserted = append(r.inserted, e)
	return r.result, nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func baseSource(orgID uuid.UUID, lastSnap *sources.Snapshot) sources.Source {
	return sources.Source{
		ID:              101,
		OrgID:           orgID,
		StoreID:         9,
		URL:             "https://loja.example.com/p/abc",
		Strategy:        sources.StrategyHTTP,
		IntervalSeconds: 60,
		Enabled:         true,
		Version:         3,
		LastSnapshot:    lastSnap,
	}
}

func TestExecute_HappyPath_MaterializesEventAndUpdatesSnapshot(t *testing.T) {
	orgID := uuid.New()
	prev := &sources.Snapshot{
		SKU:               "SKU-1",
		Titulo:            "Produto",
		Preco:             decimal.RequireFromString("100.00"),
		EstoqueDisponivel: true,
		BadgePromo:        false,
		ColetadoEm:        time.Now().UTC().Add(-time.Hour),
	}
	src := baseSource(orgID, prev)

	newSnap := sources.Snapshot{
		SKU:               "SKU-1",
		Titulo:            "Produto",
		Preco:             decimal.RequireFromString("90.00"),
		EstoqueDisponivel: true,
		BadgePromo:        true,
		ColetadoEm:        time.Now().UTC(),
	}

	sourceRepo := &inMemorySourceRepo{source: src}
	eventRepo := &inMemoryPromoEventRepo{result: true}
	collector := &fakeCollector{snapshot: newSnap}
	fixed := time.Date(2026, 6, 28, 14, 0, 0, 0, time.UTC)
	clk := &clock.FakeClock{T: fixed}

	uc := application.NewCollectSourceUseCase(sourceRepo, eventRepo, collector, clk, discardLogger())
	_, err := uc.Execute(context.Background(), application.CollectSourceInput{
		OrgID:    orgID,
		SourceID: src.ID,
	})

	require.NoError(t, err)
	require.Equal(t, 1, collector.calls)
	require.Len(t, eventRepo.inserted, 1, "evento de queda de preço deve ser materializado")

	got := eventRepo.inserted[0]
	require.Equal(t, orgID, got.OrgID)
	require.Equal(t, src.ID, got.SourceID)
	require.Equal(t, src.StoreID, got.StoreID)
	require.True(t, got.Preco.Equal(decimal.RequireFromString("90.00")))
	require.NotNil(t, got.PrecoAnterior)
	require.True(t, got.PrecoAnterior.Equal(decimal.RequireFromString("100.00")))
	require.Equal(t, fixed, got.DetectedAt)

	require.Equal(t, 1, sourceRepo.updateCalls, "snapshot da fonte deve ser atualizado uma vez")
	require.Equal(t, src.Version, sourceRepo.updatedVersion)
	require.True(t, sourceRepo.updatedSnapshot.Preco.Equal(decimal.RequireFromString("90.00")))
	require.Zero(t, sourceRepo.markErrorCalls)
}

func TestExecute_ParseErrors_MarkErrorAndNoEvent(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"selector_not_matched", application.ErrSelectorNotMatched},
		{"invalid_price", application.ErrInvalidPrice},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orgID := uuid.New()
			prev := &sources.Snapshot{
				Preco:      decimal.RequireFromString("100.00"),
				ColetadoEm: time.Now().UTC().Add(-time.Hour),
			}
			src := baseSource(orgID, prev)

			sourceRepo := &inMemorySourceRepo{source: src}
			eventRepo := &inMemoryPromoEventRepo{}
			collector := &fakeCollector{err: tc.err}
			clk := &clock.FakeClock{T: time.Now().UTC()}

			uc := application.NewCollectSourceUseCase(sourceRepo, eventRepo, collector, clk, discardLogger())
			_, err := uc.Execute(context.Background(), application.CollectSourceInput{
				OrgID:    orgID,
				SourceID: src.ID,
			})

			require.NoError(t, err, "parse error é não-retentável → use case retorna nil")
			require.Equal(t, 1, sourceRepo.markErrorCalls)
			require.Contains(t, sourceRepo.markErrorMessage, tc.err.Error())
			require.Empty(t, eventRepo.inserted, "nenhum evento materializado em parse error")
			require.Zero(t, sourceRepo.updateCalls, "snapshot não atualizado em parse error")
		})
	}
}

func TestExecute_FetchFailed_PropagatesError(t *testing.T) {
	orgID := uuid.New()
	prev := &sources.Snapshot{
		Preco:      decimal.RequireFromString("100.00"),
		ColetadoEm: time.Now().UTC().Add(-time.Hour),
	}
	src := baseSource(orgID, prev)

	sourceRepo := &inMemorySourceRepo{source: src}
	eventRepo := &inMemoryPromoEventRepo{}
	collector := &fakeCollector{err: application.ErrFetchFailed}
	clk := &clock.FakeClock{T: time.Now().UTC()}

	uc := application.NewCollectSourceUseCase(sourceRepo, eventRepo, collector, clk, discardLogger())
	_, err := uc.Execute(context.Background(), application.CollectSourceInput{
		OrgID:    orgID,
		SourceID: src.ID,
	})

	require.Error(t, err)
	require.ErrorIs(t, err, application.ErrFetchFailed, "river precisa reconhecer ErrFetchFailed via errors.Is")
	require.Empty(t, eventRepo.inserted, "fetch error não materializa evento")
	require.Zero(t, sourceRepo.updateCalls, "fetch error não atualiza snapshot")
	require.Zero(t, sourceRepo.markErrorCalls, "fetch error é retentável; não marca last_error")
}

func TestExecute_GetByIDError_Propagates(t *testing.T) {
	orgID := uuid.New()
	sourceRepo := &inMemorySourceRepo{getByIDErr: sources.ErrSourceNotFound}
	eventRepo := &inMemoryPromoEventRepo{}
	collector := &fakeCollector{}
	clk := &clock.FakeClock{T: time.Now().UTC()}

	uc := application.NewCollectSourceUseCase(sourceRepo, eventRepo, collector, clk, discardLogger())
	_, err := uc.Execute(context.Background(), application.CollectSourceInput{
		OrgID:    orgID,
		SourceID: 999,
	})

	require.ErrorIs(t, err, sources.ErrSourceNotFound)
	require.Zero(t, collector.calls)
}

func TestExecute_InsertEventError_Propagates(t *testing.T) {
	orgID := uuid.New()
	prev := &sources.Snapshot{
		Preco:      decimal.RequireFromString("100.00"),
		ColetadoEm: time.Now().UTC().Add(-time.Hour),
	}
	src := baseSource(orgID, prev)
	newSnap := sources.Snapshot{
		Preco:      decimal.RequireFromString("90.00"),
		ColetadoEm: time.Now().UTC(),
	}

	sourceRepo := &inMemorySourceRepo{source: src}
	wantErr := errors.New("insert exploded")
	eventRepo := &inMemoryPromoEventRepo{err: wantErr}
	collector := &fakeCollector{snapshot: newSnap}
	clk := &clock.FakeClock{T: time.Now().UTC()}

	uc := application.NewCollectSourceUseCase(sourceRepo, eventRepo, collector, clk, discardLogger())
	_, err := uc.Execute(context.Background(), application.CollectSourceInput{
		OrgID:    orgID,
		SourceID: src.ID,
	})

	require.ErrorIs(t, err, wantErr)
	require.Zero(t, sourceRepo.updateCalls, "se Insert falhar, snapshot não é atualizado")
}

func TestExecute_DedupedEvent_StillUpdatesSnapshot(t *testing.T) {
	orgID := uuid.New()
	prev := &sources.Snapshot{
		Preco:      decimal.RequireFromString("100.00"),
		ColetadoEm: time.Now().UTC().Add(-time.Hour),
	}
	src := baseSource(orgID, prev)
	newSnap := sources.Snapshot{
		Preco:      decimal.RequireFromString("90.00"),
		ColetadoEm: time.Now().UTC(),
	}

	sourceRepo := &inMemorySourceRepo{source: src}
	eventRepo := &inMemoryPromoEventRepo{result: false}
	collector := &fakeCollector{snapshot: newSnap}
	clk := &clock.FakeClock{T: time.Now().UTC()}

	uc := application.NewCollectSourceUseCase(sourceRepo, eventRepo, collector, clk, discardLogger())
	_, err := uc.Execute(context.Background(), application.CollectSourceInput{
		OrgID:    orgID,
		SourceID: src.ID,
	})

	require.NoError(t, err)
	require.Len(t, eventRepo.inserted, 1, "Insert é tentado mesmo quando vai ser dedupado")
	require.Equal(t, 1, sourceRepo.updateCalls, "dedup ainda assim atualiza snapshot")
}

func TestExecute_UpdateAfterCollect_ConcurrentUpdate_Propagates(t *testing.T) {
	orgID := uuid.New()
	src := baseSource(orgID, nil)
	newSnap := sources.Snapshot{
		Preco:      decimal.RequireFromString("90.00"),
		ColetadoEm: time.Now().UTC(),
	}

	sourceRepo := &inMemorySourceRepo{source: src, updateErr: sources.ErrConcurrentUpdate}
	eventRepo := &inMemoryPromoEventRepo{}
	collector := &fakeCollector{snapshot: newSnap}
	clk := &clock.FakeClock{T: time.Now().UTC()}

	uc := application.NewCollectSourceUseCase(sourceRepo, eventRepo, collector, clk, discardLogger())
	_, err := uc.Execute(context.Background(), application.CollectSourceInput{
		OrgID:    orgID,
		SourceID: src.ID,
	})

	require.ErrorIs(t, err, sources.ErrConcurrentUpdate)
}

func TestExecute_UnknownCollectorError_Propagates(t *testing.T) {
	orgID := uuid.New()
	src := baseSource(orgID, nil)
	sourceRepo := &inMemorySourceRepo{source: src}
	eventRepo := &inMemoryPromoEventRepo{}
	wantErr := errors.New("boom inesperado")
	collector := &fakeCollector{err: wantErr}
	clk := &clock.FakeClock{T: time.Now().UTC()}

	uc := application.NewCollectSourceUseCase(sourceRepo, eventRepo, collector, clk, discardLogger())
	_, err := uc.Execute(context.Background(), application.CollectSourceInput{
		OrgID:    orgID,
		SourceID: src.ID,
	})

	require.ErrorIs(t, err, wantErr)
	require.Zero(t, sourceRepo.markErrorCalls)
	require.Zero(t, sourceRepo.updateCalls)
}

func TestExecute_NilLogger_FallsBackToDefault(t *testing.T) {
	orgID := uuid.New()
	src := baseSource(orgID, nil)
	sourceRepo := &inMemorySourceRepo{source: src}
	eventRepo := &inMemoryPromoEventRepo{}
	collector := &fakeCollector{snapshot: sources.Snapshot{Preco: decimal.RequireFromString("10.00")}}
	clk := &clock.FakeClock{T: time.Now().UTC()}

	uc := application.NewCollectSourceUseCase(sourceRepo, eventRepo, collector, clk, nil)
	_, err := uc.Execute(context.Background(), application.CollectSourceInput{
		OrgID:    orgID,
		SourceID: src.ID,
	})

	require.NoError(t, err)
}

func TestExecute_ParseError_MarkErrorFails_PropagatesMarkError(t *testing.T) {
	orgID := uuid.New()
	src := baseSource(orgID, nil)
	wantMarkErr := errors.New("mark exploded")
	sourceRepo := &inMemorySourceRepo{source: src, markErrorErr: wantMarkErr}
	eventRepo := &inMemoryPromoEventRepo{}
	collector := &fakeCollector{err: application.ErrSelectorNotMatched}
	clk := &clock.FakeClock{T: time.Now().UTC()}

	uc := application.NewCollectSourceUseCase(sourceRepo, eventRepo, collector, clk, discardLogger())
	_, err := uc.Execute(context.Background(), application.CollectSourceInput{
		OrgID:    orgID,
		SourceID: src.ID,
	})

	require.ErrorIs(t, err, wantMarkErr)
}

func TestExecute_NoPriceDrop_OnlyUpdatesSnapshot(t *testing.T) {
	orgID := uuid.New()
	prev := &sources.Snapshot{
		Preco:      decimal.RequireFromString("100.00"),
		ColetadoEm: time.Now().UTC().Add(-time.Hour),
	}
	src := baseSource(orgID, prev)
	newSnap := sources.Snapshot{
		Preco:      decimal.RequireFromString("100.00"),
		ColetadoEm: time.Now().UTC(),
	}

	sourceRepo := &inMemorySourceRepo{source: src}
	eventRepo := &inMemoryPromoEventRepo{}
	collector := &fakeCollector{snapshot: newSnap}
	clk := &clock.FakeClock{T: time.Now().UTC()}

	uc := application.NewCollectSourceUseCase(sourceRepo, eventRepo, collector, clk, discardLogger())
	_, err := uc.Execute(context.Background(), application.CollectSourceInput{
		OrgID:    orgID,
		SourceID: src.ID,
	})

	require.NoError(t, err)
	require.Empty(t, eventRepo.inserted, "sem queda → sem evento")
	require.Equal(t, 1, sourceRepo.updateCalls, "snapshot ainda assim atualizado para manter last_collected_at")
}
