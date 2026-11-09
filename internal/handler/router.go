// Rutas y cadena de middleware.
package handler

import (
	"net/http"

	"accounts-api/internal/service"
)

// Handlers reúne los services que usan los handlers HTTP.
type Handlers struct {
	auth   *service.AuthService
	tokens *service.TokenIssuer // valida el access token en el middleware Auth
}

func NewHandlers(auth *service.AuthService, tokens *service.TokenIssuer) *Handlers {
	return &Handlers{auth: auth, tokens: tokens}
}

// NewRouter registra las rutas (Go 1.22+: método y ruta en el patrón) y aplica el middleware.
func NewRouter(h *Handlers) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/register", h.Register)
	mux.HandleFunc("POST /auth/login", h.Login)
	mux.HandleFunc("POST /auth/refresh", h.Refresh)
	mux.HandleFunc("POST /auth/logout", h.Auth(h.Logout))
	return Logging(JSONNotFound(mux))
}
