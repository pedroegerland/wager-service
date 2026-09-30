package sqs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/app/port"
	"github.com/pedroegerland/wager-service/internal/app/wagering"
	"github.com/pedroegerland/wager-service/internal/domain"
	"github.com/pedroegerland/wager-service/internal/domain/money"
	"github.com/pedroegerland/wager-service/internal/domain/wager"
)

type messageEnvelope struct {
	MessageID  string          `json:"messageId"`
	Type       string          `json:"type"`
	OccurredAt string          `json:"occurredAt"`
	Data       json.RawMessage `json:"data"`
}

type requestedOperation struct {
	ProviderID                     string    `json:"providerId"`
	ExternalTransactionID          string    `json:"externalTransactionId"`
	IdempotencyKey                 string    `json:"idempotencyKey"`
	PlayerID                       string    `json:"playerId"`
	WalletID                       string    `json:"walletId"`
	RoundID                        string    `json:"roundId"`
	GameID                         string    `json:"gameId"`
	Kind                           string    `json:"kind"`
	Money                          money.DTO `json:"money"`
	ReferenceExternalTransactionID string    `json:"referenceExternalTransactionId,omitempty"`
	CorrelationID                  string    `json:"correlationId,omitempty"`
}

type permanentError struct{ err error }

func (p permanentError) Error() string { return p.err.Error() }
func (p permanentError) Unwrap() error { return p.err }

func isPermanentError(err error) bool {
	var ve *domain.ValidationError
	var perm permanentError
	return errors.As(err, &perm) || errors.As(err, &ve) ||
		errors.Is(err, wagering.ErrWalletNotFound) ||
		errors.Is(err, wagering.ErrIdempotencyConflict) ||
		errors.Is(err, wagering.ErrExternalIDReused) ||
		errors.Is(err, wagering.ErrInboxHashMismatch) ||
		errors.Is(err, wagering.ErrInboxInconsistent)
}

func (c *OperationConsumer) operationFromMessage(body, sqsID string) (wagering.Operation, error) {
	var env messageEnvelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		return wagering.Operation{}, permanentError{fmt.Errorf("messageEnvelope: %w", err)}
	}
	if env.MessageID == "" {
		return wagering.Operation{}, permanentError{errors.New("messageEnvelope: messageId is required")}
	}
	if env.Type != "WagerTransactionRequested" {
		return wagering.Operation{}, permanentError{fmt.Errorf("messageEnvelope: unsupported type %q", env.Type)}
	}
	var d requestedOperation
	if err := json.Unmarshal(env.Data, &d); err != nil {
		return wagering.Operation{}, permanentError{fmt.Errorf("data: %w", err)}
	}
	if d.IdempotencyKey == "" {
		return wagering.Operation{}, permanentError{errors.New("data.idempotencyKey is required")}
	}
	playerID, err := uuid.Parse(d.PlayerID)
	if err != nil {
		return wagering.Operation{}, permanentError{errors.New("data.playerId must be a uuid")}
	}
	walletID, err := uuid.Parse(d.WalletID)
	if err != nil {
		return wagering.Operation{}, permanentError{errors.New("data.walletId must be a uuid")}
	}
	kind, err := wager.ParseExternalKind(d.Kind)
	if err != nil {
		return wagering.Operation{}, permanentError{err}
	}
	m, err := money.ParseNonNegative(d.Money.Amount, d.Money.Currency)
	if err != nil {
		return wagering.Operation{}, permanentError{err}
	}
	corr := d.CorrelationID
	if corr == "" {
		corr = env.MessageID
	}
	sum := sha256.Sum256([]byte(body))
	return wagering.Operation{
		ProviderID:                     d.ProviderID,
		ExternalTransactionID:          d.ExternalTransactionID,
		IdempotencyKey:                 d.IdempotencyKey,
		PlayerID:                       playerID,
		WalletID:                       walletID,
		RoundID:                        d.RoundID,
		GameID:                         d.GameID,
		Kind:                           kind,
		Money:                          m,
		ReferenceExternalTransactionID: d.ReferenceExternalTransactionID,
		CorrelationID:                  corr,
		Inbox: &port.InboxMessage{
			ConsumerName: OperationConsumerName,
			MessageID:    env.MessageID,
			PayloadHash:  hex.EncodeToString(sum[:]),
		},
	}, nil
}
