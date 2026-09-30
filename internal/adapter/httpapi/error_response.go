package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/pedroegerland/wager-service/internal/app/port"
	"github.com/pedroegerland/wager-service/internal/app/wagering"
	"github.com/pedroegerland/wager-service/internal/app/wallets"
	"github.com/pedroegerland/wager-service/internal/domain"
	"github.com/pedroegerland/wager-service/internal/domain/money"
)

func (h *handlers) writeErrorResponse(w http.ResponseWriter, r *http.Request, err error) {
	var ve *domain.ValidationError
	switch {
	case errors.As(err, &ve):
		writeJSON(w, http.StatusBadRequest, errorBody{Code: "VALIDATION_ERROR", Message: ve.Msg, Field: ve.Field})
	case errors.Is(err, money.ErrInvalidAmount), errors.Is(err, money.ErrInvalidCurrency),
		errors.Is(err, money.ErrNegativeNotAllowed), errors.Is(err, money.ErrOverflow):
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	case errors.Is(err, wallets.ErrWalletNotFound), errors.Is(err, wagering.ErrWalletNotFound),
		errors.Is(err, wallets.ErrTransactionNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
	case errors.Is(err, wallets.ErrWalletExists):
		writeError(w, http.StatusConflict, "WALLET_ALREADY_EXISTS", err.Error())
	case errors.Is(err, wagering.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "IDEMPOTENCY_KEY_CONFLICT", err.Error())
	case errors.Is(err, wagering.ErrExternalIDReused):
		writeError(w, http.StatusConflict, "EXTERNAL_TRANSACTION_ID_REUSED", err.Error())
	case errors.Is(err, port.ErrUnavailable), errors.Is(err, context.DeadlineExceeded):
		w.Header().Set("Retry-After", "2")
		writeError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "temporarily unavailable, retry later")
	default:
		h.log.ErrorContext(r.Context(), "unhandled error", "err", err, "correlationId", CorrelationID(r.Context()))
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal error")
	}
}
