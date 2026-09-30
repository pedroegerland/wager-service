package event

import (
	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/domain/money"
	"github.com/pedroegerland/wager-service/internal/domain/wallet"
)

type WalletBalanceChanged struct {
	WalletID      uuid.UUID        `json:"walletId"`
	TransactionID uuid.UUID        `json:"transactionId"`
	Direction     wallet.Direction `json:"direction"`
	Money         money.DTO        `json:"money"`
	BalanceBefore money.DTO        `json:"balanceBefore"`
	BalanceAfter  money.DTO        `json:"balanceAfter"`
	WalletVersion int64            `json:"walletVersion"`
}

func NewWalletBalanceChanged(entry wallet.LedgerEntry, walletVersion int64, correlationID string) Envelope {
	d := WalletBalanceChanged{
		WalletID: entry.WalletID(), TransactionID: entry.TransactionID(), Direction: entry.Direction(),
		Money: entry.Amount().DTO(), BalanceBefore: entry.BalanceBefore().DTO(), BalanceAfter: entry.BalanceAfter().DTO(),
		WalletVersion: walletVersion,
	}
	return newEnvelope(TypeWalletBalanceChanged, entry.WalletID(), correlationID, entry.TransactionID().String(), entry.CreatedAt(), d)
}
