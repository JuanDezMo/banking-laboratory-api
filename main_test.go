package main

import (
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
