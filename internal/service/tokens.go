// Access tokens (JWT HS256) y refresh tokens opacos (RN-26, I-08).
package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"accounts-api/internal/model"
)

const (
	AccessTokenTTL   = 15 * time.Minute
	refreshTokenSize = 32 // bytes aleatorios
)

// accessClaims son las claims del access token: sub, role, iat y exp.
type accessClaims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

// TokenIssuer firma y valida access tokens.
type TokenIssuer struct {
	secret []byte
	now    func() time.Time // reloj del servidor; en pruebas se reemplaza
}

// NewTokenIssuer crea el emisor con el secreto de JWT_SECRET.
func NewTokenIssuer(secret string) *TokenIssuer {
	return &TokenIssuer{secret: []byte(secret), now: time.Now}
}

// Issue firma un access token HS256 válido por 15 minutos.
func (t *TokenIssuer) Issue(userID string, role model.Role) (string, error) {
	now := t.now()
	claims := accessClaims{
		Role: string(role),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(AccessTokenTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
}

// Validate comprueba el access token según I-08 y devuelve el usuario y su rol.
// Cualquier fallo devuelve ErrUnauthorized.
func (t *TokenIssuer) Validate(raw string) (userID string, role model.Role, err error) {
	claims := &accessClaims{}
	_, err = jwt.ParseWithClaims(raw, claims,
		func(*jwt.Token) (any, error) { return t.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}), // rechaza cualquier otro algoritmo, incluido "none"
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(), // rechaza un iat en el futuro
		jwt.WithLeeway(0),  // sin tolerancia de desfase: vencido es vencido
		jwt.WithTimeFunc(t.now),
	)
	if err != nil {
		return "", "", model.ErrUnauthorized
	}

	// Claims obligatorias con valores válidos.
	userID, uuidOK := normalizeUUID(claims.Subject)
	role = model.Role(claims.Role)
	if claims.IssuedAt == nil || !uuidOK || (role != model.RoleCustomer && role != model.RoleOperator) {
		return "", "", model.ErrUnauthorized
	}
	return userID, role, nil
}

// newRefreshToken genera un refresh token aleatorio (32 bytes en base64 URL) y su hash.
func newRefreshToken() (token, hash string) {
	b := make([]byte, refreshTokenSize)
	rand.Read(b) // desde Go 1.24, crypto/rand.Read nunca devuelve error
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, hashRefreshToken(token)
}

// hashRefreshToken devuelve el SHA-256 en hexadecimal: es lo único que se guarda en la BD.
func hashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
