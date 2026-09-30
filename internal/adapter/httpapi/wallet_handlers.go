package httpapi

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/domain"
	"github.com/pedroegerland/wager-service/internal/domain/money"
)

func (h *handlers) openWallet(w http.ResponseWriter, r *http.Request) {
	var req openWalletRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}
	playerID, err := uuid.Parse(req.PlayerID)
	if err != nil {
		h.writeErrorResponse(w, r, domain.Invalid("playerId", "must be a uuid"))
		return
	}
	initial, err := money.ParseNonNegative(req.InitialBalance.Amount, req.InitialBalance.Currency)
	if err != nil {
		h.writeErrorResponse(w, r, err)
		return
	}
	wal, err := h.wallets.Open(r.Context(), playerID, initial, CorrelationID(r.Context()))
	if err != nil {
		h.writeErrorResponse(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, walletResponseFrom(wal))
}

func (h *handlers) getWallet(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidPathParam(w, r, "walletId")
	if !ok {
		return
	}
	wal, err := h.wallets.Get(r.Context(), id)
	if err != nil {
		h.writeErrorResponse(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, walletResponseFrom(wal))
}

func (h *handlers) getLedger(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidPathParam(w, r, "walletId")
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page, err := h.wallets.Ledger(r.Context(), id, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		if err.Error() == "bad cursor" {
			writeJSON(w, http.StatusBadRequest, errorBody{Code: "VALIDATION_ERROR", Message: "bad cursor", Field: "cursor"})
			return
		}
		h.writeErrorResponse(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ledgerResponseFrom(page))
}

func (h *handlers) reconcile(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidPathParam(w, r, "walletId")
	if !ok {
		return
	}
	rec, err := h.wallets.Reconcile(r.Context(), id)
	if err != nil {
		h.writeErrorResponse(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, reconciliationResponse{
		WalletID: rec.WalletID, StoredBalance: rec.StoredBalance.DTO(), CalculatedBalance: rec.CalculatedBalance.DTO(),
		Difference: rec.Difference.DTO(), Consistent: rec.Consistent, CheckedEntries: rec.CheckedEntries,
	})
}
