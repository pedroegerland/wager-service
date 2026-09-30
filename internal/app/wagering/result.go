package wagering

import (
	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/domain/money"
	"github.com/pedroegerland/wager-service/internal/domain/wager"
)

type Result struct {
	TransactionID    uuid.UUID
	Status           wager.Status
	Balance          *money.Money
	FailureCode      wager.FailureCode
	IdempotentReplay bool
}

func resultFromTransaction(tx *wager.Transaction, replay bool) Result {
	return Result{
		TransactionID:    tx.ID(),
		Status:           tx.Status(),
		Balance:          tx.BalanceAfter(),
		FailureCode:      tx.FailureCode(),
		IdempotentReplay: replay,
	}
}
