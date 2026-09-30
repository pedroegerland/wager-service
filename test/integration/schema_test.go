//go:build integration

package integration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/pedroegerland/wager-service/internal/adapter/postgres"
)

func pgCode(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

func TestSchemaGuards(t *testing.T) {
	ctx := context.Background()
	walletID, _ := openWallet(t, "10.00")

	_, err := pool.Exec(ctx, `UPDATE wallet_ledger_entries SET amount = amount + 1 WHERE wallet_id = $1`, walletID)
	if pgCode(err) != "23001" || !strings.Contains(err.Error(), "append-only") {
		t.Errorf("ledger update: %v", err)
	}
	_, err = pool.Exec(ctx, `DELETE FROM wallet_ledger_entries WHERE wallet_id = $1`, walletID)
	if pgCode(err) != "23001" {
		t.Errorf("ledger delete: %v", err)
	}

	_, err = pool.Exec(ctx, `UPDATE wallets SET balance = -1 WHERE id = $1`, walletID)
	if pgCode(err) != "23514" {
		t.Errorf("negative balance: %v", err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO wallet_ledger_entries (id, wallet_id, transaction_id, direction, amount, balance_before, balance_after, created_at)
		SELECT gen_random_uuid(), wallet_id, transaction_id, 'DEBIT', 100, 1000, 950, now() FROM wallet_ledger_entries WHERE wallet_id = $1 LIMIT 1`, walletID)
	if pgCode(err) != "23514" {
		t.Errorf("ledger arithmetic: %v", err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO wallet_ledger_entries (id, wallet_id, transaction_id, direction, amount, balance_before, balance_after, created_at)
		SELECT gen_random_uuid(), wallet_id, transaction_id, 'CREDIT', 1000, 0, 1000, now() FROM wallet_ledger_entries WHERE wallet_id = $1 LIMIT 1`, walletID)
	if pgCode(err) != "23505" {
		t.Errorf("duplicate ledger entry: %v", err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO wager_transactions (id, origin, kind, status, wallet_id, player_id, currency, amount, created_at, updated_at, processed_at)
		SELECT gen_random_uuid(), 'INTERNAL', 'OPENING', 'PROCESSED', id, player_id, currency, 100, now(), now(), now() FROM wallets WHERE id = $1`, walletID)
	if pgCode(err) != "23505" {
		t.Errorf("second opening: %v", err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO wager_transactions (id, origin, kind, status, wallet_id, player_id, currency, amount, created_at, updated_at)
		SELECT gen_random_uuid(), 'EXTERNAL', 'BET', 'PENDING', id, player_id, currency, 100, now(), now() FROM wallets WHERE id = $1`, walletID)
	if pgCode(err) != "23514" {
		t.Errorf("external without metadata: %v", err)
	}

	_, err = pool.Exec(ctx, `INSERT INTO wager_transactions (id, origin, kind, status, wallet_id, player_id, currency, amount, provider_id, external_transaction_id, idempotency_key, payload_hash, round_id, game_id, created_at, updated_at)
		SELECT gen_random_uuid(), 'EXTERNAL', 'BET', 'REJECTED', id, player_id, currency, 100, 'p', 'x', 'k', 'h', 'r', 'g', now(), now() FROM wallets WHERE id = $1`, walletID)
	if pgCode(err) != "23514" {
		t.Errorf("rejected without code: %v", err)
	}
}

func TestMigrationsRoundTrip(t *testing.T) {
	ctx := context.Background()
	name := "migtest_" + shortID()
	if _, err := pool.Exec(ctx, `CREATE DATABASE `+name); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DROP DATABASE `+name+` WITH (FORCE)`)

	url := strings.Replace(databaseURL, "/wager?", "/"+name+"?", 1)
	m, err := postgres.NewMigrator(url)
	must(t, err)
	defer m.Close()

	must(t, m.Up())
	v, dirty, err := m.Version()
	must(t, err)
	if v != 2 || dirty {
		t.Errorf("after up: v=%d dirty=%v", v, dirty)
	}
	must(t, m.Down())
	v, _, err = m.Version()
	must(t, err)
	if v != 0 {
		t.Errorf("after down: v=%d", v)
	}
	must(t, m.Up())
	must(t, m.Up())
}
