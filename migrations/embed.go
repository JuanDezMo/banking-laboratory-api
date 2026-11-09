// Ejecutor de migraciones: aplica en orden los archivos .sql de esta carpeta,
// incluidos en el binario con embed.
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"strings"
)

//go:embed *.sql
var files embed.FS

// migrationLockID identifica el advisory lock de las migraciones. Si varias instancias
// arrancan a la vez, solo una aplica cada migración; las demás esperan.
const migrationLockID = 727001

const createTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    text        PRIMARY KEY,
    applied_at timestamptz NOT NULL DEFAULT date_trunc('milliseconds', now())
)`

// Apply aplica, en orden de nombre, las migraciones que aún no están en schema_migrations.
// Cada archivo se aplica en su propia transacción: si falla, no queda a medias.
func Apply(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, createTable); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	// fs.ReadDir devuelve los archivos ordenados por nombre: 001_…, 002_…
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		if err := applyFile(ctx, db, entry.Name()); err != nil {
			return fmt.Errorf("migration %s: %w", entry.Name(), err)
		}
	}
	return nil
}

// applyFile aplica un archivo si su versión no está registrada.
func applyFile(ctx context.Context, db *sql.DB, name string) error {
	version := strings.TrimSuffix(name, ".sql")

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // no hace nada si ya se hizo Commit

	// El lock se libera solo al terminar la transacción.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationLockID); err != nil {
		return err
	}

	var applied bool
	err = tx.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&applied)
	if err != nil || applied {
		return err
	}

	content, err := files.ReadFile(name)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, string(content)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
		return err
	}
	return tx.Commit()
}
