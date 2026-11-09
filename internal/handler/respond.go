// Lectura de peticiones (decodeObject) y escritura de respuestas (writeJSON, writeError).
package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"accounts-api/internal/model"
)

// maxBodyBytes es el tamaño máximo del cuerpo de una petición (I-06).
const maxBodyBytes = 16 * 1024

// ---------------------------------------------------------------------------
// Respuestas
// ---------------------------------------------------------------------------

// errorResponse es el cuerpo de todos los errores: { "code": ..., "message": ... }.
type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// statusByCode traduce cada código del catálogo a su estado HTTP (spec 7.2).
var statusByCode = map[string]int{
	"BAD_REQUEST":                     http.StatusBadRequest,
	"UNAUTHORIZED":                    http.StatusUnauthorized,
	"INVALID_CREDENTIALS":             http.StatusUnauthorized,
	"INVALID_REFRESH_TOKEN":           http.StatusUnauthorized,
	"FORBIDDEN":                       http.StatusForbidden,
	"NOT_FOUND":                       http.StatusNotFound,
	"ROUTE_NOT_FOUND":                 http.StatusNotFound,
	"METHOD_NOT_ALLOWED":              http.StatusMethodNotAllowed,
	"EMAIL_ALREADY_REGISTERED":        http.StatusConflict,
	"IDEMPOTENCY_REQUEST_IN_PROGRESS": http.StatusConflict,
	"IDEMPOTENCY_KEY_REUSED":          http.StatusUnprocessableEntity,
	"INSUFFICIENT_FUNDS":              http.StatusUnprocessableEntity,
	"CASH_LIMIT_EXCEEDED":             http.StatusUnprocessableEntity,
	"DAILY_RECHARGE_COUNT_EXCEEDED":   http.StatusUnprocessableEntity,
	"DAILY_RECHARGE_AMOUNT_EXCEEDED":  http.StatusUnprocessableEntity,
	"BALANCE_LIMIT_EXCEEDED":          http.StatusUnprocessableEntity,
	"TOO_MANY_REQUESTS":               http.StatusTooManyRequests,
	"INTERNAL_ERROR":                  http.StatusInternalServerError,
}

// writeJSON responde con el estado y el valor dado en JSON UTF-8 (I-13).
func writeJSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		// Solo pasa si value tiene un tipo que no se puede serializar: es un error de programación.
		slog.Error("encode response", "error", err)
		status = http.StatusInternalServerError
		body, _ = json.Marshal(errorResponse{model.ErrInternal.Code, model.ErrInternal.Message})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(body)
}

// writeError responde con el error del catálogo que corresponda.
// Cualquier error que no sea un AppError conocido se registra y se responde como 500,
// sin exponer detalles internos (spec 7.1).
func writeError(w http.ResponseWriter, err error) {
	var appErr *model.AppError
	status, known := 0, false
	if errors.As(err, &appErr) {
		status, known = statusByCode[appErr.Code]
	}
	if !known {
		slog.Error("internal error", "error", err)
		appErr, status = model.ErrInternal, http.StatusInternalServerError
	}

	// RN-29: el login bloqueado indica cuántos segundos esperar.
	var blocked *model.LoginBlockedError
	if errors.As(err, &blocked) {
		w.Header().Set("Retry-After", strconv.Itoa(blocked.RetryAfter))
	}

	writeJSON(w, status, errorResponse{Code: appErr.Code, Message: appErr.Message})
}

// ---------------------------------------------------------------------------
// Peticiones
// ---------------------------------------------------------------------------

// jsonBody es el cuerpo de una petición ya validado como objeto JSON, con sus campos
// guardados por nombre exacto. Guarda el primer error: después de un error, los
// métodos devuelven valores cero y Err() informa ese primer error.
//
// Uso en un handler:
//
//	body := decodeObject(r)
//	in := RegisterInput{Name: body.String("name"), Email: body.String("email")}
//	if err := body.Err(); err != nil { ... }
type jsonBody struct {
	fields map[string]json.RawMessage
	err    error
}

// Err devuelve el primer error encontrado al leer el cuerpo o sus campos.
func (b *jsonBody) Err() error { return b.err }

// decodeObject lee el cuerpo de la petición y comprueba (I-06, I-14):
//   - Content-Type application/json (se acepta charset=utf-8).
//   - Tamaño máximo de 16 KB.
//   - Raíz del JSON es un objeto, sin claves repetidas y sin datos después del objeto.
//
// Cualquier fallo se guarda como ErrInvalidRequestBody.
func decodeObject(r *http.Request) *jsonBody {
	fields, err := readObject(r)
	if err != nil {
		return &jsonBody{err: model.ErrInvalidRequestBody}
	}
	return &jsonBody{fields: fields}
}

// readObject hace el trabajo de decodeObject. Sus errores son internos: el llamador
// los reemplaza todos por ErrInvalidRequestBody.
func readObject(r *http.Request) (map[string]json.RawMessage, error) {
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		return nil, errors.New("content type is not application/json")
	}

	// Se lee un byte más del máximo para saber si el cuerpo lo supera.
	data, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBodyBytes {
		return nil, errors.New("body too large")
	}

	dec := json.NewDecoder(bytes.NewReader(data))

	// La raíz debe ser un objeto: el primer token es '{'.
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errors.New("root is not an object")
	}

	fields := map[string]json.RawMessage{}
	for dec.More() {
		// Cada iteración lee una clave y su valor completo.
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key := tok.(string) // dentro de un objeto, el token de la clave siempre es un string
		if _, repeated := fields[key]; repeated {
			return nil, errors.New("duplicate key " + key) // I-14
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, err
		}
		fields[key] = value
	}

	// Cierre del objeto '}' y nada más después.
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("data after the object")
	}
	return fields, nil
}

// isJSONContentType acepta "application/json" con o sin "; charset=utf-8".
func isJSONContentType(header string) bool {
	mediaType, params, err := mime.ParseMediaType(header)
	if err != nil || mediaType != "application/json" {
		return false
	}
	charset, hasCharset := params["charset"]
	return !hasCharset || strings.EqualFold(charset, "utf-8")
}

// field devuelve el valor crudo de un campo obligatorio. Las claves distinguen
// mayúsculas (I-14). Un campo ausente o en null es un error (I-03).
func (b *jsonBody) field(key string) (json.RawMessage, bool) {
	if b.err != nil {
		return nil, false
	}
	raw, ok := b.fields[key]
	if !ok || string(raw) == "null" {
		b.err = model.ErrInvalidRequestBody
		return nil, false
	}
	return raw, true
}

// String lee un campo obligatorio de tipo texto. Un número, booleano u objeto es un error (I-03).
func (b *jsonBody) String(key string) string {
	raw, ok := b.field(key)
	if !ok {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		b.err = model.ErrInvalidRequestBody
		return ""
	}
	return value
}

// Int64 lee un campo obligatorio numérico con valor entero (I-01):
//   - 150000 y 150000.0 y 1.5e5 se aceptan como 150000.
//   - 150000.5, "150000" (texto) o true son errores.
//   - Un entero fuera del rango de int64 se satura a math.MaxInt64 o math.MinInt64,
//     para que el service responda "Amount exceeds maximum" o "Amount must be greater than zero".
func (b *jsonBody) Int64(key string) int64 {
	raw, ok := b.field(key)
	if !ok {
		return 0
	}
	value, err := parseInteger(raw)
	if err != nil {
		b.err = model.ErrInvalidRequestBody
		return 0
	}
	return value
}

// parseInteger convierte un número JSON con valor entero en int64.
func parseInteger(raw json.RawMessage) (int64, error) {
	// Ojo: json.Unmarshal en json.Number también acepta el texto "150000".
	// Por eso se comprueba antes que el valor sea un número JSON (empieza con '-' o un dígito).
	text := string(raw)
	if text[0] != '-' && (text[0] < '0' || text[0] > '9') {
		return 0, errors.New("not a JSON number") // texto, booleano, objeto…
	}

	// Caso común: entero escrito sin decimales ni exponente.
	if value, err := strconv.ParseInt(text, 10, 64); err == nil {
		return value, nil
	} else if errors.Is(err, strconv.ErrRange) {
		return saturate(text), nil
	}

	// Con decimales o exponente: se acepta solo si el valor es entero.
	// Con ErrRange, f es ±Inf (número enorme) o un valor cercano a 0 (número diminuto);
	// ambos casos se resuelven con las comprobaciones de abajo.
	f, err := strconv.ParseFloat(text, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, err
	}
	if f != math.Trunc(f) {
		return 0, errors.New("number has a fractional part")
	}
	if f >= math.MaxInt64 {
		return math.MaxInt64, nil
	}
	if f <= math.MinInt64 {
		return math.MinInt64, nil
	}
	return int64(f), nil
}

// saturate devuelve el extremo de int64 según el signo del número.
func saturate(text string) int64 {
	if strings.HasPrefix(text, "-") {
		return math.MinInt64
	}
	return math.MaxInt64
}
