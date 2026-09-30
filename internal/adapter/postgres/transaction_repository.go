package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedroegerland/wager-service/internal/domain/money"
	"github.com/pedroegerland/wager-service/internal/domain/wager"
)

type transactionRepository struct {
	tx pgx.Tx
}

const transactionColumns = `id, origin, kind, status, wallet_id, player_id, currency, amount,
	provider_id, external_transaction_id, idempotency_key, payload_hash, round_id, game_id,
	reference_external_transaction_id, reference_transaction_id, failure_code, balance_after,
	attempts, next_attempt_at, created_at, updated_at, processed_at`

func scanTransaction(row pgx.Row) (*wager.Transaction, error) {
	var (
		s                                   wager.Snapshot
		origin, kind, status, currency      string
		amount                              int64
		providerID, extID, key, hash        *string
		roundID, gameID, refExtID, failCode *string
		balanceAfter                        *int64
	)
	err := row.Scan(&s.ID, &origin, &kind, &status, &s.WalletID, &s.PlayerID, &currency, &amount,
		&providerID, &extID, &key, &hash, &roundID, &gameID,
		&refExtID, &s.ReferenceTxID, &failCode, &balanceAfter,
		&s.Attempts, &s.NextAttemptAt, &s.CreatedAt, &s.UpdatedAt, &s.ProcessedAt)
	if err != nil {
		return nil, translateError(err)
	}
	s.Origin, s.Kind, s.Status = wager.Origin(origin), wager.Kind(kind), wager.Status(status)
	if s.Money, err = money.FromUnits(amount, currency); err != nil {
		return nil, err
	}
	if balanceAfter != nil {
		b, err := money.FromUnits(*balanceAfter, currency)
		if err != nil {
			return nil, err
		}
		s.BalanceAfter = &b
	}
	if failCode != nil {
		s.FailureCode = wager.FailureCode(*failCode)
	}
	if s.Origin == wager.OriginExternal {
		s.External = &wager.External{
			ProviderID:            stringOrEmpty(providerID),
			ExternalTransactionID: stringOrEmpty(extID),
			IdempotencyKey:        stringOrEmpty(key),
			PayloadHash:           stringOrEmpty(hash),
			RoundID:               stringOrEmpty(roundID),
			GameID:                stringOrEmpty(gameID),
			ReferenceExternalTxID: stringOrEmpty(refExtID),
		}
	}
	return wager.Rehydrate(s)
}

func stringOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (r *transactionRepository) Insert(ctx context.Context, t *wager.Transaction) error {
	s := t.Snapshot()
	var providerID, extID, key, hash, roundID, gameID, refExtID *string
	if e := s.External; e != nil {
		providerID, extID, key, hash = &e.ProviderID, &e.ExternalTransactionID, &e.IdempotencyKey, &e.PayloadHash
		roundID, gameID, refExtID = &e.RoundID, &e.GameID, nullableString(e.ReferenceExternalTxID)
	}
	var balanceAfter *int64
	if s.BalanceAfter != nil {
		u := s.BalanceAfter.Units()
		balanceAfter = &u
	}
	_, err := r.tx.Exec(ctx, `INSERT INTO wager_transactions (`+transactionColumns+`) VALUES
		($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23)`,
		s.ID, string(s.Origin), string(s.Kind), string(s.Status), s.WalletID, s.PlayerID, s.Money.Currency(), s.Money.Units(),
		providerID, extID, key, hash, roundID, gameID,
		refExtID, s.ReferenceTxID, nullableString(string(s.FailureCode)), balanceAfter,
		s.Attempts, s.NextAttemptAt, s.CreatedAt, s.UpdatedAt, s.ProcessedAt)
	return translateError(err)
}

func (r *transactionRepository) Update(ctx context.Context, t *wager.Transaction) error {
	s := t.Snapshot()
	var balanceAfter *int64
	if s.BalanceAfter != nil {
		u := s.BalanceAfter.Units()
		balanceAfter = &u
	}
	_, err := r.tx.Exec(ctx, `UPDATE wager_transactions SET
		status = $2, reference_transaction_id = $3, failure_code = $4, balance_after = $5,
		attempts = $6, next_attempt_at = $7, updated_at = $8, processed_at = $9
		WHERE id = $1`,
		s.ID, string(s.Status), s.ReferenceTxID, nullableString(string(s.FailureCode)), balanceAfter,
		s.Attempts, s.NextAttemptAt, s.UpdatedAt, s.ProcessedAt)
	return translateError(err)
}

func (r *transactionRepository) Get(ctx context.Context, id uuid.UUID) (*wager.Transaction, error) {
	return scanTransaction(r.tx.QueryRow(ctx, `SELECT `+transactionColumns+` FROM wager_transactions WHERE id = $1`, id))
}

func (r *transactionRepository) FindByIdempotencyKey(ctx context.Context, providerID, key string) (*wager.Transaction, error) {
	return scanTransaction(r.tx.QueryRow(ctx, `SELECT `+transactionColumns+` FROM wager_transactions
		WHERE origin = 'EXTERNAL' AND provider_id = $1 AND idempotency_key = $2`, providerID, key))
}

func (r *transactionRepository) FindByExternalID(ctx context.Context, providerID, externalID string) (*wager.Transaction, error) {
	return scanTransaction(r.tx.QueryRow(ctx, `SELECT `+transactionColumns+` FROM wager_transactions
		WHERE origin = 'EXTERNAL' AND provider_id = $1 AND external_transaction_id = $2`, providerID, externalID))
}

func (r *transactionRepository) HasProcessedReversal(ctx context.Context, providerID, referenceExternalID string) (bool, error) {
	var exists bool
	err := r.tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM wager_transactions
		WHERE provider_id = $1 AND reference_external_transaction_id = $2
		  AND kind IN ('REFUND', 'ROLLBACK') AND status = 'PROCESSED')`, providerID, referenceExternalID).Scan(&exists)
	return exists, translateError(err)
}

func (r *transactionRepository) ClaimPendingReferences(ctx context.Context, now time.Time, limit int) ([]*wager.Transaction, error) {
	rows, err := r.tx.Query(ctx, `SELECT `+transactionColumns+` FROM wager_transactions
		WHERE status = 'PENDING_REFERENCE' AND next_attempt_at <= $1
		ORDER BY next_attempt_at
		LIMIT $2
		FOR UPDATE SKIP LOCKED`, now, limit)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	var out []*wager.Transaction
	for rows.Next() {
		t, err := scanTransaction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, translateError(rows.Err())
}
