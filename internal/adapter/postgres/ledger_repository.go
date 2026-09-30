package postgres

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/pedroegerland/wager-service/internal/app/port"
	"github.com/pedroegerland/wager-service/internal/domain/money"
	"github.com/pedroegerland/wager-service/internal/domain/wallet"
)

type ledgerRepository struct {
	tx pgx.Tx
}

func (r *ledgerRepository) Insert(ctx context.Context, e wallet.LedgerEntry) error {
	_, err := r.tx.Exec(ctx, `INSERT INTO wallet_ledger_entries
		(id, wallet_id, transaction_id, direction, amount, balance_before, balance_after, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		e.ID(), e.WalletID(), e.TransactionID(), string(e.Direction()), e.Amount().Units(),
		e.BalanceBefore().Units(), e.BalanceAfter().Units(), e.CreatedAt())
	return translateError(err)
}

func encodeCursor(seq int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(seq, 10)))
}

func decodeCursor(c string) (int64, error) {
	if c == "" {
		return 0, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return 0, fmt.Errorf("bad cursor")
	}
	n, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("bad cursor")
	}
	return n, nil
}

func (r *ledgerRepository) List(ctx context.Context, walletID uuid.UUID, cursor string, limit int) (port.LedgerPage, error) {
	after, err := decodeCursor(cursor)
	if err != nil {
		return port.LedgerPage{}, err
	}

	cur, err := r.walletCurrency(ctx, walletID)
	if err != nil {
		return port.LedgerPage{}, err
	}
	rows, err := r.tx.Query(ctx, `SELECT seq, id, wallet_id, transaction_id, direction, amount, balance_before, balance_after, created_at
		FROM wallet_ledger_entries
		WHERE wallet_id = $1 AND seq > $2
		ORDER BY seq
		LIMIT $3`, walletID, after, limit+1)
	if err != nil {
		return port.LedgerPage{}, translateError(err)
	}
	defer rows.Close()

	var page port.LedgerPage
	var lastSeq int64
	for rows.Next() {
		var (
			seq                 int64
			id, wID, txID       uuid.UUID
			dir                 string
			amt, before, afterB int64
			createdAt           time.Time
		)
		if err := rows.Scan(&seq, &id, &wID, &txID, &dir, &amt, &before, &afterB, &createdAt); err != nil {
			return port.LedgerPage{}, translateError(err)
		}
		if len(page.Entries) == limit {
			page.NextCursor = encodeCursor(lastSeq)
			break
		}
		e, err := wallet.RehydrateLedgerEntry(id, wID, txID, wallet.Direction(dir),
			money.MustFromUnits(amt, cur), money.MustFromUnits(before, cur), money.MustFromUnits(afterB, cur), createdAt)
		if err != nil {
			return port.LedgerPage{}, err
		}
		page.Entries = append(page.Entries, e)
		lastSeq = seq
	}
	return page, translateError(rows.Err())
}

func (r *ledgerRepository) walletCurrency(ctx context.Context, walletID uuid.UUID) (string, error) {
	var cur string
	err := r.tx.QueryRow(ctx, `SELECT currency FROM wallets WHERE id = $1`, walletID).Scan(&cur)
	return cur, translateError(err)
}

func (r *ledgerRepository) Sum(ctx context.Context, walletID uuid.UUID) (int64, int, error) {
	var net int64
	var count int
	err := r.tx.QueryRow(ctx, `SELECT
		COALESCE(SUM(CASE direction WHEN 'CREDIT' THEN amount ELSE -amount END), 0), COUNT(*)
		FROM wallet_ledger_entries WHERE wallet_id = $1`, walletID).Scan(&net, &count)
	return net, count, translateError(err)
}
