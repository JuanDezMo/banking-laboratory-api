// Package testdb prepara la BD de pruebas de integración (bank_test).
//
// Cada llamada a Open crea un esquema vacío propio del paquete de pruebas, aplica las
// migraciones y devuelve una conexión que trabaja en ese esquema. Así los paquetes, que
// go test ejecuta en paralelo, no se pisan entre sí, y cada prueba empieza sin datos.
package testdb

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // registra el driver "pgx"

	"accounts-api/migrations"
)

// Open devuelve una BD limpia y migrada. La prueba falla si falta TEST_DATABASE_URL.
func Open(t *testing.T) *sql.DB {
	t.Helper()
	baseURL := os.Getenv("TEST_DATABASE_URL")
	if baseURL == "" {
		t.Fatal("TEST_DATABASE_URL is required for integration tests " +
			"(start the db with `docker compose up -d db` and run `set -a; source .env; set +a`)")
	}

	// go test ejecuta cada paquete en su carpeta: el nombre de la carpeta identifica el esquema.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	schema := "test_" + filepath.Base(cwd)

	ctx := context.Background()
	admin := open(t, baseURL)
	defer admin.Close()
	// El nombre viene de la carpeta del paquete (texto fijo del proyecto), no de una entrada externa.
	if _, err := admin.ExecContext(ctx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE; CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("reset schema %s: %v", schema, err)
	}

	db := open(t, withSearchPath(t, baseURL, schema))
	t.Cleanup(func() { db.Close() })
	if err := migrations.Apply(ctx, db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return db
}

func open(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	return db
}

// withSearchPath agrega search_path a la URL: todas las conexiones usan el esquema dado.
func withSearchPath(t *testing.T, dsn, schema string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}
