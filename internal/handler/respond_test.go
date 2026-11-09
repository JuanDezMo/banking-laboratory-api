package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"accounts-api/internal/model"
)

// ---------------------------------------------------------------------------
// writeJSON y writeError
// ---------------------------------------------------------------------------

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusCreated, map[string]string{"name": "Ana Pérez"})

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if body := rec.Body.String(); body != `{"name":"Ana Pérez"}` {
		t.Errorf("body = %s", body)
	}
}

// Cada error del catálogo responde con su estado HTTP del spec (7.2) y { code, message }.
func TestWriteErrorCatalogStatus(t *testing.T) {
	tests := []struct {
		err    *model.AppError
		status int
	}{
		{model.ErrInvalidRequestBody, 400}, {model.ErrInvalidName, 400}, {model.ErrInvalidEmail, 400},
		{model.ErrPasswordTooShort, 400}, {model.ErrPasswordTooLong, 400}, {model.ErrInvalidAccountType, 400},
		{model.ErrInvalidAccountID, 400}, {model.ErrInvalidAccountNumber, 400}, {model.ErrInvalidRechargeType, 400},
		{model.ErrInvalidAmount, 400}, {model.ErrAmountTooLarge, 400}, {model.ErrSameAccount, 400},
		{model.ErrInvalidPagination, 400}, {model.ErrInvalidIdempotencyKey, 400},
		{model.ErrUnauthorized, 401}, {model.ErrInvalidCredentials, 401}, {model.ErrInvalidRefreshToken, 401},
		{model.ErrForbidden, 403},
		{model.ErrAccountNotFound, 404}, {model.ErrRouteNotFound, 404}, {model.ErrMethodNotAllowed, 405},
		{model.ErrEmailAlreadyRegistered, 409}, {model.ErrIdempotencyInProgress, 409},
		{model.ErrIdempotencyKeyReused, 422}, {model.ErrInsufficientFunds, 422}, {model.ErrCashLimitExceeded, 422},
		{model.ErrDailyRechargeCountExceeded, 422}, {model.ErrDailyRechargeAmountExceeded, 422},
		{model.ErrBalanceLimitExceeded, 422},
		{model.ErrTooManyLoginAttempts, 429},
		{model.ErrInternal, 500},
	}
	for _, tc := range tests {
		t.Run(tc.err.Message, func(t *testing.T) {
			rec := httptest.NewRecorder()
			// Envuelto con %w, como llegaría desde un service.
			writeError(rec, fmt.Errorf("context: %w", tc.err))

			if rec.Code != tc.status {
				t.Errorf("status = %d, want %d", rec.Code, tc.status)
			}
			assertErrorBody(t, rec, tc.err.Code, tc.err.Message)
		})
	}
}

// Un error desconocido responde 500 sin exponer su texto.
func TestWriteErrorUnknown(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, errors.New("pq: connection refused at 10.0.0.5"))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	assertErrorBody(t, rec, "INTERNAL_ERROR", "Internal error")
	if strings.Contains(rec.Body.String(), "10.0.0.5") {
		t.Error("internal details leaked in the response")
	}
}

func TestWriteErrorRetryAfter(t *testing.T) {
	rec := httptest.NewRecorder()
	writeError(rec, &model.LoginBlockedError{RetryAfter: 300})

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "300" {
		t.Errorf("Retry-After = %q, want 300", got)
	}
	assertErrorBody(t, rec, "TOO_MANY_REQUESTS", "Too many login attempts")
}

// assertErrorBody comprueba el Content-Type y el cuerpo { code, message } exacto.
func assertErrorBody(t *testing.T, rec *httptest.ResponseRecorder, code, message string) {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	want, _ := json.Marshal(errorResponse{code, message})
	if got := rec.Body.String(); got != string(want) {
		t.Errorf("body = %s, want %s", got, want)
	}
}

// ---------------------------------------------------------------------------
// decodeObject
// ---------------------------------------------------------------------------

// newRequest crea una petición POST con el cuerpo y Content-Type dados.
func newRequest(contentType, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	return r
}

// v1: formato de la petición (I-06).
func TestDecodeObjectRequestFormat(t *testing.T) {
	big := `{"name":"` + strings.Repeat("a", maxBodyBytes) + `"}`
	tests := []struct {
		name        string
		contentType string
		body        string
		wantErr     bool
	}{
		{"valid", "application/json", `{"name":"Ana"}`, false},
		{"valid with charset", "application/json; charset=UTF-8", `{"name":"Ana"}`, false},
		{"missing content type", "", `{"name":"Ana"}`, true},
		{"wrong content type", "text/plain", `{"name":"Ana"}`, true},
		{"other charset", "application/json; charset=latin1", `{"name":"Ana"}`, true},
		{"body larger than 16 KB", "application/json", big, true},
		{"broken json", "application/json", `{"name":`, true},
		{"empty body", "application/json", ``, true},
		{"root is an array", "application/json", `[{"name":"Ana"}]`, true},
		{"root is a string", "application/json", `"Ana"`, true},
		{"data after the object", "application/json", `{"name":"Ana"} {}`, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := decodeObject(newRequest(tc.contentType, tc.body)).Err()
			checkBodyErr(t, err, tc.wantErr)
		})
	}
}

// v2: claves duplicadas, desconocidas y con otras mayúsculas (I-03, I-14).
func TestDecodeObjectKeys(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"duplicate key", `{"name":"Ana","name":"Eva"}`, true},
		{"unknown key is ignored", `{"name":"Ana","extra":{"a":[1,2]}}`, false},
		{"different case does not match", `{"Name":"Ana"}`, true}, // "name" falta
		{"different case is ignored if the field exists", `{"name":"Ana","NAME":"Eva"}`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := decodeObject(newRequest("application/json", tc.body))
			name := body.String("name")
			checkBodyErr(t, body.Err(), tc.wantErr)
			if !tc.wantErr && name != "Ana" {
				t.Errorf("name = %q, want Ana", name)
			}
		})
	}
}

// v3: null, faltantes y tipos (I-01, I-03).
func TestDecodeObjectFields(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    int64
		wantErr bool
	}{
		{"integer", `{"amount":150000}`, 150000, false},
		{"spaces around", `{ "amount" :  150000 }`, 150000, false},
		{"integer with .0", `{"amount":150000.0}`, 150000, false},
		{"exponent", `{"amount":1e3}`, 1000, false},
		{"exponent with decimals", `{"amount":1.5e5}`, 150000, false},
		{"negative", `{"amount":-5}`, -5, false},
		{"zero", `{"amount":0}`, 0, false},
		{"huge integer saturates", `{"amount":99999999999999999999}`, math.MaxInt64, false},
		{"huge exponent saturates", `{"amount":1e400}`, math.MaxInt64, false},
		{"huge negative integer saturates", `{"amount":-99999999999999999999}`, math.MinInt64, false},
		{"huge negative saturates", `{"amount":-1e30}`, math.MinInt64, false},
		{"fractional", `{"amount":1.5}`, 0, true},
		{"string", `{"amount":"150000"}`, 0, true},
		{"boolean", `{"amount":true}`, 0, true},
		{"null", `{"amount":null}`, 0, true},
		{"missing", `{}`, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := decodeObject(newRequest("application/json", tc.body))
			got := body.Int64("amount")
			checkBodyErr(t, body.Err(), tc.wantErr)
			if !tc.wantErr && got != tc.want {
				t.Errorf("amount = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestDecodeObjectStringType(t *testing.T) {
	for _, raw := range []string{`123`, `true`, `{}`, `["a"]`, `null`} {
		body := decodeObject(newRequest("application/json", `{"name":`+raw+`}`))
		body.String("name")
		checkBodyErr(t, body.Err(), true)
	}
}

// Después del primer error, los demás campos no cambian el error guardado.
func TestDecodeObjectKeepsFirstError(t *testing.T) {
	body := decodeObject(newRequest("application/json", `{"name":1,"amount":5}`))
	body.String("name")
	if got := body.Int64("amount"); got != 0 {
		t.Errorf("after an error, Int64 should return 0, got %d", got)
	}
	checkBodyErr(t, body.Err(), true)
}

func checkBodyErr(t *testing.T, err error, wantErr bool) {
	t.Helper()
	switch {
	case wantErr && err != model.ErrInvalidRequestBody:
		t.Errorf("error = %v, want ErrInvalidRequestBody", err)
	case !wantErr && err != nil:
		t.Errorf("unexpected error: %v", err)
	}
}
