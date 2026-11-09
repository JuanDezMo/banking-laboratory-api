// Errores de negocio: AppError y todos los errores del catálogo (bank-lab-spec.md, sección 7.2).
// model no sabe nada de HTTP: el estado HTTP de cada código lo decide handler/respond.go.
package model

import (
	"errors"
	"fmt"
)

// AppError es un error del catálogo: un código estable y un mensaje en inglés.
type AppError struct {
	Code    string // código del catálogo, por ejemplo "INSUFFICIENT_FUNDS"
	Message string // mensaje legible, por ejemplo "Insufficient funds"
}

func (e *AppError) Error() string { return e.Message }

// Catálogo. Los nombres siguen las variantes de BankError del spec, con el prefijo Err.
var (
	// 400 BAD_REQUEST
	ErrInvalidRequestBody    = &AppError{"BAD_REQUEST", "Invalid request body"}
	ErrInvalidName           = &AppError{"BAD_REQUEST", "Invalid name"}
	ErrInvalidEmail          = &AppError{"BAD_REQUEST", "Invalid email"}
	ErrPasswordTooShort      = &AppError{"BAD_REQUEST", "Password too short"}
	ErrPasswordTooLong       = &AppError{"BAD_REQUEST", "Password too long"}
	ErrInvalidAccountType    = &AppError{"BAD_REQUEST", "Invalid account type"}
	ErrInvalidAccountID      = &AppError{"BAD_REQUEST", "Invalid account id"}
	ErrInvalidAccountNumber  = &AppError{"BAD_REQUEST", "Invalid account number"}
	ErrInvalidRechargeType   = &AppError{"BAD_REQUEST", "Invalid recharge type"}
	ErrInvalidAmount         = &AppError{"BAD_REQUEST", "Amount must be greater than zero"}
	ErrAmountTooLarge        = &AppError{"BAD_REQUEST", "Amount exceeds maximum"}
	ErrSameAccount           = &AppError{"BAD_REQUEST", "Cannot transfer to the same account"}
	ErrInvalidPagination     = &AppError{"BAD_REQUEST", "Invalid pagination"}
	ErrInvalidIdempotencyKey = &AppError{"BAD_REQUEST", "Invalid idempotency key"}

	// 401
	ErrUnauthorized        = &AppError{"UNAUTHORIZED", "Missing or invalid token"}
	ErrInvalidCredentials  = &AppError{"INVALID_CREDENTIALS", "Invalid email or password"}
	ErrInvalidRefreshToken = &AppError{"INVALID_REFRESH_TOKEN", "Invalid refresh token"}

	// 403
	ErrForbidden = &AppError{"FORBIDDEN", "Access denied"}

	// 404 y 405
	ErrAccountNotFound  = &AppError{"NOT_FOUND", "Account not found"}
	ErrRouteNotFound    = &AppError{"ROUTE_NOT_FOUND", "Route not found"}
	ErrMethodNotAllowed = &AppError{"METHOD_NOT_ALLOWED", "Method not allowed"}

	// 409
	ErrEmailAlreadyRegistered = &AppError{"EMAIL_ALREADY_REGISTERED", "Email already registered"}
	ErrIdempotencyInProgress  = &AppError{"IDEMPOTENCY_REQUEST_IN_PROGRESS", "Request with this idempotency key is in progress"}

	// 422
	ErrIdempotencyKeyReused        = &AppError{"IDEMPOTENCY_KEY_REUSED", "Idempotency key already used with a different request"}
	ErrInsufficientFunds           = &AppError{"INSUFFICIENT_FUNDS", "Insufficient funds"}
	ErrCashLimitExceeded           = &AppError{"CASH_LIMIT_EXCEEDED", "Cash recharge limit exceeded"}
	ErrDailyRechargeCountExceeded  = &AppError{"DAILY_RECHARGE_COUNT_EXCEEDED", "Daily recharge count exceeded"}
	ErrDailyRechargeAmountExceeded = &AppError{"DAILY_RECHARGE_AMOUNT_EXCEEDED", "Daily recharge amount exceeded"}
	ErrBalanceLimitExceeded        = &AppError{"BALANCE_LIMIT_EXCEEDED", "Balance limit exceeded"}

	// 429: se devuelve envuelto en LoginBlockedError, que agrega los segundos de espera.
	ErrTooManyLoginAttempts = &AppError{"TOO_MANY_REQUESTS", "Too many login attempts"}

	// 500
	ErrInternal = &AppError{"INTERNAL_ERROR", "Internal error"}
)

// LoginBlockedError indica que el login está bloqueado (RN-29) y cuántos segundos faltan.
// Envuelve a ErrTooManyLoginAttempts, así errors.Is y errors.As lo reconocen como ese error.
type LoginBlockedError struct {
	RetryAfter int // segundos, para el encabezado Retry-After
}

func (e *LoginBlockedError) Error() string {
	return fmt.Sprintf("%s (retry after %d seconds)", ErrTooManyLoginAttempts.Message, e.RetryAfter)
}

func (e *LoginBlockedError) Unwrap() error { return ErrTooManyLoginAttempts }

// storableErrors son los errores de negocio (404 y 422) que se guardan con la
// idempotency key y se repiten igual al reintentar (spec 6 y 8.1).
var storableErrors = map[string]*AppError{}

func init() {
	for _, e := range []*AppError{
		ErrAccountNotFound,
		ErrInsufficientFunds,
		ErrCashLimitExceeded,
		ErrDailyRechargeCountExceeded,
		ErrDailyRechargeAmountExceeded,
		ErrBalanceLimitExceeded,
	} {
		storableErrors[e.Code] = e
	}
}

// StorableError devuelve el AppError si err es un error que se guarda con la idempotency key.
func StorableError(err error) (*AppError, bool) {
	var appErr *AppError
	if !errors.As(err, &appErr) {
		return nil, false
	}
	stored, ok := storableErrors[appErr.Code]
	return stored, ok
}

// StoredError devuelve el error del catálogo con ese code, guardado con una idempotency key.
func StoredError(code string) (*AppError, bool) {
	e, ok := storableErrors[code]
	return e, ok
}
