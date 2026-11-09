package handler

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"accounts-api/internal/model"
)

func TestLoggingDoesNotLogSecrets(t *testing.T) {
	// Se captura la salida de slog en un buffer y se restaura al terminar.
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	handler := Logging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	req := httptest.NewRequest(http.MethodPost, "/auth/login?token=query-secret",
		strings.NewReader(`{"password":"body-secret"}`))
	req.Header.Set("Authorization", "Bearer header-secret")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	out := logs.String()
	for _, secret := range []string{"header-secret", "body-secret", "query-secret", "Authorization"} {
		if strings.Contains(out, secret) {
			t.Errorf("log contains %q: %s", secret, out)
		}
	}
	for _, expected := range []string{"method=POST", "path=/auth/login", "status=201"} {
		if !strings.Contains(out, expected) {
			t.Errorf("log should contain %q: %s", expected, out)
		}
	}
}

func TestJSONNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /accounts", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, []string{})
	})
	// Un handler propio que responde 404 en JSON no debe ser modificado.
	mux.HandleFunc("GET /accounts/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, model.ErrAccountNotFound)
	})
	handler := JSONNotFound(mux)

	tests := []struct {
		name    string
		method  string
		path    string
		status  int
		code    string
		message string
	}{
		{"unknown route", http.MethodGet, "/nope", 404, "ROUTE_NOT_FOUND", "Route not found"},
		{"method not allowed", http.MethodDelete, "/accounts", 405, "METHOD_NOT_ALLOWED", "Method not allowed"},
		{"own 404 is kept", http.MethodGet, "/accounts/123", 404, "NOT_FOUND", "Account not found"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))

			if rec.Code != tc.status {
				t.Errorf("status = %d, want %d", rec.Code, tc.status)
			}
			assertErrorBody(t, rec, tc.code, tc.message)
		})
	}

	// La ruta existente sigue funcionando igual.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/accounts", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "[]" {
		t.Errorf("existing route: status %d, body %s", rec.Code, rec.Body.String())
	}
}

func TestJSONNotFoundKeepsAllowHeader(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /accounts", func(w http.ResponseWriter, r *http.Request) {})
	rec := httptest.NewRecorder()
	JSONNotFound(mux).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/accounts", nil))

	if allow := rec.Header().Get("Allow"); !strings.Contains(allow, "GET") {
		t.Errorf("Allow = %q, want it to include GET", allow)
	}
}
