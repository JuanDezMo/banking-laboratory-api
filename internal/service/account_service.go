// Crear, listar y consultar cuentas; historial.
package service

import (
	"context"
	"database/sql"
	"uuid"

	"accounts-api/internal/model"
	"accounts-api/internal/repository"
)

// AccountService implementa F-05 a F-07. El rol (403) lo comprueba el handler, antes de
// leer el cuerpo (spec 6); aquí se reciben solo peticiones con el rol permitido.
type AccountService struct {
	db       *sql.DB
	accounts repository.AccountRepo
}

func NewAccountService(db *sql.DB) *AccountService {
	return &AccountService{db: db}
}

// Create abre una cuenta con saldo 0 a nombre del customer (F-05, RN-02, RN-03, RN-21).
func (s *AccountService) Create(ctx context.Context, owner model.AuthUser, accountType string) (model.Account, error) {
	kind, err := model.ParseAccountType(accountType) // paso 4
	if err != nil {
		return model.Account{}, err
	}
	account := model.Account{
		ID:                  uuid.NewV4().String(),
		OwnerID:             owner.ID,
		Kind:                kind,
		OverdraftLimitCents: kind.OverdraftLimitCents(),
	}
	return s.accounts.Create(ctx, s.db, account, repository.RandomAccountNumber)
}

// List devuelve las cuentas del customer en el orden de I-11 (F-06).
func (s *AccountService) List(ctx context.Context, owner model.AuthUser) ([]model.Account, error) {
	return s.accounts.ListByOwner(ctx, s.db, owner.ID)
}

// Get devuelve una cuenta (F-07). Un customer solo ve las suyas: una cuenta ajena
// responde lo mismo que una inexistente (RN-24).
func (s *AccountService) Get(ctx context.Context, user model.AuthUser, rawID string) (model.Account, error) {
	id, err := NormalizeAccountID(rawID) // paso 2
	if err != nil {
		return model.Account{}, err
	}
	account, err := s.accounts.FindByID(ctx, s.db, id) // paso 3
	if err != nil {
		return model.Account{}, err
	}
	if user.Role == model.RoleCustomer && account.OwnerID != user.ID {
		return model.Account{}, model.ErrAccountNotFound
	}
	return account, nil
}
