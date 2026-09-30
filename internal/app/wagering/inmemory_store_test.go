package wagering

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/app/port"
	"github.com/pedroegerland/wager-service/internal/domain/event"
	"github.com/pedroegerland/wager-service/internal/domain/wager"
	"github.com/pedroegerland/wager-service/internal/domain/wallet"
)

type inMemoryStore struct {
	mu      sync.Mutex
	wallets map[uuid.UUID]*wallet.Wallet
	txs     map[uuid.UUID]*wager.Transaction
	ledger  []wallet.LedgerEntry
	events  []event.Envelope
	inbox   map[string]string
}

func newInMemoryStore() *inMemoryStore {
	return &inMemoryStore{
		wallets: map[uuid.UUID]*wallet.Wallet{},
		txs:     map[uuid.UUID]*wager.Transaction{},
		inbox:   map[string]string{},
	}
}

func (s *inMemoryStore) repos() port.Repos {
	return port.Repos{
		Wallets:      inMemoryWallets{s},
		Transactions: inMemoryTransactions{s},
		Ledger:       inMemoryLedger{s},
		Outbox:       inMemoryOutbox{s},
		Inbox:        inMemoryInbox{s},
	}
}

func (s *inMemoryStore) Do(ctx context.Context, fn func(context.Context, port.Repos) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(ctx, s.repos())
}

func (s *inMemoryStore) DoSnapshot(ctx context.Context, fn func(context.Context, port.Repos) error) error {
	return s.Do(ctx, fn)
}

func (s *inMemoryStore) walletByID(id uuid.UUID) (*wallet.Wallet, error) {
	w, ok := s.wallets[id]
	if !ok {
		return nil, port.ErrNotFound
	}
	return cloneWallet(w), nil
}

func (s *inMemoryStore) transactionByID(id uuid.UUID) (*wager.Transaction, error) {
	t, ok := s.txs[id]
	if !ok {
		return nil, port.ErrNotFound
	}
	return cloneTransaction(t), nil
}

func (s *inMemoryStore) findTransaction(match func(*wager.Transaction) bool) (*wager.Transaction, error) {
	for _, t := range s.txs {
		if match(t) {
			return cloneTransaction(t), nil
		}
	}
	return nil, port.ErrNotFound
}

func cloneWallet(w *wallet.Wallet) *wallet.Wallet {
	c, _ := wallet.Rehydrate(w.ID(), w.PlayerID(), w.Balance(), w.Version(), w.CreatedAt(), w.UpdatedAt())
	return c
}

func cloneTransaction(t *wager.Transaction) *wager.Transaction {
	c, _ := wager.Rehydrate(t.Snapshot())
	return c
}

type inMemoryWallets struct{ *inMemoryStore }

func (s inMemoryWallets) Insert(_ context.Context, w *wallet.Wallet) error {
	s.wallets[w.ID()] = cloneWallet(w)
	return nil
}

func (s inMemoryWallets) Get(_ context.Context, id uuid.UUID) (*wallet.Wallet, error) {
	return s.walletByID(id)
}

func (s inMemoryWallets) GetForUpdate(_ context.Context, id uuid.UUID) (*wallet.Wallet, error) {
	return s.walletByID(id)
}

func (s inMemoryWallets) Update(_ context.Context, w *wallet.Wallet, expectedVersion int64) error {
	if s.wallets[w.ID()].Version() != expectedVersion {
		return port.ErrStale
	}
	s.wallets[w.ID()] = cloneWallet(w)
	return nil
}

type inMemoryTransactions struct{ *inMemoryStore }

func (s inMemoryTransactions) Insert(_ context.Context, t *wager.Transaction) error {
	if e := t.External(); e != nil {
		for _, other := range s.txs {
			oe := other.External()
			if oe == nil || oe.ProviderID != e.ProviderID {
				continue
			}
			if oe.IdempotencyKey == e.IdempotencyKey {
				return &port.ConflictError{Constraint: "wager_tx_provider_key_unique"}
			}
			if oe.ExternalTransactionID == e.ExternalTransactionID {
				return &port.ConflictError{Constraint: "wager_tx_provider_external_id_unique"}
			}
		}
	}
	s.txs[t.ID()] = cloneTransaction(t)
	return nil
}

func (s inMemoryTransactions) Update(_ context.Context, t *wager.Transaction) error {
	s.txs[t.ID()] = cloneTransaction(t)
	return nil
}

func (s inMemoryTransactions) Get(_ context.Context, id uuid.UUID) (*wager.Transaction, error) {
	return s.transactionByID(id)
}

func (s inMemoryTransactions) FindByIdempotencyKey(_ context.Context, providerID, key string) (*wager.Transaction, error) {
	return s.findTransaction(func(t *wager.Transaction) bool {
		e := t.External()
		return e != nil && e.ProviderID == providerID && e.IdempotencyKey == key
	})
}

func (s inMemoryTransactions) FindByExternalID(_ context.Context, providerID, externalID string) (*wager.Transaction, error) {
	return s.findTransaction(func(t *wager.Transaction) bool {
		e := t.External()
		return e != nil && e.ProviderID == providerID && e.ExternalTransactionID == externalID
	})
}

func (s inMemoryTransactions) HasProcessedReversal(_ context.Context, providerID, referenceExternalID string) (bool, error) {
	_, err := s.findTransaction(func(t *wager.Transaction) bool {
		e := t.External()
		return e != nil && e.ProviderID == providerID && e.ReferenceExternalTxID == referenceExternalID &&
			t.Kind().IsReversal() && t.Status() == wager.StatusProcessed
	})
	return err == nil, nil
}

func (s inMemoryTransactions) ClaimPendingReferences(_ context.Context, now time.Time, limit int) ([]*wager.Transaction, error) {
	var due []*wager.Transaction
	for _, t := range s.txs {
		if t.Status() == wager.StatusPendingReference && !t.NextAttemptAt().After(now) {
			due = append(due, cloneTransaction(t))
			if len(due) == limit {
				break
			}
		}
	}
	return due, nil
}

type inMemoryLedger struct{ *inMemoryStore }

func (s inMemoryLedger) Insert(_ context.Context, e wallet.LedgerEntry) error {
	s.ledger = append(s.ledger, e)
	return nil
}

func (s inMemoryLedger) List(context.Context, uuid.UUID, string, int) (port.LedgerPage, error) {
	return port.LedgerPage{}, nil
}

func (s inMemoryLedger) Sum(_ context.Context, walletID uuid.UUID) (int64, int, error) {
	var net int64
	count := 0
	for _, e := range s.ledger {
		if e.WalletID() != walletID {
			continue
		}
		count++
		if e.Direction() == wallet.Credit {
			net += e.Amount().Units()
		} else {
			net -= e.Amount().Units()
		}
	}
	return net, count, nil
}

type inMemoryOutbox struct{ *inMemoryStore }

func (s inMemoryOutbox) Add(_ context.Context, events ...event.Envelope) error {
	s.events = append(s.events, events...)
	return nil
}

func (s inMemoryOutbox) Claim(context.Context, string, time.Duration, int) ([]port.OutboxRecord, error) {
	return nil, nil
}

func (s inMemoryOutbox) MarkPublished(context.Context, uuid.UUID, string) error { return nil }

func (s inMemoryOutbox) Reschedule(context.Context, uuid.UUID, string, time.Time, string) error {
	return nil
}

func (s inMemoryOutbox) Lag(context.Context) (time.Duration, error) { return 0, nil }

type inMemoryInbox struct{ *inMemoryStore }

func (s inMemoryInbox) Register(_ context.Context, m port.InboxMessage, _ time.Time) (port.InboxState, error) {
	key := m.ConsumerName + "/" + m.MessageID
	if hash, ok := s.inbox[key]; ok {
		if hash != m.PayloadHash {
			return port.InboxHashMismatch, nil
		}
		return port.InboxDuplicate, nil
	}
	s.inbox[key] = m.PayloadHash
	return port.InboxNew, nil
}

func (s inMemoryInbox) Complete(context.Context, port.InboxMessage, time.Time) error { return nil }

type steppingClock struct{ now time.Time }

func (c *steppingClock) Now() time.Time {
	c.now = c.now.Add(time.Millisecond)
	return c.now
}
