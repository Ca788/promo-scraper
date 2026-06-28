package domain

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

var ErrSourceNotFound = errors.New("source not found")

var ErrConcurrentUpdate = errors.New("source version conflict")

type SourceRepository interface {
	GetEligible(ctx context.Context, orgID uuid.UUID, limit int32) ([]Source, error)

	GetByID(ctx context.Context, orgID uuid.UUID, id int64) (Source, error)

	UpdateAfterCollect(ctx context.Context, orgID uuid.UUID, id int64, version int, snapshot Snapshot) error

	MarkError(ctx context.Context, orgID uuid.UUID, id int64, msg string) error

	WithTx(ctx context.Context, fn func(SourceRepository) error) error
}
