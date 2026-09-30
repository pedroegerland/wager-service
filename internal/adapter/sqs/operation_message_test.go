package sqs

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/app/wagering"
	"github.com/pedroegerland/wager-service/internal/domain/wager"
)

const validMessage = `{
  "messageId": "msg-123",
  "type": "WagerTransactionRequested",
  "occurredAt": "2026-09-08T12:00:00.000Z",
  "data": {
    "providerId": "provider-a",
    "externalTransactionId": "transaction-123",
    "idempotencyKey": "provider-a:transaction-123",
    "playerId": "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1",
    "walletId": "0192f291-27dd-7d3f-8071-5f8685deef37",
    "roundId": "round-987",
    "gameId": "fortune-chimp",
    "kind": "BET",
    "money": { "amount": "25.00", "currency": "BRL" }
  }
}`

func TestOperationFromMessage(t *testing.T) {
	c := &OperationConsumer{}
	op, err := c.operationFromMessage(validMessage, "sqs-1")
	if err != nil {
		t.Fatal(err)
	}
	if op.Kind != wager.KindBet || op.Money.Units() != 2500 || op.IdempotencyKey != "provider-a:transaction-123" {
		t.Errorf("%+v", op)
	}
	if op.Inbox == nil || op.Inbox.MessageID != "msg-123" || op.Inbox.ConsumerName != OperationConsumerName || op.Inbox.PayloadHash == "" {
		t.Errorf("inbox %+v", op.Inbox)
	}
	if op.CorrelationID != "msg-123" {
		t.Errorf("correlation defaults to messageId, got %s", op.CorrelationID)
	}
	if op.PlayerID != uuid.MustParse("0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1") {
		t.Error("player id")
	}
	again, _ := c.operationFromMessage(validMessage, "sqs-2")
	if again.Inbox.PayloadHash != op.Inbox.PayloadHash {
		t.Error("hash must depend on the body only")
	}
}

func TestOperationFromMessageRejectsInvalid(t *testing.T) {
	c := &OperationConsumer{}
	cases := map[string]string{
		"not json":        `{`,
		"no message id":   strings.Replace(validMessage, `"messageId": "msg-123",`, "", 1),
		"wrong type":      strings.Replace(validMessage, "WagerTransactionRequested", "Other", 1),
		"no key":          strings.Replace(validMessage, `"idempotencyKey": "provider-a:transaction-123",`, "", 1),
		"bad player":      strings.Replace(validMessage, "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1", "nope", 1),
		"bad wallet":      strings.Replace(validMessage, "0192f291-27dd-7d3f-8071-5f8685deef37", "nope", 1),
		"opening kind":    strings.Replace(validMessage, `"BET"`, `"OPENING"`, 1),
		"bad amount":      strings.Replace(validMessage, `"25.00"`, `"25"`, 1),
		"negative amount": strings.Replace(validMessage, `"25.00"`, `"-1.00"`, 1),
	}
	for name, body := range cases {
		_, err := c.operationFromMessage(body, "sqs")
		if err == nil {
			t.Errorf("%s: expected error", name)
			continue
		}
		if !isPermanentError(err) {
			t.Errorf("%s: should be permanent, got %v", name, err)
		}
	}
}

func TestIsPermanentError(t *testing.T) {
	for _, err := range []error{wagering.ErrWalletNotFound, wagering.ErrIdempotencyConflict, wagering.ErrExternalIDReused, wagering.ErrInboxHashMismatch, wagering.ErrInboxInconsistent} {
		if !isPermanentError(err) {
			t.Errorf("%v should be permanent", err)
		}
	}
	if isPermanentError(errors.New("connection refused")) {
		t.Error("unknown errors are transient")
	}
}

func TestRedeliveryDelay(t *testing.T) {
	max := 30 * time.Second
	if d := redeliveryDelay(0, max); d != 2*time.Second {
		t.Errorf("first: %s", d)
	}
	if d := redeliveryDelay(3, max); d != 8*time.Second {
		t.Errorf("third: %s", d)
	}
	if d := redeliveryDelay(20, max); d != max {
		t.Errorf("capped: %s", d)
	}
}
