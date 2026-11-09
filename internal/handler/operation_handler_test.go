package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"uuid"

	"accounts-api/internal/model"
	"accounts-api/internal/repository"
	"accounts-api/internal/service"
)

// realOperatorToken crea un operator en la BD (la idempotency key tiene FK a users) y firma su token.
func realOperatorToken(t *testing.T, db *sql.DB) string {
	t.Helper()
	op, err := repository.UserRepo{}.Create(context.Background(), db, model.User{
		ID: uuid.NewV4().String(), Name: "Operator", Email: "op@bank.local", PasswordHash: "x", Role: model.RoleOperator,
	})
	if err != nil {
		t.Fatal(err)
	}
	token, _ := service.NewTokenIssuer(testSecret).Issue(op.ID, model.RoleOperator)
	return token
}

// doRecharge envía POST /recharge con el token y la Idempotency-Key dados ("" = sin encabezado).
func doRecharge(router http.Handler, token, key, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/recharge", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// T5.9: misma key dos veces → mismo movimiento; la 2.ª con Idempotent-Replayed: true.
func TestRechargeEndpoint(t *testing.T) {
	router, db := newTestRouterDB(t)
	operator := realOperatorToken(t, db)
	customer, _ := loginAs(t, router, "ana@mail.com")
	account := createAccount(t, router, customer, "savings")
	number := account["accountNumber"].(string)
	key := uuid.NewV4().String()
	body := `{"accountNumber":"` + number + `","amount":150000,"rechargeType":"salary"}`

	first := doRecharge(router, operator, key, body)
	if first.Code != http.StatusCreated || first.Header().Get("Idempotent-Replayed") != "" {
		t.Fatalf("first: status = %d, replayed = %q, body = %s", first.Code, first.Header().Get("Idempotent-Replayed"), first.Body)
	}
	var m map[string]any
	json.Unmarshal(first.Body.Bytes(), &m)
	details, _ := m["details"].(map[string]any)
	if len(m) != 6 || m["accountId"] != account["id"] || m["movementType"] != "recharge" ||
		m["amount"] != float64(150000) || len(details) != 1 || details["rechargeType"] != "salary" ||
		!isoMillis.MatchString(m["createdAt"].(string)) {
		t.Errorf("body = %s", first.Body)
	}

	// Misma petición con otro orden de claves, 150000.0 y la key en mayúsculas: es la misma.
	reordered := `{"rechargeType":"salary", "amount":150000.0, "accountNumber":"` + number + `"}`
	second := doRecharge(router, operator, strings.ToUpper(key), reordered)
	if second.Code != http.StatusCreated || second.Header().Get("Idempotent-Replayed") != "true" ||
		second.Body.String() != first.Body.String() {
		t.Errorf("replay: status = %d, replayed = %q, body = %s; want the same 201 body with Idempotent-Replayed: true",
			second.Code, second.Header().Get("Idempotent-Replayed"), second.Body)
	}

	rec := doAuth(router, http.MethodGet, "/accounts/"+account["id"].(string), customer, "")
	var got map[string]any
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got["balance"] != float64(150000) {
		t.Errorf("balance = %v, want 150000 (replay must not recharge again)", got["balance"])
	}
}

// Pasos 1 a 3 en el handler, en orden; y los errores guardados se repiten con el encabezado.
func TestRechargeEndpointErrors(t *testing.T) {
	router, db := newTestRouterDB(t)
	operator := realOperatorToken(t, db)
	customer, _ := loginAs(t, router, "ana@mail.com")
	key := uuid.NewV4().String()

	tests := []struct {
		name, token, key, body string
		status                 int
		code, message          string
	}{
		{"1 no token", "", "", `x`, 401, "UNAUTHORIZED", "Missing or invalid token"},
		{"2 customer", customer, "", `x`, 403, "FORBIDDEN", "Access denied"},
		{"3 missing key", operator, "", `x`, 400, "BAD_REQUEST", "Invalid idempotency key"},
		{"3 key without dashes", operator, strings.ReplaceAll(key, "-", ""), `x`, 400, "BAD_REQUEST", "Invalid idempotency key"},
		{"5 invalid body", operator, key, `{"accountNumber":"0012345678","amount":"1000","rechargeType":"cash"}`, 400, "BAD_REQUEST", "Invalid request body"},
		{"6 number as JSON number", operator, key, `{"accountNumber":12345678,"amount":1000,"rechargeType":"cash"}`, 400, "BAD_REQUEST", "Invalid request body"},
		{"6 invalid number", operator, key, `{"accountNumber":"12345","amount":1000,"rechargeType":"cash"}`, 400, "BAD_REQUEST", "Invalid account number"},
		{"7 invalid type", operator, key, `{"accountNumber":"0012345678","amount":1000,"rechargeType":"gift"}`, 400, "BAD_REQUEST", "Invalid recharge type"},
		{"8 decimal amount", operator, key, `{"accountNumber":"0012345678","amount":10.5,"rechargeType":"cash"}`, 400, "BAD_REQUEST", "Invalid request body"},
		{"8 amount zero", operator, key, `{"accountNumber":"0012345678","amount":0,"rechargeType":"cash"}`, 400, "BAD_REQUEST", "Amount must be greater than zero"},
		{"9 huge amount", operator, key, `{"accountNumber":"0012345678","amount":1e30,"rechargeType":"cash"}`, 400, "BAD_REQUEST", "Amount exceeds maximum"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := doRecharge(router, tc.token, tc.key, tc.body)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.status, rec.Body)
			}
			assertErrorBody(t, rec, tc.code, tc.message)
		})
	}

	// 404 guardado: la repetición devuelve el mismo error con Idempotent-Replayed: true.
	notFound := `{"accountNumber":"9999999999","amount":1000,"rechargeType":"cash"}`
	for i, wantReplayed := range []string{"", "true"} {
		rec := doRecharge(router, operator, key, notFound)
		if rec.Code != http.StatusNotFound || rec.Header().Get("Idempotent-Replayed") != wantReplayed {
			t.Errorf("attempt %d: status = %d, replayed = %q; want 404, %q", i+1, rec.Code, rec.Header().Get("Idempotent-Replayed"), wantReplayed)
		}
		assertErrorBody(t, rec, "NOT_FOUND", "Account not found")
	}

	// Misma key, otra petición → 422, sin el encabezado.
	rec := doRecharge(router, operator, key, `{"accountNumber":"9999999999","amount":2000,"rechargeType":"cash"}`)
	if rec.Code != http.StatusUnprocessableEntity || rec.Header().Get("Idempotent-Replayed") != "" {
		t.Errorf("reused key: status = %d, replayed = %q", rec.Code, rec.Header().Get("Idempotent-Replayed"))
	}
	assertErrorBody(t, rec, "IDEMPOTENCY_KEY_REUSED", "Idempotency key already used with a different request")

	// cash > 300000 → 422.
	rec = doRecharge(router, operator, uuid.NewV4().String(), `{"accountNumber":"0012345678","amount":300001,"rechargeType":"cash"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("cash limit: status = %d, want 422", rec.Code)
	}
	assertErrorBody(t, rec, "CASH_LIMIT_EXCEEDED", "Cash recharge limit exceeded")
}
