// Recarga (F-08).
package service

import (
	"context"
	"database/sql"
	"strconv"
	"uuid"

	"accounts-api/internal/model"
	"accounts-api/internal/repository"
)

// RechargeService implementa F-08. El rol (403) y el formato de la key (400) los
// comprueba el handler, antes que todo lo demás (spec 6).
type RechargeService struct {
	db        *sql.DB
	accounts  repository.AccountRepo
	movements repository.MovementRepo
}

func NewRechargeService(db *sql.DB) *RechargeService {
	return &RechargeService{db: db}
}

// RechargeInput son los campos del cuerpo, tal como llegan.
type RechargeInput struct {
	AccountNumber string
	Amount        int64
	RechargeType  string
}

// hash es el hash de la petición: accountNumber|amount|rechargeType, o "invalid" si el
// cuerpo no se pudo leer.
func (in RechargeInput) hash(bodyErr error) string {
	if bodyErr != nil {
		return invalidRequestHash
	}
	return requestHash(in.AccountNumber, strconv.FormatInt(in.Amount, 10), in.RechargeType)
}

// Recharge suma el monto al saldo de la cuenta y registra un movimiento recharge, en una
// operación atómica e idempotente (RN-14, RN-15, RN-28). key es la Idempotency-Key ya
// validada; bodyErr es el error al leer el cuerpo, que se responde en el paso 5 (después
// de revisar la key). Devuelve el movimiento y si la respuesta es una repetición.
func (s *RechargeService) Recharge(ctx context.Context, operator model.AuthUser, key string,
	in RechargeInput, bodyErr error) (model.Movement, bool, error) {

	var rechargeType model.RechargeType
	movements, replayed, err := runIdempotent(ctx, s.db, idempotentRun{
		scope:       repository.IdempotencyScope{UserID: operator.ID, Endpoint: repository.EndpointRecharge, Key: key},
		requestHash: in.hash(bodyErr),
		validate: func() error {
			if bodyErr != nil {
				return bodyErr // paso 5
			}
			if err := ValidateAccountNumber(in.AccountNumber); err != nil {
				return err // paso 6
			}
			var err error
			if rechargeType, err = model.ParseRechargeType(in.RechargeType); err != nil {
				return err // paso 7
			}
			return ValidateAmount(in.Amount) // pasos 8 y 9
		},
		operation: func(tx *sql.Tx) ([]model.Movement, error) {
			m, err := s.apply(ctx, tx, in.AccountNumber, in.Amount, rechargeType)
			return []model.Movement{m}, err
		},
	})
	if err != nil || len(movements) == 0 {
		return model.Movement{}, replayed, err
	}
	return movements[0], replayed, nil
}

// apply ejecuta los pasos 10 a 14 y escribe el saldo y el movimiento.
func (s *RechargeService) apply(ctx context.Context, tx *sql.Tx, number string, amount int64,
	rechargeType model.RechargeType) (model.Movement, error) {

	if rechargeType == model.RechargeCash && amount > model.MaxCashRechargeCents {
		return model.Movement{}, model.ErrCashLimitExceeded // paso 10 (RN-08)
	}
	account, err := s.accounts.LockByNumber(ctx, tx, number) // paso 11 (RN-09)
	if err != nil {
		return model.Movement{}, err
	}
	// Topes diarios: la cuenta ya está bloqueada, así el conteo no cambia hasta el COMMIT (RN-20).
	count, total, err := s.movements.DailyRecharges(ctx, tx, account.ID)
	if err != nil {
		return model.Movement{}, err
	}
	if count >= model.MaxDailyRecharges {
		return model.Movement{}, model.ErrDailyRechargeCountExceeded // paso 12 (RN-18)
	}
	if total+amount > model.MaxDailyRechargeCents {
		return model.Movement{}, model.ErrDailyRechargeAmountExceeded // paso 13 (RN-19)
	}
	if err := account.Deposit(amount); err != nil {
		return model.Movement{}, err // paso 14 (RN-31)
	}
	if err := s.accounts.UpdateBalance(ctx, tx, account); err != nil {
		return model.Movement{}, err
	}
	return s.movements.Insert(ctx, tx, model.Movement{
		ID:           uuid.NewV4().String(),
		AccountID:    account.ID,
		Kind:         model.MovementRecharge,
		AmountCents:  amount,
		RechargeType: rechargeType,
	})
}
