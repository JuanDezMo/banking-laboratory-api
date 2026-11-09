// Endpoints /accounts*.
package handler

import (
	"net/http"

	"accounts-api/internal/model"
)

// CreateAccount atiende POST /accounts (F-05). Solo customer.
func (h *Handlers) CreateAccount(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if !requireRole(w, user, model.RoleCustomer) { // paso 2
		return
	}
	body := decodeObject(r)
	accountType := body.String("accountType")
	if err := body.Err(); err != nil {
		writeError(w, err) // paso 3
		return
	}

	account, err := h.accounts.Create(r.Context(), user, accountType)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, newAccountResponse(account))
}

// ListAccounts atiende GET /accounts (F-06). Solo customer, sin paginación.
func (h *Handlers) ListAccounts(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if !requireRole(w, user, model.RoleCustomer) {
		return
	}

	accounts, err := h.accounts.List(r.Context(), user)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]accountResponse, len(accounts)) // sin cuentas: "items": [] (no null)
	for i, a := range accounts {
		items[i] = newAccountResponse(a)
	}
	writeJSON(w, http.StatusOK, accountListResponse{Items: items})
}

// GetAccount atiende GET /accounts/{id} (F-07). customer (las suyas) y operator.
func (h *Handlers) GetAccount(w http.ResponseWriter, r *http.Request) {
	account, err := h.accounts.Get(r.Context(), currentUser(r), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newAccountResponse(account))
}
