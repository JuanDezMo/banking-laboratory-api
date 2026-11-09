package service

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"accounts-api/internal/model"
	"accounts-api/internal/testdb"
)

// newAccountFixture crea el service y dos customers (Ana y Eva) directamente con AuthService.
func newAccountFixture(t *testing.T) (s *AccountService, ana, eva model.AuthUser) {
	t.Helper()
	return newAccountFixtureDB(t, testdb.Open(t))
}

// newAccountFixtureDB es como newAccountFixture, sobre una BD ya abierta.
func newAccountFixtureDB(t *testing.T, db *sql.DB) (s *AccountService, ana, eva model.AuthUser) {
	t.Helper()
	auth := NewAuthService(db, NewPasswordHasher(4), NewTokenIssuer(testSecret))
	ctx := context.Background()
	a, err := auth.Register(ctx, RegisterInput{"Ana", "ana@mail.com", "S3gura2026"})
	if err != nil {
		t.Fatal(err)
	}
	e, err := auth.Register(ctx, RegisterInput{"Eva", "eva@mail.com", "S3gura2026"})
	if err != nil {
		t.Fatal(err)
	}
	return NewAccountService(db), model.AuthUser{ID: a.ID, Role: a.Role}, model.AuthUser{ID: e.ID, Role: e.Role}
}

func TestCreateAccount(t *testing.T) {
	s, ana, _ := newAccountFixture(t)
	ctx := context.Background()

	checking, err := s.Create(ctx, ana, "checking")
	if err != nil {
		t.Fatal(err)
	}
	if checking.OwnerID != ana.ID || checking.Kind != model.AccountChecking || checking.BalanceCents != 0 ||
		checking.OverdraftLimitCents != 500000 || len(checking.Number) != 10 {
		t.Errorf("checking = %+v", checking)
	}
	savings, _ := s.Create(ctx, ana, "savings")
	if savings.OverdraftLimitCents != 0 || savings.Number == checking.Number {
		t.Errorf("savings = %+v", savings)
	}

	for _, bad := range []string{"", "Savings", "credit"} {
		if _, err := s.Create(ctx, ana, bad); err != model.ErrInvalidAccountType {
			t.Errorf("%q: error = %v, want ErrInvalidAccountType", bad, err)
		}
	}
}

func TestListAccounts(t *testing.T) {
	s, ana, eva := newAccountFixture(t)
	ctx := context.Background()
	for range 3 {
		s.Create(ctx, ana, "savings")
	}
	s.Create(ctx, eva, "savings")

	got, err := s.List(ctx, ana)
	if err != nil || len(got) != 3 {
		t.Fatalf("got %d accounts, %v; want Ana's 3", len(got), err)
	}
	// I-11: createdAt ascendente; con empate (mismo milisegundo), id ascendente.
	for i := 1; i < len(got); i++ {
		prev, cur := got[i-1], got[i]
		if cur.OwnerID != ana.ID || cur.CreatedAt.Before(prev.CreatedAt) ||
			(cur.CreatedAt.Equal(prev.CreatedAt) && cur.ID < prev.ID) {
			t.Errorf("position %d out of I-11 order: %+v after %+v", i, cur, prev)
		}
	}
}

// F-07 en el orden del spec: id inválido (400), inexistente o ajena (404).
func TestGetAccount(t *testing.T) {
	s, ana, eva := newAccountFixture(t)
	ctx := context.Background()
	account, _ := s.Create(ctx, ana, "savings")
	operator := model.AuthUser{ID: "00000000-0000-4000-8000-000000000001", Role: model.RoleOperator}

	tests := []struct {
		name string
		user model.AuthUser
		id   string
		want error
	}{
		{"own account", ana, account.ID, nil},
		{"uppercase id is normalized (I-15)", ana, strings.ToUpper(account.ID), nil},
		{"operator sees any account", operator, account.ID, nil},
		{"invalid id", ana, "123", model.ErrInvalidAccountID},
		{"id without dashes", ana, strings.ReplaceAll(account.ID, "-", ""), model.ErrInvalidAccountID},
		{"missing account", ana, "00000000-0000-4000-8000-000000000000", model.ErrAccountNotFound},
		{"other customer's account (RN-24)", eva, account.ID, model.ErrAccountNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.Get(ctx, tc.user, tc.id)
			if err != tc.want {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if err == nil && got.ID != account.ID {
				t.Errorf("got account %s, want %s", got.ID, account.ID)
			}
		})
	}
}

// F-10 en el orden del spec: id (400), page (400), cuenta ajena o inexistente (404).
func TestAccountMovements(t *testing.T) {
	f := newTransferFixture(t, 100000) // savings de Ana con una recarga
	ctx := context.Background()
	accounts := NewAccountService(f.db)
	operator := f.operator
	f.transfer(f.ana, newKey(), TransferInput{f.anaSavings.Number, f.evaSavings.Number, 40000}, nil)

	page, err := accounts.Movements(ctx, f.ana, f.anaSavings.ID, "", false)
	if err != nil || page.Page != 1 || len(page.Items) != 2 ||
		page.Items[0].Kind != model.MovementTransferOut || page.Items[1].Kind != model.MovementRecharge {
		t.Errorf("own history: %+v, %v; want [transfer_out, recharge]", page, err)
	}
	if page, err := accounts.Movements(ctx, operator, f.anaSavings.ID, "1", true); err != nil || len(page.Items) != 2 {
		t.Errorf("operator: %+v, %v", page, err)
	}
	if page, err := accounts.Movements(ctx, f.ana, f.anaSavings.ID, "2", true); err != nil || page.Page != 2 || page.Items == nil || len(page.Items) != 0 {
		t.Errorf("page after the last: %+v, %v; want page 2 with empty items", page, err)
	}

	tests := []struct {
		name     string
		user     model.AuthUser
		id, page string
		present  bool
		want     error
	}{
		{"2 invalid id (and invalid page)", f.ana, "abc", "0", true, model.ErrInvalidAccountID},
		{"3 page 0 (and other's account)", f.eva, f.anaSavings.ID, "0", true, model.ErrInvalidPagination},
		{"3 empty page", f.ana, f.anaSavings.ID, "", true, model.ErrInvalidPagination},
		{"3 page +5", f.ana, f.anaSavings.ID, "+5", true, model.ErrInvalidPagination},
		{"3 page 10001", f.ana, f.anaSavings.ID, "10001", true, model.ErrInvalidPagination},
		{"4 other customer's account", f.eva, f.anaSavings.ID, "1", true, model.ErrAccountNotFound},
		{"4 missing account", f.ana, "00000000-0000-4000-8000-000000000000", "", false, model.ErrAccountNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := accounts.Movements(ctx, tc.user, tc.id, tc.page, tc.present); err != tc.want {
				t.Errorf("error = %v, want %v", err, tc.want)
			}
		})
	}
}
