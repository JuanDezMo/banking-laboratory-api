-- Carga inicial de usuarios operator (RN-27): la API no permite crearlos.
-- Se puede ejecutar varias veces: si el email ya existe, no hace nada.
--
--   psql "$DATABASE_URL" -f seeds/operators.sql
--   docker exec -i accounts-api-db psql -U bank -d bank < seeds/operators.sql   # sin psql local
--
-- SOLO PARA EL LABORATORIO. Credenciales:
--   email:      operator@bank.local
--   contraseña: Operator2026!
-- El hash es argon2id (PHC) con los parámetros de internal/service/password.go.

INSERT INTO users (id, name, email, password_hash, role)
VALUES (
    '0b7e4a52-3c1d-4f8e-9a6b-2d5c8e1f0a01',
    'Bank Operator',
    'operator@bank.local',
    '$argon2id$v=19$m=19456,t=2,p=1$iozNYC6ArP3EXHjKZwCdHA$W61tPWNmwvqhNnzKJUhTDux8KGkk1UwrKFLgOo0w7GE',
    'operator'
)
ON CONFLICT (email) DO NOTHING;
