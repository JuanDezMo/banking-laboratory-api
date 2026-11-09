package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"accounts-api/internal/service"
	"accounts-api/internal/testdb"
)

const testSecret = "test-secret-with-at-least-32-characters"

// newTestRouter arma el router completo contra la BD de pruebas.
func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	db := testdb.Open(t)
	tokens := service.NewTokenIssuer(testSecret)
	auth := service.NewAuthService(db, service.NewPasswordHasher(4), tokens)
	return NewRouter(NewHandlers(auth, tokens))
}

// do envía una petición JSON al router y devuelve la respuesta.
func do(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

var isoMillis = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)

func TestRegisterEndpoint(t *testing.T) {
	router := newTestRouter(t)

	rec := do(router, http.MethodPost, "/auth/register",
		`{"name":"Ana Pérez","email":"Ana@Mail.com","password":"S3gura2026"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}

	// Exactamente los campos de la forma de un usuario, sin contraseña.
	var got map[string]any
	json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got) != 5 || got["name"] != "Ana Pérez" || got["email"] != "ana@mail.com" || got["role"] != "customer" {
		t.Errorf("body = %s", rec.Body)
	}
	if !isoMillis.MatchString(got["createdAt"].(string)) {
		t.Errorf("createdAt %v is not ISO 8601 with milliseconds", got["createdAt"])
	}

	rec = do(router, http.MethodPost, "/auth/register", `{"name":"Eva","email":"ana@mail.com","password":"S3gura2026"}`)
	if rec.Code != http.StatusConflict {
		t.Errorf("duplicate: status = %d, want 409", rec.Code)
	}
	assertErrorBody(t, rec, "EMAIL_ALREADY_REGISTERED", "Email already registered")
}

func TestRegisterEndpointErrors(t *testing.T) {
	router := newTestRouter(t)
	tests := []struct {
		name    string
		body    string
		message string
	}{
		{"1 invalid body (field missing)", `{"name":"Ana","email":"ana@mail.com"}`, "Invalid request body"},
		{"1 invalid body wins over invalid name", `{"name":"","email":1,"password":"x"}`, "Invalid request body"},
		{"2 invalid name", `{"name":"","email":"ana@mail.com","password":"S3gura2026"}`, "Invalid name"},
		{"3 invalid email", `{"name":"Ana","email":"ana","password":"S3gura2026"}`, "Invalid email"},
		{"4 password too short", `{"name":"Ana","email":"ana@mail.com","password":"1234567"}`, "Password too short"},
		{"5 password too long", `{"name":"Ana","email":"ana@mail.com","password":"` + strings.Repeat("a", 129) + `"}`, "Password too long"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(router, http.MethodPost, "/auth/register", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
			assertErrorBody(t, rec, "BAD_REQUEST", tc.message)
		})
	}
}

func TestLoginEndpoint(t *testing.T) {
	router := newTestRouter(t)
	do(router, http.MethodPost, "/auth/register", `{"name":"Ana","email":"ana@mail.com","password":"S3gura2026"}`)

	rec := do(router, http.MethodPost, "/auth/login", `{"email":"ana@mail.com","password":"S3gura2026"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var pair map[string]any
	json.Unmarshal(rec.Body.Bytes(), &pair)
	if len(pair) != 4 || pair["tokenType"] != "Bearer" || pair["expiresIn"] != float64(900) ||
		pair["accessToken"] == "" || pair["refreshToken"] == "" {
		t.Errorf("body = %s", rec.Body)
	}

	// 5 fallidos → 429 con Retry-After, aun con la contraseña correcta.
	for range 5 {
		rec = do(router, http.MethodPost, "/auth/login", `{"email":"ana@mail.com","password":"incorrecta"}`)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("failed login: status = %d", rec.Code)
		}
	}
	assertErrorBody(t, rec, "INVALID_CREDENTIALS", "Invalid email or password")

	rec = do(router, http.MethodPost, "/auth/login", `{"email":"ana@mail.com","password":"S3gura2026"}`)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Errorf("status = %d, Retry-After = %q; want 429 with Retry-After", rec.Code, rec.Header().Get("Retry-After"))
	}
	assertErrorBody(t, rec, "TOO_MANY_REQUESTS", "Too many login attempts")
}

func TestAuthRoutesMethodNotAllowed(t *testing.T) {
	router := newTestRouter(t)
	rec := do(router, http.MethodGet, "/auth/login", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
	assertErrorBody(t, rec, "METHOD_NOT_ALLOWED", "Method not allowed")
}

// loginPair registra y hace login de Ana; devuelve el par de tokens del JSON.
func loginPair(t *testing.T, router http.Handler) (access, refresh string) {
	t.Helper()
	do(router, http.MethodPost, "/auth/register", `{"name":"Ana","email":"ana@mail.com","password":"S3gura2026"}`)
	rec := do(router, http.MethodPost, "/auth/login", `{"email":"ana@mail.com","password":"S3gura2026"}`)
	var pair struct{ AccessToken, RefreshToken string }
	if err := json.Unmarshal(rec.Body.Bytes(), &pair); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("login: status = %d, body = %s", rec.Code, rec.Body)
	}
	return pair.AccessToken, pair.RefreshToken
}

// doAuth es como do, con "Authorization: Bearer <access>".
func doAuth(router http.Handler, method, path, access, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+access)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestRefreshEndpoint(t *testing.T) {
	router := newTestRouter(t)
	_, refresh := loginPair(t, router)

	rec := do(router, http.MethodPost, "/auth/refresh", `{"refreshToken":"`+refresh+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var pair map[string]any
	json.Unmarshal(rec.Body.Bytes(), &pair)
	if len(pair) != 4 || pair["tokenType"] != "Bearer" || pair["expiresIn"] != float64(900) ||
		pair["refreshToken"] == refresh || pair["refreshToken"] == "" {
		t.Errorf("body = %s", rec.Body)
	}

	// Reusar el token anterior → 401.
	rec = do(router, http.MethodPost, "/auth/refresh", `{"refreshToken":"`+refresh+`"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("reused token: status = %d, want 401", rec.Code)
	}
	assertErrorBody(t, rec, "INVALID_REFRESH_TOKEN", "Invalid refresh token")

	// Paso 1: cuerpo inválido → 400.
	for _, body := range []string{`{}`, `{"refreshToken":null}`, `{"refreshToken":1}`, `[]`} {
		rec = do(router, http.MethodPost, "/auth/refresh", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", body, rec.Code)
		}
		assertErrorBody(t, rec, "BAD_REQUEST", "Invalid request body")
	}
}

func TestLogoutEndpoint(t *testing.T) {
	router := newTestRouter(t)
	access, refresh := loginPair(t, router)
	body := `{"refreshToken":"` + refresh + `"}`

	// Paso 1: sin access token → 401 UNAUTHORIZED, aunque el cuerpo sea inválido.
	rec := do(router, http.MethodPost, "/auth/logout", `no es json`)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", rec.Code)
	}
	assertErrorBody(t, rec, "UNAUTHORIZED", "Missing or invalid token")

	// Paso 2: cuerpo inválido → 400.
	rec = doAuth(router, http.MethodPost, "/auth/logout", access, `{"token":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid body: status = %d, want 400", rec.Code)
	}

	// Logout correcto → 204 sin cuerpo.
	rec = doAuth(router, http.MethodPost, "/auth/logout", access, body)
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("logout: status = %d, body = %q; want 204 without body", rec.Code, rec.Body)
	}

	// Paso 3: el token ya no está vigente → 401 en logout y en refresh.
	rec = doAuth(router, http.MethodPost, "/auth/logout", access, body)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("second logout: status = %d, want 401", rec.Code)
	}
	assertErrorBody(t, rec, "INVALID_REFRESH_TOKEN", "Invalid refresh token")
	rec = do(router, http.MethodPost, "/auth/refresh", body)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("refresh after logout: status = %d, want 401", rec.Code)
	}
}
