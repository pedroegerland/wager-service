package event

import (
	"time"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/domain/money"
	"github.com/pedroegerland/wager-service/internal/domain/wager"
)

type WagerTransactionProcessed struct {
	TransactionID          uuid.UUID  `json:"transactionId"`
	WalletID               uuid.UUID  `json:"walletId"`
	PlayerID               uuid.UUID  `json:"playerId"`
	Kind                   wager.Kind `json:"kind"`
	Money                  money.DTO  `json:"money"`
	BalanceAfter           money.DTO  `json:"balanceAfter"`
	ProviderID             string     `json:"providerId,omitempty"`
	ExternalTransactionID  string     `json:"externalTransactionId,omitempty"`
	RoundID                string     `json:"roundId,omitempty"`
	GameID                 string     `json:"gameId,omitempty"`
	ReferenceTransactionID *uuid.UUID `json:"referenceTransactionId,omitempty"`
	ProcessedAt            time.Time  `json:"processedAt"`
}

type WagerTransactionRejected struct {
	TransactionID         uuid.UUID         `json:"transactionId"`
	WalletID              uuid.UUID         `json:"walletId"`
	PlayerID              uuid.UUID         `json:"playerId"`
	Kind                  wager.Kind        `json:"kind"`
	Money                 money.DTO         `json:"money"`
	FailureCode           wager.FailureCode `json:"failureCode"`
	ProviderID            string            `json:"providerId"`
	ExternalTransactionID string            `json:"externalTransactionId"`
	RoundID               string            `json:"roundId"`
	RejectedAt            time.Time         `json:"rejectedAt"`
}

type WagerTransactionPendingReference struct {
	TransactionID                  uuid.UUID  `json:"transactionId"`
	WalletID                       uuid.UUID  `json:"walletId"`
	Kind                           wager.Kind `json:"kind"`
	ProviderID                     string     `json:"providerId"`
	ExternalTransactionID          string     `json:"externalTransactionId"`
	ReferenceExternalTransactionID string     `json:"referenceExternalTransactionId"`
	Attempt                        int        `json:"attempt"`
	NextAttemptAt                  *time.Time `json:"nextAttemptAt,omitempty"`
}

func NewWagerTransactionProcessed(tx *wager.Transaction, correlationID string) Envelope {
	at := tx.UpdatedAt()
	if tx.ProcessedAt() != nil {
		at = *tx.ProcessedAt()
	}
	d := WagerTransactionProcessed{
		TransactionID: tx.ID(), WalletID: tx.WalletID(), PlayerID: tx.PlayerID(), Kind: tx.Kind(),
		Money: tx.Money().DTO(), ReferenceTransactionID: tx.ReferenceTxID(), ProcessedAt: at,
	}
	if b := tx.BalanceAfter(); b != nil {
		d.BalanceAfter = b.DTO()
	}
	if e := tx.External(); e != nil {
		d.ProviderID, d.ExternalTransactionID, d.RoundID, d.GameID = e.ProviderID, e.ExternalTransactionID, e.RoundID, e.GameID
	}
	return newEnvelope(TypeWagerTransactionProcessed, tx.WalletID(), correlationID, tx.ID().String(), at, d)
}

func NewWagerTransactionRejected(tx *wager.Transaction, correlationID string) Envelope {
	e := tx.External()
	at := tx.UpdatedAt()
	d := WagerTransactionRejected{
		TransactionID: tx.ID(), WalletID: tx.WalletID(), PlayerID: tx.PlayerID(), Kind: tx.Kind(),
		Money: tx.Money().DTO(), FailureCode: tx.FailureCode(), RejectedAt: at,
	}
	if e != nil {
		d.ProviderID, d.ExternalTransactionID, d.RoundID = e.ProviderID, e.ExternalTransactionID, e.RoundID
	}
	return newEnvelope(TypeWagerTransactionRejected, tx.WalletID(), correlationID, tx.ID().String(), at, d)
}

func NewWagerTransactionPendingReference(tx *wager.Transaction, correlationID string) Envelope {
	e := tx.External()
	d := WagerTransactionPendingReference{
		TransactionID: tx.ID(), WalletID: tx.WalletID(), Kind: tx.Kind(), Attempt: tx.Attempts(), NextAttemptAt: tx.NextAttemptAt(),
	}
	if e != nil {
		d.ProviderID, d.ExternalTransactionID, d.ReferenceExternalTransactionID = e.ProviderID, e.ExternalTransactionID, e.ReferenceExternalTxID
	}
	return newEnvelope(TypeWagerTransactionPendingReference, tx.WalletID(), correlationID, tx.ID().String(), tx.UpdatedAt(), d)
}
