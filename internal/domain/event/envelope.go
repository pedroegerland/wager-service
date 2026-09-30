package event

import (
	"time"

	"github.com/google/uuid"
)

const (
	TypeWagerTransactionProcessed        = "WagerTransactionProcessed"
	TypeWagerTransactionRejected         = "WagerTransactionRejected"
	TypeWagerTransactionPendingReference = "WagerTransactionPendingReference"
	TypeWalletBalanceChanged             = "WalletBalanceChanged"
)

type Envelope struct {
	EventID       uuid.UUID `json:"eventId"`
	EventType     string    `json:"eventType"`
	AggregateID   uuid.UUID `json:"aggregateId"`
	CorrelationID string    `json:"correlationId"`
	CausationID   string    `json:"causationId,omitempty"`
	OccurredAt    time.Time `json:"occurredAt"`
	Version       int       `json:"version"`
	Data          any       `json:"data"`
}

func newEnvelope(typ string, aggregate uuid.UUID, correlationID, causationID string, at time.Time, data any) Envelope {
	return Envelope{
		EventID:       uuid.Must(uuid.NewV7()),
		EventType:     typ,
		AggregateID:   aggregate,
		CorrelationID: correlationID,
		CausationID:   causationID,
		OccurredAt:    at.UTC(),
		Version:       1,
		Data:          data,
	}
}
