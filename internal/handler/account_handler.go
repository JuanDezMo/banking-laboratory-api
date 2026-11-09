// Endpoints /accounts*.
package handler

import (
	"net/http"

	"accounts-api/internal/model"
	"accounts-api/internal/service"
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

// ListMovements atiende GET /accounts/{id}/movements?page=N (F-10). customer (las suyas) y operator.
func (h *Handlers) ListMovements(w http.ResponseWriter, r *http.Request) {
	pages, present := r.URL.Query()["page"]
	rawPage := ""
	if present {
		rawPage = pages[0] // si se repite, vale el primero
	}

	result, err := h.accounts.Movements(r.Context(), currentUser(r), r.PathValue("id"), rawPage, present)
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]movementResponse, len(result.Items)) // página vacía: "items": [] (no null)
	for i, m := range result.Items {
		items[i] = newMovementResponse(m)
	}
	writeJSON(w, http.StatusOK, movementPageResponse{
		Page:     result.Page,
		PageSize: service.MovementPageSize,
		Items:    items,
	})
}
