// Flujo idempotente común a recargas y transferencias (spec 8.1, RN-28).
package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	"accounts-api/internal/model"
	"accounts-api/internal/repository"
)

// invalidRequestHash es el hash de un cuerpo inválido: nunca coincide con una petición
// guardada, así que reusar una key con un cuerpo inválido da IDEMPOTENCY_KEY_REUSED.
const invalidRequestHash = "invalid"

// requestHash es el SHA-256 en hexadecimal de los campos unidos con "|", en orden fijo.
// No depende del orden de las claves ni de los espacios del JSON.
func requestHash(fields ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(fields, "|")))
	return hex.EncodeToString(sum[:])
}

// idempotentRun describe una operación idempotente.
type idempotentRun struct {
	scope       repository.IdempotencyScope
	requestHash string
	// validate corre los pasos de formato (cuerpo, números, montos). Sus errores no se guardan.
	validate func() error
	// operation corre dentro de la transacción. Sus errores de negocio (404, 422) se guardan
	// con la key; cualquier otro error deshace todo.
	operation func(tx *sql.Tx) ([]model.Movement, error)
}

// runIdempotent ejecuta la operación con la key (spec 8.1). Devuelve los movimientos,
// si la respuesta es una repetición (Idempotent-Replayed) y el error.
// Todo ocurre en una transacción, en el orden del spec:
//
//	advisory lock (409) → key guardada (repetir o 422) → validate (400) →
//	SAVEPOINT → operation → guardar key con el resultado → COMMIT
func runIdempotent(ctx context.Context, db *sql.DB, run idempotentRun) (movements []model.Movement, replayed bool, err error) {
	var keys repository.IdempotencyRepo

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()

	// Paso 4: petición en curso, repetida o con la key reusada.
	locked, err := keys.TryLock(ctx, tx, run.scope)
	if err != nil {
		return nil, false, err
	}
	if !locked {
		return nil, false, model.ErrIdempotencyInProgress
	}
	stored, err := keys.Find(ctx, tx, run.scope)
	if err != nil {
		return nil, false, err
	}
	if stored != nil {
		return replay(ctx, tx, *stored, run.requestHash)
	}

	// Pasos de formato: el error se responde sin guardar la key.
	if err := run.validate(); err != nil {
		return nil, false, err
	}

	// El savepoint permite deshacer la operación fallida y aun así guardar su error.
	if _, err := tx.ExecContext(ctx, `SAVEPOINT op`); err != nil {
		return nil, false, err
	}
	movements, opErr := run.operation(tx)
	result := repository.StoredResult{RequestHash: run.requestHash}
	if opErr != nil {
		businessErr, storable := model.StorableError(opErr)
		if !storable {
			return nil, false, opErr // error inesperado: se deshace todo
		}
		if _, err := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT op`); err != nil {
			return nil, false, err
		}
		result.ErrorCode = businessErr.Code
	} else {
		for _, m := range movements {
			result.MovementIDs = append(result.MovementIDs, m.ID)
		}
	}

	if err := keys.Save(ctx, tx, run.scope, result); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return movements, false, opErr
}

// replay devuelve el resultado guardado: los mismos movimientos (mismos ids y fechas) o el
// mismo error. Si la petición es distinta, IDEMPOTENCY_KEY_REUSED.
func replay(ctx context.Context, tx *sql.Tx, stored repository.StoredResult, hash string) ([]model.Movement, bool, error) {
	if stored.RequestHash != hash {
		return nil, false, model.ErrIdempotencyKeyReused
	}
	if stored.ErrorCode != "" {
		storedErr, ok := model.StoredError(stored.ErrorCode)
		if !ok {
			return nil, false, fmt.Errorf("replay: unknown stored error code %q", stored.ErrorCode)
		}
		return nil, true, storedErr
	}
	movements, err := repository.MovementRepo{}.FindByIDs(ctx, tx, stored.MovementIDs)
	if err != nil {
		return nil, false, err
	}
	return movements, true, nil
}
