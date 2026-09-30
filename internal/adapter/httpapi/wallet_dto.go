package httpapi

import (
	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/app/port"
	"github.com/pedroegerland/wager-service/internal/domain/money"
	"github.com/pedroegerland/wager-service/internal/domain/wallet"
)

type openWalletRequest struct {
	PlayerID       string    `json:"playerId"`
	InitialBalance money.DTO `json:"initialBalance"`
}

type walletResponse struct {
	ID       uuid.UUID `json:"id"`
	PlayerID uuid.UUID `json:"playerId"`
	Balance  money.DTO `json:"balance"`
	Version  int64     `json:"version"`
}

func walletResponseFrom(w *wallet.Wallet) walletResponse {
	return walletResponse{ID: w.ID(), PlayerID: w.PlayerID(), Balance: w.Balance().DTO(), Version: w.Version()}
}

type ledgerEntryResponse struct {
	ID            uuid.UUID `json:"id"`
	WalletID      uuid.UUID `json:"walletId"`
	TransactionID uuid.UUID `json:"transactionId"`
	Direction     string    `json:"direction"`
	Money         money.DTO `json:"money"`
	BalanceBefore money.DTO `json:"balanceBefore"`
	BalanceAfter  money.DTO `json:"balanceAfter"`
	CreatedAt     string    `json:"createdAt"`
}

type ledgerResponse struct {
	Entries    []ledgerEntryResponse `json:"entries"`
	NextCursor string                `json:"nextCursor,omitempty"`
}

func ledgerResponseFrom(p port.LedgerPage) ledgerResponse {
	out := ledgerResponse{Entries: make([]ledgerEntryResponse, 0, len(p.Entries)), NextCursor: p.NextCursor}
	for _, e := range p.Entries {
		out.Entries = append(out.Entries, ledgerEntryResponse{
			ID: e.ID(), WalletID: e.WalletID(), TransactionID: e.TransactionID(), Direction: string(e.Direction()),
			Money: e.Amount().DTO(), BalanceBefore: e.BalanceBefore().DTO(), BalanceAfter: e.BalanceAfter().DTO(),
			CreatedAt: e.CreatedAt().UTC().Format("2006-01-02T15:04:05.000Z07:00"),
		})
	}
	return out
}

type reconciliationResponse struct {
	WalletID          uuid.UUID `json:"walletId"`
	StoredBalance     money.DTO `json:"storedBalance"`
	CalculatedBalance money.DTO `json:"calculatedBalance"`
	Difference        money.DTO `json:"difference"`
	Consistent        bool      `json:"consistent"`
	CheckedEntries    int       `json:"checkedEntries"`
}
