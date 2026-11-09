package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"

	"accounts-api/internal/model"
	"accounts-api/internal/testdb"
)

func newAuthService(t *testing.T) (*AuthService, *sql.DB) {
	t.Helper()
	db := testdb.Open(t)
	return NewAuthService(db, NewPasswordHasher(4), NewTokenIssuer(testSecret)), db
}

// Un caso por error de F-01, en el orden del spec 6 (el paso 1, cuerpo inválido, es del handler).
func TestRegisterValidationOrder(t *testing.T) {
	s, _ := newAuthService(t)
	ctx := context.Background()
	valid := RegisterInput{Name: "Ana Pérez", Email: "ana@mail.com", Password: "S3gura2026"}
	if _, err := s.Register(ctx, valid); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		in   RegisterInput
		want error
	}{
		// Cada caso tiene además los errores de los pasos siguientes: solo debe salir el primero.
		{"2 invalid name", RegisterInput{"", "bad", "123"}, model.ErrInvalidName},
		{"3 invalid email", RegisterInput{"Ana", "bad", "123"}, model.ErrInvalidEmail},
		{"4 password too short", RegisterInput{"Ana", "ana@mail.com", "123"}, model.ErrPasswordTooShort},
		{"5 password too long", RegisterInput{"Ana", "ana@mail.com", strings.Repeat("a", 129)}, model.ErrPasswordTooLong},
		{"6 email already registered (other case)", RegisterInput{"Ana", " ANA@mail.com ", "S3gura2026"}, model.ErrEmailAlreadyRegistered},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.Register(ctx, tc.in); err != tc.want {
				t.Errorf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestRegisterNormalizesAndHashes(t *testing.T) {
	s, _ := newAuthService(t)
	user, err := s.Register(context.Background(),
		RegisterInput{Name: "  Ana Pérez ", Email: " Ana@Mail.com", Password: "S3gura2026"})
	if err != nil {
		t.Fatal(err)
	}
	if user.Name != "Ana Pérez" || user.Email != "ana@mail.com" || user.Role != model.RoleCustomer {
		t.Errorf("user = %+v", user)
	}
	if !strings.HasPrefix(user.PasswordHash, "$argon2id$") || strings.Contains(user.PasswordHash, "S3gura2026") {
		t.Errorf("password must be stored as argon2id hash, got %q", user.PasswordHash)
	}
}

func TestLogin(t *testing.T) {
	s, db := newAuthService(t)
	ctx := context.Background()
	user, _ := s.Register(ctx, RegisterInput{"Ana", "ana@mail.com", "S3gura2026"})

	pair, err := s.Login(ctx, LoginInput{Email: " ANA@mail.com ", Password: "S3gura2026"})
	if err != nil {
		t.Fatal(err)
	}
	if pair.ExpiresIn != 900 || pair.RefreshToken == "" {
		t.Errorf("pair = %+v", pair)
	}
	userID, role, err := s.tokens.Validate(pair.AccessToken)
	if err != nil || userID != user.ID || role != model.RoleCustomer {
		t.Errorf("access token: %s %s %v", userID, role, err)
	}

	// En la BD solo está el hash del refresh token.
	var stored string
	db.QueryRow(`SELECT token_hash FROM refresh_tokens WHERE user_id = $1`, user.ID).Scan(&stored)
	if stored != hashRefreshToken(pair.RefreshToken) {
		t.Errorf("stored %q, want the SHA-256 of the refresh token", stored)
	}
}

func TestLoginInvalidCredentials(t *testing.T) {
	s, _ := newAuthService(t)
	ctx := context.Background()
	s.Register(ctx, RegisterInput{"Ana", "ana@mail.com", "S3gura2026"})

	for _, in := range []LoginInput{
		{"ana@mail.com", "incorrecta"},   // contraseña incorrecta
		{"nadie@mail.com", "S3gura2026"}, // email inexistente: mismo error
	} {
		if _, err := s.Login(ctx, in); err != model.ErrInvalidCredentials {
			t.Errorf("%+v: error = %v, want ErrInvalidCredentials", in, err)
		}
	}
}

// RN-29: 5 fallidos → el 6.º se bloquea, aun con la contraseña correcta, sin sumar intentos.
func TestLoginRateLimit(t *testing.T) {
	s, db := newAuthService(t)
	ctx := context.Background()
	s.Register(ctx, RegisterInput{"Ana", "ana@mail.com", "S3gura2026"})

	for i := range 5 {
		if _, err := s.Login(ctx, LoginInput{"ana@mail.com", "incorrecta"}); err != model.ErrInvalidCredentials {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
	}

	_, err := s.Login(ctx, LoginInput{"ana@mail.com", "S3gura2026"})
	var blocked *model.LoginBlockedError
	if !errors.As(err, &blocked) || blocked.RetryAfter < 1 || blocked.RetryAfter > 900 {
		t.Fatalf("6th attempt: error = %v, want LoginBlockedError with 1..900 seconds", err)
	}

	var attempts int
	db.QueryRow(`SELECT count(*) FROM login_attempts`).Scan(&attempts)
	if attempts != 5 {
		t.Errorf("blocked attempts must not be recorded: %d rows, want 5", attempts)
	}
}

func TestLoginSuccessClearsAttempts(t *testing.T) {
	s, db := newAuthService(t)
	ctx := context.Background()
	s.Register(ctx, RegisterInput{"Ana", "ana@mail.com", "S3gura2026"})

	for range 4 {
		s.Login(ctx, LoginInput{"ana@mail.com", "incorrecta"})
	}
	if _, err := s.Login(ctx, LoginInput{"ana@mail.com", "S3gura2026"}); err != nil {
		t.Fatal(err)
	}
	var attempts int
	db.QueryRow(`SELECT count(*) FROM login_attempts`).Scan(&attempts)
	if attempts != 0 {
		t.Errorf("successful login must clear attempts: %d rows", attempts)
	}
}

// Con 10 logins fallidos simultáneos, el lock por email deja registrar exactamente 5.
func TestLoginRateLimitConcurrent(t *testing.T) {
	s, db := newAuthService(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Login(ctx, LoginInput{"nadie@mail.com", "incorrecta"})
		}()
	}
	wg.Wait()

	var attempts int
	db.QueryRow(`SELECT count(*) FROM login_attempts`).Scan(&attempts)
	if attempts != maxFailedLogins {
		t.Errorf("recorded %d attempts, want exactly %d", attempts, maxFailedLogins)
	}
}

// login registra a Ana e inicia sesión; devuelve su usuario y su par de tokens.
func login(t *testing.T, s *AuthService) (model.User, TokenPair) {
	t.Helper()
	ctx := context.Background()
	user, err := s.Register(ctx, RegisterInput{"Ana", "ana@mail.com", "S3gura2026"})
	if err != nil {
		t.Fatal(err)
	}
	pair, err := s.Login(ctx, LoginInput{"ana@mail.com", "S3gura2026"})
	if err != nil {
		t.Fatal(err)
	}
	return user, pair
}

// F-03: el refresh entrega un par nuevo y el token usado deja de servir (RN-26).
func TestRefresh(t *testing.T) {
	s, _ := newAuthService(t)
	ctx := context.Background()
	user, first := login(t, s)

	second, err := s.Refresh(ctx, first.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if second.RefreshToken == first.RefreshToken || second.ExpiresIn != 900 {
		t.Errorf("second pair = %+v", second)
	}
	if userID, role, err := s.tokens.Validate(second.AccessToken); err != nil || userID != user.ID || role != model.RoleCustomer {
		t.Errorf("new access token: %s %s %v", userID, role, err)
	}

	// Reusar el token anterior → 401; el nuevo sí funciona.
	if _, err := s.Refresh(ctx, first.RefreshToken); err != model.ErrInvalidRefreshToken {
		t.Errorf("reused token: error = %v, want ErrInvalidRefreshToken", err)
	}
	if _, err := s.Refresh(ctx, second.RefreshToken); err != nil {
		t.Errorf("new token: %v", err)
	}
}

func TestRefreshInvalidTokens(t *testing.T) {
	s, db := newAuthService(t)
	ctx := context.Background()
	_, pair := login(t, s)
	db.Exec(`UPDATE refresh_tokens SET expires_at = now() - interval '1 second'`)

	for name, token := range map[string]string{
		"expired": pair.RefreshToken,
		"unknown": "no-existe",
		"empty":   "",
		"no hash": hashRefreshToken(pair.RefreshToken), // el hash guardado no sirve como token
	} {
		if _, err := s.Refresh(ctx, token); err != model.ErrInvalidRefreshToken {
			t.Errorf("%s: error = %v, want ErrInvalidRefreshToken", name, err)
		}
	}
}

// T3.7: dos refresh simultáneos con el mismo token → exactamente uno gana. 10 rondas.
func TestRefreshConcurrent(t *testing.T) {
	s, _ := newAuthService(t)
	ctx := context.Background()
	_, pair := login(t, s)
	token := pair.RefreshToken

	for round := range 10 {
		results := make(chan TokenPair, 2)
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				p, err := s.Refresh(ctx, token)
				if err != nil {
					errs <- err
					return
				}
				results <- p
			}()
		}
		wg.Wait()
		close(results)
		close(errs)

		if len(results) != 1 || len(errs) != 1 {
			t.Fatalf("round %d: %d successes and %d errors, want 1 and 1", round+1, len(results), len(errs))
		}
		if err := <-errs; err != model.ErrInvalidRefreshToken {
			t.Fatalf("round %d: loser error = %v, want ErrInvalidRefreshToken", round+1, err)
		}
		token = (<-results).RefreshToken // la siguiente ronda usa el token ganador
	}
}

// F-04: logout revoca el token; solo el dueño puede hacerlo.
func TestLogout(t *testing.T) {
	s, _ := newAuthService(t)
	ctx := context.Background()
	ana, pair := login(t, s)
	eva := model.AuthUser{ID: "00000000-0000-4000-8000-000000000000", Role: model.RoleCustomer}
	anaAuth := model.AuthUser{ID: ana.ID, Role: ana.Role}

	if err := s.Logout(ctx, eva, pair.RefreshToken); err != model.ErrInvalidRefreshToken {
		t.Errorf("other user: error = %v, want ErrInvalidRefreshToken", err)
	}
	if err := s.Logout(ctx, anaAuth, pair.RefreshToken); err != nil {
		t.Fatalf("own token: %v", err)
	}
	if err := s.Logout(ctx, anaAuth, pair.RefreshToken); err != model.ErrInvalidRefreshToken {
		t.Errorf("second logout: error = %v, want ErrInvalidRefreshToken", err)
	}
	if _, err := s.Refresh(ctx, pair.RefreshToken); err != model.ErrInvalidRefreshToken {
		t.Errorf("refresh after logout: error = %v, want ErrInvalidRefreshToken", err)
	}
}
