-- I5: movimientos e idempotencia (bank-lab-spec.md, secciones 3.5 y 3.6).

-- Log de movimientos: solo se agrega, nunca se actualiza ni se borra (RN-16).
CREATE TABLE movements (
    id                 uuid        PRIMARY KEY,
    account_id         uuid        NOT NULL REFERENCES accounts (id),
    kind               text        NOT NULL CHECK (kind IN ('recharge', 'withdrawal', 'transfer_in', 'transfer_out')),
    -- Siempre positivo; el signo lo da kind (RN-17). Máximo de RN-30.
    amount_cents       bigint      NOT NULL CHECK (amount_cents > 0 AND amount_cents <= 1000000000000),
    related_account_id uuid        REFERENCES accounts (id),  -- solo en transferencias
    recharge_type      text        CHECK (recharge_type IN ('cash', 'employer_bonus', 'salary')),  -- solo en recargas
    created_at         timestamptz NOT NULL DEFAULT date_trunc('milliseconds', now()),

    CHECK ((kind = 'recharge') = (recharge_type IS NOT NULL)),
    CHECK ((kind IN ('transfer_in', 'transfer_out')) = (related_account_id IS NOT NULL))
);

-- Historial paginado (I-11) y conteo de recargas del día.
CREATE INDEX movements_account_created_at_idx ON movements (account_id, created_at DESC, id DESC);

-- Resultado de cada petición idempotente, por usuario + endpoint + key (RN-28).
-- Una fila de más de 24 horas se considera inexistente.
CREATE TABLE idempotency_keys (
    user_id      uuid        NOT NULL REFERENCES users (id),
    endpoint     text        NOT NULL CHECK (endpoint IN ('recharge', 'transfer')),
    key          uuid        NOT NULL,
    request_hash text        NOT NULL,
    movement_ids uuid[],     -- éxito: 1 en recarga, 2 en transferencia ([out, in])
    error_code   text,       -- error de negocio guardado (404/422)
    created_at   timestamptz NOT NULL DEFAULT date_trunc('milliseconds', now()),

    PRIMARY KEY (user_id, endpoint, key),
    CHECK ((movement_ids IS NULL) <> (error_code IS NULL))
);
