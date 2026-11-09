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
