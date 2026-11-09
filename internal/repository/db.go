// Interfaz DBTX: la cumplen *sql.DB y *sql.Tx.
package repository

import (
	"context"
	"database/sql"
)

// DBTX permite usar la misma función de repositorio fuera o dentro de una transacción.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
