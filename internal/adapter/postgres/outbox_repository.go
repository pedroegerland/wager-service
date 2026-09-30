package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedroegerland/wager-service/internal/app/port"
	"github.com/pedroegerland/wager-service/internal/domain/event"
)

type outboxRepository struct {
	tx pgx.Tx
}

func (r *outboxRepository) Add(ctx context.Context, events ...event.Envelope) error {
	for _, ev := range events {
		payload, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		_, err = r.tx.Exec(ctx, `INSERT INTO outbox_events
			(id, aggregate_id, event_type, payload, occurred_at, next_attempt_at)
			VALUES ($1, $2, $3, $4, $5, $5)`,
			ev.EventID, ev.AggregateID, ev.EventType, payload, ev.OccurredAt)
		if err != nil {
			return translateError(err)
		}
	}
	return nil
}

func (r *outboxRepository) Claim(ctx context.Context, owner string, lease time.Duration, limit int) ([]port.OutboxRecord, error) {
	now := time.Now().UTC()
	rows, err := r.tx.Query(ctx, `UPDATE outbox_events SET locked_by = $1, locked_until = $2
		WHERE id IN (
			SELECT id FROM outbox_events
			WHERE published_at IS NULL AND next_attempt_at <= $3
			  AND (locked_until IS NULL OR locked_until < $3 OR locked_by = $1)
			ORDER BY occurred_at, id
			LIMIT $4
			FOR UPDATE SKIP LOCKED)
		RETURNING id, aggregate_id, event_type, payload, occurred_at, attempts`,
		owner, now.Add(lease), now, limit)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	var out []port.OutboxRecord
	for rows.Next() {
		var rec port.OutboxRecord
		if err := rows.Scan(&rec.ID, &rec.AggregateID, &rec.EventType, &rec.Payload, &rec.OccurredAt, &rec.Attempts); err != nil {
			return nil, translateError(err)
		}
		out = append(out, rec)
	}
	return out, translateError(rows.Err())
}

func (r *outboxRepository) MarkPublished(ctx context.Context, id uuid.UUID, owner string) error {
	tag, err := r.tx.Exec(ctx, `UPDATE outbox_events
		SET published_at = now(), attempts = attempts + 1, locked_by = NULL, locked_until = NULL, last_error = NULL
		WHERE id = $1 AND locked_by = $2 AND published_at IS NULL`, id, owner)
	if err != nil {
		return translateError(err)
	}
	if tag.RowsAffected() == 0 {
		return port.ErrStale
	}
	return nil
}

func (r *outboxRepository) Reschedule(ctx context.Context, id uuid.UUID, owner string, next time.Time, lastErr string) error {
	_, err := r.tx.Exec(ctx, `UPDATE outbox_events
		SET attempts = attempts + 1, next_attempt_at = $3, locked_by = NULL, locked_until = NULL, last_error = left($4, 500)
		WHERE id = $1 AND locked_by = $2`, id, owner, next, lastErr)
	return translateError(err)
}

func (r *outboxRepository) Lag(ctx context.Context) (time.Duration, error) {
	var oldest *time.Time
	err := r.tx.QueryRow(ctx, `SELECT MIN(occurred_at) FROM outbox_events WHERE published_at IS NULL`).Scan(&oldest)
	if err != nil {
		return 0, translateError(err)
	}
	if oldest == nil {
		return 0, nil
	}
	return time.Since(*oldest), nil
}
