// Middleware: logs, autenticación (I3) y 404/405 en JSON.
package handler

import (
	"log/slog"
	"net/http"
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
