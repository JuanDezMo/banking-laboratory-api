// Rutas y cadena de middleware.
package handler

import (
	"net/http"

	"accounts-api/internal/service"
)

// Handlers reúne los services que usan los handlers HTTP.
type Handlers struct {
	auth *service.AuthService
}

func NewHandlers(auth *service.AuthService) *Handlers {
	return &Handlers{auth: auth}
}

// NewRouter registra las rutas (Go 1.22+: método y ruta en el patrón) y aplica el middleware.
func NewRouter(h *Handlers) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/register", h.Register)
	mux.HandleFunc("POST /auth/login", h.Login)
	return Logging(JSONNotFound(mux))
}
