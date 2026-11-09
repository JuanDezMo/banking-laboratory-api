-- I2: usuarios, sesiones, intentos de login y cuentas (bank-lab-spec.md, secciones 3.1 a 3.4).
-- Las fechas usan milisegundos (I-09) y la hora de la BD (I-10).
-- Los ids (UUID v4) los genera la API con el paquete estándar uuid.

CREATE TABLE users (
    id            uuid        PRIMARY KEY,
    name          text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    -- Se guarda recortado y en minúsculas, así UNIQUE no distingue mayúsculas (RN-25).
    email         text        NOT NULL UNIQUE CHECK (char_length(email) <= 254),
    password_hash text        NOT NULL,  -- argon2id en formato PHC
    role          text        NOT NULL CHECK (role IN ('customer', 'operator')),
    created_at    timestamptz NOT NULL DEFAULT date_trunc('milliseconds', now())
);

CREATE TABLE refresh_tokens (
    id         uuid        PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES users (id),
    -- SHA-256 del token; el token en claro nunca se guarda.
    token_hash text        NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,  -- NULL mientras esté vigente
    created_at timestamptz NOT NULL DEFAULT date_trunc('milliseconds', now())
);

-- Solo intentos fallidos; un login exitoso borra los del email (RN-29).
CREATE TABLE login_attempts (
    id         uuid        PRIMARY KEY,
    email      text        NOT NULL,  -- recortado y en minúsculas; exista o no el usuario
    created_at timestamptz NOT NULL DEFAULT date_trunc('milliseconds', now())
);

CREATE INDEX login_attempts_email_created_at_idx ON login_attempts (email, created_at);

CREATE TABLE accounts (
    id                    uuid        PRIMARY KEY,
    owner_id              uuid        NOT NULL REFERENCES users (id),
    number                text        NOT NULL UNIQUE CHECK (number ~ '^[0-9]{10}$'),
    kind                  text        NOT NULL CHECK (kind IN ('savings', 'checking')),
    balance_cents         bigint      NOT NULL DEFAULT 0,
    overdraft_limit_cents bigint      NOT NULL DEFAULT 0 CHECK (overdraft_limit_cents >= 0),
    created_at            timestamptz NOT NULL DEFAULT date_trunc('milliseconds', now()),

    -- Últimas defensas en la BD (RN-04, RN-05, RN-31).
    CHECK (balance_cents >= -overdraft_limit_cents),
    CHECK (balance_cents <= 1000000000000000)
);

CREATE INDEX accounts_owner_created_at_idx ON accounts (owner_id, created_at, id);
