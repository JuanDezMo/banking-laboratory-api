package handler

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"accounts-api/internal/model"
	"accounts-api/internal/service"
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

// ---------------------------------------------------------------------------
// Auth y requireRole (T3.3, T3.4). No usan la BD.
// ---------------------------------------------------------------------------

const authTestUserID = "11111111-2222-4333-8444-555555555555"

// authRequest pasa una petición con el encabezado Authorization dado por Auth.
// Devuelve la respuesta y el usuario que vio el handler (nil si no se llamó).
func authRequest(t *testing.T, authorization string) (*httptest.ResponseRecorder, *model.AuthUser) {
	t.Helper()
	h := &Handlers{tokens: service.NewTokenIssuer(testSecret)}
	var seen *model.AuthUser
	protected := h.Auth(func(w http.ResponseWriter, r *http.Request) {
		user := currentUser(r)
		seen = &user
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	protected(rec, req)
	return rec, seen
}

func TestAuthRejects(t *testing.T) {
	valid, _ := service.NewTokenIssuer(testSecret).Issue(authTestUserID, model.RoleCustomer)
	otherSecret, _ := service.NewTokenIssuer("another-secret-with-at-least-32-chars").Issue(authTestUserID, model.RoleCustomer)

	for name, header := range map[string]string{
		"no header":      "",
		"no scheme":      valid,
		"basic scheme":   "Basic " + valid,
		"empty token":    "Bearer ",
		"garbage token":  "Bearer abc.def.ghi",
		"other secret":   "Bearer " + otherSecret,
		"two spaces":     "Bearer  " + valid,
		"trailing space": "Bearer " + valid + " ",
	} {
		t.Run(name, func(t *testing.T) {
			rec, seen := authRequest(t, header)
			if rec.Code != http.StatusUnauthorized || seen != nil {
				t.Fatalf("status = %d, handler called = %v; want 401 without calling the handler", rec.Code, seen != nil)
			}
			assertErrorBody(t, rec, "UNAUTHORIZED", "Missing or invalid token")
		})
	}
}

func TestAuthValidToken(t *testing.T) {
	token, _ := service.NewTokenIssuer(testSecret).Issue(authTestUserID, model.RoleOperator)

	// La palabra Bearer no distingue mayúsculas (I-08).
	for _, scheme := range []string{"Bearer", "bearer", "BEARER"} {
		rec, seen := authRequest(t, scheme+" "+token)
		if rec.Code != http.StatusNoContent || seen == nil {
			t.Fatalf("%s: status = %d, handler called = %v", scheme, rec.Code, seen != nil)
		}
		if seen.ID != authTestUserID || seen.Role != model.RoleOperator {
			t.Errorf("%s: user = %+v", scheme, *seen)
		}
	}
}

func TestRequireRole(t *testing.T) {
	customer := model.AuthUser{ID: authTestUserID, Role: model.RoleCustomer}

	rec := httptest.NewRecorder()
	if requireRole(rec, customer, model.RoleOperator) {
		t.Fatal("customer passed an operator-only check")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	assertErrorBody(t, rec, "FORBIDDEN", "Access denied")

	rec = httptest.NewRecorder()
	if !requireRole(rec, customer, model.RoleCustomer, model.RoleOperator) || rec.Body.Len() != 0 {
		t.Errorf("allowed role: must return true without writing a response")
	}
}
