package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/adapter/auth"
	"github.com/pedroegerland/wager-service/internal/app/wagering"
	"github.com/pedroegerland/wager-service/internal/domain"
	"github.com/pedroegerland/wager-service/internal/domain/money"
	"github.com/pedroegerland/wager-service/internal/domain/wager"
)

func (h *handlers) submitOperation(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" || len(key) > 255 {
		writeJSON(w, http.StatusBadRequest, errorBody{Code: "VALIDATION_ERROR", Message: "Idempotency-Key header is required", Field: "Idempotency-Key"})
		return
	}
	var req submitOperationRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}
	p, _ := auth.PrincipalFrom(r.Context())
	if !p.OwnsProvider(req.ProviderID) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "token is not allowed to act for this provider")
		return
	}
	op, err := operationFromRequest(req, key, CorrelationID(r.Context()))
	if err != nil {
		h.writeErrorResponse(w, r, err)
		return
	}
	res, err := h.wagers.Process(r.Context(), op)
	if err != nil {
		h.writeErrorResponse(w, r, err)
		return
	}
	writeJSON(w, httpStatusFor(res.Status), submitOperationResponseFrom(res))
}

func operationFromRequest(req submitOperationRequest, key, correlationID string) (wagering.Operation, error) {
	playerID, err := uuid.Parse(req.PlayerID)
	if err != nil {
		return wagering.Operation{}, domain.Invalid("playerId", "must be a uuid")
	}
	walletID, err := uuid.Parse(req.WalletID)
	if err != nil {
		return wagering.Operation{}, domain.Invalid("walletId", "must be a uuid")
	}
	kind, err := wager.ParseExternalKind(req.Kind)
	if err != nil {
		return wagering.Operation{}, domain.Invalid("kind", "%v", err)
	}
	m, err := money.ParseNonNegative(req.Money.Amount, req.Money.Currency)
	if err != nil {
		return wagering.Operation{}, domain.Invalid("money", "%v", err)
	}
	return wagering.Operation{
		ProviderID:                     req.ProviderID,
		ExternalTransactionID:          req.ExternalTransactionID,
		IdempotencyKey:                 key,
		PlayerID:                       playerID,
		WalletID:                       walletID,
		RoundID:                        req.RoundID,
		GameID:                         req.GameID,
		Kind:                           kind,
		Money:                          m,
		ReferenceExternalTransactionID: req.ReferenceExternalTransactionID,
		CorrelationID:                  correlationID,
	}, nil
}

func httpStatusFor(s wager.Status) int {
	switch s {
	case wager.StatusProcessed:
		return http.StatusOK
	case wager.StatusPendingReference, wager.StatusPending:
		return http.StatusAccepted
	case wager.StatusRejected:
		return http.StatusUnprocessableEntity
	}
	return http.StatusInternalServerError
}

func submitOperationResponseFrom(res wagering.Result) submitOperationResponse {
	out := submitOperationResponse{
		TransactionID: res.TransactionID, Status: string(res.Status),
		FailureCode: string(res.FailureCode), IdempotentReplay: res.IdempotentReplay,
	}
	if res.Balance != nil {
		d := res.Balance.DTO()
		out.Balance = &d
	}
	return out
}

func (h *handlers) getTransaction(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidPathParam(w, r, "transactionId")
	if !ok {
		return
	}
	tx, err := h.wallets.GetTransaction(r.Context(), id)
	if err != nil {
		h.writeErrorResponse(w, r, err)
		return
	}
	p, _ := auth.PrincipalFrom(r.Context())

	if !p.IsInternal() && (tx.ProviderID() == "" || !p.OwnsProvider(tx.ProviderID())) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "transaction not found")
		return
	}
	writeJSON(w, http.StatusOK, transactionResponseFrom(tx))
}

func (h *handlers) getProviderTransaction(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("providerId")
	p, _ := auth.PrincipalFrom(r.Context())
	if !p.OwnsProvider(providerID) {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "token is not allowed to read this provider")
		return
	}
	tx, err := h.wallets.GetProviderTransaction(r.Context(), providerID, r.PathValue("externalTransactionId"))
	if err != nil {
		h.writeErrorResponse(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, transactionResponseFrom(tx))
}
