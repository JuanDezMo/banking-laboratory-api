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
