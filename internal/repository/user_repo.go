// Tablas users, refresh_tokens y login_attempts.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"uuid"

	"github.com/jackc/pgx/v5/pgconn"

	"accounts-api/internal/model"
)

// Códigos de error de PostgreSQL que se traducen a errores del catálogo.
const pgUniqueViolation = "23505"

// loginLockNamespace separa los advisory locks del login de otros locks de la aplicación.
const loginLockNamespace = 1

// UserRepo agrupa el SQL de usuarios y sesiones. No tiene estado: la conexión o la
// transacción llega como parámetro (DBTX).
type UserRepo struct{}

// Create inserta el usuario y devuelve su fecha de creación (hora de la BD, I-10).
// Un email ya registrado devuelve ErrEmailAlreadyRegistered.
func (UserRepo) Create(ctx context.Context, db DBTX, u model.User) (model.User, error) {
	err := db.QueryRowContext(ctx,
		`INSERT INTO users (id, name, email, password_hash, role)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING created_at`,
		u.ID, u.Name, u.Email, u.PasswordHash, u.Role).
		Scan(&u.CreatedAt)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == "users_email_key" {
		return model.User{}, model.ErrEmailAlreadyRegistered
	}
	return u, err
}

// FindByEmail busca un usuario por email. Si no existe devuelve nil, sin error.
func (UserRepo) FindByEmail(ctx context.Context, db DBTX, email string) (*model.User, error) {
	var u model.User
	err := db.QueryRowContext(ctx,
		`SELECT id, name, email, password_hash, role, created_at
		   FROM users WHERE email = $1`, email).
		Scan(&u.ID, &u.Name, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// ---------------------------------------------------------------------------
// Intentos de login (RN-29)
// ---------------------------------------------------------------------------

// LockLoginEmail bloquea los logins de un email hasta que termine la transacción.
// Así dos intentos simultáneos del mismo email no superan el límite. Debe llamarse
// dentro de una transacción (*sql.Tx).
func (UserRepo) LockLoginEmail(ctx context.Context, tx DBTX, email string) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1, hashtext($2))`, loginLockNamespace, email)
	return err
}

// RecentFailedLogins cuenta los intentos fallidos del email en los últimos 15 minutos y
// calcula cuántos segundos faltan para que el más antiguo salga de la ventana.
func (UserRepo) RecentFailedLogins(ctx context.Context, db DBTX, email string) (count, retryAfter int, err error) {
	err = db.QueryRowContext(ctx,
		`SELECT count(*),
		        COALESCE(ceil(extract(epoch FROM min(created_at) + interval '15 minutes' - now())), 0)::int
		   FROM login_attempts
		  WHERE email = $1 AND created_at > now() - interval '15 minutes'`, email).
		Scan(&count, &retryAfter)
	return count, max(retryAfter, 1), err
}

// RecordFailedLogin registra un intento fallido del email.
func (UserRepo) RecordFailedLogin(ctx context.Context, db DBTX, email string) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO login_attempts (id, email) VALUES ($1, $2)`, uuid.NewV4().String(), email)
	return err
}

// ClearFailedLogins borra los intentos fallidos del email (login exitoso).
func (UserRepo) ClearFailedLogins(ctx context.Context, db DBTX, email string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM login_attempts WHERE email = $1`, email)
	return err
}

// ---------------------------------------------------------------------------
// Refresh tokens (RN-26)
// ---------------------------------------------------------------------------

// CreateRefreshToken guarda el hash de un refresh token que vence en 7 días (hora de la BD).
func (UserRepo) CreateRefreshToken(ctx context.Context, db DBTX, userID, tokenHash string) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at)
		 VALUES ($1, $2, $3, date_trunc('milliseconds', now()) + interval '7 days')`,
		uuid.NewV4().String(), userID, tokenHash)
	return err
}

// ActiveRefreshToken es un refresh token vigente, con el rol actual de su usuario.
type ActiveRefreshToken struct {
	ID     string
	UserID string
	Role   model.Role
}

// LockActiveRefreshToken busca por hash un refresh token no revocado ni vencido (hora de
// la BD) y bloquea su fila hasta que termine la transacción. Si no existe devuelve nil, sin error.
//
// Si dos transacciones piden el mismo token, la segunda espera el bloqueo; cuando la primera
// lo revoca y confirma, PostgreSQL vuelve a evaluar el WHERE y la segunda ya no lo encuentra.
// Debe llamarse dentro de una transacción (*sql.Tx).
func (UserRepo) LockActiveRefreshToken(ctx context.Context, tx DBTX, tokenHash string) (*ActiveRefreshToken, error) {
	var t ActiveRefreshToken
	err := tx.QueryRowContext(ctx,
		`SELECT rt.id, rt.user_id, u.role
		   FROM refresh_tokens rt
		   JOIN users u ON u.id = rt.user_id
		  WHERE rt.token_hash = $1 AND rt.revoked_at IS NULL AND rt.expires_at > now()
		    FOR UPDATE OF rt`, tokenHash).
		Scan(&t.ID, &t.UserID, &t.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// RevokeRefreshToken marca como revocado el refresh token con ese id.
func (UserRepo) RevokeRefreshToken(ctx context.Context, db DBTX, id string) error {
	_, err := db.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked_at = date_trunc('milliseconds', now()) WHERE id = $1`, id)
	return err
}

// RevokeUserRefreshToken revoca el refresh token si está vigente y es del usuario dado.
// Devuelve false si no había ninguno que cumpla las tres condiciones. Es una sola
// sentencia UPDATE, así que dos logouts simultáneos no pueden revocarlo los dos.
func (UserRepo) RevokeUserRefreshToken(ctx context.Context, db DBTX, userID, tokenHash string) (bool, error) {
	res, err := db.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked_at = date_trunc('milliseconds', now())
		  WHERE token_hash = $1 AND user_id = $2 AND revoked_at IS NULL AND expires_at > now()`,
		tokenHash, userID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
