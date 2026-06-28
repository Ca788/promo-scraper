package infrastructure

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func TestAcquire_RespectsRate(t *testing.T) {
	t.Parallel()

	registry := NewTokenBucketRegistry(rate.Every(50*time.Millisecond), 1)
	ctx := context.Background()

	require.NoError(t, registry.Acquire(ctx, 1))

	start := time.Now()
	require.NoError(t, registry.Acquire(ctx, 1))
	elapsed := time.Since(start)

	require.GreaterOrEqual(t, elapsed, 30*time.Millisecond, "segunda chamada deve aguardar pelo limit")
	require.Less(t, elapsed, 250*time.Millisecond, "segunda chamada nao deve demorar tanto")
}

func TestAcquire_IsolatedPerStore(t *testing.T) {
	t.Parallel()

	registry := NewTokenBucketRegistry(rate.Every(200*time.Millisecond), 1)
	ctx := context.Background()

	require.NoError(t, registry.Acquire(ctx, 10))
	require.NoError(t, registry.Acquire(ctx, 20))

	var wg sync.WaitGroup
	wg.Add(2)
	start := time.Now()

	go func() {
		defer wg.Done()
		require.NoError(t, registry.Acquire(ctx, 30))
	}()
	go func() {
		defer wg.Done()
		require.NoError(t, registry.Acquire(ctx, 40))
	}()

	wg.Wait()
	elapsed := time.Since(start)
	require.Less(t, elapsed, 100*time.Millisecond, "lojas diferentes nao devem compartilhar bucket")
}

func TestAcquire_ContextCancelled(t *testing.T) {
	t.Parallel()

	registry := NewTokenBucketRegistry(rate.Every(time.Second), 1)
	bgCtx := context.Background()

	require.NoError(t, registry.Acquire(bgCtx, 99))

	ctx, cancel := context.WithCancel(bgCtx)
	errCh := make(chan error, 1)
	go func() {
		errCh <- registry.Acquire(ctx, 99)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		require.Error(t, err)
		require.True(t, errors.Is(err, context.Canceled), "esperava context.Canceled, got %v", err)
	case <-time.After(time.Second):
		t.Fatal("Acquire nao retornou apos cancelamento do contexto")
	}
}

func TestAcquire_ConcurrentSafe(t *testing.T) {
	t.Parallel()

	registry := NewTokenBucketRegistry(rate.Inf, 1)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		storeID := int64(i % 5)
		go func() {
			defer wg.Done()
			require.NoError(t, registry.Acquire(ctx, storeID))
		}()
	}
	wg.Wait()
}
