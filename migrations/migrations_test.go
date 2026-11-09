// Prueba externa (paquete migrations_test) para poder usar testdb, que importa migrations.
package migrations_test

import (
	"context"
	"testing"

	"accounts-api/internal/testdb"
	"accounts-api/migrations"
)

// Aplicar dos veces no repite migraciones (T2.0).
func TestApplyIsIdempotent(t *testing.T) {
	db := testdb.Open(t) // ya aplicó las migraciones una vez
	ctx := context.Background()

	count := func() int {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := count()
	if before == 0 {
		t.Fatal("no migrations were applied")
	}

	if err := migrations.Apply(ctx, db); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if after := count(); after != before {
		t.Errorf("schema_migrations went from %d to %d rows", before, after)
	}
}

// Las tablas de 001_auth_accounts existen (T2.1, T2.2).
func TestAuthAccountsTables(t *testing.T) {
	db := testdb.Open(t)
	for _, table := range []string{"users", "refresh_tokens", "login_attempts", "accounts"} {
		var exists bool
		err := db.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists)
		if err != nil || !exists {
			t.Errorf("table %s does not exist (err: %v)", table, err)
		}
	}
}
