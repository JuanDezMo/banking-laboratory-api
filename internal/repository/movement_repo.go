// Tabla movements.
package repository

import (
	"context"
	"database/sql"
	"fmt"

	"accounts-api/internal/model"
)

const movementColumns = `id, account_id, kind, amount_cents, related_account_id, recharge_type, created_at`

// MovementRepo agrupa el SQL del log de movimientos. Solo inserta y lee (RN-16).
type MovementRepo struct{}

// Insert agrega el movimiento y devuelve su fecha de creación (hora de la BD, I-10).
func (MovementRepo) Insert(ctx context.Context, db DBTX, m model.Movement) (model.Movement, error) {
	err := db.QueryRowContext(ctx,
		`INSERT INTO movements (id, account_id, kind, amount_cents, related_account_id, recharge_type)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING created_at`,
		m.ID, m.AccountID, m.Kind, m.AmountCents, nullIfEmpty(m.RelatedAccountID), nullIfEmpty(string(m.RechargeType))).
		Scan(&m.CreatedAt)
	return m, err
}

// FindByIDs lee los movimientos con esos ids, en el mismo orden de ids.
// Si falta alguno devuelve un error: los ids vienen de una fila de idempotency_keys.
func (MovementRepo) FindByIDs(ctx context.Context, db DBTX, ids []string) ([]model.Movement, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT `+movementColumns+` FROM movements
		  WHERE id = ANY($1::uuid[])
		  ORDER BY array_position($1::uuid[], id)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var movements []model.Movement
	for rows.Next() {
		m, err := scanMovement(rows)
		if err != nil {
			return nil, err
		}
		movements = append(movements, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(movements) != len(ids) {
		return nil, fmt.Errorf("find movements: found %d of %d ids", len(movements), len(ids))
	}
	return movements, nil
}

func scanMovement(row scanner) (model.Movement, error) {
	var m model.Movement
	var related, rechargeType sql.NullString
	err := row.Scan(&m.ID, &m.AccountID, &m.Kind, &m.AmountCents, &related, &rechargeType, &m.CreatedAt)
	m.RelatedAccountID = related.String
	m.RechargeType = model.RechargeType(rechargeType.String)
	return m, err
}

// nullIfEmpty convierte "" en NULL para las columnas opcionales.
func nullIfEmpty(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// DailyRecharges cuenta y suma las recargas de la cuenta en el día UTC actual, según la
// hora de la BD (I-10). Se llama con la cuenta ya bloqueada (FOR UPDATE), así dos recargas
// simultáneas de la misma cuenta no ven el mismo conteo (RN-20).
func (MovementRepo) DailyRecharges(ctx context.Context, db DBTX, accountID string) (count int, totalCents int64, err error) {
	err = db.QueryRowContext(ctx,
		`SELECT count(*), coalesce(sum(amount_cents), 0)
		   FROM movements
		  WHERE account_id = $1 AND kind = 'recharge'
		    AND created_at >= date_trunc('day', now() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC'`,
		accountID).
		Scan(&count, &totalCents)
	return count, totalCents, err
}

// MovementPageSize es el tamaño fijo de una página del historial (F-10).
const MovementPageSize = 20

// ListPage devuelve una página del historial de la cuenta: createdAt descendente y, si hay
// empate, id descendente (I-11). Una página posterior a la última devuelve una lista vacía.
func (MovementRepo) ListPage(ctx context.Context, db DBTX, accountID string, page int) ([]model.Movement, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT `+movementColumns+` FROM movements
		  WHERE account_id = $1
		  ORDER BY created_at DESC, id DESC
		  LIMIT $2 OFFSET $3`,
		accountID, MovementPageSize, (page-1)*MovementPageSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	movements := []model.Movement{}
	for rows.Next() {
		m, err := scanMovement(rows)
		if err != nil {
			return nil, err
		}
		movements = append(movements, m)
	}
	return movements, rows.Err()
}
