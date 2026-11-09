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
