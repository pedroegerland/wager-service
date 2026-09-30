package httpapi

import (
	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/domain/money"
	"github.com/pedroegerland/wager-service/internal/domain/wager"
)

type submitOperationRequest struct {
	ProviderID                     string    `json:"providerId"`
	ExternalTransactionID          string    `json:"externalTransactionId"`
	PlayerID                       string    `json:"playerId"`
	WalletID                       string    `json:"walletId"`
	RoundID                        string    `json:"roundId"`
	GameID                         string    `json:"gameId"`
	Kind                           string    `json:"kind"`
	Money                          money.DTO `json:"money"`
	ReferenceExternalTransactionID string    `json:"referenceExternalTransactionId,omitempty"`
}

type submitOperationResponse struct {
	TransactionID    uuid.UUID  `json:"transactionId"`
	Status           string     `json:"status"`
	Balance          *money.DTO `json:"balance,omitempty"`
	FailureCode      string     `json:"failureCode,omitempty"`
	IdempotentReplay bool       `json:"idempotentReplay"`
}

type transactionResponse struct {
	ID                             uuid.UUID  `json:"id"`
	Kind                           string     `json:"kind"`
	Status                         string     `json:"status"`
	WalletID                       uuid.UUID  `json:"walletId"`
	PlayerID                       uuid.UUID  `json:"playerId"`
	Money                          money.DTO  `json:"money"`
	ProviderID                     string     `json:"providerId,omitempty"`
	ExternalTransactionID          string     `json:"externalTransactionId,omitempty"`
	RoundID                        string     `json:"roundId,omitempty"`
	GameID                         string     `json:"gameId,omitempty"`
	ReferenceExternalTransactionID string     `json:"referenceExternalTransactionId,omitempty"`
	ReferenceTransactionID         *uuid.UUID `json:"referenceTransactionId,omitempty"`
	FailureCode                    string     `json:"failureCode,omitempty"`
	BalanceAfter                   *money.DTO `json:"balanceAfter,omitempty"`
	Attempts                       int        `json:"attempts"`
	NextAttemptAt                  *string    `json:"nextAttemptAt,omitempty"`
	CreatedAt                      string     `json:"createdAt"`
	ProcessedAt                    *string    `json:"processedAt,omitempty"`
}

func transactionResponseFrom(t *wager.Transaction) transactionResponse {
	const layout = "2006-01-02T15:04:05.000Z07:00"
	r := transactionResponse{
		ID: t.ID(), Kind: string(t.Kind()), Status: string(t.Status()), WalletID: t.WalletID(), PlayerID: t.PlayerID(),
		Money: t.Money().DTO(), ReferenceTransactionID: t.ReferenceTxID(), FailureCode: string(t.FailureCode()),
		Attempts: t.Attempts(), CreatedAt: t.CreatedAt().UTC().Format(layout),
	}
	if e := t.External(); e != nil {
		r.ProviderID, r.ExternalTransactionID, r.RoundID, r.GameID = e.ProviderID, e.ExternalTransactionID, e.RoundID, e.GameID
		r.ReferenceExternalTransactionID = e.ReferenceExternalTxID
	}
	if b := t.BalanceAfter(); b != nil {
		d := b.DTO()
		r.BalanceAfter = &d
	}
	if n := t.NextAttemptAt(); n != nil {
		s := n.UTC().Format(layout)
		r.NextAttemptAt = &s
	}
	if p := t.ProcessedAt(); p != nil {
		s := p.UTC().Format(layout)
		r.ProcessedAt = &s
	}
	return r
}
