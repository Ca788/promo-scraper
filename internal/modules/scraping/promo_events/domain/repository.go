package domain

import "context"

type PromoEventRepository interface {
	Insert(ctx context.Context, e PromoEvent) (inserted bool, err error)
}
