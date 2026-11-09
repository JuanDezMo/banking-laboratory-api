package main

import (
	"database/sql"
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	validSecret := strings.Repeat("s", minJWTSecretLength)

	tests := []struct {
		name        string
		databaseURL string
		jwtSecret   string
		wantErr     string
	}{
		{"valid", "postgres://x", validSecret, ""},
		{"missing database url", "", validSecret, "DATABASE_URL"},
		{"missing jwt secret", "postgres://x", "", "JWT_SECRET"},
		{"short jwt secret", "postgres://x", validSecret[1:], "JWT_SECRET"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", tc.databaseURL)
			t.Setenv("JWT_SECRET", tc.jwtSecret)

			_, err := loadConfig()
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("error = %v, want it to mention %s", err, tc.wantErr)
			}
		})
	}
}

// TR.2: el pool queda limitado a maxOpenConns.
func TestConfigurePool(t *testing.T) {
	db, err := sql.Open("pgx", "postgres://unused") // sql.Open no conecta
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	configurePool(db)
	if got := db.Stats().MaxOpenConnections; got != 25 {
		t.Errorf("MaxOpenConnections = %d, want 25", got)
	}
}
