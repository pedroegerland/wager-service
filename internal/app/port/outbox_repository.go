package port

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/domain/event"
)

type OutboxRecord struct {
	ID          uuid.UUID
	AggregateID uuid.UUID
	EventType   string
	Payload     []byte
	OccurredAt  time.Time
	Attempts    int
}

type OutboxRepository interface {
	Add(ctx context.Context, events ...event.Envelope) error

	Claim(ctx context.Context, owner string, lease time.Duration, limit int) ([]OutboxRecord, error)
	MarkPublished(ctx context.Context, id uuid.UUID, owner string) error

	Reschedule(ctx context.Context, id uuid.UUID, owner string, next time.Time, lastErr string) error

	Lag(ctx context.Context) (time.Duration, error)
}
