package wagering

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/app/port"
	"github.com/pedroegerland/wager-service/internal/domain/money"
	"github.com/pedroegerland/wager-service/internal/domain/wager"
)

type Operation struct {
	ProviderID                     string
	ExternalTransactionID          string
	IdempotencyKey                 string
	PlayerID                       uuid.UUID
	WalletID                       uuid.UUID
	RoundID                        string
	GameID                         string
	Kind                           wager.Kind
	Money                          money.Money
	ReferenceExternalTransactionID string

	CorrelationID string

	Inbox *port.InboxMessage
}

func (c Operation) Hash() string {
	fields := map[string]any{
		"providerId":            c.ProviderID,
		"externalTransactionId": c.ExternalTransactionID,
		"playerId":              c.PlayerID.String(),
		"walletId":              c.WalletID.String(),
		"roundId":               c.RoundID,
		"gameId":                c.GameID,
		"kind":                  string(c.Kind),
		"money":                 map[string]string{"amount": c.Money.String(), "currency": c.Money.Currency()},
	}
	if c.ReferenceExternalTransactionID != "" {
		fields["referenceExternalTransactionId"] = c.ReferenceExternalTransactionID
	}
	b, _ := json.Marshal(fields)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
