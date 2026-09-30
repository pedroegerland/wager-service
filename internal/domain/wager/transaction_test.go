package wager

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/domain"
	"github.com/pedroegerland/wager-service/internal/domain/money"
)

var now = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func brl(u int64) money.Money { return money.MustFromUnits(u, "BRL") }

func ext(ref string) External {
	return External{
		ProviderID: "provider-a", ExternalTransactionID: "tx-1", IdempotencyKey: "provider-a:tx-1",
		PayloadHash: "abc", RoundID: "round-1", GameID: "game", ReferenceExternalTxID: ref,
	}
}

func TestParseExternalKind(t *testing.T) {
	for _, ok := range []string{"BET", "WIN", "LOSS", "REFUND", "ROLLBACK"} {
		if _, err := ParseExternalKind(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"OPENING", "bet", "", "DEPOSIT"} {
		if _, err := ParseExternalKind(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestNewExternalAmountPolicy(t *testing.T) {
	cases := []struct {
		kind   Kind
		amount money.Money
		ref    string
		ok     bool
	}{
		{KindBet, brl(100), "", true},
		{KindBet, brl(0), "", false},
		{KindWin, brl(100), "", true},
		{KindWin, brl(100), "bet-1", true},
		{KindWin, brl(0), "", false},
		{KindLoss, brl(0), "", true},
		{KindLoss, brl(1), "", false},
		{KindRefund, brl(100), "bet-1", true},
		{KindRefund, brl(100), "", false},
		{KindRefund, brl(0), "bet-1", false},
		{KindRollback, brl(100), "bet-1", true},
		{KindRollback, brl(100), "", false},
		{KindBet, brl(100), "bet-1", false},
		{KindLoss, brl(0), "bet-1", false},
		{KindOpening, brl(100), "", false},
		{Kind("NOPE"), brl(100), "", false},
	}
	for _, c := range cases {
		tx, err := NewExternal(c.kind, uuid.New(), uuid.New(), c.amount, ext(c.ref), now)
		if c.ok && err != nil {
			t.Errorf("%s %s ref=%q: unexpected %v", c.kind, c.amount, c.ref, err)
		}
		if !c.ok {
			var ve *domain.ValidationError
			if !errors.As(err, &ve) {
				t.Errorf("%s %s ref=%q: expected validation error, got %v", c.kind, c.amount, c.ref, err)
			}
			continue
		}
		if tx.Status() != StatusPending {
			t.Errorf("new tx must be PENDING, got %s", tx.Status())
		}
		if tx.Origin() != OriginExternal {
			t.Errorf("origin: %s", tx.Origin())
		}
	}
}

func TestNewExternalRequiresMetadata(t *testing.T) {
	e := ext("")
	e.GameID = ""
	if _, err := NewExternal(KindBet, uuid.New(), uuid.New(), brl(1), e, now); err == nil {
		t.Error("missing gameId should fail")
	}
	if _, err := NewExternal(KindBet, uuid.New(), uuid.New(), money.Money{}, ext(""), now); err == nil {
		t.Error("zero-value money should fail")
	}
	if _, err := NewExternal(KindBet, uuid.New(), uuid.New(), brl(-1), ext(""), now); err == nil {
		t.Error("negative money should fail")
	}
}

func TestOpening(t *testing.T) {
	tx, err := NewOpening(uuid.New(), uuid.New(), brl(1000), now)
	if err != nil {
		t.Fatal(err)
	}
	if tx.Status() != StatusProcessed || tx.Origin() != OriginInternal || tx.External() != nil {
		t.Errorf("opening state: %s %s %v", tx.Status(), tx.Origin(), tx.External())
	}
	if tx.BalanceAfter() == nil || tx.BalanceAfter().Units() != 1000 {
		t.Error("opening should record balanceAfter")
	}
	if _, err := NewOpening(uuid.New(), uuid.New(), brl(0), now); err == nil {
		t.Error("zero opening should not exist")
	}
}

func TestTransitions(t *testing.T) {
	tx, _ := NewExternal(KindBet, uuid.New(), uuid.New(), brl(100), ext(""), now)
	if err := tx.MarkProcessed(brl(900), now); err != nil {
		t.Fatal(err)
	}
	if tx.Status() != StatusProcessed || tx.ProcessedAt() == nil {
		t.Error("not processed")
	}
	var te *domain.TransitionError
	if err := tx.Reject(CodeInsufficientFunds, brl(900), now); !errors.As(err, &te) {
		t.Errorf("terminal -> rejected should fail: %v", err)
	}
	if err := tx.MarkPendingReference(now, now); !errors.As(err, &te) {
		t.Errorf("terminal -> pending ref should fail: %v", err)
	}
	if err := tx.Fail(CodeInternalError, now); !errors.As(err, &te) {
		t.Errorf("terminal -> failed should fail: %v", err)
	}
}

func TestPendingReferenceThenReject(t *testing.T) {
	tx, _ := NewExternal(KindRefund, uuid.New(), uuid.New(), brl(100), ext("bet-1"), now)
	if err := tx.MarkPendingReference(now.Add(time.Second), now); err != nil {
		t.Fatal(err)
	}
	if err := tx.MarkPendingReference(now.Add(2*time.Second), now); err != nil {
		t.Fatal("re-parking should be allowed:", err)
	}
	if tx.Attempts() != 2 {
		t.Errorf("attempts=%d", tx.Attempts())
	}
	if err := tx.Reject(CodeReferenceNotFound, brl(100), now); err != nil {
		t.Fatal(err)
	}
	if tx.FailureCode() != CodeReferenceNotFound || tx.NextAttemptAt() != nil {
		t.Error("rejection state incomplete")
	}
	if err := tx.ResolveReference(uuid.New()); !errors.Is(err, ErrTerminal) {
		t.Errorf("resolve after terminal: %v", err)
	}
}

func TestRejectNeedsCode(t *testing.T) {
	tx, _ := NewExternal(KindBet, uuid.New(), uuid.New(), brl(100), ext(""), now)
	if err := tx.Reject("", brl(1), now); err == nil {
		t.Error("empty code should fail")
	}
	if tx.Status() != StatusPending {
		t.Error("failed reject must not change status")
	}
}

func TestRehydrateRoundTrip(t *testing.T) {
	tx, _ := NewExternal(KindWin, uuid.New(), uuid.New(), brl(100), ext("bet-1"), now)
	_ = tx.ResolveReference(uuid.New())
	_ = tx.MarkProcessed(brl(200), now)

	back, err := Rehydrate(tx.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if back.ID() != tx.ID() || back.Status() != StatusProcessed || back.ReferenceTxID() == nil {
		t.Error("snapshot lost state")
	}

	if err := back.MarkProcessed(brl(1), now); err == nil {
		t.Error("rehydrated terminal tx should not transition")
	}

	s := tx.Snapshot()
	s.Origin = OriginInternal
	if _, err := Rehydrate(s); err == nil {
		t.Error("internal origin with external metadata should fail")
	}
}
