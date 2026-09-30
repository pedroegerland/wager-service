//go:build integration

package integration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/adapter/postgres"
	"github.com/pedroegerland/wager-service/internal/app/port"
	"github.com/pedroegerland/wager-service/internal/domain/money"
	"github.com/pedroegerland/wager-service/internal/domain/wager"
	"github.com/pedroegerland/wager-service/internal/domain/wallet"
	"github.com/pedroegerland/wager-service/internal/worker"
)

func TestUnitOfWorkRollsBackEverythingOnError(t *testing.T) {
	uow := postgres.NewUnitOfWork(pool)
	now := time.Now().UTC()
	w, err := wallet.Open(uuid.New(), money.MustFromUnits(1000, "BRL"), now)
	must(t, err)
	tx, err := wager.NewOpening(w.ID(), w.PlayerID(), w.Balance(), now)
	must(t, err)
	entry, err := w.OpeningEntry(tx.ID())
	must(t, err)

	boom := errors.New("simulated failure after the ledger write")
	err = uow.Do(context.Background(), func(ctx context.Context, r port.Repos) error {
		must(t, r.Wallets.Insert(ctx, w))
		must(t, r.Transactions.Insert(ctx, tx))
		must(t, r.Ledger.Insert(ctx, entry))
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected the callback error, got %v", err)
	}
	if queryInt64(t, `SELECT COUNT(*) FROM wallets WHERE id = $1`, w.ID()) != 0 ||
		queryInt64(t, `SELECT COUNT(*) FROM wager_transactions WHERE id = $1`, tx.ID()) != 0 ||
		queryInt64(t, `SELECT COUNT(*) FROM wallet_ledger_entries WHERE wallet_id = $1`, w.ID()) != 0 {
		t.Error("rows survived a rolled back transaction")
	}
}

type flakyPublisher struct {
	failuresLeft int
	published    []uuid.UUID
}

func (p *flakyPublisher) Publish(_ context.Context, rec port.OutboxRecord) error {
	if p.failuresLeft > 0 {
		p.failuresLeft--
		return errors.New("broker unavailable")
	}
	p.published = append(p.published, rec.ID)
	return nil
}

func TestOutboxRetriesAfterPublishFailure(t *testing.T) {
	id := uuid.Must(uuid.NewV7())
	owner := "retry-test-" + shortID()
	_, err := pool.Exec(context.Background(), `INSERT INTO outbox_events
		(id, aggregate_id, event_type, payload, occurred_at, next_attempt_at, locked_by, locked_until)
		VALUES ($1, gen_random_uuid(), 'IntegrationTestRetry', '{}', now(), now(), $2, now() + interval '1 minute')`, id, owner)
	must(t, err)

	pub := &flakyPublisher{failuresLeft: 1}
	p := worker.NewOutboxPublisher(postgres.NewUnitOfWork(pool), pub, worker.OutboxConfig{
		Owner: owner, BatchSize: 100, BaseBackoff: 300 * time.Millisecond, MaxBackoff: time.Second,
	}, quietLog(), nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err = p.PublishDueEvents(ctx)
	must(t, err)
	var attempts int
	var lastErr string
	var published, locked bool
	must(t, pool.QueryRow(ctx, `SELECT attempts, COALESCE(last_error, ''), published_at IS NOT NULL, locked_by IS NOT NULL FROM outbox_events WHERE id = $1`, id).
		Scan(&attempts, &lastErr, &published, &locked))
	if attempts != 1 || !strings.Contains(lastErr, "broker unavailable") || published || locked {
		t.Fatalf("after the failed attempt: attempts=%d lastErr=%q published=%v locked=%v", attempts, lastErr, published, locked)
	}
	if pub.failuresLeft != 0 {
		t.Fatal("the flaky publisher was never called")
	}

	waitUntil(t, 20*time.Second, "record to be published after the backoff", func() bool {
		_, err := p.PublishDueEvents(ctx)
		must(t, err)
		return queryInt64(t, `SELECT COUNT(*) FROM outbox_events WHERE id = $1 AND published_at IS NOT NULL`, id) == 1
	})
	if queryInt64(t, `SELECT attempts FROM outbox_events WHERE id = $1`, id) < 2 {
		t.Error("expected at least two attempts")
	}
}

func TestReconciliationReportsDivergence(t *testing.T) {
	walletID, playerID := openWallet(t, "100.00")
	if r := submitAPI(t, operation{ext: "bet-" + shortID(), kind: "BET", amount: "10.00", walletID: walletID, playerID: playerID}); r.Status != 200 {
		t.Fatal(string(r.Raw))
	}
	_, err := pool.Exec(context.Background(), `UPDATE wallets SET balance = balance + 250 WHERE id = $1`, walletID)
	must(t, err)

	r := apiRequest(t, "POST", "/wallets/"+walletID+"/reconciliation", token(t, "wallet-admin"), nil, nil)
	if r.Status != 200 || r.bool("consistent") || r.money("difference") != "2.50" || r.money("storedBalance") != "92.50" || r.money("calculatedBalance") != "90.00" {
		t.Errorf("divergence: %d %s", r.Status, r.Raw)
	}
	if queryInt64(t, `SELECT balance FROM wallets WHERE id = $1`, walletID) != 9250 {
		t.Error("reconciliation must not change the balance")
	}
	if inProcess {
		m := apiRequest(t, "GET", "/metrics", "", nil, nil)
		if !strings.Contains(string(m.Raw), "wallet_reconciliation_divergences_total") || strings.Contains(string(m.Raw), "wallet_reconciliation_divergences_total 0") {
			t.Error("divergence metric should have been incremented")
		}
	}
}

func TestLossProducesProcessedEventOnly(t *testing.T) {
	walletID, playerID := openWallet(t, "10.00")
	loss := operation{ext: "loss-" + shortID(), kind: "LOSS", amount: "0.00", walletID: walletID, playerID: playerID}
	r := submitAPI(t, loss)
	if r.Status != 200 {
		t.Fatal(string(r.Raw))
	}
	txID := r.str("transactionId")
	if n := queryInt64(t, `SELECT COUNT(*) FROM outbox_events WHERE event_type = 'WagerTransactionProcessed' AND payload->'data'->>'transactionId' = $1`, txID); n != 1 {
		t.Errorf("processed events for LOSS: %d", n)
	}
	if n := queryInt64(t, `SELECT COUNT(*) FROM outbox_events WHERE event_type = 'WalletBalanceChanged' AND payload->'data'->>'transactionId' = $1`, txID); n != 0 {
		t.Errorf("LOSS must not emit WalletBalanceChanged, got %d", n)
	}
	if queryInt64(t, `SELECT version FROM wallets WHERE id = $1`, walletID) != 1 || ledgerCount(t, walletID) != 1 {
		t.Error("LOSS changed version or ledger")
	}
}

func TestExpiredTokenIsRejected(t *testing.T) {
	tok := token(t, "short-lived")
	if r := apiRequest(t, "GET", "/wallets/"+uuid.NewString(), tok, nil, nil); r.Status != 404 {
		t.Fatalf("fresh short-lived token should authenticate (expected 404 for a random wallet), got %d %s", r.Status, r.Raw)
	}
	time.Sleep(2500 * time.Millisecond)
	if r := apiRequest(t, "GET", "/wallets/"+uuid.NewString(), tok, nil, nil); r.Status != 401 {
		t.Errorf("expired token: %d %s", r.Status, r.Raw)
	}
}
