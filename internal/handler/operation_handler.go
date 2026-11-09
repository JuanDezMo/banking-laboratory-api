// Recarga y transferencia.
package handler

import (
	"net/http"

	"accounts-api/internal/model"
	"accounts-api/internal/service"
)

// replayedHeader marca una respuesta repetida de una petición anterior con la misma key (RN-28).
const replayedHeader = "Idempotent-Replayed"

// Recharge atiende POST /recharge (F-08). Solo operator.
func (h *Handlers) Recharge(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if !requireRole(w, user, model.RoleOperator) { // paso 2
		return
	}
	key, err := service.NormalizeIdempotencyKey(r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeError(w, err) // paso 3
		return
	}

	// El error del cuerpo se pasa al service: se responde en el paso 5, después de revisar la key.
	body := decodeObject(r)
	in := service.RechargeInput{
		AccountNumber: body.String("accountNumber"),
		Amount:        body.Int64("amount"),
		RechargeType:  body.String("rechargeType"),
	}

	movement, replayed, err := h.recharges.Recharge(r.Context(), user, key, in, body.Err())
	if replayed {
		w.Header().Set(replayedHeader, "true") // también en un error guardado (404/422)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, newMovementResponse(movement))
}

// Transfer atiende POST /transfers (F-09). Solo customer, desde sus propias cuentas.
func (h *Handlers) Transfer(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if !requireRole(w, user, model.RoleCustomer) { // paso 2
		return
	}
	key, err := service.NormalizeIdempotencyKey(r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeError(w, err) // paso 3
		return
	}

	body := decodeObject(r)
	in := service.TransferInput{
		AccountOrigin:      body.String("accountOrigin"),
		AccountDestination: body.String("accountDestination"),
		Amount:             body.Int64("amount"),
	}

	result, replayed, err := h.transfers.Transfer(r.Context(), user, key, in, body.Err())
	if replayed {
		w.Header().Set(replayedHeader, "true")
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, transferResponse{
		TransferOut: newMovementResponse(result.Out),
		TransferIn:  newMovementResponse(result.In),
	})
}
