package model

import "testing"

func TestParseAccountType(t *testing.T) {
	tests := []struct {
		value     string
		want      AccountType
		overdraft int64
		wantErr   bool
	}{
		{"savings", AccountSavings, 0, false},
		{"checking", AccountChecking, 500000, false},
		{"Savings", "", 0, true},
		{" checking", "", 0, true},
		{"credit", "", 0, true},
		{"", "", 0, true},
	}
	for _, tc := range tests {
		got, err := ParseAccountType(tc.value)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("ParseAccountType(%q) = %q, %v", tc.value, got, err)
			continue
		}
		if err != nil && err != ErrInvalidAccountType {
			t.Errorf("ParseAccountType(%q): error = %v, want ErrInvalidAccountType", tc.value, err)
		}
		if err == nil && got.OverdraftLimitCents() != tc.overdraft {
			t.Errorf("%q overdraft = %d, want %d", tc.value, got.OverdraftLimitCents(), tc.overdraft)
		}
	}
}

func TestDeposit(t *testing.T) {
	tests := []struct {
		name         string
		balance, add int64
		want         int64
		wantErr      error
	}{
		{"normal", 0, 150000, 150000, nil},
		{"from negative checking", -500000, 100, -499900, nil},
		{"exactly the maximum", MaxBalanceCents - 1000, 1000, MaxBalanceCents, nil},
		{"one cent over the maximum", MaxBalanceCents - 1000, 1001, MaxBalanceCents - 1000, ErrBalanceLimitExceeded},
		{"max balance plus max amount", MaxBalanceCents, MaxAmountCents, MaxBalanceCents, ErrBalanceLimitExceeded},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := Account{BalanceCents: tc.balance}
			err := a.Deposit(tc.add)
			if err != tc.wantErr || a.BalanceCents != tc.want {
				t.Errorf("balance = %d, err = %v; want %d, %v", a.BalanceCents, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestParseRechargeType(t *testing.T) {
	for _, valid := range []string{"cash", "employer_bonus", "salary"} {
		if got, err := ParseRechargeType(valid); err != nil || string(got) != valid {
			t.Errorf("%q: got %q, %v", valid, got, err)
		}
	}
	for _, invalid := range []string{"", "Cash", "gift", "employer-bonus", " salary"} {
		if _, err := ParseRechargeType(invalid); err != ErrInvalidRechargeType {
			t.Errorf("%q: error = %v, want ErrInvalidRechargeType", invalid, err)
		}
	}
}

// T6.1: sobregiro por tipo, en el límite exacto y con 1 centavo de más.
func TestWithdraw(t *testing.T) {
	tests := []struct {
		name          string
		kind          AccountType
		balance, take int64
		want          int64
		wantErr       error
	}{
		{"savings normal", AccountSavings, 1000, 400, 600, nil},
		{"savings to exactly zero", AccountSavings, 1000, 1000, 0, nil},
		{"savings one cent over", AccountSavings, 1000, 1001, 1000, ErrInsufficientFunds},
		{"savings empty", AccountSavings, 0, 1, 0, ErrInsufficientFunds},
		{"checking into overdraft", AccountChecking, 0, 300000, -300000, nil},
		{"checking to exactly -500000", AccountChecking, 1000, 501000, -500000, nil},
		{"checking one cent over the overdraft", AccountChecking, 1000, 501001, 1000, ErrInsufficientFunds},
		{"checking already at the limit", AccountChecking, -500000, 1, -500000, ErrInsufficientFunds},
		{"max amount from zero", AccountChecking, 0, MaxAmountCents, 0, ErrInsufficientFunds},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := Account{Kind: tc.kind, BalanceCents: tc.balance, OverdraftLimitCents: tc.kind.OverdraftLimitCents()}
			err := a.Withdraw(tc.take)
			if err != tc.wantErr || a.BalanceCents != tc.want {
				t.Errorf("balance = %d, err = %v; want %d, %v", a.BalanceCents, err, tc.want, tc.wantErr)
			}
		})
	}
}
