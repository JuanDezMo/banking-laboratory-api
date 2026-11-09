package service

import (
	"context"
	"sync"
	"testing"

	"accounts-api/internal/model"
	"accounts-api/internal/repository"
)

// transferFixture: Ana tiene un savings (con fondos) y un checking (en 0); Eva, un savings.
type transferFixture struct {
	rechargeFixture
	transfers                           *TransferService
	ana, eva                            model.AuthUser
	anaSavings, anaChecking, evaSavings model.Account
}

func newTransferFixture(t *testing.T, anaFunds int64) transferFixture {
	t.Helper()
	ctx := context.Background()
	rf := newRechargeFixture(t) // operator, Ana, Eva y el savings de Ana
	evaUser, err := repository.UserRepo{}.FindByEmail(ctx, rf.db, "eva@mail.com")
	if err != nil || evaUser == nil {
		t.Fatalf("eva: %v", err)
	}
	f := transferFixture{
		rechargeFixture: rf,
		transfers:       NewTransferService(rf.db),
		ana:             model.AuthUser{ID: rf.account.OwnerID, Role: model.RoleCustomer},
		eva:             model.AuthUser{ID: evaUser.ID, Role: model.RoleCustomer},
		anaSavings:      rf.account,
	}
	accounts := NewAccountService(rf.db)
	f.anaChecking, _ = accounts.Create(ctx, f.ana, "checking")
	f.evaSavings, _ = accounts.Create(ctx, f.eva, "savings")
	if anaFunds > 0 {
		f.fund(t, f.anaSavings, anaFunds)
	}
	return f
}

// fund recarga la cuenta (salary, sin tope por recarga) para que la invariante RN-17 se cumpla.
func (f transferFixture) fund(t *testing.T, a model.Account, amount int64) {
	t.Helper()
	if _, _, err := f.recharge(newKey(), RechargeInput{a.Number, amount, "salary"}, nil); err != nil {
		t.Fatalf("fund %s: %v", a.Number, err)
	}
}

func (f transferFixture) transfer(user model.AuthUser, key string, in TransferInput, bodyErr error) (TransferResult, bool, error) {
	return f.transfers.Transfer(context.Background(), user, key, in, bodyErr)
}

func (f transferFixture) balanceOf(t *testing.T, a model.Account) int64 {
	t.Helper()
	var b int64
	if err := f.db.QueryRow(`SELECT balance_cents FROM accounts WHERE id = $1`, a.ID).Scan(&b); err != nil {
		t.Fatal(err)
	}
	return b
}

// invariantViolations cuenta las cuentas cuyo saldo no es la suma de sus movimientos firmados (RN-17).
func invariantViolations(t *testing.T, f transferFixture) int {
	t.Helper()
	var n int
	err := f.db.QueryRow(`
		SELECT count(*) FROM accounts a
		 WHERE a.balance_cents <> (
		       SELECT coalesce(sum(CASE WHEN m.kind IN ('recharge', 'transfer_in') THEN m.amount_cents
		                                ELSE -m.amount_cents END), 0)
		         FROM movements m WHERE m.account_id = a.id)`).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// T6.5: dos movimientos con el mismo monto; la repetición no mueve dinero otra vez.
func TestTransfer(t *testing.T) {
	f := newTransferFixture(t, 100000)
	key := newKey()
	in := TransferInput{AccountOrigin: f.anaSavings.Number, AccountDestination: f.evaSavings.Number, Amount: 40000}

	res, replayed, err := f.transfer(f.ana, key, in, nil)
	if err != nil || replayed {
		t.Fatalf("transfer: replayed = %v, err = %v", replayed, err)
	}
	out, inM := res.Out, res.In
	if out.Kind != model.MovementTransferOut || out.AccountID != f.anaSavings.ID || out.RelatedAccountID != f.evaSavings.ID ||
		inM.Kind != model.MovementTransferIn || inM.AccountID != f.evaSavings.ID || inM.RelatedAccountID != f.anaSavings.ID ||
		out.AmountCents != 40000 || inM.AmountCents != 40000 || out.RechargeType != "" {
		t.Errorf("movements: out = %+v, in = %+v", out, inM)
	}
	if a, e := f.balanceOf(t, f.anaSavings), f.balanceOf(t, f.evaSavings); a != 60000 || e != 40000 {
		t.Errorf("balances: ana = %d, eva = %d; want 60000, 40000", a, e)
	}

	again, replayed, err := f.transfer(f.ana, key, in, nil)
	if err != nil || !replayed || again.Out.ID != out.ID || again.In.ID != inM.ID {
		t.Errorf("replay: %+v, replayed = %v, err = %v; want the same movements", again, replayed, err)
	}
	if a := f.balanceOf(t, f.anaSavings); a != 60000 {
		t.Errorf("balance after replay = %d, want 60000", a)
	}

	// Checking: puede quedar en -500000 justo (RN-05).
	if _, _, err := f.transfer(f.ana, newKey(), TransferInput{f.anaChecking.Number, f.evaSavings.Number, 500000}, nil); err != nil {
		t.Errorf("checking to exactly -500000: %v", err)
	}
	if b := f.balanceOf(t, f.anaChecking); b != -500000 {
		t.Errorf("checking balance = %d, want -500000", b)
	}
	if n := invariantViolations(t, f); n != 0 {
		t.Errorf("%d accounts break RN-17", n)
	}
}

// T6.4: un caso por error de F-09, en el orden del spec. Cada caso trae además los errores
// de los pasos siguientes: solo debe salir el primero.
func TestTransferValidationOrder(t *testing.T) {
	f := newTransferFixture(t, 100000)
	missing := "9999999999"
	ana, eva := f.anaSavings.Number, f.evaSavings.Number
	// Una cuenta de Eva casi en el saldo máximo, para el paso 13.
	full, _ := NewAccountService(f.db).Create(context.Background(), f.eva, "savings")
	f.db.Exec(`UPDATE accounts SET balance_cents = $1 WHERE id = $2`, model.MaxBalanceCents-10, full.ID)

	tests := []struct {
		name    string
		in      TransferInput
		bodyErr error
		want    error
		stored  bool // los errores de los pasos 10 a 13 se guardan con la key
	}{
		{"5 invalid body", TransferInput{"1", "1", 0}, model.ErrInvalidRequestBody, model.ErrInvalidRequestBody, false},
		{"6 invalid origin", TransferInput{"123", "1", 0}, nil, model.ErrInvalidAccountNumber, false},
		{"6 invalid destination", TransferInput{ana, "12345678901", 0}, nil, model.ErrInvalidAccountNumber, false},
		{"7 amount zero", TransferInput{ana, ana, 0}, nil, model.ErrInvalidAmount, false},
		{"8 amount too large", TransferInput{ana, ana, model.MaxAmountCents + 1}, nil, model.ErrAmountTooLarge, false},
		{"9 same account", TransferInput{ana, ana, 1}, nil, model.ErrSameAccount, false},
		{"10 origin not found", TransferInput{missing, "8888888888", 1}, nil, model.ErrAccountNotFound, true},
		{"10 origin of another customer (RN-23)", TransferInput{eva, missing, 1}, nil, model.ErrAccountNotFound, true},
		{"11 destination not found", TransferInput{ana, missing, 200000}, nil, model.ErrAccountNotFound, true},
		{"12 insufficient funds (savings)", TransferInput{ana, full.Number, 100001}, nil, model.ErrInsufficientFunds, true},
		{"12 insufficient funds (checking overdraft)", TransferInput{f.anaChecking.Number, eva, 500001}, nil, model.ErrInsufficientFunds, true},
		{"13 destination balance limit", TransferInput{ana, full.Number, 11}, nil, model.ErrBalanceLimitExceeded, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			key := newKey()
			_, replayed, err := f.transfer(f.ana, key, tc.in, tc.bodyErr)
			if err != tc.want || replayed {
				t.Fatalf("error = %v, replayed = %v; want %v", err, replayed, tc.want)
			}
			if f.keyStored(key) != tc.stored {
				t.Errorf("key stored = %v, want %v", f.keyStored(key), tc.stored)
			}
		})
	}

	// Ninguno cambió saldos ni creó movimientos de transferencia (RN-14).
	if b := f.balanceOf(t, f.anaSavings); b != 100000 {
		t.Errorf("origin balance = %d, want 100000", b)
	}
	var n int
	f.db.QueryRow(`SELECT count(*) FROM movements WHERE kind <> 'recharge'`).Scan(&n)
	if n != 0 {
		t.Errorf("%d transfer movements created, want 0", n)
	}
}

// T6.7 y T6.8: transferencias cruzadas A→B y B→A en paralelo. Con el bloqueo por id
// ascendente no hay deadlock, todas se aplican y la invariante RN-17 se mantiene.
func TestTransferConcurrency(t *testing.T) {
	f := newTransferFixture(t, 1_000_000)
	f.fund(t, f.evaSavings, 1_000_000)
	const perDirection = 20

	var wg sync.WaitGroup
	errs := make(chan error, 2*perDirection)
	send := func(user model.AuthUser, from, to model.Account) {
		defer wg.Done()
		_, _, err := f.transfer(user, newKey(), TransferInput{from.Number, to.Number, 100}, nil)
		if err != nil {
			errs <- err
		}
	}
	for range perDirection {
		wg.Add(2)
		go send(f.ana, f.anaSavings, f.evaSavings)
		go send(f.eva, f.evaSavings, f.anaSavings)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("transfer failed (deadlock?): %v", err)
	}

	// Mismo número de transferencias en cada sentido: los saldos vuelven a ser los iniciales.
	if a, e := f.balanceOf(t, f.anaSavings), f.balanceOf(t, f.evaSavings); a != 1_000_000 || e != 1_000_000 {
		t.Errorf("balances: ana = %d, eva = %d; want 1000000 each", a, e)
	}
	var n int
	f.db.QueryRow(`SELECT count(*) FROM movements WHERE kind IN ('transfer_in', 'transfer_out')`).Scan(&n)
	if n != 4*perDirection {
		t.Errorf("%d transfer movements, want %d", n, 4*perDirection)
	}
	if v := invariantViolations(t, f); v != 0 {
		t.Errorf("%d accounts break RN-17", v)
	}
}

// Con fondos para 10 transferencias y 20 simultáneas, se aplican exactamente 10:
// el FOR UPDATE impide gastar el mismo saldo dos veces.
func TestTransferConcurrencyNoDoubleSpend(t *testing.T) {
	f := newTransferFixture(t, 1000)
	var wg sync.WaitGroup
	results := make(chan error, 20)
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := f.transfer(f.ana, newKey(), TransferInput{f.anaSavings.Number, f.evaSavings.Number, 100}, nil)
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	ok, insufficient := 0, 0
	for err := range results {
		switch err {
		case nil:
			ok++
		case model.ErrInsufficientFunds:
			insufficient++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if ok != 10 || insufficient != 10 || f.balanceOf(t, f.anaSavings) != 0 {
		t.Errorf("ok = %d, insufficient = %d, balance = %d; want 10, 10, 0", ok, insufficient, f.balanceOf(t, f.anaSavings))
	}
	if v := invariantViolations(t, f); v != 0 {
		t.Errorf("%d accounts break RN-17", v)
	}
}
