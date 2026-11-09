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

// T7.1: 2 recargas hoy + 1 de ayer (UTC) → cuenta 2. Las transferencias no cuentan.
func TestDailyRecharges(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	repo := MovementRepo{}
	account := newAccountInDB(t, db, "ana@mail.com")
	other := newAccountInDB(t, db, "eva@mail.com")

	insert := func(kind model.MovementType, amount int64) model.Movement {
		m := model.Movement{ID: uuid.NewV4().String(), AccountID: account.ID, Kind: kind, AmountCents: amount}
		if kind == model.MovementRecharge {
			m.RechargeType = model.RechargeCash
		} else {
			m.RelatedAccountID = other.ID
		}
		m, err := repo.Insert(ctx, db, m)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	insert(model.MovementRecharge, 1000)
	insert(model.MovementRecharge, 2000)
	insert(model.MovementTransferIn, 50000)
	yesterday := insert(model.MovementRecharge, 4000)
	// Justo antes de la medianoche UTC de hoy: es de ayer.
	db.ExecContext(ctx, `UPDATE movements SET created_at = date_trunc('day', now() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' - interval '1 millisecond' WHERE id = $1`, yesterday.ID)

	count, total, err := repo.DailyRecharges(ctx, db, account.ID)
	if err != nil || count != 2 || total != 3000 {
		t.Errorf("count = %d, total = %d, err = %v; want 2, 3000", count, total, err)
	}
	if count, total, _ := repo.DailyRecharges(ctx, db, other.ID); count != 0 || total != 0 {
		t.Errorf("other account: count = %d, total = %d; want 0, 0", count, total)
	}
}

// T7.4: con 25 movimientos, página 1 = 20, página 2 = 5, página 3 = 0; orden I-11.
func TestListMovementPage(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	repo := MovementRepo{}
	account := newAccountInDB(t, db, "ana@mail.com")
	for range 25 {
		_, err := repo.Insert(ctx, db, model.Movement{ID: uuid.NewV4().String(), AccountID: account.ID,
			Kind: model.MovementRecharge, AmountCents: 1, RechargeType: model.RechargeCash})
		if err != nil {
			t.Fatal(err)
		}
	}
	// Fechas repetidas de a pares, para probar el desempate por id.
	db.ExecContext(ctx, `UPDATE movements SET created_at = '2026-01-01T00:00:00Z'::timestamptz +
		(SELECT count(*) / 2 FROM movements m2 WHERE m2.id < movements.id) * interval '1 second'`)

	var all []model.Movement
	for page, want := range []int{20, 5, 0} {
		got, err := repo.ListPage(ctx, db, account.ID, page+1)
		if err != nil || len(got) != want || got == nil {
			t.Errorf("page %d: %d movements, err = %v; want %d (non-nil)", page+1, len(got), err, want)
		}
		all = append(all, got...)
	}
	distinct := map[string]bool{}
	for _, m := range all {
		distinct[m.ID] = true
	}
	if len(distinct) != 25 {
		t.Fatalf("pages 1 and 2 together have %d distinct movements, want 25", len(distinct))
	}
	for i := 1; i < len(all); i++ {
		prev, cur := all[i-1], all[i]
		if cur.CreatedAt.After(prev.CreatedAt) || (cur.CreatedAt.Equal(prev.CreatedAt) && cur.ID > prev.ID) {
			t.Errorf("position %d out of I-11 order: %s %s after %s %s", i, cur.CreatedAt, cur.ID, prev.CreatedAt, prev.ID)
		}
	}
}
