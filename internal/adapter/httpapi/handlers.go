package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/pedroegerland/wager-service/internal/app/wagering"
	"github.com/pedroegerland/wager-service/internal/app/wallets"
)

const maxBody = 64 << 10

type handlers struct {
	wallets *wallets.Service
	wagers  *wagering.Processor
	ready   ReadinessChecker
	log     *slog.Logger
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "malformed json: "+err.Error())
		return false
	}
	return true
}

func uuidPathParam(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Code: "VALIDATION_ERROR", Message: "must be a uuid", Field: name})
		return uuid.Nil, false
	}
	return id, true
}
