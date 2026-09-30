package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/pedroegerland/wager-service/internal/app/port"
)

type inboxRepository struct {
	tx pgx.Tx
}

func (r *inboxRepository) Register(ctx context.Context, m port.InboxMessage, now time.Time) (port.InboxState, error) {
	tag, err := r.tx.Exec(ctx, `INSERT INTO inbox_messages (consumer_name, message_id, payload_hash, received_at)
		VALUES ($1, $2, $3, $4) ON CONFLICT (consumer_name, message_id) DO NOTHING`,
		m.ConsumerName, m.MessageID, m.PayloadHash, now)
	if err != nil {
		return 0, translateError(err)
	}
	if tag.RowsAffected() == 1 {
		return port.InboxNew, nil
	}
	var hash string
	err = r.tx.QueryRow(ctx, `SELECT payload_hash FROM inbox_messages WHERE consumer_name = $1 AND message_id = $2`,
		m.ConsumerName, m.MessageID).Scan(&hash)
	if err != nil {
		return 0, translateError(err)
	}
	if hash != m.PayloadHash {
		return port.InboxHashMismatch, nil
	}
	return port.InboxDuplicate, nil
}

func (r *inboxRepository) Complete(ctx context.Context, m port.InboxMessage, now time.Time) error {
	_, err := r.tx.Exec(ctx, `UPDATE inbox_messages SET completed_at = $3 WHERE consumer_name = $1 AND message_id = $2`,
		m.ConsumerName, m.MessageID, now)
	return translateError(err)
}
