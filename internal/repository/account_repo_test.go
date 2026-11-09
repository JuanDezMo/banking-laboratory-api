package repository

import (
	"context"
	"regexp"
	"testing"
	"uuid"

	"accounts-api/internal/model"
	"accounts-api/internal/testdb"
)

func newAccount(ownerID string, kind model.AccountType) model.Account {
	return model.Account{
		ID:                  uuid.NewV4().String(),
		OwnerID:             ownerID,
		Kind:                kind,
		OverdraftLimitCents: kind.OverdraftLimitCents(),
	}
}

// fixedNumbers devuelve un generador que entrega los números dados en orden y cuenta las llamadas.
func fixedNumbers(numbers ...string) (gen func() string, calls *int) {
	calls = new(int)
	return func() string {
		n := numbers[min(*calls, len(numbers)-1)]
		*calls++
		return n
	}, calls
}

func TestRandomAccountNumber(t *testing.T) {
	tenDigits := regexp.MustCompile(`^[0-9]{10}$`)
	seen := map[string]bool{}
	for range 100 {
		n := RandomAccountNumber()
		if !tenDigits.MatchString(n) {
			t.Fatalf("%q is not 10 digits", n)
		}
		seen[n] = true
	}
	if len(seen) < 99 {
		t.Errorf("only %d distinct numbers in 100", len(seen))
	}
}

// T4.1: con un número repetido, Create reintenta; con 5 colisiones seguidas, falla.
func TestCreateAccountRetriesOnDuplicateNumber(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	repo := AccountRepo{}
	owner, _ := UserRepo{}.Create(ctx, db, newUser("ana@mail.com"))

	gen, _ := fixedNumbers("0000000001")
	first, err := repo.Create(ctx, db, newAccount(owner.ID, model.AccountChecking), gen)
	if err != nil {
		t.Fatal(err)
	}
	if first.Number != "0000000001" || first.BalanceCents != 0 || first.OverdraftLimitCents != 500000 {
		t.Errorf("first = %+v", first)
	}
	if first.CreatedAt.IsZero() || first.CreatedAt.Nanosecond()%1_000_000 != 0 {
		t.Errorf("created_at %v should come from the db with millisecond precision", first.CreatedAt)
	}

	// Dos colisiones y luego un número libre: se crea en el 3.er intento.
	gen, calls := fixedNumbers("0000000001", "0000000001", "0000000002")
	second, err := repo.Create(ctx, db, newAccount(owner.ID, model.AccountSavings), gen)
	if err != nil || second.Number != "0000000002" || *calls != 3 {
		t.Errorf("second = %+v, err = %v, calls = %d; want number 0000000002 after 3 calls", second, err, *calls)
	}

	// Siempre el mismo número: 5 intentos y error.
	gen, calls = fixedNumbers("0000000001")
	if _, err := repo.Create(ctx, db, newAccount(owner.ID, model.AccountSavings), gen); err == nil || *calls != 5 {
		t.Errorf("err = %v, calls = %d; want an error after 5 calls", err, *calls)
	}
}

func TestFindAccountByID(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	repo := AccountRepo{}
	owner, _ := UserRepo{}.Create(ctx, db, newUser("ana@mail.com"))
	created, _ := repo.Create(ctx, db, newAccount(owner.ID, model.AccountSavings), RandomAccountNumber)

	found, err := repo.FindByID(ctx, db, created.ID)
	if err != nil || found.ID != created.ID || found.OwnerID != owner.ID || found.Number != created.Number ||
		found.Kind != model.AccountSavings || !found.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("found = %+v, err = %v", found, err)
	}

	if _, err := repo.FindByID(ctx, db, uuid.NewV4().String()); err != model.ErrAccountNotFound {
		t.Errorf("missing account: error = %v, want ErrAccountNotFound", err)
	}
}

// T4.3: orden I-11 (created_at ascendente, luego id ascendente) y solo las del dueño.
func TestListAccountsByOwner(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	repo := AccountRepo{}
	ana, _ := UserRepo{}.Create(ctx, db, newUser("ana@mail.com"))
	eva, _ := UserRepo{}.Create(ctx, db, newUser("eva@mail.com"))

	// Tres cuentas de Ana: las dos primeras con la misma fecha (empate → por id).
	ids := []string{
		"cccccccc-0000-4000-8000-000000000000",
		"aaaaaaaa-0000-4000-8000-000000000000",
		"bbbbbbbb-0000-4000-8000-000000000000",
	}
	for _, id := range ids {
		a := newAccount(ana.ID, model.AccountSavings)
		a.ID = id
		if _, err := repo.Create(ctx, db, a, RandomAccountNumber); err != nil {
			t.Fatal(err)
		}
	}
	db.ExecContext(ctx, `UPDATE accounts SET created_at = '2026-01-01T00:00:00Z' WHERE id IN ($1, $2)`, ids[0], ids[2])
	repo.Create(ctx, db, newAccount(eva.ID, model.AccountSavings), RandomAccountNumber)

	got, err := repo.ListByOwner(ctx, db, ana.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{ids[2], ids[0], ids[1]} // bbbb y cccc (empate, por id), luego aaaa (más nueva)
	if len(got) != len(want) {
		t.Fatalf("got %d accounts, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Errorf("position %d: id %s, want %s", i, got[i].ID, want[i])
		}
	}

	none, err := repo.ListByOwner(ctx, db, uuid.NewV4().String())
	if err != nil || none == nil || len(none) != 0 {
		t.Errorf("no accounts: got %#v, %v; want empty non-nil slice", none, err)
	}
}
