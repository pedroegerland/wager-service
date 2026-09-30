package wagering

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/pedroegerland/wager-service/internal/app/port"
	"github.com/pedroegerland/wager-service/internal/domain/event"
	"github.com/pedroegerland/wager-service/internal/domain/wager"
	"github.com/pedroegerland/wager-service/internal/domain/wallet"
)

type outcome struct {
	tx            *wager.Transaction
	wallet        *wallet.Wallet
	versionBefore int64
	entry         *wallet.LedgerEntry
	events        []event.Envelope
}

func (p *Processor) persistOutcome(ctx context.Context, r port.Repos, out *outcome) error {
	if out.entry != nil {
		if err := r.Ledger.Insert(ctx, *out.entry); err != nil {
			return err
		}
		if err := r.Wallets.Update(ctx, out.wallet, out.versionBefore); err != nil {
			return err
		}
	}
	if len(out.events) > 0 {
		if err := r.Outbox.Add(ctx, out.events...); err != nil {
			return err
		}
	}
	return nil
}

func (p *Processor) applyBusinessRules(ctx context.Context, r port.Repos, tx *wager.Transaction, w *wallet.Wallet, now time.Time, correlationID string) (*outcome, error) {
	out := &outcome{tx: tx, wallet: w, versionBefore: w.Version()}
	ext := tx.External()

	reject := func(code wager.FailureCode) (*outcome, error) {
		if err := tx.Reject(code, w.Balance(), now); err != nil {
			return nil, err
		}
		out.events = append(out.events, event.NewWagerTransactionRejected(tx, correlationID))
		return out, nil
	}
	processed := func() (*outcome, error) {
		if err := tx.MarkProcessed(w.Balance(), now); err != nil {
			return nil, err
		}
		out.events = append(out.events, event.NewWagerTransactionProcessed(tx, correlationID))
		if out.entry != nil {
			out.events = append(out.events, event.NewWalletBalanceChanged(*out.entry, w.Version(), correlationID))
		}
		return out, nil
	}
	move := func(dir wallet.Direction, insufficient wager.FailureCode) (*outcome, error) {
		var entry wallet.LedgerEntry
		var err error
		if dir == wallet.Debit {
			entry, err = w.Debit(tx.Money(), tx.ID(), now)
		} else {
			entry, err = w.Credit(tx.Money(), tx.ID(), now)
		}
		if errors.Is(err, wallet.ErrInsufficientFunds) {
			return reject(insufficient)
		}
		if err != nil {
			return nil, fmt.Errorf("apply %s: %w", tx.Kind(), err)
		}
		out.entry = &entry
		return processed()
	}

	if w.PlayerID() != tx.PlayerID() {
		return reject(wager.CodeWalletMismatch)
	}
	if w.Currency() != tx.Money().Currency() {
		return reject(wager.CodeCurrencyMismatch)
	}

	var ref *wager.Transaction
	if ext.ReferenceExternalTxID != "" {
		var err error
		ref, err = r.Transactions.FindByExternalID(ctx, ext.ProviderID, ext.ReferenceExternalTxID)
		switch {
		case errors.Is(err, port.ErrNotFound):
			return p.waitForReference(out, now, correlationID)
		case err != nil:
			return nil, err
		}
		switch ref.Status() {
		case wager.StatusPending, wager.StatusPendingReference:

			return p.waitForReference(out, now, correlationID)
		case wager.StatusRejected, wager.StatusFailed:
			return reject(wager.CodeReferenceNotProcessed)
		}
		refExt := ref.External()
		if ref.WalletID() != tx.WalletID() || ref.PlayerID() != tx.PlayerID() ||
			refExt == nil || refExt.RoundID != ext.RoundID ||
			ref.Money().Currency() != tx.Money().Currency() {
			return reject(wager.CodeReferenceMismatch)
		}
		if err := tx.ResolveReference(ref.ID()); err != nil {
			return nil, err
		}
	}

	switch tx.Kind() {
	case wager.KindBet:
		return move(wallet.Debit, wager.CodeInsufficientFunds)

	case wager.KindWin:
		if ref != nil && ref.Kind() != wager.KindBet {
			return reject(wager.CodeReferenceKindNotAllowed)
		}
		return move(wallet.Credit, "")

	case wager.KindLoss:
		return processed()

	case wager.KindRefund:
		if ref.Kind() != wager.KindBet {
			return reject(wager.CodeReferenceKindNotAllowed)
		}
		if !ref.Money().Equal(tx.Money()) {
			return reject(wager.CodeReferenceMismatch)
		}
		reversed, err := r.Transactions.HasProcessedReversal(ctx, ext.ProviderID, ext.ReferenceExternalTxID)
		if err != nil {
			return nil, err
		}
		if reversed {
			return reject(wager.CodeReferenceAlreadyReversed)
		}
		return move(wallet.Credit, "")

	case wager.KindRollback:
		var dir wallet.Direction
		switch ref.Kind() {
		case wager.KindBet:
			dir = wallet.Credit
		case wager.KindWin, wager.KindRefund:
			dir = wallet.Debit
		default:
			return reject(wager.CodeReferenceKindNotAllowed)
		}
		if !ref.Money().Equal(tx.Money()) {
			return reject(wager.CodeReferenceMismatch)
		}
		reversed, err := r.Transactions.HasProcessedReversal(ctx, ext.ProviderID, ext.ReferenceExternalTxID)
		if err != nil {
			return nil, err
		}
		if reversed {
			return reject(wager.CodeReferenceAlreadyReversed)
		}
		return move(dir, wager.CodeReversalInsufficientFunds)
	}
	return nil, fmt.Errorf("unhandled kind %s", tx.Kind())
}
