// Endpoints /auth/*.
package handler

import (
	"net/http"

	"accounts-api/internal/service"
)

// Register atiende POST /auth/register (F-01).
func (h *Handlers) Register(w http.ResponseWriter, r *http.Request) {
	body := decodeObject(r)
	in := service.RegisterInput{
		Name:     body.String("name"),
		Email:    body.String("email"),
		Password: body.String("password"),
	}
	if err := body.Err(); err != nil {
		writeError(w, err) // paso 1: cuerpo inválido
		return
	}

	user, err := h.auth.Register(r.Context(), in)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, newUserResponse(user))
}

// Login atiende POST /auth/login (F-02).
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	body := decodeObject(r)
	in := service.LoginInput{
		Email:    body.String("email"),
		Password: body.String("password"),
	}
	if err := body.Err(); err != nil {
		writeError(w, err)
		return
	}

	pair, err := h.auth.Login(r.Context(), in)
	if err != nil {
		writeError(w, err) // 429 incluye Retry-After
		return
	}
	writeJSON(w, http.StatusOK, newTokenPairResponse(pair))
}

// Refresh atiende POST /auth/refresh (F-03). Es público.
func (h *Handlers) Refresh(w http.ResponseWriter, r *http.Request) {
	body := decodeObject(r)
	token := body.String("refreshToken")
	if err := body.Err(); err != nil {
		writeError(w, err)
		return
	}

	pair, err := h.auth.Refresh(r.Context(), token)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newTokenPairResponse(pair))
}

// Logout atiende POST /auth/logout (F-04). El middleware Auth ya validó el access token (paso 1).
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	body := decodeObject(r)
	token := body.String("refreshToken")
	if err := body.Err(); err != nil {
		writeError(w, err) // paso 2
		return
	}

	if err := h.auth.Logout(r.Context(), currentUser(r), token); err != nil {
		writeError(w, err) // paso 3
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
