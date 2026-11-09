package service

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"accounts-api/internal/model"
)

const (
	testSecret = "test-secret-with-at-least-32-characters"
	testUserID = "a41e5b2c-3d4e-4f5a-8b6c-7d8e9f0a1b2c"
)

// issuerAt crea un emisor con el reloj fijo en t.
func issuerAt(t time.Time) *TokenIssuer {
	issuer := NewTokenIssuer(testSecret)
	issuer.now = func() time.Time { return t }
	return issuer
}

func TestIssueClaims(t *testing.T) {
	now := time.Date(2026, 11, 9, 14, 0, 0, 0, time.UTC)
	raw, err := issuerAt(now).Issue(testUserID, model.RoleCustomer)
	if err != nil {
		t.Fatal(err)
	}

	// Se leen las claims sin validar la firma, solo para inspeccionarlas.
	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(raw, claims); err != nil {
		t.Fatal(err)
	}
	if claims["sub"] != testUserID || claims["role"] != "customer" {
		t.Errorf("sub/role = %v/%v", claims["sub"], claims["role"])
	}
	iat, exp := claims["iat"].(float64), claims["exp"].(float64)
	if iat != float64(now.Unix()) || exp-iat != 900 {
		t.Errorf("iat=%v exp=%v, want exp-iat=900", iat, exp)
	}
}

func TestValidateAccepts(t *testing.T) {
	now := time.Date(2026, 11, 9, 14, 0, 0, 0, time.UTC)
	raw, _ := issuerAt(now).Issue(testUserID, model.RoleOperator)

	// Un segundo antes de vencer sigue siendo válido.
	userID, role, err := issuerAt(now.Add(AccessTokenTTL - time.Second)).Validate(raw)
	if err != nil || userID != testUserID || role != model.RoleOperator {
		t.Errorf("got %s %s %v", userID, role, err)
	}
}

func TestValidateRejects(t *testing.T) {
	now := time.Date(2026, 11, 9, 14, 0, 0, 0, time.UTC)
	valid, _ := issuerAt(now).Issue(testUserID, model.RoleCustomer)

	sign := func(method jwt.SigningMethod, key any, claims jwt.MapClaims) string {
		raw, err := jwt.NewWithClaims(method, claims).SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	base := func() jwt.MapClaims {
		return jwt.MapClaims{"sub": testUserID, "role": "customer", "iat": now.Unix(), "exp": now.Add(time.Minute).Unix()}
	}
	without := func(key string) jwt.MapClaims { c := base(); delete(c, key); return c }
	with := func(key string, value any) jwt.MapClaims { c := base(); c[key] = value; return c }

	tests := []struct {
		name string
		raw  string
		at   time.Time
	}{
		{"expired (exactly at exp)", valid, now.Add(AccessTokenTTL)},
		{"other secret", sign(jwt.SigningMethodHS256, []byte("another-secret-with-32-characters!!"), base()), now},
		{"alg none", sign(jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, base()), now},
		{"alg HS512", sign(jwt.SigningMethodHS512, []byte(testSecret), base()), now},
		{"without exp", sign(jwt.SigningMethodHS256, []byte(testSecret), without("exp")), now},
		{"without iat", sign(jwt.SigningMethodHS256, []byte(testSecret), without("iat")), now},
		{"iat in the future", sign(jwt.SigningMethodHS256, []byte(testSecret), with("iat", now.Add(time.Minute).Unix())), now},
		{"sub is not a uuid", sign(jwt.SigningMethodHS256, []byte(testSecret), with("sub", "123")), now},
		{"unknown role", sign(jwt.SigningMethodHS256, []byte(testSecret), with("role", "admin")), now},
		{"garbage", "not.a.jwt", now},
		{"tampered", valid[:len(valid)-2] + "xx", now},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := issuerAt(tc.at).Validate(tc.raw); err != model.ErrUnauthorized {
				t.Errorf("error = %v, want ErrUnauthorized", err)
			}
		})
	}
}

func TestRefreshToken(t *testing.T) {
	token, hash := newRefreshToken()
	other, _ := newRefreshToken()

	if len(token) != 43 { // 32 bytes en base64 URL sin relleno
		t.Errorf("token length = %d, want 43", len(token))
	}
	if token == other {
		t.Error("two refresh tokens must differ")
	}
	if len(hash) != 64 || hash != hashRefreshToken(token) || strings.Contains(hash, token) {
		t.Errorf("hash %q is not the SHA-256 hex of the token", hash)
	}
}
