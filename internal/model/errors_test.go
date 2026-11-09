package model

import (
	"errors"
	"fmt"
	"testing"
)

// Cada error del catálogo con el code y message exactos del spec (sección 7.2).
func TestCatalog(t *testing.T) {
	tests := []struct {
		err     *AppError
		code    string
		message string
	}{
		{ErrInvalidRequestBody, "BAD_REQUEST", "Invalid request body"},
		{ErrInvalidName, "BAD_REQUEST", "Invalid name"},
		{ErrInvalidEmail, "BAD_REQUEST", "Invalid email"},
		{ErrPasswordTooShort, "BAD_REQUEST", "Password too short"},
		{ErrPasswordTooLong, "BAD_REQUEST", "Password too long"},
		{ErrInvalidAccountType, "BAD_REQUEST", "Invalid account type"},
		{ErrInvalidAccountID, "BAD_REQUEST", "Invalid account id"},
		{ErrInvalidAccountNumber, "BAD_REQUEST", "Invalid account number"},
		{ErrInvalidRechargeType, "BAD_REQUEST", "Invalid recharge type"},
		{ErrInvalidAmount, "BAD_REQUEST", "Amount must be greater than zero"},
		{ErrAmountTooLarge, "BAD_REQUEST", "Amount exceeds maximum"},
		{ErrSameAccount, "BAD_REQUEST", "Cannot transfer to the same account"},
		{ErrInvalidPagination, "BAD_REQUEST", "Invalid pagination"},
		{ErrInvalidIdempotencyKey, "BAD_REQUEST", "Invalid idempotency key"},
		{ErrUnauthorized, "UNAUTHORIZED", "Missing or invalid token"},
		{ErrInvalidCredentials, "INVALID_CREDENTIALS", "Invalid email or password"},
		{ErrInvalidRefreshToken, "INVALID_REFRESH_TOKEN", "Invalid refresh token"},
		{ErrForbidden, "FORBIDDEN", "Access denied"},
		{ErrAccountNotFound, "NOT_FOUND", "Account not found"},
		{ErrRouteNotFound, "ROUTE_NOT_FOUND", "Route not found"},
		{ErrMethodNotAllowed, "METHOD_NOT_ALLOWED", "Method not allowed"},
		{ErrEmailAlreadyRegistered, "EMAIL_ALREADY_REGISTERED", "Email already registered"},
		{ErrIdempotencyInProgress, "IDEMPOTENCY_REQUEST_IN_PROGRESS", "Request with this idempotency key is in progress"},
		{ErrIdempotencyKeyReused, "IDEMPOTENCY_KEY_REUSED", "Idempotency key already used with a different request"},
		{ErrInsufficientFunds, "INSUFFICIENT_FUNDS", "Insufficient funds"},
		{ErrCashLimitExceeded, "CASH_LIMIT_EXCEEDED", "Cash recharge limit exceeded"},
		{ErrDailyRechargeCountExceeded, "DAILY_RECHARGE_COUNT_EXCEEDED", "Daily recharge count exceeded"},
		{ErrDailyRechargeAmountExceeded, "DAILY_RECHARGE_AMOUNT_EXCEEDED", "Daily recharge amount exceeded"},
		{ErrBalanceLimitExceeded, "BALANCE_LIMIT_EXCEEDED", "Balance limit exceeded"},
		{ErrTooManyLoginAttempts, "TOO_MANY_REQUESTS", "Too many login attempts"},
		{ErrInternal, "INTERNAL_ERROR", "Internal error"},
	}
	for _, tc := range tests {
		t.Run(tc.message, func(t *testing.T) {
			if tc.err.Code != tc.code || tc.err.Message != tc.message {
				t.Errorf("got {%s, %s}, want {%s, %s}", tc.err.Code, tc.err.Message, tc.code, tc.message)
			}
			if tc.err.Error() != tc.message {
				t.Errorf("Error() = %q, want %q", tc.err.Error(), tc.message)
			}
		})
	}
}

// errors.As recupera el AppError aunque venga envuelto con %w.
func TestAppErrorWrapped(t *testing.T) {
	wrapped := fmt.Errorf("transfer: %w", ErrInsufficientFunds)

	var appErr *AppError
	if !errors.As(wrapped, &appErr) {
		t.Fatal("errors.As did not find the AppError")
	}
	if appErr != ErrInsufficientFunds {
		t.Errorf("got %v, want ErrInsufficientFunds", appErr)
	}
}

func TestLoginBlockedError(t *testing.T) {
	var err error = &LoginBlockedError{RetryAfter: 120}

	if !errors.Is(err, ErrTooManyLoginAttempts) {
		t.Error("errors.Is should match ErrTooManyLoginAttempts")
	}
	var appErr *AppError
	if !errors.As(err, &appErr) || appErr.Code != "TOO_MANY_REQUESTS" {
		t.Errorf("errors.As should find TOO_MANY_REQUESTS, got %v", appErr)
	}
	var blocked *LoginBlockedError
	if !errors.As(err, &blocked) || blocked.RetryAfter != 120 {
		t.Errorf("errors.As should find RetryAfter=120, got %v", blocked)
	}
	if want := "Too many login attempts (retry after 120 seconds)"; err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestStorableErrors(t *testing.T) {
	for _, e := range []*AppError{ErrAccountNotFound, ErrInsufficientFunds, ErrCashLimitExceeded,
		ErrDailyRechargeCountExceeded, ErrDailyRechargeAmountExceeded, ErrBalanceLimitExceeded} {
		if got, ok := StorableError(fmt.Errorf("wrapped: %w", e)); !ok || got != e {
			t.Errorf("%s: StorableError = %v, %v", e.Code, got, ok)
		}
		if got, ok := StoredError(e.Code); !ok || got != e {
			t.Errorf("%s: StoredError = %v, %v", e.Code, got, ok)
		}
	}
	// Los errores de los pasos 1 a 9 no se guardan.
	for _, e := range []error{ErrInvalidRequestBody, ErrInvalidAmount, ErrForbidden, ErrIdempotencyKeyReused, errors.New("db down")} {
		if _, ok := StorableError(e); ok {
			t.Errorf("%v must not be storable", e)
		}
	}
	if _, ok := StoredError("BAD_REQUEST"); ok {
		t.Error("BAD_REQUEST must not be a stored code")
	}
}
