package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"testing"
	"uuid"

	"accounts-api/internal/model"
	"accounts-api/internal/repository"
	"accounts-api/internal/testdb"
)

// rechargeFixture tiene un operator real (la key tiene FK a users) y una cuenta de Ana.
type rechargeFixture struct {
	db       *sql.DB
	s        *RechargeService
	operator model.AuthUser
	account  model.Account
}

func newRechargeFixture(t *testing.T) rechargeFixture {
	t.Helper()
	db := testdb.Open(t)
	ctx := context.Background()
	op, err := repository.UserRepo{}.Create(ctx, db, model.User{
		ID: uuid.NewV4().String(), Name: "Operator", Email: "op@bank.local",
		PasswordHash: "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA", Role: model.RoleOperator,
	})
	if err != nil {
		t.Fatal(err)
	}
	accounts, ana, _ := newAccountFixtureDB(t, db)
	account, err := accounts.Create(ctx, ana, "savings")
	if err != nil {
		t.Fatal(err)
	}
	return rechargeFixture{db: db, s: NewRechargeService(db),
		operator: model.AuthUser{ID: op.ID, Role: model.RoleOperator}, account: account}
}

func (f rechargeFixture) recharge(key string, in RechargeInput, bodyErr error) (model.Movement, bool, error) {
	return f.s.Recharge(context.Background(), f.operator, key, in, bodyErr)
}

func (f rechargeFixture) balance(t *testing.T) int64 {
	t.Helper()
	var balance int64
	f.db.QueryRow(`SELECT balance_cents FROM accounts WHERE id = $1`, f.account.ID).Scan(&balance)
	return balance
}

func (f rechargeFixture) keyStored(key string) bool {
	var n int
	f.db.QueryRow(`SELECT count(*) FROM idempotency_keys WHERE key = $1`, key).Scan(&n)
	return n == 1
}

func newKey() string { return uuid.NewV4().String() }

// T5.7: el hash usa los campos en orden fijo, no el texto del JSON.
func TestRechargeRequestHash(t *testing.T) {
	in := RechargeInput{AccountNumber: "0012345678", Amount: 150000, RechargeType: "salary"}
	sum := sha256.Sum256([]byte("0012345678|150000|salary"))
	if got := in.hash(nil); got != hex.EncodeToString(sum[:]) {
		t.Errorf("hash = %s, want sha256 of accountNumber|amount|rechargeType", got)
	}
	// El handler arma el mismo RechargeInput para {"amount":150000,"rechargeType":"salary","accountNumber":"0012345678"}
	// y para 150000.0: el orden de las claves y la forma del número no cambian el hash.
	if same := (RechargeInput{RechargeType: "salary", AccountNumber: "0012345678", Amount: 150000}); same.hash(nil) != in.hash(nil) {
		t.Error("same fields must give the same hash")
	}
	if other := (RechargeInput{AccountNumber: "0012345678", Amount: 150001, RechargeType: "salary"}); other.hash(nil) == in.hash(nil) {
		t.Error("different amount must give a different hash")
	}
	if got := in.hash(model.ErrInvalidRequestBody); got != "invalid" {
		t.Errorf("invalid body: hash = %q, want \"invalid\"", got)
	}
}

func TestRecharge(t *testing.T) {
	f := newRechargeFixture(t)
	key := newKey()
	in := RechargeInput{AccountNumber: f.account.Number, Amount: 150000, RechargeType: "salary"}

	m, replayed, err := f.recharge(key, in, nil)
	if err != nil || replayed {
		t.Fatalf("first: replayed = %v, err = %v", replayed, err)
	}
	if m.AccountID != f.account.ID || m.Kind != model.MovementRecharge || m.AmountCents != 150000 ||
		m.RechargeType != model.RechargeSalary || m.RelatedAccountID != "" || m.CreatedAt.IsZero() {
		t.Errorf("movement = %+v", m)
	}
	if b := f.balance(t); b != 150000 {
		t.Fatalf("balance = %d, want 150000", b)
	}

	// Criterio de salida de I5: repetir no cambia el saldo y devuelve el mismo movimiento.
	again, replayed, err := f.recharge(key, in, nil)
	if err != nil || !replayed || again.ID != m.ID || !again.CreatedAt.Equal(m.CreatedAt) {
		t.Errorf("replay: movement = %+v, replayed = %v, err = %v; want the same movement", again, replayed, err)
	}
	if b := f.balance(t); b != 150000 {
		t.Errorf("balance after replay = %d, want 150000", b)
	}

	// La key es por usuario: otro operator con la misma key hace su propia recarga.
	other := f
	op2, _ := repository.UserRepo{}.Create(context.Background(), f.db, model.User{
		ID: uuid.NewV4().String(), Name: "Op 2", Email: "op2@bank.local", PasswordHash: "x", Role: model.RoleOperator})
	other.operator = model.AuthUser{ID: op2.ID, Role: model.RoleOperator}
	if m2, replayed, err := other.recharge(key, in, nil); err != nil || replayed || m2.ID == m.ID {
		t.Errorf("other operator, same key: replayed = %v, err = %v", replayed, err)
	}
	if b := f.balance(t); b != 300000 {
		t.Errorf("balance = %d, want 300000", b)
	}
}

// T5.8: un caso por error de F-08 (sin topes diarios), en el orden del spec. Cada caso
// trae también los errores de los pasos siguientes: solo debe salir el primero.
func TestRechargeValidationOrder(t *testing.T) {
	f := newRechargeFixture(t)
	missing := "9999999999"
	f.db.Exec(`UPDATE accounts SET balance_cents = $1 WHERE id = $2`, model.MaxBalanceCents-100, f.account.ID)

	tests := []struct {
		name    string
		in      RechargeInput
		bodyErr error
		want    error
		stored  bool // los errores de los pasos 10 a 14 se guardan con la key
	}{
		{"5 invalid body", RechargeInput{"123", 0, "gift"}, model.ErrInvalidRequestBody, model.ErrInvalidRequestBody, false},
		{"6 invalid account number", RechargeInput{"123", 0, "gift"}, nil, model.ErrInvalidAccountNumber, false},
		{"6 number with spaces", RechargeInput{" 001234567", 1, "cash"}, nil, model.ErrInvalidAccountNumber, false},
		{"7 invalid recharge type", RechargeInput{missing, 0, "gift"}, nil, model.ErrInvalidRechargeType, false},
		{"8 amount zero", RechargeInput{missing, 0, "cash"}, nil, model.ErrInvalidAmount, false},
		{"8 negative amount", RechargeInput{missing, -5, "cash"}, nil, model.ErrInvalidAmount, false},
		{"9 amount too large", RechargeInput{missing, model.MaxAmountCents + 1, "cash"}, nil, model.ErrAmountTooLarge, false},
		{"10 cash limit", RechargeInput{missing, 300001, "cash"}, nil, model.ErrCashLimitExceeded, true},
		{"11 account not found", RechargeInput{missing, 101, "salary"}, nil, model.ErrAccountNotFound, true},
		{"14 balance limit", RechargeInput{f.account.Number, 101, "salary"}, nil, model.ErrBalanceLimitExceeded, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			key := newKey()
			_, replayed, err := f.recharge(key, tc.in, tc.bodyErr)
			if err != tc.want || replayed {
				t.Fatalf("error = %v, replayed = %v; want %v", err, replayed, tc.want)
			}
			if f.keyStored(key) != tc.stored {
				t.Errorf("key stored = %v, want %v", f.keyStored(key), tc.stored)
			}
		})
	}

	// Ninguno cambió el saldo ni creó movimientos.
	if b := f.balance(t); b != model.MaxBalanceCents-100 {
		t.Errorf("balance changed to %d", b)
	}
	var n int
	f.db.QueryRow(`SELECT count(*) FROM movements`).Scan(&n)
	if n != 0 {
		t.Errorf("%d movements created, want 0", n)
	}

	// Límite exacto: llegar justo al saldo máximo sí se permite.
	if _, _, err := f.recharge(newKey(), RechargeInput{f.account.Number, 100, "salary"}, nil); err != nil {
		t.Errorf("exactly the maximum balance: %v", err)
	}
}

// T5.10: misma key con otro monto → 422; key en curso → 409; cash > 300000 → 422.
func TestRechargeIdempotencyErrors(t *testing.T) {
	f := newRechargeFixture(t)
	ctx := context.Background()
	in := RechargeInput{AccountNumber: f.account.Number, Amount: 1000, RechargeType: "cash"}

	t.Run("same key, other request", func(t *testing.T) {
		key := newKey()
		if _, _, err := f.recharge(key, in, nil); err != nil {
			t.Fatal(err)
		}
		changed := in
		changed.Amount = 2000
		if _, replayed, err := f.recharge(key, changed, nil); err != model.ErrIdempotencyKeyReused || replayed {
			t.Errorf("other amount: error = %v, replayed = %v; want ErrIdempotencyKeyReused", err, replayed)
		}
		// Paso 4 antes que el 5: con cuerpo inválido también es 422, no 400.
		if _, _, err := f.recharge(key, RechargeInput{}, model.ErrInvalidRequestBody); err != model.ErrIdempotencyKeyReused {
			t.Errorf("invalid body: error = %v; want ErrIdempotencyKeyReused", err)
		}
		if b := f.balance(t); b != 1000 {
			t.Errorf("balance = %d, want 1000", b)
		}
	})

	t.Run("key in progress", func(t *testing.T) {
		key := newKey()
		// Otra transacción tiene el lock de la key, como una petición que aún no termina.
		tx, _ := f.db.BeginTx(ctx, nil)
		defer tx.Rollback()
		scope := repository.IdempotencyScope{UserID: f.operator.ID, Endpoint: repository.EndpointRecharge, Key: key}
		if ok, err := (repository.IdempotencyRepo{}).TryLock(ctx, tx, scope); !ok || err != nil {
			t.Fatalf("lock: %v %v", ok, err)
		}
		if _, _, err := f.recharge(key, in, nil); err != model.ErrIdempotencyInProgress {
			t.Errorf("error = %v, want ErrIdempotencyInProgress", err)
		}
		if _, _, err := f.recharge(key, in, model.ErrInvalidRequestBody); err != model.ErrIdempotencyInProgress {
			t.Errorf("invalid body: error = %v, want ErrIdempotencyInProgress (step 4 before 5)", err)
		}
		tx.Rollback()
		if _, _, err := f.recharge(key, in, nil); err != nil {
			t.Errorf("after the first finishes: %v", err)
		}
	})

	t.Run("cash limit is stored and replayed", func(t *testing.T) {
		key := newKey()
		cash := RechargeInput{AccountNumber: f.account.Number, Amount: 300001, RechargeType: "cash"}
		if _, _, err := f.recharge(key, cash, nil); err != model.ErrCashLimitExceeded {
			t.Fatalf("error = %v, want ErrCashLimitExceeded", err)
		}
		if _, replayed, err := f.recharge(key, cash, nil); err != model.ErrCashLimitExceeded || !replayed {
			t.Errorf("replay: error = %v, replayed = %v; want the stored error, replayed", err, replayed)
		}
		cash.Amount = 300000 // límite exacto, con otra key
		if _, _, err := f.recharge(newKey(), cash, nil); err != nil {
			t.Errorf("cash 300000: %v", err)
		}
	})

	t.Run("stored 404 is replayed even if the account appears later", func(t *testing.T) {
		key := newKey()
		number := "0000000042"
		req := RechargeInput{AccountNumber: number, Amount: 1, RechargeType: "salary"}
		if _, _, err := f.recharge(key, req, nil); err != model.ErrAccountNotFound {
			t.Fatalf("error = %v, want ErrAccountNotFound", err)
		}
		a := model.Account{ID: uuid.NewV4().String(), OwnerID: f.account.OwnerID, Kind: model.AccountSavings}
		if _, err := (repository.AccountRepo{}).Create(ctx, f.db, a, func() string { return number }); err != nil {
			t.Fatal(err)
		}
		if _, replayed, err := f.recharge(key, req, nil); err != model.ErrAccountNotFound || !replayed {
			t.Errorf("replay: error = %v, replayed = %v; want stored ErrAccountNotFound", err, replayed)
		}
	})
}
