# Accounts API (Go)

API REST de un **laboratorio bancario**: clientes que se registran, abren cuentas de ahorro o corrientes, reciben recargas de un operador del banco y transfieren dinero entre cuentas, con historial de movimientos.

Está escrita en Go con la librería estándar (`net/http`, `database/sql`) sobre PostgreSQL, y cumple un contrato HTTP fijo (`openapi.yaml`) pensado para que una implementación en otro lenguaje (por ejemplo, Rust) se comporte exactamente igual.

---

## Contenido

1. [Requisitos](#1-requisitos)
2. [Inicio rápido con Docker](#2-inicio-rápido-con-docker)
3. [Configuración (`.env`)](#3-configuración-env)
4. [Operación con Docker](#4-operación-con-docker)
5. [Ejecutar la API en local (sin contenedor)](#5-ejecutar-la-api-en-local-sin-contenedor)
6. [Pruebas](#6-pruebas)
7. [Recorrido de ejemplo con `curl`](#7-recorrido-de-ejemplo-con-curl)
8. [Arquitectura y estructura del proyecto](#8-arquitectura-y-estructura-del-proyecto)
9. [Convenciones de la API](#9-convenciones-de-la-api)
10. [Roles y permisos](#10-roles-y-permisos)
11. [Reglas de negocio (RN-01 a RN-31)](#11-reglas-de-negocio-rn-01-a-rn-31)
12. [Funcionalidades (F-01 a F-10)](#12-funcionalidades-f-01-a-f-10)
13. [Idempotencia](#13-idempotencia)
14. [Límite de intentos de login](#14-límite-de-intentos-de-login)
15. [Catálogo de errores](#15-catálogo-de-errores)
16. [Reglas de interoperabilidad (I-01 a I-15)](#16-reglas-de-interoperabilidad-i-01-a-i-15)
17. [Modelo de datos](#17-modelo-de-datos)
18. [Concurrencia y garantías](#18-concurrencia-y-garantías)
19. [Documentos relacionados](#19-documentos-relacionados)

---

## 1. Requisitos

| Para | Necesitas |
|---|---|
| Levantar todo con Docker | Docker con **Docker Compose v2** (`docker compose …`) |
| Ejecutar la API o las pruebas en local | **Go 1.27** o superior (el `go.mod` declara `go 1.27.3`) |
| Seguir el recorrido de ejemplo | `curl` y `jq`; para generar UUID, `uuidgen` (o `cat /proc/sys/kernel/random/uuid` en Linux) |

Puertos que se usan: **3000** (API) y **5432** (PostgreSQL; configurable con `POSTGRES_PORT`).

---

## 2. Inicio rápido con Docker

Desde la carpeta del proyecto:

```bash
# 1. Variables de entorno (una sola vez). Cambia JWT_SECRET por un texto aleatorio de 32+ caracteres.
cp .env.example .env

# 2. Construir la imagen de la API y levantar API + base de datos.
docker compose up -d --build

# 3. Comprobar que ambos servicios están arriba (la BD debe decir "healthy").
docker compose ps

# 4. Cargar el usuario operator (la API no permite crearlos; ver RN-27).
#    Hacerlo DESPUÉS del paso 2: la API crea las tablas al arrancar.
docker exec -i accounts-api-db psql -U bank -d bank < seeds/operators.sql

# 5. Probar que responde (una ruta inexistente devuelve 404 en JSON).
curl -i http://localhost:3000/x
```

Respuesta esperada del paso 5:

```http
HTTP/1.1 404 Not Found
Content-Type: application/json; charset=utf-8

{"code":"ROUTE_NOT_FOUND","message":"Route not found"}
```

Qué pasa al arrancar:

1. Arranca `db` (imagen oficial `postgres:16-alpine`, reloj en UTC). La primera vez que se crea el volumen, el script `docker/postgres/init/01-create-test-db.sql` crea además la BD de pruebas `bank_test`.
2. `api` espera a que `db` esté **healthy** (`pg_isready`) y luego arranca.
3. La API lee la configuración, conecta a la BD, **aplica las migraciones pendientes** (`migrations/*.sql`, incluidas en el binario) y queda escuchando en `:3000`.

En los logs (`docker compose logs -f api`) se ve:

```
INFO database connected
INFO migrations applied
INFO server listening addr=:3000
```

### Credenciales de laboratorio

| Usuario | Email | Contraseña | Cómo se crea |
|---|---|---|---|
| Operador | `operator@bank.local` | `Operator2026!` | `seeds/operators.sql` (paso 4) |
| Clientes | Los que registres | Los que elijas | `POST /auth/register` |

> Solo para el laboratorio. El seed guarda la contraseña como hash argon2id y se puede ejecutar varias veces (`ON CONFLICT DO NOTHING`).

---

## 3. Configuración (`.env`)

`docker compose` lee el archivo `.env` de la carpeta del proyecto. La API, cuando corre en local, también lo carga con su propio lector (`env.go`), que **no pisa** variables que ya existan en el entorno.

| Variable | Usada por | Valor por defecto | Descripción |
|---|---|---|---|
| `POSTGRES_USER` | `db` | `bank` | Usuario de PostgreSQL |
| `POSTGRES_PASSWORD` | `db` | `bank` | Contraseña de PostgreSQL |
| `POSTGRES_DB` | `db` | `bank` | Base de datos principal |
| `POSTGRES_PORT` | `db` | `5432` | Puerto publicado en el host |
| `DATABASE_URL` | API en local | — (obligatoria) | URL de conexión. En Docker, `docker-compose.yml` la arma apuntando al host `db` |
| `TEST_DATABASE_URL` | Pruebas | — (obligatoria para pruebas) | URL de `bank_test` |
| `JWT_SECRET` | API | — (obligatoria) | Secreto para firmar los access tokens. **Mínimo 32 caracteres**; si falta o es corto, la API termina con código 1 y `docker compose` se niega a arrancar |

Ejemplo (`.env.example`):

```dotenv
POSTGRES_USER=bank
POSTGRES_PASSWORD=bank
POSTGRES_DB=bank
POSTGRES_PORT=5432

DATABASE_URL=postgres://bank:bank@localhost:5432/bank?sslmode=disable
TEST_DATABASE_URL=postgres://bank:bank@localhost:5432/bank_test?sslmode=disable
JWT_SECRET=cambia-esto-por-un-secreto-de-al-menos-32-caracteres
```

Para generar un secreto: `openssl rand -base64 48`.

`.env` está en `.gitignore` y en `.dockerignore`: nunca se versiona ni entra en la imagen.

---

## 4. Operación con Docker

### Servicios

| Servicio | Contenedor | Imagen | Puerto | Notas |
|---|---|---|---|---|
| `api` | `accounts-api` | Construida con el `Dockerfile` | 3000 | Binario estático; imagen final `alpine` con usuario sin root |
| `db` | `accounts-api-db` | `postgres:16-alpine` | 5432 | Zona horaria UTC; datos en el volumen `pgdata`; `healthcheck` con `pg_isready` |

El `Dockerfile` tiene dos etapas: compila con `golang:1.27.3-alpine` (`CGO_ENABLED=0`, `-trimpath`) y copia solo el binario a `alpine:3.24`.

### Comandos habituales

| Acción | Comando |
|---|---|
| Levantar todo (reconstruyendo la API) | `docker compose up -d --build` |
| Levantar solo la BD | `docker compose up -d db` |
| Ver estado | `docker compose ps` |
| Ver logs de la API | `docker compose logs -f api` |
| Reconstruir tras cambiar código | `docker compose up -d --build api` |
| Cargar el operator | `docker exec -i accounts-api-db psql -U bank -d bank < seeds/operators.sql` |
| Entrar a la BD | `docker exec -it accounts-api-db psql -U bank -d bank` |
| Ver tablas / migraciones | `\dt` y `select * from schema_migrations;` dentro de `psql` |
| Detener sin borrar nada | `docker compose stop` |
| Borrar contenedores (conserva datos) | `docker compose down` |
| **Borrar todo, incluidos los datos** | `docker compose down -v` |

### Empezar de cero

```bash
docker compose down -v          # borra contenedores y el volumen pgdata
docker compose up -d --build    # recrea la BD (y bank_test) y aplica migraciones
docker exec -i accounts-api-db psql -U bank -d bank < seeds/operators.sql
```

### Problemas comunes

| Síntoma | Causa | Solución |
|---|---|---|
| `JWT_SECRET is required (copy .env.example to .env)` al hacer `up` | No existe `.env` o le falta `JWT_SECRET` | `cp .env.example .env` y definir un secreto de 32+ caracteres |
| La API termina con `JWT_SECRET is required and must have at least 32 characters` | Secreto corto | Usar uno más largo |
| `relation "users" does not exist` al cargar el seed | El seed se ejecutó antes de que la API aplicara las migraciones | Levantar la API primero (`docker compose up -d`) y repetir el seed |
| Las pruebas fallan con `database "bank_test" does not exist` | El volumen se creó antes de existir el script de init (solo corre con el volumen vacío) | `docker compose down -v` y volver a levantar, o crearla a mano: `docker exec accounts-api-db psql -U bank -c "CREATE DATABASE bank_test"` |
| `bind: address already in use` | Otro proceso usa el 3000 o el 5432 | Liberar el puerto o cambiar `POSTGRES_PORT` / el mapeo de `api` |
| El login del operator da `401` | No se cargó el seed | Ejecutar el paso 4 del inicio rápido |

---

## 5. Ejecutar la API en local (sin contenedor)

Útil para desarrollar: la BD en Docker y la API con `go run`.

```bash
cp .env.example .env              # si aún no existe
docker compose up -d db           # solo PostgreSQL
go run .                          # lee .env, aplica migraciones y escucha en :3000
docker exec -i accounts-api-db psql -U bank -d bank < seeds/operators.sql
```

Detener: `Ctrl+C`. El servidor hace un apagado ordenado (deja de aceptar conexiones, espera las peticiones en curso hasta 10 s) y termina con el log `server stopped`.

> No ejecutes a la vez `go run .` y el contenedor `api`: los dos usan el puerto 3000.

---

## 6. Pruebas

Las pruebas unitarias (modelo, validaciones, JWT, `decodeObject`) no usan la BD. Las de integración (repositorios, services y handlers con `httptest`) usan la BD real `bank_test`: cada paquete crea un esquema limpio, aplica las migraciones y trabaja aislado. **Sin `TEST_DATABASE_URL`, las pruebas de integración fallan** (no se saltan).

```bash
docker compose up -d db           # la BD debe estar arriba
set -a; source .env; set +a       # exporta TEST_DATABASE_URL y el resto
go test ./...                     # todas las pruebas
go test -race ./...               # con el detector de carreras (recomendado)
go test -cover ./internal/...     # cobertura por paquete
```

Pruebas de concurrencia destacadas:

| Comando | Qué verifica |
|---|---|
| `go test -race -count=20 -run Concurrency ./internal/service/` | Transferencias cruzadas A→B y B→A en paralelo sin deadlock; no se gasta dos veces el mismo saldo; invariante RN-17 |
| `go test -race -count=10 -run TestRechargeDailyCountConcurrent ./internal/service/` | Seis recargas simultáneas: solo se aplican 5 (RN-18, RN-20) |
| `go test -race -run TestRefreshConcurrent ./internal/service/` | Dos refresh simultáneos con el mismo token: solo uno lo renueva |
| `go test -race -run TestLoginRateLimitConcurrent ./internal/service/` | Diez logins fallidos simultáneos registran exactamente 5 intentos |

Calidad mínima antes de cada commit:

```bash
gofmt -l .        # no debe listar archivos
go vet ./...      # sin advertencias
go build ./...
go test -race ./...
```

`VERIFICACION-INTEROPERABILIDAD.md` lista, para cada regla I-01 a I-15, las pruebas que la cubren.

---

## 7. Recorrido de ejemplo con `curl`

Con la API en `http://localhost:3000` y el operator cargado. Los valores entre `$…` se guardan en variables de la shell.

```bash
API=http://localhost:3000
```

**1. Registrar dos clientes (F-01)**

```bash
curl -s -X POST $API/auth/register -H 'Content-Type: application/json' \
  -d '{"name":"Ana Pérez","email":"ana@mail.com","password":"S3gura2026"}' | jq
curl -s -X POST $API/auth/register -H 'Content-Type: application/json' \
  -d '{"name":"Eva Ruiz","email":"eva@mail.com","password":"S3gura2026"}' | jq
```

```json
{
  "id": "6df4e87a-397f-4071-9ff8-5564fe8ad41f",
  "name": "Ana Pérez",
  "email": "ana@mail.com",
  "role": "customer",
  "createdAt": "2026-11-09T22:00:10.179Z"
}
```

**2. Iniciar sesión (F-02) y guardar los tokens**

```bash
login() { curl -s -X POST $API/auth/login -H 'Content-Type: application/json' \
            -d "{\"email\":\"$1\",\"password\":\"$2\"}"; }

ANA_LOGIN=$(login ana@mail.com S3gura2026)
ANA=$(echo "$ANA_LOGIN" | jq -r .accessToken)
ANA_REFRESH=$(echo "$ANA_LOGIN" | jq -r .refreshToken)
EVA=$(login eva@mail.com S3gura2026 | jq -r .accessToken)
OP=$(login operator@bank.local 'Operator2026!' | jq -r .accessToken)
```

**3. Crear cuentas (F-05)**

```bash
ANA_ACC=$(curl -s -X POST $API/accounts -H "Authorization: Bearer $ANA" \
  -H 'Content-Type: application/json' -d '{"accountType":"savings"}')
echo "$ANA_ACC" | jq
ANA_ID=$(echo "$ANA_ACC" | jq -r .id)
ANA_NUM=$(echo "$ANA_ACC" | jq -r .accountNumber)

EVA_NUM=$(curl -s -X POST $API/accounts -H "Authorization: Bearer $EVA" \
  -H 'Content-Type: application/json' -d '{"accountType":"checking"}' | jq -r .accountNumber)
```

```json
{
  "id": "1ea7171b-5887-4d04-bd4f-0ade514d6864",
  "accountNumber": "7879718876",
  "accountType": "savings",
  "balance": 0,
  "createdAt": "2026-11-09T22:00:10.383Z"
}
```

**4. Recargar como operator (F-08), con idempotencia**

```bash
KEY=$(uuidgen)
curl -s -X POST $API/recharge -H "Authorization: Bearer $OP" -H "Idempotency-Key: $KEY" \
  -H 'Content-Type: application/json' \
  -d "{\"accountNumber\":\"$ANA_NUM\",\"amount\":150000,\"rechargeType\":\"salary\"}" | jq
```

```json
{
  "id": "a5429baa-6c55-4483-86b8-f6a0ccdd497d",
  "accountId": "1ea7171b-5887-4d04-bd4f-0ade514d6864",
  "movementType": "recharge",
  "amount": 150000,
  "details": { "rechargeType": "salary" },
  "createdAt": "2026-11-09T22:00:10.501Z"
}
```

Repetir **el mismo comando con la misma `$KEY`** no recarga otra vez: devuelve el mismo cuerpo con `Idempotent-Replayed: true` (compruébalo con `curl -i`).

**5. Transferir (F-09)**

```bash
curl -s -X POST $API/transfers -H "Authorization: Bearer $ANA" -H "Idempotency-Key: $(uuidgen)" \
  -H 'Content-Type: application/json' \
  -d "{\"accountOrigin\":\"$ANA_NUM\",\"accountDestination\":\"$EVA_NUM\",\"amount\":40000}" | jq
```

Responde `201` con `{ "transferOut": {…}, "transferIn": {…} }`.

**6. Consultar la cuenta (F-07), listar cuentas (F-06) e historial (F-10)**

```bash
curl -s $API/accounts/$ANA_ID -H "Authorization: Bearer $ANA" | jq .balance             # 110000
curl -s $API/accounts -H "Authorization: Bearer $ANA" | jq '.items | length'            # 1
curl -s "$API/accounts/$ANA_ID/movements?page=1" -H "Authorization: Bearer $ANA" | jq   # transfer_out, recharge
```

**7. Renovar tokens (F-03) y cerrar sesión (F-04)**

```bash
NEW_REFRESH=$(curl -s -X POST $API/auth/refresh -H 'Content-Type: application/json' \
  -d "{\"refreshToken\":\"$ANA_REFRESH\"}" | jq -r .refreshToken)

curl -s -o /dev/null -w '%{http_code}\n' -X POST $API/auth/logout \
  -H "Authorization: Bearer $ANA" -H 'Content-Type: application/json' \
  -d "{\"refreshToken\":\"$NEW_REFRESH\"}"                                               # 204
```

Reusar `$ANA_REFRESH` (ya rotado) o `$NEW_REFRESH` (revocado por el logout) responde `401 INVALID_REFRESH_TOKEN`.

### Colección de Postman

`postman/accounts-api.postman_collection.json` automatiza este mismo recorrido (registro, login con guardado de tokens, cuentas, consulta, recarga, transferencia e historial, más casos de error y refresh/logout). Impórtala en Postman (*Import*) y ejecútala con *Run collection*; no requiere configurar nada más que `baseUrl` si la API no está en `http://localhost:3000`. Las instrucciones están en la descripción de la colección. También se puede correr por consola:

```bash
npx newman run postman/accounts-api.postman_collection.json
```

---

## 8. Arquitectura y estructura del proyecto

Capas con dependencias en un solo sentido: `handler → service → repository → model`.

| Capa | Responsabilidad | No hace |
|---|---|---|
| `handler` | Rutas, middleware (logs, recuperación de pánicos, autenticación, 404/405 en JSON), leer el cuerpo y los encabezados, convertir errores en códigos HTTP | Reglas de negocio, SQL |
| `service` | Orden de validación, reglas, permisos (rol y propiedad de la cuenta), transacciones, idempotencia, hash de contraseñas y tokens | Conocer HTTP |
| `repository` | SQL, bloqueos (`FOR UPDATE`, advisory locks), fuente de la hora (`now()` de la BD) | Decidir reglas |
| `model` | Entidades, reglas puras (`Account.Withdraw`, `Account.Deposit`) y el catálogo de errores | Entrada/salida |

```
go_accounts_api/
├─ main.go, env.go             # arranque, configuración, pool de BD, servidor y apagado ordenado
├─ Dockerfile, docker-compose.yml, .dockerignore
├─ docker/postgres/init/       # crea bank_test la primera vez
├─ migrations/                 # 001_auth_accounts.sql, 002_movements_idempotency.sql (+ embed.go)
├─ seeds/operators.sql         # usuario operator de laboratorio
└─ internal/
   ├─ model/        # User, Account, Movement, errores del catálogo
   ├─ repository/   # user_repo, account_repo, movement_repo, idempotency_repo
   ├─ service/      # auth, account, recharge, transfer, idempotency (flujo común), validate, tokens, password
   ├─ handler/      # router, middleware, auth/account/operation handlers, dto, respond (decodeObject)
   └─ testdb/       # BD de pruebas: esquema limpio por paquete
```

Dependencias externas: `github.com/jackc/pgx/v5` (driver de PostgreSQL para `database/sql`), `github.com/golang-jwt/jwt/v5` (JWT) y `golang.org/x/crypto` (argon2id).

Cadena de middleware: `Logging → Recover → JSONNotFound → ServeMux`, y `Auth` en cada ruta protegida.

---

## 9. Convenciones de la API

- **JSON** en peticiones y respuestas. Las peticiones con cuerpo deben enviar `Content-Type: application/json` (se acepta `; charset=utf-8`). Cuerpo máximo: **16 KB**.
- Nombres de campo en **camelCase** y sensibles a mayúsculas. Los campos desconocidos se ignoran.
- **Autenticación:** salvo registro, login y refresh, todas las peticiones envían `Authorization: Bearer <accessToken>` (la palabra `Bearer` no distingue mayúsculas).
- **Dinero en centavos enteros** (`amount`, `balance`): `1.500,00` se envía como `150000`. Nunca decimales.
- **Ids** (usuario, cuenta, movimiento): UUID v4. Los UUID que envía el cliente aceptan mayúsculas y se normalizan a minúsculas.
- **Número de cuenta:** 10 dígitos, único, generado por el sistema. Identifica la cuenta en recargas y transferencias.
- **Fechas:** ISO 8601 en UTC con 3 decimales: `2026-11-09T14:30:00.123Z`. La hora oficial es la de la BD.
- **Sobregiro:** `savings` = 0; `checking` = 500000 centavos (5.000,00).
- Los movimientos **nunca se editan ni se borran**. Contraseñas y refresh tokens **nunca** se guardan en claro.

### Formas de las respuestas

**Usuario**

```json
{ "id": "1b7e…", "name": "Ana Pérez", "email": "ana@mail.com", "role": "customer", "createdAt": "2026-11-09T14:00:00.000Z" }
```

**Par de tokens**

```json
{ "accessToken": "eyJ…", "refreshToken": "r9X…", "tokenType": "Bearer", "expiresIn": 900 }
```

| Campo | Descripción |
|---|---|
| `accessToken` | JWT HS256 con `sub` (id del usuario), `role`, `iat` y `exp`. Válido **15 minutos** |
| `refreshToken` | Texto aleatorio opaco (32 bytes). Válido **7 días**, de **un solo uso** |
| `tokenType` | Siempre `Bearer` |
| `expiresIn` | Segundos de vida del access token (900) |

**Cuenta**

```json
{ "id": "6f1c…", "accountNumber": "0012345678", "accountType": "savings", "balance": 0, "createdAt": "2026-11-09T14:30:00.000Z" }
```

**Movimiento**

```json
{
  "id": "a41e…",
  "accountId": "6f1c…",
  "movementType": "recharge",
  "amount": 150000,
  "details": { "rechargeType": "salary" },
  "createdAt": "2026-11-09T14:30:00.000Z"
}
```

| `movementType` | Efecto en el saldo | `details` |
|---|---|---|
| `recharge` | Suma | `{ "rechargeType": "cash" \| "employer_bonus" \| "salary" }` |
| `transfer_in` | Suma | `{ "relatedAccountId": "<cuenta origen>" }` |
| `transfer_out` | Resta | `{ "relatedAccountId": "<cuenta destino>" }` |
| `withdrawal` | Resta | `{}` (el tipo existe, pero no hay endpoint) |

`amount` siempre es positivo; el signo lo da `movementType`.

**Error**

```json
{ "code": "INSUFFICIENT_FUNDS", "message": "Insufficient funds" }
```

---

## 10. Roles y permisos

| Rol | Quién es | Cómo se crea | Qué puede hacer |
|---|---|---|---|
| `customer` | Titular de cuentas | Autorregistro (F-01) | Crear, listar y consultar **sus** cuentas; transferir desde ellas; ver su historial |
| `operator` | Personal del banco | Carga inicial (`seeds/operators.sql`), nunca desde la API | Recargar cualquier cuenta; consultar cualquier cuenta y su historial |

Matriz de acceso:

| Endpoint | Sin token | `customer` | `operator` |
|---|---|---|---|
| `POST /auth/register`, `/auth/login`, `/auth/refresh` | ✅ | ✅ | ✅ |
| `POST /auth/logout` | 401 | ✅ | ✅ |
| `POST /accounts` | 401 | ✅ | 403 |
| `GET /accounts` | 401 | ✅ (las suyas) | 403 |
| `GET /accounts/{id}` | 401 | ✅ (ajena → 404) | ✅ |
| `GET /accounts/{id}/movements` | 401 | ✅ (ajena → 404) | ✅ |
| `POST /recharge` | 401 | 403 | ✅ |
| `POST /transfers` | 401 | ✅ (origen propio) | 403 |

Una cuenta ajena se responde como **inexistente (404)**, para no revelar que existe.

---

## 11. Reglas de negocio (RN-01 a RN-31)

| ID | Regla | Dónde se aplica |
|---|---|---|
| RN-01 | Todo el dinero se maneja en **centavos enteros**. | Todo el sistema |
| RN-02 | Una cuenta es `savings` o `checking`; cualquier otro tipo se rechaza. | F-05 |
| RN-03 | Una cuenta nueva tiene saldo 0. El saldo solo cambia por recargas y transferencias. | F-05, F-08, F-09 |
| RN-04 | **Ahorros** nunca puede quedar en negativo (sobregiro 0). | F-09 |
| RN-05 | **Corriente** puede quedar en negativo hasta `-500000` centavos. | F-09 |
| RN-06 | Recarga: solo se aceptan los tipos `cash`, `employer_bonus` y `salary`. | F-08 |
| RN-07 | Recarga: el monto debe ser mayor que 0. | F-08 |
| RN-08 | Recarga en `cash`: máximo **300000** centavos (3.000,00) por recarga. Los otros tipos no tienen tope por recarga. | F-08 |
| RN-09 | Recarga: la cuenta se identifica por su **número** y debe existir. | F-08 |
| RN-10 | Transferencia: el monto debe ser mayor que 0. | F-09 |
| RN-11 | Transferencia: origen y destino deben ser cuentas distintas. | F-09 |
| RN-12 | Transferencia: ambas cuentas se identifican por su **número** y deben existir. | F-09 |
| RN-13 | Transferencia: la cuenta origen debe tener fondos según su límite (RN-04 y RN-05). | F-09 |
| RN-14 | Recargas y transferencias son **atómicas** (todo o nada). | F-08, F-09 |
| RN-15 | Cada recarga genera **1** movimiento; cada transferencia genera **2** (uno por cuenta). | F-08, F-09 |
| RN-16 | El log de movimientos es **append-only**: no se actualiza ni se borra. | Todo el sistema |
| RN-17 | Invariante: `saldo = suma de los movimientos firmados` (recarga y `transfer_in` suman; retiro y `transfer_out` restan). | Todo el sistema |
| RN-18 | Tope diario de recargas por cuenta: máximo **5 recargas por día**, sumando todos los tipos. | F-08 |
| RN-19 | Tope diario de monto recargado por cuenta: máximo **300000000** centavos (3.000.000,00) por día, sumando todos los tipos. La recarga que haría superar el tope se rechaza completa. | F-08 |
| RN-20 | La verificación de RN-18 y RN-19 y el registro de la recarga ocurren en la **misma operación atómica**, para que dos recargas simultáneas no superen el tope. | F-08 |
| RN-21 | Toda cuenta pertenece a un `customer`, que es quien la crea. | F-05 |
| RN-22 | Solo un `operator` puede recargar. | F-08 |
| RN-23 | Un `customer` solo transfiere desde sus propias cuentas; el destino puede ser de cualquier cliente. Un origen ajeno se trata como inexistente (404). | F-09 |
| RN-24 | Un `customer` solo ve sus cuentas y movimientos; un `operator` ve todas. Una cuenta ajena se trata como inexistente (404). | F-06, F-07, F-10 |
| RN-25 | El email es único (sin distinguir mayúsculas); la contraseña tiene mínimo 8 caracteres y se guarda con un hash seguro (argon2id). | F-01 |
| RN-26 | El access token dura **15 minutos** y el refresh token **7 días**. El refresh token es de **un solo uso**: cada renovación lo revoca y entrega uno nuevo. | F-02, F-03, F-04 |
| RN-27 | Los `operator` se crean con una carga inicial del sistema, no desde la API. El registro público solo crea `customer`. | F-01 |
| RN-28 | Recargas y transferencias son **idempotentes**: repetir una petición con el mismo `Idempotency-Key` no repite la operación y devuelve la misma respuesta. | F-08, F-09 |
| RN-29 | Máximo **5 intentos fallidos de login por email en 15 minutos**. Al superarlo, el login se bloquea para ese email hasta que salga de la ventana. | F-02 |
| RN-30 | El monto de una operación es como máximo **1000000000000** centavos (10.000.000.000,00). | F-08, F-09 |
| RN-31 | El saldo de una cuenta es como máximo **1000000000000000** centavos. La operación que lo superaría se rechaza. | F-08, F-09 |

> **Día** para RN-18 y RN-19: día calendario en **UTC** (00:00:00 a 23:59:59) según la hora de la BD. A medianoche UTC los topes vuelven a cero.

---

## 12. Funcionalidades (F-01 a F-10)

### Resumen

| ID | Funcionalidad | Método y ruta | Quién | OK | Errores |
|---|---|---|---|---|---|
| F-01 | Registrar cliente | `POST /auth/register` | Público | 201 | 400, 409 |
| F-02 | Iniciar sesión | `POST /auth/login` | Público | 200 | 400, 401, 429 |
| F-03 | Renovar tokens | `POST /auth/refresh` | Público | 200 | 400, 401 |
| F-04 | Cerrar sesión | `POST /auth/logout` | `customer`, `operator` | 204 | 400, 401 |
| F-05 | Crear cuenta | `POST /accounts` | `customer` | 201 | 400, 401, 403 |
| F-06 | Listar mis cuentas | `GET /accounts` | `customer` | 200 | 401, 403 |
| F-07 | Consultar cuenta | `GET /accounts/{id}` | `customer` (las suyas), `operator` | 200 | 400, 401, 404 |
| F-08 | Recargar cuenta | `POST /recharge` | `operator` | 201 | 400, 401, 403, 404, 409, 422 |
| F-09 | Transferir | `POST /transfers` | `customer` (desde las suyas) | 201 | 400, 401, 403, 404, 409, 422 |
| F-10 | Historial de movimientos | `GET /accounts/{id}/movements` | `customer` (las suyas), `operator` | 200 | 400, 401, 404 |

Cualquier endpoint puede responder además `500 INTERNAL_ERROR` ante un fallo inesperado. Una ruta inexistente responde `404 ROUTE_NOT_FOUND` y un método no soportado `405 METHOD_NOT_ALLOWED` (con el encabezado `Allow`).

**Orden de validación:** cada funcionalidad evalúa sus validaciones **en el orden de su tabla** y responde solo el **primer** error encontrado. En los endpoints protegidos, siempre van primero el token (401) y el rol (403).

---

### F-01. Registrar cliente — `POST /auth/register`

Crea un usuario con rol **`customer`**. El rol no se puede elegir.

**Petición**

```json
{ "name": "Ana Pérez", "email": "ana@mail.com", "password": "S3gura2026" }
```

| Campo | Tipo | Obligatorio | Valores |
|---|---|---|---|
| `name` | texto | Sí | 1 a 100 caracteres tras recortar espacios al inicio y al final |
| `email` | texto | Sí | Formato de email, máximo 254 caracteres; se guarda recortado y en minúsculas |
| `password` | texto | Sí | 8 a 128 caracteres; no se recortan espacios |

Las longitudes se cuentan en caracteres Unicode, no en bytes.

**Respuesta `201 Created`:** el usuario, sin contraseña.

**Reglas:** RN-25, RN-27.

| # | Validación | HTTP | `code` | `message` |
|---|---|---|---|---|
| 1 | Cuerpo JSON válido con los campos obligatorios | 400 | `BAD_REQUEST` | `Invalid request body` |
| 2 | `name` de 1 a 100 caracteres | 400 | `BAD_REQUEST` | `Invalid name` |
| 3 | `email` con formato válido y máximo 254 caracteres | 400 | `BAD_REQUEST` | `Invalid email` |
| 4 | `password` con al menos 8 caracteres | 400 | `BAD_REQUEST` | `Password too short` |
| 5 | `password` con máximo 128 caracteres | 400 | `BAD_REQUEST` | `Password too long` |
| 6 | El email no está registrado (sin distinguir mayúsculas) | 409 | `EMAIL_ALREADY_REGISTERED` | `Email already registered` |

---

### F-02. Iniciar sesión — `POST /auth/login`

**Petición**

```json
{ "email": "ana@mail.com", "password": "S3gura2026" }
```

**Respuesta `200 OK`:** par de tokens.

**Reglas:** RN-26, RN-29.

| # | Validación | HTTP | `code` | `message` |
|---|---|---|---|---|
| 1 | Cuerpo JSON válido con los campos obligatorios | 400 | `BAD_REQUEST` | `Invalid request body` |
| 2 | El email tiene menos de 5 intentos fallidos en los últimos 15 minutos | 429 | `TOO_MANY_REQUESTS` | `Too many login attempts` |
| 3 | El email existe y la contraseña coincide | 401 | `INVALID_CREDENTIALS` | `Invalid email or password` |

- El error 3 no indica si falló el email o la contraseña, y tarda lo mismo en ambos casos: así no se revela qué emails existen.
- El error 3 suma un intento fallido; un login exitoso reinicia el contador.
- El 429 incluye `Retry-After: <segundos>`. Ver [sección 14](#14-límite-de-intentos-de-login).

---

### F-03. Renovar tokens — `POST /auth/refresh`

Entrega un **nuevo** par de tokens. El refresh token recibido queda revocado (rotación).

**Petición**

```json
{ "refreshToken": "r9X…" }
```

**Respuesta `200 OK`:** par de tokens nuevo. El rol del nuevo access token se toma de la BD.

**Reglas:** RN-26.

| # | Validación | HTTP | `code` | `message` |
|---|---|---|---|---|
| 1 | Cuerpo JSON válido con los campos obligatorios | 400 | `BAD_REQUEST` | `Invalid request body` |
| 2 | El refresh token existe, no está revocado ni vencido | 401 | `INVALID_REFRESH_TOKEN` | `Invalid refresh token` |

Si dos peticiones usan el mismo refresh token a la vez, **solo una** lo renueva; la otra recibe 401.

---

### F-04. Cerrar sesión — `POST /auth/logout`

Revoca el refresh token. El access token sigue siendo válido hasta que venza (máximo 15 minutos).

**Petición** (con `Authorization: Bearer …`)

```json
{ "refreshToken": "r9X…" }
```

**Respuesta `204 No Content`**, sin cuerpo.

**Reglas:** RN-26.

| # | Validación | HTTP | `code` | `message` |
|---|---|---|---|---|
| 1 | Access token válido | 401 | `UNAUTHORIZED` | `Missing or invalid token` |
| 2 | Cuerpo JSON válido con los campos obligatorios | 400 | `BAD_REQUEST` | `Invalid request body` |
| 3 | El refresh token está vigente y es del mismo usuario | 401 | `INVALID_REFRESH_TOKEN` | `Invalid refresh token` |

---

### F-05. Crear cuenta — `POST /accounts`

Crea una cuenta de ahorros (`savings`) o corriente (`checking`) a nombre del `customer` del token. Nace con saldo 0, sin movimientos y con un número de 10 dígitos generado al azar (único).

**Petición**

```json
{ "accountType": "savings" }
```

| Campo | Tipo | Obligatorio | Valores |
|---|---|---|---|
| `accountType` | texto | Sí | `savings`, `checking` (exactos, sensibles a mayúsculas) |

**Respuesta `201 Created`:** la cuenta.

**Reglas:** RN-02, RN-03, RN-21.

| # | Validación | HTTP | `code` | `message` |
|---|---|---|---|---|
| 1 | Access token válido | 401 | `UNAUTHORIZED` | `Missing or invalid token` |
| 2 | Rol `customer` | 403 | `FORBIDDEN` | `Access denied` |
| 3 | Cuerpo JSON válido con los campos obligatorios | 400 | `BAD_REQUEST` | `Invalid request body` |
| 4 | `accountType` es `savings` o `checking` | 400 | `BAD_REQUEST` | `Invalid account type` |

---

### F-06. Listar mis cuentas — `GET /accounts`

Devuelve las cuentas del `customer` del token, sin paginación, ordenadas por `createdAt` ascendente y, si hay empate, por `id` ascendente.

**Respuesta `200 OK`**

```json
{
  "items": [
    { "id": "6f1c…", "accountNumber": "0012345678", "accountType": "savings", "balance": 150000, "createdAt": "2026-11-09T14:30:00.000Z" },
    { "id": "9a2d…", "accountNumber": "0012345679", "accountType": "checking", "balance": 0, "createdAt": "2026-11-09T15:00:00.000Z" }
  ]
}
```

Sin cuentas: `{ "items": [] }`.

**Reglas:** RN-24.

| # | Validación | HTTP | `code` | `message` |
|---|---|---|---|---|
| 1 | Access token válido | 401 | `UNAUTHORIZED` | `Missing or invalid token` |
| 2 | Rol `customer` | 403 | `FORBIDDEN` | `Access denied` |

---

### F-07. Consultar cuenta — `GET /accounts/{id}`

Devuelve los datos y el saldo actual de una cuenta. Un `customer` solo puede consultar las suyas; un `operator`, cualquiera.

**Respuesta `200 OK`:** la cuenta.

**Reglas:** RN-24.

| # | Validación | HTTP | `code` | `message` |
|---|---|---|---|---|
| 1 | Access token válido | 401 | `UNAUTHORIZED` | `Missing or invalid token` |
| 2 | `id` tiene formato UUID (`8-4-4-4-12`) | 400 | `BAD_REQUEST` | `Invalid account id` |
| 3 | La cuenta existe y, si es `customer`, es suya | 404 | `NOT_FOUND` | `Account not found` |

---

### F-08. Recargar cuenta — `POST /recharge`

Solo un `operator`. Suma un monto al saldo de una cuenta identificada por su **número** y registra **un** movimiento `recharge`. Todo en una operación atómica, incluida la verificación de los topes diarios.

**Petición**

```http
POST /recharge
Authorization: Bearer <token de operator>
Idempotency-Key: 3f0c9a52-7d1e-4b8a-9c2f-5e6d7a8b9c01
Content-Type: application/json

{ "accountNumber": "0012345678", "amount": 150000, "rechargeType": "salary" }
```

| Campo | Tipo | Obligatorio | Valores |
|---|---|---|---|
| `accountNumber` | texto | Sí | Exactamente 10 dígitos |
| `amount` | entero (centavos) | Sí | 1 a 1000000000000; si es `cash`, máximo 300000 |
| `rechargeType` | texto | Sí | `cash`, `employer_bonus`, `salary` |

| Encabezado | Obligatorio | Valores |
|---|---|---|
| `Idempotency-Key` | Sí | UUID generado por el cliente, uno por operación ([sección 13](#13-idempotencia)) |

**Respuesta `201 Created`:** el movimiento creado.

```json
{
  "id": "a41e…",
  "accountId": "6f1c…",
  "movementType": "recharge",
  "amount": 150000,
  "details": { "rechargeType": "salary" },
  "createdAt": "2026-11-09T14:30:00.000Z"
}
```

**Reglas:** RN-06, RN-07, RN-08, RN-09, RN-14, RN-15, RN-18, RN-19, RN-20, RN-22, RN-28, RN-30, RN-31.

| # | Validación | HTTP | `code` | `message` | ¿Se guarda con la key? |
|---|---|---|---|---|---|
| 1 | Access token válido | 401 | `UNAUTHORIZED` | `Missing or invalid token` | No |
| 2 | Rol `operator` | 403 | `FORBIDDEN` | `Access denied` | No |
| 3 | `Idempotency-Key` presente y con formato UUID | 400 | `BAD_REQUEST` | `Invalid idempotency key` | No |
| 4 | Key ya usada: misma petición → respuesta guardada; otra petición → 422; primera aún en curso → 409 | 422 / 409 | `IDEMPOTENCY_KEY_REUSED` / `IDEMPOTENCY_REQUEST_IN_PROGRESS` | ver catálogo | — |
| 5 | Cuerpo JSON válido con los campos obligatorios | 400 | `BAD_REQUEST` | `Invalid request body` | No |
| 6 | `accountNumber` tiene 10 dígitos | 400 | `BAD_REQUEST` | `Invalid account number` | No |
| 7 | `rechargeType` permitido | 400 | `BAD_REQUEST` | `Invalid recharge type` | No |
| 8 | `amount > 0` | 400 | `BAD_REQUEST` | `Amount must be greater than zero` | No |
| 9 | `amount` ≤ 1000000000000 | 400 | `BAD_REQUEST` | `Amount exceeds maximum` | No |
| 10 | Recarga `cash` ≤ 300000 | 422 | `CASH_LIMIT_EXCEEDED` | `Cash recharge limit exceeded` | **Sí** |
| 11 | La cuenta existe | 404 | `NOT_FOUND` | `Account not found` | **Sí** |
| 12 | La cuenta tiene menos de 5 recargas en el día | 422 | `DAILY_RECHARGE_COUNT_EXCEEDED` | `Daily recharge count exceeded` | **Sí** |
| 13 | Recargado en el día + `amount` ≤ 300000000 | 422 | `DAILY_RECHARGE_AMOUNT_EXCEEDED` | `Daily recharge amount exceeded` | **Sí** |
| 14 | Saldo resultante ≤ 1000000000000000 | 422 | `BALANCE_LIMIT_EXCEEDED` | `Balance limit exceeded` | **Sí** |

Cómo se cumple RN-20: la recarga bloquea la fila de la cuenta (`SELECT … FOR UPDATE`) **antes** de contar las recargas del día. Una segunda recarga de la misma cuenta espera a que termine la primera y cuenta con su resultado ya confirmado, así nunca hay 6 recargas aunque lleguen a la vez.

---

### F-09. Transferir entre cuentas — `POST /transfers`

Solo un `customer`, desde sus propias cuentas. Debita el origen, acredita el destino (de cualquier cliente) y registra **dos** movimientos: `transfer_out` en el origen y `transfer_in` en el destino. Si algo falla, no cambia ningún saldo.

**Petición**

```http
POST /transfers
Authorization: Bearer <token de customer>
Idempotency-Key: 7b2e1c44-0f3a-4d5b-8e6f-1a2b3c4d5e6f
Content-Type: application/json

{ "accountOrigin": "0012345678", "accountDestination": "0098765432", "amount": 40000 }
```

| Campo | Tipo | Obligatorio | Valores |
|---|---|---|---|
| `accountOrigin` | texto | Sí | 10 dígitos; cuenta existente y del `customer` del token |
| `accountDestination` | texto | Sí | 10 dígitos; cuenta existente y distinta del origen |
| `amount` | entero (centavos) | Sí | 1 a 1000000000000 |

| Encabezado | Obligatorio | Valores |
|---|---|---|
| `Idempotency-Key` | Sí | UUID generado por el cliente, uno por operación |

**Respuesta `201 Created`**

```json
{
  "transferOut": {
    "id": "c7a0…", "accountId": "6f1c…", "movementType": "transfer_out", "amount": 40000,
    "details": { "relatedAccountId": "b92d…" }, "createdAt": "2026-11-09T14:35:00.000Z"
  },
  "transferIn": {
    "id": "d813…", "accountId": "b92d…", "movementType": "transfer_in", "amount": 40000,
    "details": { "relatedAccountId": "6f1c…" }, "createdAt": "2026-11-09T14:35:00.000Z"
  }
}
```

**Reglas:** RN-04, RN-05, RN-10, RN-11, RN-12, RN-13, RN-14, RN-15, RN-23, RN-28, RN-30, RN-31.

| # | Validación | HTTP | `code` | `message` | ¿Se guarda con la key? |
|---|---|---|---|---|---|
| 1 | Access token válido | 401 | `UNAUTHORIZED` | `Missing or invalid token` | No |
| 2 | Rol `customer` | 403 | `FORBIDDEN` | `Access denied` | No |
| 3 | `Idempotency-Key` presente y con formato UUID | 400 | `BAD_REQUEST` | `Invalid idempotency key` | No |
| 4 | Key ya usada: igual que en F-08 | 422 / 409 | `IDEMPOTENCY_KEY_REUSED` / `IDEMPOTENCY_REQUEST_IN_PROGRESS` | ver catálogo | — |
| 5 | Cuerpo JSON válido con los campos obligatorios | 400 | `BAD_REQUEST` | `Invalid request body` | No |
| 6 | `accountOrigin` y `accountDestination` tienen 10 dígitos | 400 | `BAD_REQUEST` | `Invalid account number` | No |
| 7 | `amount > 0` | 400 | `BAD_REQUEST` | `Amount must be greater than zero` | No |
| 8 | `amount` ≤ 1000000000000 | 400 | `BAD_REQUEST` | `Amount exceeds maximum` | No |
| 9 | `accountOrigin` ≠ `accountDestination` | 400 | `BAD_REQUEST` | `Cannot transfer to the same account` | No |
| 10 | La cuenta origen existe y es del `customer` del token | 404 | `NOT_FOUND` | `Account not found` | **Sí** |
| 11 | La cuenta destino existe | 404 | `NOT_FOUND` | `Account not found` | **Sí** |
| 12 | Fondos suficientes según el sobregiro (savings 0, checking −500000) | 422 | `INSUFFICIENT_FUNDS` | `Insufficient funds` | **Sí** |
| 13 | Saldo resultante del destino ≤ 1000000000000000 | 422 | `BALANCE_LIMIT_EXCEEDED` | `Balance limit exceeded` | **Sí** |

Las dos cuentas se bloquean en orden de `id` ascendente: dos transferencias cruzadas (A→B y B→A) piden los bloqueos en el mismo orden y nunca se bloquean entre sí (deadlock).

---

### F-10. Consultar historial — `GET /accounts/{id}/movements?page=N`

Devuelve los movimientos de una cuenta en páginas **fijas de 20**, del más reciente al más antiguo (`createdAt` descendente y, con empate, `id` descendente). Un `customer` solo puede consultar las suyas; un `operator`, cualquiera.

| Parámetro | Tipo | Obligatorio | Valores |
|---|---|---|---|
| `page` | entero en la query | No (por defecto 1) | 1 a 10000, en decimal sin signo ni decimales |

**Respuesta `200 OK`**

```json
{
  "page": 1,
  "pageSize": 20,
  "items": [
    { "id": "c7a0…", "accountId": "6f1c…", "movementType": "transfer_out", "amount": 40000,
      "details": { "relatedAccountId": "b92d…" }, "createdAt": "2026-11-09T14:35:00.000Z" },
    { "id": "a41e…", "accountId": "6f1c…", "movementType": "recharge", "amount": 150000,
      "details": { "rechargeType": "salary" }, "createdAt": "2026-11-09T14:30:00.000Z" }
  ]
}
```

Una página posterior a la última devuelve `"items": []` (no es error).

**Reglas:** RN-24.

| # | Validación | HTTP | `code` | `message` |
|---|---|---|---|---|
| 1 | Access token válido | 401 | `UNAUTHORIZED` | `Missing or invalid token` |
| 2 | `id` tiene formato UUID | 400 | `BAD_REQUEST` | `Invalid account id` |
| 3 | `page` es un entero entre 1 y 10000 | 400 | `BAD_REQUEST` | `Invalid pagination` |
| 4 | La cuenta existe y, si es `customer`, es suya | 404 | `NOT_FOUND` | `Account not found` |

---

## 13. Idempotencia

Evita que un reintento del cliente (por ejemplo, tras un corte de red) mueva el dinero dos veces. Aplica a `POST /recharge` y `POST /transfers`.

| Punto | Regla |
|---|---|
| Encabezado | `Idempotency-Key`, obligatorio. UUID; el cliente genera uno **nuevo por cada operación** |
| Alcance | Única por **usuario + endpoint**. Dos usuarios pueden usar la misma key sin conflicto; en mayúsculas o minúsculas es la misma key |
| "Misma petición" | Mismos valores de los campos del cuerpo (`accountNumber`, `amount`, `rechargeType` o `accountOrigin`, `accountDestination`, `amount`). No importan el orden de las claves, los espacios ni escribir `150000.0` en vez de `150000` |
| Primera petición | Se procesa normalmente. La key y su resultado se guardan **en la misma transacción** que la operación |
| Repetición con la misma petición | No se ejecuta de nuevo. Devuelve **el mismo código y el mismo cuerpo** (mismos ids y fechas) con el encabezado `Idempotent-Replayed: true` |
| Repetición con otra petición | `422 IDEMPOTENCY_KEY_REUSED`. No se ejecuta nada |
| Repetición mientras la primera sigue en curso | `409 IDEMPOTENCY_REQUEST_IN_PROGRESS`. El cliente reintenta después |
| Qué se guarda | `201` y los `404`/`422` de reglas de negocio (pasos 10 en adelante). **No** se guardan `400`, `401`, `403` ni `500`: el cliente puede corregir y reintentar con la misma key |
| Vigencia | 24 horas desde la primera petición. Después, la key se puede usar como nueva |

**Consecuencia importante:** un rechazo de negocio es la respuesta definitiva de esa key. Si una recarga se rechazó por el tope diario a las 23:50 UTC, repetirla con **la misma key** a las 00:10 vuelve a dar el mismo 422 (con `Idempotent-Replayed: true`). Para intentarlo de nuevo hay que usar **otra key**, porque es otra operación.

Cómo funciona (todo en una transacción):

```
BEGIN
  pg_try_advisory_xact_lock(usuario, endpoint, key)  → ocupado: 409
  buscar la key (últimas 24 h)                        → misma petición: repetir · distinta: 422
  validar el cuerpo (pasos 5–9)                       → 400, sin guardar la key
  SAVEPOINT op
     bloqueos, reglas, UPDATE de saldos, INSERT de movimientos
     ├─ éxito            → guardar la key con los ids de los movimientos
     └─ error de negocio → ROLLBACK TO SAVEPOINT op → guardar la key con el código de error
COMMIT
```

Ejemplo:

```http
POST /transfers   (Idempotency-Key: 3f0c…)      → 201 Created            (se transfiere)
POST /transfers   (misma key, mismo cuerpo)     → 201 Created            (no se transfiere de nuevo)
                                                  Idempotent-Replayed: true
POST /transfers   (misma key, amount distinto)  → 422 IDEMPOTENCY_KEY_REUSED
```

---

## 14. Límite de intentos de login

| Punto | Regla |
|---|---|
| Qué se cuenta | Cada login con credenciales incorrectas suma un intento fallido **para ese email**, exista o no la cuenta |
| Límite | 5 intentos fallidos en una ventana móvil de **15 minutos** |
| Al superarlo | `429 TOO_MANY_REQUESTS` con `Retry-After: <segundos>`. Se rechaza **incluso con la contraseña correcta**, y estos rechazos no suman intentos |
| Desbloqueo | Automático, cuando el intento fallido más antiguo de la ventana cumple 15 minutos |
| Login exitoso | Reinicia el contador del email |
| Email | Se compara en minúsculas y sin espacios al inicio ni al final |
| Almacenamiento | En la BD (tabla `login_attempts`), compartido por todas las instancias. Los logins de un mismo email se atienden de a uno (advisory lock), así el límite es exacto aunque lleguen a la vez |

---

## 15. Catálogo de errores

Todos los errores tienen el mismo cuerpo `{ "code", "message" }` y `Content-Type: application/json; charset=utf-8`. Los errores internos no exponen detalles de la BD ni trazas (se registran en el log del servidor).

### Criterio de códigos HTTP

| HTTP | Se usa cuando |
|---|---|
| 400 | La petición está mal formada o viola una validación de entrada |
| 401 | Falta autenticación o las credenciales/tokens no son válidos |
| 403 | El usuario está autenticado, pero su rol no le permite usar el endpoint |
| 404 | El recurso o la ruta no existe, o el recurso existe pero el usuario no puede verlo |
| 405 | El método HTTP no está soportado en esa ruta |
| 409 | Email ya registrado, o petición en curso con la misma `Idempotency-Key` |
| 422 | La petición es válida, pero **viola una regla de negocio** |
| 429 | Demasiados intentos de login |
| 500 | Error inesperado del servidor |

### Catálogo completo

| `code` | HTTP | `message` | Cuándo ocurre | Funcionalidades |
|---|---|---|---|---|
| `BAD_REQUEST` | 400 | `Invalid request body` | JSON mal formado, campo faltante, `null` o con tipo incorrecto; `Content-Type` incorrecto; cuerpo > 16 KB; claves duplicadas | F-01 a F-05, F-08, F-09 |
| `BAD_REQUEST` | 400 | `Invalid name` | Nombre vacío o de más de 100 caracteres | F-01 |
| `BAD_REQUEST` | 400 | `Invalid email` | Email con formato inválido o de más de 254 caracteres | F-01 |
| `BAD_REQUEST` | 400 | `Password too short` | Contraseña de menos de 8 caracteres | F-01 |
| `BAD_REQUEST` | 400 | `Password too long` | Contraseña de más de 128 caracteres | F-01 |
| `BAD_REQUEST` | 400 | `Invalid idempotency key` | `Idempotency-Key` ausente o sin formato UUID | F-08, F-09 |
| `BAD_REQUEST` | 400 | `Amount exceeds maximum` | `amount` > 1000000000000 | F-08, F-09 |
| `BAD_REQUEST` | 400 | `Invalid account type` | `accountType` distinto de `savings` o `checking` | F-05 |
| `BAD_REQUEST` | 400 | `Invalid account id` | `id` en la ruta no es un UUID | F-07, F-10 |
| `BAD_REQUEST` | 400 | `Invalid account number` | Número de cuenta sin 10 dígitos | F-08, F-09 |
| `BAD_REQUEST` | 400 | `Invalid recharge type` | Tipo distinto de `cash`, `employer_bonus`, `salary` | F-08 |
| `BAD_REQUEST` | 400 | `Amount must be greater than zero` | `amount` ≤ 0 | F-08, F-09 |
| `BAD_REQUEST` | 400 | `Cannot transfer to the same account` | Origen igual al destino | F-09 |
| `BAD_REQUEST` | 400 | `Invalid pagination` | `page` fuera de 1–10000 o no entero | F-10 |
| `UNAUTHORIZED` | 401 | `Missing or invalid token` | Sin access token, token inválido o vencido | F-04 a F-10 |
| `INVALID_CREDENTIALS` | 401 | `Invalid email or password` | Login fallido | F-02 |
| `INVALID_REFRESH_TOKEN` | 401 | `Invalid refresh token` | Refresh token vencido, revocado, inexistente o de otro usuario | F-03, F-04 |
| `FORBIDDEN` | 403 | `Access denied` | Rol no permitido para el endpoint | F-05, F-06, F-08, F-09 |
| `NOT_FOUND` | 404 | `Account not found` | La cuenta no existe, o es de otro cliente | F-07 a F-10 |
| `ROUTE_NOT_FOUND` | 404 | `Route not found` | La ruta no existe | Todas |
| `METHOD_NOT_ALLOWED` | 405 | `Method not allowed` | Método no soportado (incluye el encabezado `Allow`) | Todas |
| `EMAIL_ALREADY_REGISTERED` | 409 | `Email already registered` | Registro con un email existente | F-01 |
| `IDEMPOTENCY_REQUEST_IN_PROGRESS` | 409 | `Request with this idempotency key is in progress` | La primera petición con esa key aún no termina | F-08, F-09 |
| `IDEMPOTENCY_KEY_REUSED` | 422 | `Idempotency key already used with a different request` | La key ya se usó con otros datos | F-08, F-09 |
| `INSUFFICIENT_FUNDS` | 422 | `Insufficient funds` | El saldo no alcanza considerando el sobregiro | F-09 |
| `CASH_LIMIT_EXCEEDED` | 422 | `Cash recharge limit exceeded` | Recarga `cash` mayor a 300000 | F-08 |
| `DAILY_RECHARGE_COUNT_EXCEEDED` | 422 | `Daily recharge count exceeded` | La cuenta ya tiene 5 recargas en el día | F-08 |
| `DAILY_RECHARGE_AMOUNT_EXCEEDED` | 422 | `Daily recharge amount exceeded` | La recarga haría superar 300000000 en el día | F-08 |
| `BALANCE_LIMIT_EXCEEDED` | 422 | `Balance limit exceeded` | El saldo resultante superaría 1000000000000000 | F-08, F-09 |
| `TOO_MANY_REQUESTS` | 429 | `Too many login attempts` | 5 intentos fallidos de login en 15 minutos (con `Retry-After`) | F-02 |
| `INTERNAL_ERROR` | 500 | `Internal error` | Fallo inesperado (BD caída, `panic` en un handler…) | Todas |

---

## 16. Reglas de interoperabilidad (I-01 a I-15)

Fijan comportamientos que cada lenguaje o librería resolvería distinto por defecto. Cualquier implementación del contrato debe cumplirlas.

| ID | Tema | Regla |
|---|---|---|
| I-01 | `amount` | Número JSON con valor entero exacto entre 1 y 1000000000000. `150000.0` y `1e3` se aceptan; `150000.5`, `"150000"`, `true` o `null` → `Invalid request body` |
| I-02 | Saldo máximo | El saldo nunca supera 1000000000000000; toda suma se verifica antes de aplicarse |
| I-03 | Campos del cuerpo | Desconocidos se ignoran. Obligatorio ausente o `null`, o con tipo JSON incorrecto → `Invalid request body`. Texto fuera de la lista → su error específico (`Invalid account type`, `Invalid recharge type`) |
| I-04 | Números de cuenta | Siempre texto de exactamente 10 dígitos ASCII, sin recortar espacios. Como número JSON → `Invalid request body`; texto con otro formato → `Invalid account number` |
| I-05 | Textos del registro | Longitudes en caracteres Unicode. `name` recortado, 1–100. `email` recortado, en minúsculas, ≤ 254, `^[^\s@]+@[^\s@]+\.[^\s@]+$`. `password` sin recortar, 8–128 |
| I-06 | Formato de la petición | `Content-Type: application/json` (acepta `; charset=utf-8`). Sin él, cuerpo vacío, JSON inválido, > 16 KB o raíz que no es objeto → `Invalid request body` |
| I-07 | Rutas y métodos | Ruta inexistente → 404 `ROUTE_NOT_FOUND`; método no soportado → 405 `METHOD_NOT_ALLOWED`; siempre `{code, message}` |
| I-08 | Access token | JWT solo HS256 (cualquier otro algoritmo, incluido `none`, se rechaza). Claims obligatorias `sub`, `role`, `iat`, `exp`. Sin tolerancia de reloj. `Bearer` sin distinguir mayúsculas. Cualquier fallo → 401 `UNAUTHORIZED` |
| I-09 | Fechas | ISO 8601 UTC con exactamente 3 decimales y `Z` |
| I-10 | Fuente de la hora | La de la BD para `createdAt`, el día de los topes y el vencimiento de refresh tokens. El `exp` del JWT se valida con la hora del servidor |
| I-11 | Orden de las listas | Historial: `createdAt` ↓, empate por `id` ↓. Cuentas: `createdAt` ↑, empate por `id` ↑ |
| I-12 | Parámetro `page` | Ausente → 1. Presente: entero decimal sin signo ni decimales, 1–10000. `0`, `1.5`, `-1`, `abc`, vacío → `Invalid pagination` |
| I-13 | Codificación | UTF-8. Respuestas con `Content-Type: application/json; charset=utf-8`; la 204 sin cuerpo |
| I-14 | Nombres y claves duplicadas | Los nombres distinguen mayúsculas (`"Amount"` no es `"amount"`). Un objeto con clave repetida → `Invalid request body` |
| I-15 | UUID | Formato canónico `8-4-4-4-12` en hexadecimal; mayúsculas aceptadas y normalizadas. Sin guiones, entre llaves, `urn:uuid:` o con espacios → `Invalid account id` / `Invalid idempotency key` |

La lista de pruebas que cubren cada regla está en `VERIFICACION-INTEROPERABILIDAD.md`.

---

## 17. Modelo de datos

Migraciones en `migrations/` (se aplican solas al arrancar; tabla de control `schema_migrations`). Todas las fechas se guardan con precisión de milisegundos y la hora de la BD.

| Tabla | Contenido | Restricciones destacadas |
|---|---|---|
| `users` | Usuarios (`customer` y `operator`) | `email` único (guardado en minúsculas); `role IN ('customer','operator')`; `password_hash` argon2id |
| `refresh_tokens` | Refresh tokens emitidos | Solo el **SHA-256** del token (`token_hash` único); `expires_at` = creación + 7 días; `revoked_at` al rotar o cerrar sesión |
| `login_attempts` | Intentos fallidos de login por email | Índice `(email, created_at)`; un login exitoso los borra |
| `accounts` | Cuentas | `number` único de 10 dígitos; `kind IN ('savings','checking')`; `CHECK (balance_cents >= -overdraft_limit_cents)`; `CHECK (balance_cents <= 1e15)` |
| `movements` | Log de movimientos (solo se agrega) | `amount_cents` entre 1 y 1e12; `recharge_type` solo en recargas; `related_account_id` solo en transferencias; índice `(account_id, created_at DESC, id DESC)` |
| `idempotency_keys` | Resultado de cada petición idempotente | PK `(user_id, endpoint, key)`; guarda `movement_ids` (éxito) **o** `error_code` (rechazo), nunca ambos |

Los `CHECK` de saldo son la última defensa: aunque el código fallara, la BD no aceptaría un saldo fuera de rango.

---

## 18. Concurrencia y garantías

| Situación | Cómo se resuelve |
|---|---|
| Dos transferencias cruzadas (A→B y B→A) | `SELECT … FOR UPDATE` de ambas cuentas en orden de `id` ascendente: sin deadlocks |
| Gastar el mismo saldo dos veces | La cuenta origen queda bloqueada hasta el `COMMIT`; la siguiente operación ve el saldo actualizado |
| Topes diarios con recargas simultáneas | Se bloquea la cuenta **antes** de contar las recargas del día (RN-20) |
| Misma `Idempotency-Key` en paralelo | `pg_try_advisory_xact_lock`: la segunda recibe 409 en lugar de esperar |
| Dos refresh con el mismo token | `FOR UPDATE` de la fila del token; solo uno lo rota, el otro recibe 401 |
| Intentos de login simultáneos | `pg_advisory_xact_lock` por email: el límite de 5 es exacto |
| Cálculo de hashes argon2 | Semáforo de un hash por CPU: cada hash usa ~19 MiB |
| Muchas peticiones a la vez | Pool de máximo **25 conexiones** a PostgreSQL; las peticiones esperan una libre en vez de fallar |
| `panic` en un handler | El middleware `Recover` responde `500 INTERNAL_ERROR` y registra la pila |
| Apagado (`Ctrl+C`, `docker stop`) | Deja de aceptar conexiones y espera hasta 10 s a que terminen las peticiones en curso |

Logs: cada petición registra método, ruta, estado y duración. **Nunca** se registran cuerpos, la query, tokens ni el encabezado `Authorization`.

---

## 19. Documentos relacionados

| Documento | Ubicación | Contenido |
|---|---|---|
| `postman/accounts-api.postman_collection.json` | Este repositorio | Colección de Postman con pruebas automáticas del flujo completo |
| `PLAN-DE-TRABAJO.md` | Este repositorio | Iteraciones, tareas, mediciones y estado del proyecto |
| `VERIFICACION-INTEROPERABILIDAD.md` | Este repositorio | Pruebas que cubren cada regla I-01 a I-15 |
| `openapi.yaml` | Carpeta superior | Contrato HTTP formal (esquemas, códigos y ejemplos) |
| `funcionalidades-y-errores.md` | Carpeta superior | Especificación funcional sin tecnología (fuente de las secciones 9 a 16) |
| `bank-lab-spec.md` | Carpeta superior | Especificación completa: modelo de datos, orden de validaciones, idempotencia |
| `arquitectura-go.md` | Carpeta superior | Decisiones de arquitectura de esta implementación |
