package service

import (
	"strings"
	"testing"

	"accounts-api/internal/model"
)

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"simple", "Ana Pérez", "Ana Pérez", false},
		{"trims spaces", "  Ana  ", "Ana", false},
		{"one character", "A", "A", false},
		{"100 characters", strings.Repeat("a", 100), strings.Repeat("a", 100), false},
		{"100 accented characters (200 bytes)", strings.Repeat("é", 100), strings.Repeat("é", 100), false},
		{"empty", "", "", true},
		{"only spaces", "   ", "", true},
		{"101 characters", strings.Repeat("a", 101), "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeName(tc.input)
			checkErr(t, err, tc.wantErr, model.ErrInvalidName)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeEmail(t *testing.T) {
	domain := "@mail.com" // 9 caracteres
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"simple", "ana@mail.com", "ana@mail.com", false},
		{"trims and lowercases", "  Ana@Mail.COM ", "ana@mail.com", false},
		{"254 characters", strings.Repeat("a", 245) + domain, strings.Repeat("a", 245) + domain, false},
		{"255 characters", strings.Repeat("a", 246) + domain, "", true},
		{"without @", "ana.mail.com", "", true},
		{"two @", "ana@x@mail.com", "", true},
		{"without dot in domain", "ana@mail", "", true},
		{"dot at the end", "ana@mail.", "", true},
		{"dot at the start of domain", "ana@.com", "", true},
		{"empty local part", "@mail.com", "", true},
		{"inner space", "an a@mail.com", "", true},
		{"inner unicode space", "ana x@mail.com", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeEmail(tc.input)
			checkErr(t, err, tc.wantErr, model.ErrInvalidEmail)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  error
	}{
		{"8 characters", "12345678", nil},
		{"128 characters", strings.Repeat("a", 128), nil},
		{"spaces count", "    1234", nil},
		{"8 accented characters", strings.Repeat("ñ", 8), nil},
		{"7 characters", "1234567", model.ErrPasswordTooShort},
		{"129 characters", strings.Repeat("a", 129), model.ErrPasswordTooLong},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidatePassword(tc.input); err != tc.want {
				t.Errorf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestValidateAccountNumber(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"0123456789", false},
		{"012345678", true},   // 9 dígitos
		{"01234567890", true}, // 11 dígitos
		{"01234 6789", true},  // espacio
		{" 0123456789", true}, // no se recortan espacios
		{"01234567a9", true},  // letra
		{"٠١٢٣٤٥٦٧٨٩", true},  // dígitos no ASCII
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			checkErr(t, ValidateAccountNumber(tc.input), tc.wantErr, model.ErrInvalidAccountNumber)
		})
	}
}

func TestNormalizeUUID(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"lowercase", "a41e5b2c-3d4e-4f5a-8b6c-7d8e9f0a1b2c", "a41e5b2c-3d4e-4f5a-8b6c-7d8e9f0a1b2c", false},
		{"uppercase is normalized", "A41E5B2C-3D4E-4F5A-8B6C-7D8E9F0A1B2C", "a41e5b2c-3d4e-4f5a-8b6c-7d8e9f0a1b2c", false},
		{"any version", "a41e5b2c-3d4e-1f5a-0b6c-7d8e9f0a1b2c", "a41e5b2c-3d4e-1f5a-0b6c-7d8e9f0a1b2c", false},
		{"without hyphens", "a41e5b2c3d4e4f5a8b6c7d8e9f0a1b2c", "", true},
		{"with braces", "{a41e5b2c-3d4e-4f5a-8b6c-7d8e9f0a1b2c}", "", true},
		{"urn", "urn:uuid:a41e5b2c-3d4e-4f5a-8b6c-7d8e9f0a1b2c", "", true},
		{"non hex", "g41e5b2c-3d4e-4f5a-8b6c-7d8e9f0a1b2c", "", true},
		{"hyphen misplaced", "a41e5b2-c3d4e-4f5a-8b6c-7d8e9f0a1b2c", "", true},
		{"with space", " a41e5b2c-3d4e-4f5a-8b6c-7d8e9f0a1b2c", "", true},
		{"empty", "", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeAccountID(tc.input)
			checkErr(t, err, tc.wantErr, model.ErrInvalidAccountID)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			// La key de idempotencia usa la misma regla, con su propio error.
			_, err = NormalizeIdempotencyKey(tc.input)
			checkErr(t, err, tc.wantErr, model.ErrInvalidIdempotencyKey)
		})
	}
}

func TestParsePage(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		present bool
		want    int
		wantErr bool
	}{
		{"absent", "", false, 1, false},
		{"1", "1", true, 1, false},
		{"10000", "10000", true, 10000, false},
		{"leading zeros", "007", true, 7, false},
		{"0", "0", true, 0, true},
		{"10001", "10001", true, 0, true},
		{"empty", "", true, 0, true},
		{"negative", "-1", true, 0, true},
		{"plus sign", "+5", true, 0, true},
		{"decimal", "1.5", true, 0, true},
		{"text", "abc", true, 0, true},
		{"huge", "99999999999999999999999", true, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParsePage(tc.raw, tc.present)
			checkErr(t, err, tc.wantErr, model.ErrInvalidPagination)
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// checkErr comprueba que err sea nil o exactamente el error esperado del catálogo.
func checkErr(t *testing.T, err error, wantErr bool, expected error) {
	t.Helper()
	switch {
	case wantErr && err != expected:
		t.Errorf("error = %v, want %v", err, expected)
	case !wantErr && err != nil:
		t.Errorf("unexpected error: %v", err)
	}
}
