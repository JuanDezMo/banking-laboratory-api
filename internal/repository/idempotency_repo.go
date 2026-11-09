// Tabla idempotency_keys y advisory lock.
package repository

import (
	"context"
	"database/sql"
	"errors"
)

// idempotencyLockNamespace separa estos advisory locks de los del login (loginLockNamespace).
const idempotencyLockNamespace = 2

// IdempotencyEndpoint es el endpoint al que pertenece una key (la key es única por usuario + endpoint).
type IdempotencyEndpoint string

const (
	EndpointRecharge IdempotencyEndpoint = "recharge"
	EndpointTransfer IdempotencyEndpoint = "transfer"
)

// IdempotencyScope identifica una key: usuario + endpoint + key.
type IdempotencyScope struct {
	UserID   string
	Endpoint IdempotencyEndpoint
	Key      string // UUID en minúsculas
}

// StoredResult es el resultado guardado de una petición. Tiene MovementIDs (éxito) o ErrorCode.
type StoredResult struct {
	RequestHash string
	MovementIDs []string
	ErrorCode   string
}

// IdempotencyRepo agrupa el SQL de idempotencia (spec 8.1).
type IdempotencyRepo struct{}

// TryLock intenta tomar el advisory lock de la key hasta el fin de la transacción. No espera:
// si otra transacción lo tiene, devuelve false (petición en curso → 409).
// Debe llamarse dentro de una transacción (*sql.Tx).
func (IdempotencyRepo) TryLock(ctx context.Context, tx DBTX, s IdempotencyScope) (bool, error) {
	var locked bool
	err := tx.QueryRowContext(ctx,
		`SELECT pg_try_advisory_xact_lock($1, hashtext($2 || ':' || $3 || ':' || $4))`,
		idempotencyLockNamespace, s.UserID, string(s.Endpoint), s.Key).
		Scan(&locked)
	return locked, err
}

// Find devuelve el resultado guardado en las últimas 24 horas (hora de la BD).
// Una fila más antigua se considera inexistente: devuelve nil, sin error.
func (IdempotencyRepo) Find(ctx context.Context, db DBTX, s IdempotencyScope) (*StoredResult, error) {
	var r StoredResult
	var errorCode sql.NullString
	err := db.QueryRowContext(ctx,
		`SELECT request_hash, coalesce(movement_ids::text[], '{}'), error_code
		   FROM idempotency_keys
		  WHERE user_id = $1 AND endpoint = $2 AND key = $3
		    AND created_at > now() - interval '24 hours'`,
		s.UserID, string(s.Endpoint), s.Key).
		Scan(&r.RequestHash, &r.MovementIDs, &errorCode)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.ErrorCode = errorCode.String
	return &r, nil
}

// Save guarda el resultado de la key. Si existe una fila vencida (más de 24 horas) con la
// misma key, la reemplaza. Se llama con el lock de TryLock tomado y después de Find,
// así que una fila vigente no puede existir.
func (IdempotencyRepo) Save(ctx context.Context, db DBTX, s IdempotencyScope, r StoredResult) error {
	var movementIDs any // NULL si no hay movimientos
	if len(r.MovementIDs) > 0 {
		movementIDs = r.MovementIDs
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO idempotency_keys (user_id, endpoint, key, request_hash, movement_ids, error_code)
		 VALUES ($1, $2, $3, $4, $5::uuid[], $6)
		 ON CONFLICT (user_id, endpoint, key) DO UPDATE
		    SET request_hash = excluded.request_hash,
		        movement_ids = excluded.movement_ids,
		        error_code   = excluded.error_code,
		        created_at   = excluded.created_at
		  WHERE idempotency_keys.created_at <= now() - interval '24 hours'`,
		s.UserID, string(s.Endpoint), s.Key, r.RequestHash, movementIDs, nullIfEmpty(r.ErrorCode))
	return err
}
