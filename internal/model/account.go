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
