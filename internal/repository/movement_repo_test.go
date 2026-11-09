package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"
	"uuid"

	"accounts-api/internal/model"
	"accounts-api/internal/testdb"
)

// newAccountInDB crea un customer y una cuenta suya; devuelve la cuenta.
func newAccountInDB(t *testing.T, db *sql.DB, email string) model.Account {
	t.Helper()
	ctx := context.Background()
	owner, err := UserRepo{}.Create(ctx, db, newUser(email))
	if err != nil {
		t.Fatal(err)
	}
	a, err := AccountRepo{}.Create(ctx, db, newAccount(owner.ID, model.AccountSavings), RandomAccountNumber)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// T5.4: lo insertado se lee igual, en el orden de los ids pedidos.
func TestInsertAndFindMovements(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	repo := MovementRepo{}
	from := newAccountInDB(t, db, "ana@mail.com")
	to := newAccountInDB(t, db, "eva@mail.com")

	recharge, err := repo.Insert(ctx, db, model.Movement{
		ID: uuid.NewV4().String(), AccountID: from.ID, Kind: model.MovementRecharge,
		AmountCents: 150000, RechargeType: model.RechargeSalary,
	})
	if err != nil {
		t.Fatal(err)
	}
	if recharge.CreatedAt.IsZero() || recharge.CreatedAt.Nanosecond()%1_000_000 != 0 {
		t.Errorf("created_at %v should come from the db with millisecond precision", recharge.CreatedAt)
	}
	transferIn, err := repo.Insert(ctx, db, model.Movement{
		ID: uuid.NewV4().String(), AccountID: to.ID, Kind: model.MovementTransferIn,
		AmountCents: 500, RelatedAccountID: from.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := repo.FindByIDs(ctx, db, []string{transferIn.ID, recharge.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d movements, want 2", len(got))
	}
	for i, want := range []model.Movement{transferIn, recharge} {
		if !equalMovement(got[i], want) {
			t.Errorf("position %d: got %+v, want %+v", i, got[i], want)
		}
	}

	if _, err := repo.FindByIDs(ctx, db, []string{recharge.ID, uuid.NewV4().String()}); err == nil {
		t.Error("missing id: want an error")
	}

	// El CHECK de la BD: una recarga sin tipo se rechaza.
	_, err = repo.Insert(ctx, db, model.Movement{
		ID: uuid.NewV4().String(), AccountID: from.ID, Kind: model.MovementRecharge, AmountCents: 1,
	})
	if err == nil {
		t.Error("recharge without recharge_type: want a CHECK violation")
	}
}

// equalMovement compara todos los campos; las fechas, con Equal (la zona puede diferir).
func equalMovement(a, b model.Movement) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return false
	}
	a.CreatedAt, b.CreatedAt = time.Time{}, time.Time{}
	return a == b
}

func TestLockByNumberAndUpdateBalance(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	repo := AccountRepo{}
	account := newAccountInDB(t, db, "ana@mail.com")

	tx, _ := db.BeginTx(ctx, nil)
	defer tx.Rollback()
	locked, err := repo.LockByNumber(ctx, tx, account.Number)
	if err != nil || locked.ID != account.ID {
		t.Fatalf("locked = %+v, err = %v", locked, err)
	}
	locked.BalanceCents = 1000
	if err := repo.UpdateBalance(ctx, tx, locked); err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	found, _ := repo.FindByID(ctx, db, account.ID)
	if found.BalanceCents != 1000 {
		t.Errorf("balance = %d, want 1000", found.BalanceCents)
	}
	if _, err := repo.LockByNumber(ctx, db, "no-existe!"); err != model.ErrAccountNotFound {
		t.Errorf("missing number: error = %v, want ErrAccountNotFound", err)
	}

	// CHECK de la BD: un savings no puede quedar en negativo (última defensa de RN-04).
	found.BalanceCents = -1
	if err := repo.UpdateBalance(ctx, db, found); err == nil {
		t.Error("negative savings balance: want a CHECK violation")
	}
}
