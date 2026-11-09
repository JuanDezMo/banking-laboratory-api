package repository

import (
	"context"
	"testing"
	"uuid"

	"accounts-api/internal/model"
	"accounts-api/internal/testdb"
)

func newUser(email string) model.User {
	return model.User{
		ID:           uuid.NewV4().String(),
		Name:         "Ana Pérez",
		Email:        email,
		PasswordHash: "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA",
		Role:         model.RoleCustomer,
	}
}

func TestCreateUser(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	repo := UserRepo{}

	created, err := repo.Create(ctx, db, newUser("ana@mail.com"))
	if err != nil {
		t.Fatal(err)
	}
	if created.CreatedAt.IsZero() || created.CreatedAt.Nanosecond()%1_000_000 != 0 {
		t.Errorf("created_at %v should come from the db with millisecond precision", created.CreatedAt)
	}

	// Mismo email, otro id → ErrEmailAlreadyRegistered.
	if _, err := repo.Create(ctx, db, newUser("ana@mail.com")); err != model.ErrEmailAlreadyRegistered {
		t.Errorf("duplicate email: error = %v, want ErrEmailAlreadyRegistered", err)
	}
}

func TestFindByEmail(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	repo := UserRepo{}
	created, _ := repo.Create(ctx, db, newUser("ana@mail.com"))

	found, err := repo.FindByEmail(ctx, db, "ana@mail.com")
	if err != nil || found == nil || found.ID != created.ID || found.Role != model.RoleCustomer {
		t.Errorf("found = %+v, err = %v", found, err)
	}

	missing, err := repo.FindByEmail(ctx, db, "nadie@mail.com")
	if err != nil || missing != nil {
		t.Errorf("missing user: got %+v, %v; want nil, nil", missing, err)
	}
}

func TestFailedLogins(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	repo := UserRepo{}

	for range 5 {
		if err := repo.RecordFailedLogin(ctx, db, "ana@mail.com"); err != nil {
			t.Fatal(err)
		}
	}
	repo.RecordFailedLogin(ctx, db, "otro@mail.com") // otro email no cuenta

	count, retryAfter, err := repo.RecentFailedLogins(ctx, db, "ana@mail.com")
	if err != nil || count != 5 {
		t.Fatalf("count = %d, err = %v; want 5", count, err)
	}
	if retryAfter < 899 || retryAfter > 900 {
		t.Errorf("retryAfter = %d, want about 900 seconds", retryAfter)
	}

	// Un intento de hace 16 minutos ya salió de la ventana.
	db.ExecContext(ctx, `UPDATE login_attempts SET created_at = now() - interval '16 minutes'
	                      WHERE id = (SELECT id FROM login_attempts WHERE email = 'ana@mail.com' LIMIT 1)`)
	if count, _, _ := repo.RecentFailedLogins(ctx, db, "ana@mail.com"); count != 4 {
		t.Errorf("after moving one attempt out of the window: count = %d, want 4", count)
	}

	if err := repo.ClearFailedLogins(ctx, db, "ana@mail.com"); err != nil {
		t.Fatal(err)
	}
	if count, _, _ := repo.RecentFailedLogins(ctx, db, "ana@mail.com"); count != 0 {
		t.Errorf("after clear: count = %d, want 0", count)
	}
}

func TestCreateRefreshToken(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	repo := UserRepo{}
	user, _ := repo.Create(ctx, db, newUser("ana@mail.com"))

	if err := repo.CreateRefreshToken(ctx, db, user.ID, "hash-1"); err != nil {
		t.Fatal(err)
	}
	var days float64
	db.QueryRowContext(ctx,
		`SELECT extract(epoch FROM expires_at - created_at) / 86400 FROM refresh_tokens WHERE token_hash = 'hash-1'`).
		Scan(&days)
	if days < 6.99 || days > 7.01 {
		t.Errorf("refresh token lasts %.3f days, want 7", days)
	}
}

// T3.1: un token vigente se encuentra; revocado o vencido, no.
func TestLockActiveRefreshToken(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	repo := UserRepo{}
	user, _ := repo.Create(ctx, db, newUser("ana@mail.com"))
	repo.CreateRefreshToken(ctx, db, user.ID, "hash-1")

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	found, err := repo.LockActiveRefreshToken(ctx, tx, "hash-1")
	if err != nil || found == nil || found.UserID != user.ID || found.Role != model.RoleCustomer {
		t.Fatalf("active token: got %+v, %v", found, err)
	}
	if err := repo.RevokeRefreshToken(ctx, tx, found.ID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// Revocado: ya no es válido.
	if again, err := repo.LockActiveRefreshToken(ctx, db, "hash-1"); err != nil || again != nil {
		t.Errorf("revoked token: got %+v, %v; want nil, nil", again, err)
	}

	// Inexistente y vencido: tampoco.
	if missing, err := repo.LockActiveRefreshToken(ctx, db, "no-existe"); err != nil || missing != nil {
		t.Errorf("missing token: got %+v, %v; want nil, nil", missing, err)
	}
	repo.CreateRefreshToken(ctx, db, user.ID, "hash-expired")
	db.ExecContext(ctx, `UPDATE refresh_tokens SET expires_at = now() - interval '1 second' WHERE token_hash = 'hash-expired'`)
	if expired, err := repo.LockActiveRefreshToken(ctx, db, "hash-expired"); err != nil || expired != nil {
		t.Errorf("expired token: got %+v, %v; want nil, nil", expired, err)
	}
}

func TestRevokeUserRefreshToken(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	repo := UserRepo{}
	ana, _ := repo.Create(ctx, db, newUser("ana@mail.com"))
	eva, _ := repo.Create(ctx, db, newUser("eva@mail.com"))
	repo.CreateRefreshToken(ctx, db, ana.ID, "hash-ana")

	if ok, err := repo.RevokeUserRefreshToken(ctx, db, eva.ID, "hash-ana"); err != nil || ok {
		t.Errorf("other user's token: ok = %v, err = %v; want false", ok, err)
	}
	if ok, err := repo.RevokeUserRefreshToken(ctx, db, ana.ID, "hash-ana"); err != nil || !ok {
		t.Errorf("own token: ok = %v, err = %v; want true", ok, err)
	}
	if ok, err := repo.RevokeUserRefreshToken(ctx, db, ana.ID, "hash-ana"); err != nil || ok {
		t.Errorf("already revoked: ok = %v, err = %v; want false", ok, err)
	}
}
