package handler

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"uuid"

	"accounts-api/internal/model"
	"accounts-api/internal/service"
)

var tenDigits = regexp.MustCompile(`^[0-9]{10}$`)

// operatorToken firma un access token de operator. Auth solo valida el JWT, así que no
// hace falta que el operator exista en la BD.
func operatorToken(t *testing.T) string {
	t.Helper()
	token, err := service.NewTokenIssuer(testSecret).Issue("00000000-0000-4000-8000-0000000000ff", model.RoleOperator)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// createAccount crea una cuenta y devuelve el JSON de la respuesta.
func createAccount(t *testing.T, router http.Handler, access, accountType string) map[string]any {
	t.Helper()
	rec := doAuth(router, http.MethodPost, "/accounts", access, `{"accountType":"`+accountType+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create account: status = %d, body = %s", rec.Code, rec.Body)
	}
	var account map[string]any
	json.Unmarshal(rec.Body.Bytes(), &account)
	return account
}

// assertAccountShape comprueba la forma de una cuenta del contrato: exactamente 5 campos.
func assertAccountShape(t *testing.T, account map[string]any, accountType string) {
	t.Helper()
	number, _ := account["accountNumber"].(string)
	createdAt, _ := account["createdAt"].(string)
	if len(account) != 5 || account["accountType"] != accountType || account["balance"] != float64(0) ||
		!tenDigits.MatchString(number) || !isoMillis.MatchString(createdAt) || account["id"] == "" {
		t.Errorf("account = %v", account)
	}
}

// F-05 en el orden del spec: 401, 403, 400 cuerpo, 400 tipo.
func TestCreateAccountEndpoint(t *testing.T) {
	router := newTestRouter(t)
	access, _ := loginPair(t, router)

	assertAccountShape(t, createAccount(t, router, access, "savings"), "savings")
	assertAccountShape(t, createAccount(t, router, access, "checking"), "checking")

	tests := []struct {
		name, token, body string
		status            int
		code, message     string
	}{
		{"1 no token", "", `no es json`, 401, "UNAUTHORIZED", "Missing or invalid token"},
		{"2 operator, even with invalid body", operatorToken(t), `no es json`, 403, "FORBIDDEN", "Access denied"},
		{"3 invalid body", access, `{"type":"savings"}`, 400, "BAD_REQUEST", "Invalid request body"},
		{"3 accountType not a string", access, `{"accountType":1}`, 400, "BAD_REQUEST", "Invalid request body"},
		{"4 invalid account type", access, `{"accountType":"Savings"}`, 400, "BAD_REQUEST", "Invalid account type"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := doAuth(router, http.MethodPost, "/accounts", tc.token, tc.body)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.status, rec.Body)
			}
			assertErrorBody(t, rec, tc.code, tc.message)
		})
	}
}

// F-06: solo las cuentas propias; sin cuentas, "items": []; operator → 403.
func TestListAccountsEndpoint(t *testing.T) {
	router := newTestRouter(t)
	ana, _ := loginAs(t, router, "ana@mail.com")
	eva, _ := loginAs(t, router, "eva@mail.com")

	rec := doAuth(router, http.MethodGet, "/accounts", ana, "")
	if rec.Code != http.StatusOK || rec.Body.String() != `{"items":[]}` {
		t.Errorf("empty list: status = %d, body = %s; want 200 {\"items\":[]}", rec.Code, rec.Body)
	}

	first := createAccount(t, router, ana, "savings")
	second := createAccount(t, router, ana, "checking")
	createAccount(t, router, eva, "savings")

	rec = doAuth(router, http.MethodGet, "/accounts", ana, "")
	var list struct{ Items []map[string]any }
	json.Unmarshal(rec.Body.Bytes(), &list)
	if rec.Code != http.StatusOK || len(list.Items) != 2 {
		t.Fatalf("status = %d, body = %s; want Ana's 2 accounts", rec.Code, rec.Body)
	}
	ids := map[any]bool{list.Items[0]["id"]: true, list.Items[1]["id"]: true}
	if !ids[first["id"]] || !ids[second["id"]] {
		t.Errorf("items %v are not Ana's accounts", list.Items)
	}

	rec = doAuth(router, http.MethodGet, "/accounts", operatorToken(t), "")
	if rec.Code != http.StatusForbidden {
		t.Errorf("operator: status = %d, want 403", rec.Code)
	}
	assertErrorBody(t, rec, "FORBIDDEN", "Access denied")
}

// F-07: propia 200, ajena 404 (RN-24), operator 200, id no UUID 400, sin token 401.
func TestGetAccountEndpoint(t *testing.T) {
	router := newTestRouter(t)
	ana, _ := loginAs(t, router, "ana@mail.com")
	eva, _ := loginAs(t, router, "eva@mail.com")
	account := createAccount(t, router, ana, "savings")
	id := account["id"].(string)

	tests := []struct {
		name, token, id string
		status          int
		code, message   string
	}{
		{"1 no token, even with invalid id", "", "123", 401, "UNAUTHORIZED", "Missing or invalid token"},
		{"2 invalid id", ana, "123", 400, "BAD_REQUEST", "Invalid account id"},
		{"3 missing account", ana, "00000000-0000-4000-8000-000000000000", 404, "NOT_FOUND", "Account not found"},
		{"3 other customer's account", eva, id, 404, "NOT_FOUND", "Account not found"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := doAuth(router, http.MethodGet, "/accounts/"+tc.id, tc.token, "")
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.status, rec.Body)
			}
			assertErrorBody(t, rec, tc.code, tc.message)
		})
	}

	for name, token := range map[string]string{"owner": ana, "operator": operatorToken(t)} {
		rec := doAuth(router, http.MethodGet, "/accounts/"+strings.ToUpper(id), token, "")
		var got map[string]any
		json.Unmarshal(rec.Body.Bytes(), &got)
		if rec.Code != http.StatusOK || got["id"] != id {
			t.Errorf("%s: status = %d, body = %s; want 200 with id in lowercase", name, rec.Code, rec.Body)
		}
	}
}

// T4.7: flujo completo registro → login → crear → listar → consultar.
func TestAccountFlow(t *testing.T) {
	router := newTestRouter(t)

	rec := do(router, http.MethodPost, "/auth/register", `{"name":"Ana","email":"ana@mail.com","password":"S3gura2026"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	rec = do(router, http.MethodPost, "/auth/login", `{"email":"ana@mail.com","password":"S3gura2026"}`)
	var pair struct{ AccessToken string }
	json.Unmarshal(rec.Body.Bytes(), &pair)
	if rec.Code != http.StatusOK || pair.AccessToken == "" {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}

	created := createAccount(t, router, pair.AccessToken, "checking")

	rec = doAuth(router, http.MethodGet, "/accounts", pair.AccessToken, "")
	var list struct{ Items []map[string]any }
	json.Unmarshal(rec.Body.Bytes(), &list)
	if rec.Code != http.StatusOK || len(list.Items) != 1 || list.Items[0]["id"] != created["id"] {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}

	rec = doAuth(router, http.MethodGet, "/accounts/"+created["id"].(string), pair.AccessToken, "")
	var got map[string]any
	json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body)
	}
	for key, want := range created {
		if got[key] != want {
			t.Errorf("get %s = %v, want %v (same as when created)", key, got[key], want)
		}
	}
}

// T7.6: { page, pageSize: 20, items }; cuenta ajena → 404; page inválida → 400.
func TestListMovementsEndpoint(t *testing.T) {
	router, db := newTestRouterDB(t)
	operator := realOperatorToken(t, db)
	ana, _ := loginAs(t, router, "ana@mail.com")
	eva, _ := loginAs(t, router, "eva@mail.com")
	account := createAccount(t, router, ana, "savings")
	id := account["id"].(string)
	for range 3 {
		doRecharge(router, operator, uuid.NewV4().String(),
			`{"accountNumber":"`+account["accountNumber"].(string)+`","amount":1000,"rechargeType":"cash"}`)
	}

	for _, token := range []string{ana, operator} {
		rec := doAuth(router, http.MethodGet, "/accounts/"+id+"/movements", token, "")
		var page struct {
			Page, PageSize int
			Items          []map[string]any
		}
		json.Unmarshal(rec.Body.Bytes(), &page)
		if rec.Code != http.StatusOK || page.Page != 1 || page.PageSize != 20 || len(page.Items) != 3 ||
			page.Items[0]["movementType"] != "recharge" || page.Items[0]["accountId"] != id {
			t.Errorf("status = %d, body = %s", rec.Code, rec.Body)
		}
	}

	rec := doAuth(router, http.MethodGet, "/accounts/"+id+"/movements?page=2", ana, "")
	if rec.Code != http.StatusOK || rec.Body.String() != `{"page":2,"pageSize":20,"items":[]}` {
		t.Errorf("page 2: status = %d, body = %s", rec.Code, rec.Body)
	}

	tests := []struct {
		name, token, path string
		status            int
		code, message     string
	}{
		{"1 no token", "", "/accounts/abc/movements?page=0", 401, "UNAUTHORIZED", "Missing or invalid token"},
		{"2 invalid id", ana, "/accounts/abc/movements?page=0", 400, "BAD_REQUEST", "Invalid account id"},
		{"3 page 0", eva, "/accounts/" + id + "/movements?page=0", 400, "BAD_REQUEST", "Invalid pagination"},
		{"3 page 1.5", ana, "/accounts/" + id + "/movements?page=1.5", 400, "BAD_REQUEST", "Invalid pagination"},
		{"3 empty page", ana, "/accounts/" + id + "/movements?page=", 400, "BAD_REQUEST", "Invalid pagination"},
		{"4 other customer's account", eva, "/accounts/" + id + "/movements", 404, "NOT_FOUND", "Account not found"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := doAuth(router, http.MethodGet, tc.path, tc.token, "")
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.status, rec.Body)
			}
			assertErrorBody(t, rec, tc.code, tc.message)
		})
	}
}
