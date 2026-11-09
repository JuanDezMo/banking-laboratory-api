// Middleware: logs, recuperación de pánicos, autenticación y 404/405 en JSON.
package handler

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"accounts-api/internal/model"
)

// statusRecorder envuelve el ResponseWriter para saber qué estado se respondió.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK // Write sin WriteHeader implica 200
	}
	return r.ResponseWriter.Write(b)
}

// Unwrap permite a http.ResponseController llegar al ResponseWriter original.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// Logging registra método, ruta, estado y duración de cada petición.
// Nunca registra cuerpos, la query, tokens ni el encabezado Authorization.
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}

		next.ServeHTTP(rec, r)

		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

// Recover convierte un panic en un handler en 500 INTERNAL_ERROR con el cuerpo del
// catálogo (spec 7.1), en lugar de cortar la conexión. Registra el valor y la pila con
// slog.Error. Va dentro de Logging, para que el log de la petición muestre status=500.
// Si el handler ya había empezado a responder, solo se registra: no se puede cambiar el estado.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			value := recover()
			if value == nil {
				return
			}
			if value == http.ErrAbortHandler {
				panic(value) // cancelación deliberada de net/http: se respeta
			}
			slog.Error("panic", "method", r.Method, "path", r.URL.Path,
				"error", value, "stack", string(debug.Stack()))
			writeJSON(w, http.StatusInternalServerError,
				errorResponse{Code: model.ErrInternal.Code, Message: model.ErrInternal.Message})
		}()
		next.ServeHTTP(w, r)
	})
}

// notFoundRewriter detecta las respuestas 404 y 405 en texto plano que genera http.ServeMux
// y las reemplaza por el error JSON del catálogo (I-07).
type notFoundRewriter struct {
	http.ResponseWriter
	rewritten bool // true si se reemplazó la respuesta; lo que escriba ServeMux se descarta
}

func (w *notFoundRewriter) WriteHeader(status int) {
	isPlainText := strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain")
	if isPlainText && (status == http.StatusNotFound || status == http.StatusMethodNotAllowed) {
		w.rewritten = true
		w.Header().Del("X-Content-Type-Options")
		err := model.ErrRouteNotFound
		if status == http.StatusMethodNotAllowed {
			err = model.ErrMethodNotAllowed // ServeMux ya puso el encabezado Allow; se conserva
		}
		writeError(w.ResponseWriter, err)
		return
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *notFoundRewriter) Write(b []byte) (int, error) {
	if w.rewritten {
		return len(b), nil // descarta "404 page not found" de ServeMux
	}
	return w.ResponseWriter.Write(b)
}

func (w *notFoundRewriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// JSONNotFound hace que las rutas inexistentes y los métodos no soportados respondan
// en JSON. Los handlers propios siempre escriben application/json, así que no se ven afectados.
func JSONNotFound(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&notFoundRewriter{ResponseWriter: w}, r)
	})
}

// ---------------------------------------------------------------------------
// Autenticación (I-08) y roles
// ---------------------------------------------------------------------------

// authUserKey es la clave del usuario autenticado en el context. Un tipo propio y sin
// exportar evita choques con claves de otros paquetes.
type authUserKey struct{}

// Auth exige un access token válido en "Authorization: Bearer <token>" y guarda el usuario
// en el context de la petición. Cualquier fallo responde 401 UNAUTHORIZED y no llama a next.
func (h *Handlers) Auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			writeError(w, model.ErrUnauthorized)
			return
		}
		userID, role, err := h.tokens.Validate(token)
		if err != nil {
			writeError(w, model.ErrUnauthorized)
			return
		}
		user := model.AuthUser{ID: userID, Role: role}
		next(w, r.WithContext(context.WithValue(r.Context(), authUserKey{}, user)))
	}
}

// bearerToken extrae el token de "Bearer <token>". La palabra Bearer no distingue
// mayúsculas (I-08).
func bearerToken(header string) (string, bool) {
	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return "", false
	}
	return token, true
}

// currentUser devuelve el usuario que guardó Auth. Solo se usa en handlers protegidos por Auth:
// si falta, es un error de programación al registrar la ruta.
func currentUser(r *http.Request) model.AuthUser {
	user, ok := r.Context().Value(authUserKey{}).(model.AuthUser)
	if !ok {
		panic("currentUser: route is not wrapped with Auth")
	}
	return user
}

// requireRole responde 403 FORBIDDEN y devuelve false si el usuario no tiene uno de los
// roles permitidos. Va después de Auth (401) y antes de leer el cuerpo (spec 6).
//
//	user := currentUser(r)
//	if !requireRole(w, user, model.RoleOperator) { return }
func requireRole(w http.ResponseWriter, user model.AuthUser, allowed ...model.Role) bool {
	for _, role := range allowed {
		if user.Role == role {
			return true
		}
	}
	writeError(w, model.ErrForbidden)
	return false
}
