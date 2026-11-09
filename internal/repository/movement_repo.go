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
