// Account, AccountType y las reglas Withdraw y Deposit.
package model

import "time"

// AccountType es el tipo de cuenta (RN-02).
type AccountType string

const (
	AccountSavings  AccountType = "savings"  // ahorros: nunca en negativo (RN-04)
	AccountChecking AccountType = "checking" // corriente: sobregiro hasta 500000 centavos (RN-05)
)

// CheckingOverdraftLimitCents es el sobregiro máximo de una cuenta corriente (RN-05).
const CheckingOverdraftLimitCents = 500000

// ParseAccountType acepta exactamente "savings" o "checking" (distingue mayúsculas).
func ParseAccountType(value string) (AccountType, error) {
	switch t := AccountType(value); t {
	case AccountSavings, AccountChecking:
		return t, nil
	}
	return "", ErrInvalidAccountType
}

// OverdraftLimitCents es el sobregiro permitido para el tipo de cuenta (RN-04, RN-05).
func (t AccountType) OverdraftLimitCents() int64 {
	if t == AccountChecking {
		return CheckingOverdraftLimitCents
	}
	return 0
}

// Account es una cuenta bancaria. Los montos están en centavos (RN-01).
type Account struct {
	ID                  string
	OwnerID             string // el customer que la creó (RN-21)
	Number              string // 10 dígitos, único
	Kind                AccountType
	BalanceCents        int64
	OverdraftLimitCents int64
	CreatedAt           time.Time
}

// Deposit suma amount al saldo. Si el saldo superaría el máximo (RN-31), no cambia nada
// y devuelve ErrBalanceLimitExceeded. amount debe ser positivo y ≤ MaxAmountCents (ya validado),
// así que la suma no desborda int64.
func (a *Account) Deposit(amount int64) error {
	if a.BalanceCents+amount > MaxBalanceCents {
		return ErrBalanceLimitExceeded
	}
	a.BalanceCents += amount
	return nil
}

// Withdraw resta amount del saldo. Si el saldo quedaría por debajo del sobregiro permitido
// (RN-04: savings 0; RN-05: checking -500000), no cambia nada y devuelve ErrInsufficientFunds.
// amount debe ser positivo y ≤ MaxAmountCents (ya validado), así que la resta no desborda int64.
func (a *Account) Withdraw(amount int64) error {
	if a.BalanceCents-amount < -a.OverdraftLimitCents {
		return ErrInsufficientFunds
	}
	a.BalanceCents -= amount
	return nil
}
