package infrastructure

import (
	"context"
	"sync"

	"golang.org/x/time/rate"
)

type TokenBucketRegistry struct {
	mu           sync.Mutex
	buckets      map[int64]*rate.Limiter
	defaultRate  rate.Limit
	defaultBurst int
}

func NewTokenBucketRegistry(defaultRate rate.Limit, defaultBurst int) *TokenBucketRegistry {
	if defaultBurst < 1 {
		defaultBurst = 1
	}
	return &TokenBucketRegistry{
		buckets:      make(map[int64]*rate.Limiter),
		defaultRate:  defaultRate,
		defaultBurst: defaultBurst,
	}
}

func (r *TokenBucketRegistry) Acquire(ctx context.Context, storeID int64) error {
	limiter := r.getOrCreate(storeID)
	return limiter.Wait(ctx)
}

func (r *TokenBucketRegistry) getOrCreate(storeID int64) *rate.Limiter {
	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.buckets[storeID]; ok {
		return existing
	}
	limiter := rate.NewLimiter(r.defaultRate, r.defaultBurst)
	r.buckets[storeID] = limiter
	return limiter
}
