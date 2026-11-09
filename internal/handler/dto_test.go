package handler

import (
	"encoding/json"
	"testing"
	"time"

	"accounts-api/internal/model"
)

func TestFormatTime(t *testing.T) {
	bogota := time.FixedZone("-05:00", -5*60*60)
	tests := []struct {
		name  string
		input time.Time
		want  string
	}{
		{"other zone goes to UTC", time.Date(2026, 11, 9, 9, 30, 0, 123_000_000, bogota), "2026-11-09T14:30:00.123Z"},
		{"zero milliseconds keep 3 decimals", time.Date(2026, 11, 9, 14, 30, 0, 0, time.UTC), "2026-11-09T14:30:00.000Z"},
		{"microseconds are truncated", time.Date(2026, 11, 9, 14, 30, 0, 123_999_999, time.UTC), "2026-11-09T14:30:00.123Z"},
		{"day changes when converting", time.Date(2026, 11, 9, 22, 0, 0, 0, bogota), "2026-11-10T03:00:00.000Z"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatTime(tc.input); got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

// T7.5: recarga y transferencia serializan igual que los ejemplos de openapi.yaml.
func TestMovementResponseJSON(t *testing.T) {
	createdAt := time.Date(2026, 11, 9, 14, 30, 0, 0, time.UTC)
	tests := []struct {
		name string
		m    model.Movement
		want string
	}{
		{"recharge (RechargeMovement example)", model.Movement{
			ID: "a41e5c1d-0b2a-4f3e-9d8c-7b6a5f4e3d21", AccountID: "6f1c4e2a-8b1d-4c3e-9f7a-2d5b8c1e0a34",
			Kind: model.MovementRecharge, AmountCents: 150000, RechargeType: model.RechargeSalary, CreatedAt: createdAt,
		}, `{"id":"a41e5c1d-0b2a-4f3e-9d8c-7b6a5f4e3d21","accountId":"6f1c4e2a-8b1d-4c3e-9f7a-2d5b8c1e0a34",` +
			`"movementType":"recharge","amount":150000,"details":{"rechargeType":"salary"},"createdAt":"2026-11-09T14:30:00.000Z"}`},
		{"transfer_out (TransferResult example)", model.Movement{
			ID: "c7a01b2c-3d4e-4f5a-8b6c-7d8e9f0a1b2c", AccountID: "6f1c4e2a-8b1d-4c3e-9f7a-2d5b8c1e0a34",
			Kind: model.MovementTransferOut, AmountCents: 40000, RelatedAccountID: "b92d7e6f-5a4b-4c3d-8e2f-1a0b9c8d7e6f",
			CreatedAt: createdAt.Add(5 * time.Minute),
		}, `{"id":"c7a01b2c-3d4e-4f5a-8b6c-7d8e9f0a1b2c","accountId":"6f1c4e2a-8b1d-4c3e-9f7a-2d5b8c1e0a34",` +
			`"movementType":"transfer_out","amount":40000,"details":{"relatedAccountId":"b92d7e6f-5a4b-4c3d-8e2f-1a0b9c8d7e6f"},` +
			`"createdAt":"2026-11-09T14:35:00.000Z"}`},
		{"transfer_in", model.Movement{
			ID: "d8131a2b-3c4d-4e5f-9a6b-7c8d9e0f1a2b", AccountID: "b92d7e6f-5a4b-4c3d-8e2f-1a0b9c8d7e6f",
			Kind: model.MovementTransferIn, AmountCents: 40000, RelatedAccountID: "6f1c4e2a-8b1d-4c3e-9f7a-2d5b8c1e0a34",
			CreatedAt: createdAt.Add(5 * time.Minute),
		}, `{"id":"d8131a2b-3c4d-4e5f-9a6b-7c8d9e0f1a2b","accountId":"b92d7e6f-5a4b-4c3d-8e2f-1a0b9c8d7e6f",` +
			`"movementType":"transfer_in","amount":40000,"details":{"relatedAccountId":"6f1c4e2a-8b1d-4c3e-9f7a-2d5b8c1e0a34"},` +
			`"createdAt":"2026-11-09T14:35:00.000Z"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(newMovementResponse(tc.m))
			if err != nil || string(got) != tc.want {
				t.Errorf("got  %s\nwant %s (err %v)", got, tc.want, err)
			}
		})
	}
}
