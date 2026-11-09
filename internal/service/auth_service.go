// Registro, login, refresh y logout.
package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"uuid"

	"accounts-api/internal/model"
	"accounts-api/internal/repository"
)

// maxFailedLogins es el máximo de intentos fallidos por email en 15 minutos (RN-29).
const maxFailedLogins = 5

// AuthService implementa F-01 a F-04.
type AuthService struct {
	db        *sql.DB
	users     repository.UserRepo
	passwords *PasswordHasher
	tokens    *TokenIssuer
}

func NewAuthService(db *sql.DB, passwords *PasswordHasher, tokens *TokenIssuer) *AuthService {
	return &AuthService{db: db, passwords: passwords, tokens: tokens}
}

// RegisterInput son los datos del registro, tal como llegan en el cuerpo.
type RegisterInput struct {
	Name     string
	Email    string
	Password string
}

// TokenPair es la respuesta del login y del refresh.
type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int // segundos de vida del access token
}

// Register crea un customer (F-01). Las validaciones siguen el orden del spec 6:
// nombre, email, contraseña corta, contraseña larga y email ya registrado.
func (s *AuthService) Register(ctx context.Context, in RegisterInput) (model.User, error) {
	name, err := NormalizeName(in.Name)
	if err != nil {
		return model.User{}, err
	}
	email, err := NormalizeEmail(in.Email)
	if err != nil {
		return model.User{}, err
	}
	if err := ValidatePassword(in.Password); err != nil {
		return model.User{}, err
	}

	user := model.User{
		ID:           uuid.NewV4().String(),
		Name:         name,
		Email:        email,
		PasswordHash: s.passwords.Hash(in.Password),
		Role:         model.RoleCustomer, // el registro público solo crea customers (RN-27)
	}
	// Si el email ya existe, el UNIQUE de la BD lo detecta, incluso con registros simultáneos.
	return s.users.Create(ctx, s.db, user)
}

// LoginInput son los datos del login.
type LoginInput struct {
	Email    string
	Password string
}

// Login valida las credenciales y entrega un par de tokens (F-02).
// Todo ocurre en una transacción con un lock por email, para que el límite de RN-29
// se cumpla aunque lleguen varios intentos simultáneos.
func (s *AuthService) Login(ctx context.Context, in LoginInput) (TokenPair, error) {
	// El email se compara recortado y en minúsculas, aunque no tenga formato válido.
	email := strings.ToLower(strings.TrimSpace(in.Email))

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TokenPair{}, err
	}
	defer tx.Rollback() // no hace nada si ya se hizo Commit

	if err := s.users.LockLoginEmail(ctx, tx, email); err != nil {
		return TokenPair{}, err
	}

	// Paso 2: límite de intentos. Estos rechazos no suman intentos.
	failed, retryAfter, err := s.users.RecentFailedLogins(ctx, tx, email)
	if err != nil {
		return TokenPair{}, err
	}
	if failed >= maxFailedLogins {
		return TokenPair{}, &model.LoginBlockedError{RetryAfter: retryAfter}
	}

	// Paso 3: credenciales. El error no dice si falló el email o la contraseña.
	user, err := s.users.FindByEmail(ctx, tx, email)
	if err != nil {
		return TokenPair{}, err
	}
	valid := false
	if user == nil {
		s.passwords.VerifyDummy(in.Password) // mismo tiempo de respuesta que con un email real
	} else if valid, err = s.passwords.Verify(in.Password, user.PasswordHash); err != nil {
		return TokenPair{}, fmt.Errorf("verify password of user %s: %w", user.ID, err)
	}

	if !valid {
		if err := s.users.RecordFailedLogin(ctx, tx, email); err != nil {
			return TokenPair{}, err
		}
		// Se confirma la transacción para guardar el intento, y luego se responde el error.
		if err := tx.Commit(); err != nil {
			return TokenPair{}, err
		}
		return TokenPair{}, model.ErrInvalidCredentials
	}

	// Login exitoso: se reinicia el contador y se emiten los tokens.
	if err := s.users.ClearFailedLogins(ctx, tx, email); err != nil {
		return TokenPair{}, err
	}
	pair, err := s.issueTokens(ctx, tx, user.ID, user.Role)
	if err != nil {
		return TokenPair{}, err
	}
	return pair, tx.Commit()
}

// issueTokens firma un access token y guarda el hash de un refresh token nuevo.
func (s *AuthService) issueTokens(ctx context.Context, db repository.DBTX, userID string, role model.Role) (TokenPair, error) {
	access, err := s.tokens.Issue(userID, role)
	if err != nil {
		return TokenPair{}, fmt.Errorf("issue access token: %w", err)
	}
	refresh, refreshHash := newRefreshToken()
	if err := s.users.CreateRefreshToken(ctx, db, userID, refreshHash); err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int(AccessTokenTTL.Seconds()),
	}, nil
}

// Refresh entrega un par de tokens nuevo y revoca el refresh token recibido (F-03, RN-26).
// Revocar el viejo y guardar el nuevo ocurre en una transacción que bloquea la fila del
// token: si llegan dos peticiones con el mismo token, solo una lo renueva (spec 8).
func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (TokenPair, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TokenPair{}, err
	}
	defer tx.Rollback()

	// Paso 2: existe, no está revocado ni vencido.
	current, err := s.users.LockActiveRefreshToken(ctx, tx, hashRefreshToken(refreshToken))
	if err != nil {
		return TokenPair{}, err
	}
	if current == nil {
		return TokenPair{}, model.ErrInvalidRefreshToken
	}

	if err := s.users.RevokeRefreshToken(ctx, tx, current.ID); err != nil {
		return TokenPair{}, err
	}
	// El rol sale de la BD, no del access token anterior.
	pair, err := s.issueTokens(ctx, tx, current.UserID, current.Role)
	if err != nil {
		return TokenPair{}, err
	}
	return pair, tx.Commit()
}

// Logout revoca el refresh token del usuario autenticado (F-04). El token debe estar
// vigente y ser de ese usuario; si no, ErrInvalidRefreshToken.
func (s *AuthService) Logout(ctx context.Context, user model.AuthUser, refreshToken string) error {
	revoked, err := s.users.RevokeUserRefreshToken(ctx, s.db, user.ID, hashRefreshToken(refreshToken))
	if err != nil {
		return err
	}
	if !revoked {
		return model.ErrInvalidRefreshToken
	}
	return nil
}
