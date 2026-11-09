package main

import (
	"os"
	"path/filepath"
	"testing"
)

// writeEnvFile crea un .env temporal con el contenido dado y devuelve su ruta.
func writeEnvFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDotEnv(t *testing.T) {
	path := writeEnvFile(t, `
# comentario
TEST_PLAIN=value
TEST_SPACES =  spaced value
TEST_DOUBLE="quoted"
TEST_SINGLE='single'
TEST_EQUALS=postgres://u:p@h/db?sslmode=disable
TEST_EXISTING=from-file
`)
	// t.Setenv restaura el valor al terminar la prueba.
	t.Setenv("TEST_EXISTING", "from-env")
	for _, k := range []string{"TEST_PLAIN", "TEST_SPACES", "TEST_DOUBLE", "TEST_SINGLE", "TEST_EQUALS"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}

	if err := loadDotEnv(path); err != nil {
		t.Fatalf("loadDotEnv: %v", err)
	}

	want := map[string]string{
		"TEST_PLAIN":    "value",
		"TEST_SPACES":   "spaced value",
		"TEST_DOUBLE":   "quoted",
		"TEST_SINGLE":   "single",
		"TEST_EQUALS":   "postgres://u:p@h/db?sslmode=disable",
		"TEST_EXISTING": "from-env", // el entorno real tiene prioridad
	}
	for key, expected := range want {
		if got := os.Getenv(key); got != expected {
			t.Errorf("%s = %q, want %q", key, got, expected)
		}
	}
}

func TestLoadDotEnvMissingFile(t *testing.T) {
	if err := loadDotEnv(filepath.Join(t.TempDir(), "missing.env")); err != nil {
		t.Fatalf("missing file should be ignored, got %v", err)
	}
}

func TestLoadDotEnvInvalidLine(t *testing.T) {
	path := writeEnvFile(t, "VALID=1\nINVALID_LINE\n")
	if err := loadDotEnv(path); err == nil {
		t.Fatal("expected error for line without '='")
	}
}
