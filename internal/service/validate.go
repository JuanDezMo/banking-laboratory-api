// Validaciones de formato: textos del registro, número de cuenta, UUID y page.
// Devuelven el error del catálogo que corresponde a cada campo.
package service

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"accounts-api/internal/model"
)

// Límites de I-05 e I-12. Las longitudes se cuentan en caracteres Unicode, no en bytes.
const (
	maxNameLength     = 100
	maxEmailLength    = 254
	minPasswordLength = 8
	maxPasswordLength = 128
	accountNumberLen  = 10
	maxPage           = 10000
)

// NormalizeName recorta los espacios al inicio y al final y exige de 1 a 100 caracteres.
func NormalizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	length := utf8.RuneCountInString(name)
	if length < 1 || length > maxNameLength {
		return "", model.ErrInvalidName
	}
	return name, nil
}

// NormalizeEmail recorta, pasa a minúsculas y valida el email (I-05).
func NormalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if utf8.RuneCountInString(email) > maxEmailLength || !isValidEmail(email) {
		return "", model.ErrInvalidEmail
	}
	return email, nil
}

// isValidEmail equivale a la expresión ^[^\s@]+@[^\s@]+\.[^\s@]+$ del contrato.
// Se escribe a mano porque \s en el paquete regexp de Go solo reconoce espacios ASCII,
// y el contrato considera cualquier espacio Unicode.
func isValidEmail(email string) bool {
	local, domain, found := strings.Cut(email, "@")
	if !found || local == "" || domain == "" {
		return false
	}
	// Ningún espacio ni otra '@' en todo el email.
	if strings.ContainsFunc(email, unicode.IsSpace) || strings.Contains(domain, "@") {
		return false
	}
	// El dominio necesita un punto que no esté ni al inicio ni al final.
	dot := strings.LastIndex(domain, ".")
	return dot > 0 && dot < len(domain)-1
}

// ValidatePassword exige de 8 a 128 caracteres. No recorta espacios (I-05).
func ValidatePassword(password string) error {
	length := utf8.RuneCountInString(password)
	if length < minPasswordLength {
		return model.ErrPasswordTooShort
	}
	if length > maxPasswordLength {
		return model.ErrPasswordTooLong
	}
	return nil
}

// ValidateAccountNumber exige exactamente 10 dígitos ASCII, sin recortar espacios (I-04).
func ValidateAccountNumber(number string) error {
	if len(number) != accountNumberLen || !isASCIIDigits(number) {
		return model.ErrInvalidAccountNumber
	}
	return nil
}

// NormalizeAccountID valida el id de cuenta de la ruta y lo devuelve en minúsculas (I-15).
func NormalizeAccountID(id string) (string, error) {
	normalized, ok := normalizeUUID(id)
	if !ok {
		return "", model.ErrInvalidAccountID
	}
	return normalized, nil
}

// NormalizeIdempotencyKey valida el encabezado Idempotency-Key y lo devuelve en minúsculas (I-15).
func NormalizeIdempotencyKey(key string) (string, error) {
	normalized, ok := normalizeUUID(key)
	if !ok {
		return "", model.ErrInvalidIdempotencyKey
	}
	return normalized, nil
}

// normalizeUUID acepta solo el formato canónico 8-4-4-4-12 en hexadecimal (36 caracteres),
// con mayúsculas o minúsculas, y lo devuelve en minúsculas. No exige versión ni variante.
func normalizeUUID(value string) (string, bool) {
	if len(value) != 36 {
		return "", false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return "", false
			}
		default:
			if !isHexDigit(c) {
				return "", false
			}
		}
	}
	return strings.ToLower(value), true
}

// ParsePage interpreta el parámetro page (I-12). present indica si vino en la URL:
// ausente → 1; presente → entero decimal sin signo ni decimales, de 1 a 10000.
func ParsePage(raw string, present bool) (int, error) {
	if !present {
		return 1, nil
	}
	// Se exigen solo dígitos antes de convertir: strconv.Atoi aceptaría "+5".
	if raw == "" || !isASCIIDigits(raw) {
		return 0, model.ErrInvalidPagination
	}
	page, err := strconv.Atoi(raw)
	if err != nil || page < 1 || page > maxPage {
		return 0, model.ErrInvalidPagination
	}
	return page, nil
}

func isASCIIDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
