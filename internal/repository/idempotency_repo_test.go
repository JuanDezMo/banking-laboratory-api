package repository

import (
	"context"
	"testing"
	"uuid"

	"accounts-api/internal/testdb"
)

func newScope(t *testing.T, userID string) IdempotencyScope {
	t.Helper()
	return IdempotencyScope{UserID: userID, Endpoint: EndpointRecharge, Key: uuid.NewV4().String()}
}

// T5.5: lo guardado se encuentra; con más de 24 horas, no (y se puede volver a guardar).
func TestIdempotencyFindAndSave(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	repo := IdempotencyRepo{}
	user, _ := UserRepo{}.Create(ctx, db, newUser("op@mail.com"))
	movementID := uuid.NewV4().String() // movement_ids no tiene FK: basta un uuid

	success := newScope(t, user.ID)
	if err := repo.Save(ctx, db, success, StoredResult{RequestHash: "h1", MovementIDs: []string{movementID}}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Find(ctx, db, success)
	if err != nil || got == nil || got.RequestHash != "h1" || len(got.MovementIDs) != 1 ||
		got.MovementIDs[0] != movementID || got.ErrorCode != "" {
		t.Errorf("success: got %+v, %v", got, err)
	}

	failure := newScope(t, user.ID)
	repo.Save(ctx, db, failure, StoredResult{RequestHash: "h2", ErrorCode: "NOT_FOUND"})
	got, err = repo.Find(ctx, db, failure)
	if err != nil || got == nil || got.ErrorCode != "NOT_FOUND" || len(got.MovementIDs) != 0 {
		t.Errorf("error result: got %+v, %v", got, err)
	}

	// Misma key en otro endpoint: es otra key.
	other := success
	other.Endpoint = EndpointTransfer
	if got, err := repo.Find(ctx, db, other); err != nil || got != nil {
		t.Errorf("other endpoint: got %+v, %v; want nil", got, err)
	}

	// Hace 25 horas: no se encuentra, y Save la reemplaza.
	db.ExecContext(ctx, `UPDATE idempotency_keys SET created_at = now() - interval '25 hours' WHERE key = $1`, success.Key)
	if got, err := repo.Find(ctx, db, success); err != nil || got != nil {
		t.Errorf("25 hours old: got %+v, %v; want nil", got, err)
	}
	if err := repo.Save(ctx, db, success, StoredResult{RequestHash: "h3", ErrorCode: "NOT_FOUND"}); err != nil {
		t.Fatalf("save over an expired row: %v", err)
	}
	if got, _ := repo.Find(ctx, db, success); got == nil || got.RequestHash != "h3" {
		t.Errorf("after replacing expired row: got %+v", got)
	}
}

// T5.6: dos transacciones con la misma key → la segunda no obtiene el lock.
func TestIdempotencyTryLock(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	repo := IdempotencyRepo{}
	scope := newScope(t, uuid.NewV4().String())

	first, _ := db.BeginTx(ctx, nil)
	defer first.Rollback()
	second, _ := db.BeginTx(ctx, nil)
	defer second.Rollback()

	if ok, err := repo.TryLock(ctx, first, scope); err != nil || !ok {
		t.Fatalf("first: ok = %v, err = %v; want true", ok, err)
	}
	if ok, err := repo.TryLock(ctx, second, scope); err != nil || ok {
		t.Errorf("second, same key: ok = %v, err = %v; want false", ok, err)
	}
	other := scope
	other.Key = uuid.NewV4().String()
	if ok, err := repo.TryLock(ctx, second, other); err != nil || !ok {
		t.Errorf("second, other key: ok = %v, err = %v; want true", ok, err)
	}

	// Al terminar la primera transacción, el lock se libera.
	first.Rollback()
	third, _ := db.BeginTx(ctx, nil)
	defer third.Rollback()
	if ok, err := repo.TryLock(ctx, third, scope); err != nil || !ok {
		t.Errorf("after release: ok = %v, err = %v; want true", ok, err)
	}
}
