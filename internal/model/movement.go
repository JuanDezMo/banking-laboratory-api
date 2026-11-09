// Movement, MovementType y RechargeType.
package model

import "time"

// MovementType es el tipo de movimiento. El monto siempre es positivo; el tipo da el signo (RN-17).
type MovementType string

const (
	MovementRecharge    MovementType = "recharge"     // suma
	MovementWithdrawal  MovementType = "withdrawal"   // resta (sin endpoint por ahora)
	MovementTransferIn  MovementType = "transfer_in"  // suma
	MovementTransferOut MovementType = "transfer_out" // resta
)

// RechargeType es el tipo de una recarga (RN-06).
type RechargeType string

const (
	RechargeCash          RechargeType = "cash"
	RechargeEmployerBonus RechargeType = "employer_bonus"
	RechargeSalary        RechargeType = "salary"
)

// Límites de montos en centavos.
const (
	MaxAmountCents       = 1_000_000_000_000     // por operación (RN-30)
	MaxCashRechargeCents = 300_000               // por recarga en efectivo (RN-08)
	MaxBalanceCents      = 1_000_000_000_000_000 // saldo máximo de una cuenta (RN-31)
)

// ParseRechargeType acepta exactamente cash, employer_bonus o salary.
func ParseRechargeType(value string) (RechargeType, error) {
	switch t := RechargeType(value); t {
	case RechargeCash, RechargeEmployerBonus, RechargeSalary:
		return t, nil
	}
	return "", ErrInvalidRechargeType
}

// Movement es una línea del log de movimientos (RN-16: nunca se edita ni se borra).
type Movement struct {
	ID               string
	AccountID        string
	Kind             MovementType
	AmountCents      int64
	RelatedAccountID string       // solo en transferencias; "" en otro caso
	RechargeType     RechargeType // solo en recargas; "" en otro caso
	CreatedAt        time.Time
}
