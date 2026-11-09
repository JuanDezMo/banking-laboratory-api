// Tabla accounts.
package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"

	"github.com/jackc/pgx/v5/pgconn"

	"accounts-api/internal/model"
)

// maxAccountNumberAttempts es cuántos números se prueban antes de rendirse.
const maxAccountNumberAttempts = 5

// accountColumns es la lista de columnas en el orden de scanAccount.
const accountColumns = `id, owner_id, number, kind, balance_cents, overdraft_limit_cents, created_at`

// AccountRepo agrupa el SQL de cuentas. No tiene estado: la conexión llega como parámetro (DBTX).
type AccountRepo struct{}

// tenDigits es 10^10: los números van de 0000000000 a 9999999999.
var tenDigits = big.NewInt(10_000_000_000)

// RandomAccountNumber genera 10 dígitos aleatorios con crypto/rand (puede empezar con ceros).
func RandomAccountNumber() string {
	n, _ := rand.Int(rand.Reader, tenDigits) // desde Go 1.24, crypto/rand no devuelve error
	return fmt.Sprintf("%010d", n)
}

// Create inserta la cuenta con un número de newNumber. Si el número ya existe (UNIQUE),
// prueba otro, hasta 5 veces. Devuelve la cuenta con su número y fecha de creación (hora de la BD).
//
// No debe llamarse dentro de una transacción: en PostgreSQL un INSERT fallido aborta la
// transacción y el reintento fallaría.
func (AccountRepo) Create(ctx context.Context, db DBTX, a model.Account, newNumber func() string) (model.Account, error) {
	for range maxAccountNumberAttempts {
		a.Number = newNumber()
		err := db.QueryRowContext(ctx,
			`INSERT INTO accounts (id, owner_id, number, kind, overdraft_limit_cents)
			 VALUES ($1, $2, $3, $4, $5)
			 RETURNING balance_cents, created_at`,
			a.ID, a.OwnerID, a.Number, a.Kind, a.OverdraftLimitCents).
			Scan(&a.BalanceCents, &a.CreatedAt)

		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == "accounts_number_key" {
			continue // número repetido: se prueba otro
		}
		return a, err
	}
	return model.Account{}, fmt.Errorf("create account: %d account number collisions in a row", maxAccountNumberAttempts)
}

// FindByID busca una cuenta por id. Si no existe devuelve ErrAccountNotFound.
func (AccountRepo) FindByID(ctx context.Context, db DBTX, id string) (model.Account, error) {
	a, err := scanAccount(db.QueryRowContext(ctx,
		`SELECT `+accountColumns+` FROM accounts WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Account{}, model.ErrAccountNotFound
	}
	return a, err
}

// ListByOwner devuelve las cuentas del usuario, por fecha de creación ascendente y,
// si hay empate, por id ascendente (I-11). El orden de un uuid en PostgreSQL es el mismo
// que el de su texto en minúsculas. Sin cuentas devuelve una lista vacía, no nil.
func (AccountRepo) ListByOwner(ctx context.Context, db DBTX, ownerID string) ([]model.Account, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT `+accountColumns+` FROM accounts WHERE owner_id = $1 ORDER BY created_at, id`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	accounts := []model.Account{}
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

// scanner lo cumplen *sql.Row y *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanAccount(row scanner) (model.Account, error) {
	var a model.Account
	err := row.Scan(&a.ID, &a.OwnerID, &a.Number, &a.Kind, &a.BalanceCents, &a.OverdraftLimitCents, &a.CreatedAt)
	return a, err
}

// LockByNumber busca una cuenta por número y bloquea su fila hasta el fin de la
// transacción (FOR UPDATE). Si no existe devuelve ErrAccountNotFound.
func (AccountRepo) LockByNumber(ctx context.Context, tx DBTX, number string) (model.Account, error) {
	a, err := scanAccount(tx.QueryRowContext(ctx,
		`SELECT `+accountColumns+` FROM accounts WHERE number = $1 FOR UPDATE`, number))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Account{}, model.ErrAccountNotFound
	}
	return a, err
}

// UpdateBalance guarda el saldo de la cuenta. Los CHECK de la tabla son la última defensa
// de RN-04, RN-05 y RN-31.
func (AccountRepo) UpdateBalance(ctx context.Context, db DBTX, a model.Account) error {
	_, err := db.ExecContext(ctx, `UPDATE accounts SET balance_cents = $2 WHERE id = $1`, a.ID, a.BalanceCents)
	return err
}
