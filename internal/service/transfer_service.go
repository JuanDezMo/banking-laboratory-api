// Transferencia (F-09).
package service

import (
	"context"
	"database/sql"
	"strconv"
	"uuid"

	"accounts-api/internal/model"
	"accounts-api/internal/repository"
)

// TransferService implementa F-09. El rol (403) y el formato de la key (400) los
// comprueba el handler, antes que todo lo demás (spec 6).
type TransferService struct {
	db        *sql.DB
	accounts  repository.AccountRepo
	movements repository.MovementRepo
}

func NewTransferService(db *sql.DB) *TransferService {
	return &TransferService{db: db}
}

// TransferInput son los campos del cuerpo, tal como llegan.
type TransferInput struct {
	AccountOrigin      string
	AccountDestination string
	Amount             int64
}

// TransferResult son los dos movimientos de una transferencia.
type TransferResult struct {
	Out model.Movement // transfer_out en la cuenta origen
	In  model.Movement // transfer_in en la cuenta destino
}

// hash es el hash de la petición: accountOrigin|accountDestination|amount, o "invalid".
func (in TransferInput) hash(bodyErr error) string {
	if bodyErr != nil {
		return invalidRequestHash
	}
	return requestHash(in.AccountOrigin, in.AccountDestination, strconv.FormatInt(in.Amount, 10))
}

// Transfer debita el origen, acredita el destino y registra dos movimientos, en una
// operación atómica e idempotente (RN-13 a RN-15, RN-23, RN-28). key es la Idempotency-Key
// ya validada; bodyErr se responde en el paso 5, después de revisar la key.
func (s *TransferService) Transfer(ctx context.Context, customer model.AuthUser, key string,
	in TransferInput, bodyErr error) (TransferResult, bool, error) {

	movements, replayed, err := runIdempotent(ctx, s.db, idempotentRun{
		scope:       repository.IdempotencyScope{UserID: customer.ID, Endpoint: repository.EndpointTransfer, Key: key},
		requestHash: in.hash(bodyErr),
		validate: func() error {
			if bodyErr != nil {
				return bodyErr // paso 5
			}
			if err := ValidateAccountNumber(in.AccountOrigin); err != nil {
				return err // paso 6
			}
			if err := ValidateAccountNumber(in.AccountDestination); err != nil {
				return err // paso 6
			}
			if err := ValidateAmount(in.Amount); err != nil {
				return err // pasos 7 y 8
			}
			if in.AccountOrigin == in.AccountDestination {
				return model.ErrSameAccount // paso 9 (RN-11)
			}
			return nil
		},
		operation: func(tx *sql.Tx) ([]model.Movement, error) {
			return s.apply(ctx, tx, customer, in)
		},
	})
	if err != nil {
		return TransferResult{}, replayed, err
	}
	return TransferResult{Out: movements[0], In: movements[1]}, replayed, nil // guardados como [out, in]
}

// apply ejecuta los pasos 10 a 13 y escribe los saldos y los dos movimientos.
func (s *TransferService) apply(ctx context.Context, tx *sql.Tx, customer model.AuthUser,
	in TransferInput) ([]model.Movement, error) {

	refs, err := s.accounts.ResolveNumbers(ctx, tx, in.AccountOrigin, in.AccountDestination)
	if err != nil {
		return nil, err
	}
	origin, ok := refs[in.AccountOrigin]
	if !ok || origin.OwnerID != customer.ID {
		return nil, model.ErrAccountNotFound // paso 10: un origen ajeno se trata como inexistente (RN-23)
	}
	destination, ok := refs[in.AccountDestination]
	if !ok {
		return nil, model.ErrAccountNotFound // paso 11
	}

	// Bloqueo por id ascendente: dos transferencias cruzadas no se bloquean entre sí (deadlock).
	locked, err := s.accounts.LockByIDs(ctx, tx, origin.ID, destination.ID)
	if err != nil {
		return nil, err
	}
	from, to := locked[0], locked[1]
	if from.ID != origin.ID {
		from, to = to, from
	}

	if err := from.Withdraw(in.Amount); err != nil {
		return nil, err // paso 12 (RN-13)
	}
	if err := to.Deposit(in.Amount); err != nil {
		return nil, err // paso 13 (RN-31)
	}
	for _, a := range []model.Account{from, to} {
		if err := s.accounts.UpdateBalance(ctx, tx, a); err != nil {
			return nil, err
		}
	}

	out, err := s.movements.Insert(ctx, tx, model.Movement{
		ID: uuid.NewV4().String(), AccountID: from.ID, Kind: model.MovementTransferOut,
		AmountCents: in.Amount, RelatedAccountID: to.ID,
	})
	if err != nil {
		return nil, err
	}
	inMovement, err := s.movements.Insert(ctx, tx, model.Movement{
		ID: uuid.NewV4().String(), AccountID: to.ID, Kind: model.MovementTransferIn,
		AmountCents: in.Amount, RelatedAccountID: from.ID,
	})
	if err != nil {
		return nil, err
	}
	return []model.Movement{out, inMovement}, nil
}
