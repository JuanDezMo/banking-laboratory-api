# Plan de trabajo: backend en Go

Plan para implementar la API del laboratorio bancario en Go, en esta carpeta (`go_accounts_api/`).

**Referencias** (en la carpeta superior):

| Documento | Uso en este plan |
|---|---|
| `../funcionalidades-y-errores.md` | Qué debe hacer cada endpoint (F-01 a F-10), reglas RN-01 a RN-31, interoperabilidad I-01 a I-14 |
| `../openapi.yaml` | Contrato HTTP exacto (JSON, códigos, errores) |
| `../bank-lab-spec.md` | Modelo de datos (sección 3), orden de validaciones (sección 6), catálogo de errores (7.2), idempotencia (8.1) |
| `../arquitectura-go.md` | Stack, capas, estructura de archivos y patrones |

---

## 0. Forma de trabajo

### 0.1 Reglas de las tareas

- **Corta:** cada tarea dura **2 horas o menos**. Si se pasa, se divide antes de seguir.
- **Medible:** cada tarea tiene una columna **Medición**: un comando y el resultado esperado. La tarea está terminada solo cuando la medición se cumple.
- **Estimación:** XS ≤ 1 h · S ≤ 2 h.

### 0.2 Definición de terminado (aplica a todas las tareas)

- [ ] `gofmt -l .` no lista archivos y `go vet ./...` no da advertencias.
- [ ] `go build ./...` y `go test ./...` pasan.
- [ ] La medición de la tarea se cumple.

### 0.3 Cierre de iteración

- Claude no hace commits. Al terminar una iteración que genera valor (se cumple su **criterio de salida**), Claude entrega un resumen de lo hecho y avisa para que el usuario aplique el commit.
- Cada iteración termina con un **indicador**: tareas cerradas / tareas planeadas, y cuántas mediciones fallaron a la primera.

### 0.4 Pruebas durante las iteraciones

- **Unitarias** para todo lo que no toca la BD (`model`, `validate.go`, `decodeObject`, JWT).
- **De integración** (handler + service + BD real con `httptest`) contra `TEST_DATABASE_URL` (BD `bank_test`). `internal/testdb` crea un esquema limpio por paquete y aplica las migraciones; sin `TEST_DATABASE_URL` la prueba **falla**. Antes de `go test ./...`: `set -a; source .env; set +a`.
- La **suite de caja negra** completa contra `openapi.yaml` queda para la iteración final (IF).

### 0.5 Resumen

| Iteración | Objetivo | Tareas | Horas máx. |
|---|---|---|---|
| I0 | ✅ Entorno: BD y API en Docker, proyecto que compila y conecta | 10 | 14 |
| I1 | ✅ Base común: errores, respuestas, `decodeObject`, validaciones | 12 | 19 |
| I2 | ✅ Registro y login (F-01, F-02) | 13 | 23 |
| I3 | ✅ Sesión: refresh, auth, logout (F-03, F-04) | 7 | 11 |
| I4 | Cuentas (F-05 a F-07) | 7 | 10 |
| I5 | Recarga con idempotencia (F-08 sin topes) | 10 | 17 |
| I6 | Transferencias (F-09) | 8 | 13 |
| I7 | Topes diarios e historial (F-08 completa, F-10) | 7 | 12 |
| I8 | Interfaces y pruebas sin BD | 5 | 9 |
| I9 | Opcional: extensiones | 5 | 10 |
| IF | Verificación del contrato (caja negra) | 8 | 16 |

---

## I0: Entorno

**Objetivo:** BD en Docker y proyecto que compila, conecta y aplica migraciones.

| ID | Tarea | Est. | Medición |
|---|---|---|---|
| T0.1 | ✅ Instalar Go (estable vigente, mínimo 1.22) | XS | `go version` muestra ≥ 1.22 |
| T0.2 | ✅ `docker-compose.yml`, servicio `db`: imagen oficial `postgres:16-alpine`, reloj en UTC, `healthcheck`, volumen `pgdata`, script `docker/postgres/init/` que crea `bank_test` | S | `docker compose config` sin errores |
| T0.3 | ✅ `.env.example`, `.gitignore` | XS | `docker compose up -d` → `docker inspect -f '{{.State.Health.Status}}' accounts-api-db` = `healthy` |
| T0.4 | ✅ Verificar la BD: zona horaria y BD de pruebas | XS | `docker exec accounts-api-db psql -U bank -Atc "show timezone"` = `UTC`; existen `bank` y `bank_test` |
| T0.5 | ✅ `go mod init accounts-api`; `go get` de pgx/v5, x/crypto, jwt/v5; anotar versiones en `arquitectura-go.md` | XS | `go.mod` lista las 3 dependencias con versión exacta |
| T0.6 | ✅ Estructura de carpetas de `arquitectura-go.md` §3.3 (archivos con su `package`) | XS | `go build ./...` termina sin errores |
| T0.7 | ✅ `main.go`: cargador propio de `.env` (`env.go`, no pisa variables ya definidas), leer `DATABASE_URL` y `JWT_SECRET` (≥ 32 caracteres), abrir `*sql.DB` con `pgx`, `Ping` | S | Sin `JWT_SECRET` → código 1 y el mensaje; con `.env` → log `database connected` |
| T0.8 | ✅ Crear la tabla `schema_migrations` al arrancar (`CREATE TABLE IF NOT EXISTS`). El ejecutor con `embed` pasa a T2.0 | XS | Dos arranques seguidos sin error; la tabla existe |
| T0.9 | ✅ `http.Server` con timeouts y apagado ordenado (`signal.NotifyContext`) | S | `curl -i localhost:3000/x` responde; `Ctrl+C` → log `server stopped` y código 0 |
| T0.10 | ✅ `Dockerfile` de la API (multi-etapa, imagen final `alpine` con usuario sin root) y servicio `api` en `docker-compose.yml` (depende de `db` en estado `healthy`) | S | `docker compose up -d --build` levanta `api` y `db`; `curl -i localhost:3000/x` responde |

**Criterio de salida:** desde cero, `docker compose up -d && go run .` arranca sin errores en menos de 1 minuto.

**Conceptos de Go:** módulos, paquetes, `main`, `database/sql`, `embed`, `context`, `defer`.

---

## I1: Base común

**Objetivo:** las piezas que usan todos los endpoints, cubiertas por pruebas unitarias.

| ID | Tarea | Est. | Medición |
|---|---|---|---|
| T1.1 | ✅ `model/errors.go`: tipo `AppError` (`Code`, `Message`); `model` no conoce HTTP | XS | Compila; prueba: `errors.As` recupera el `AppError` envuelto con `%w` |
| T1.2 | ✅ Variables del catálogo spec 7.2 + `LoginBlockedError` (envuelve `ErrTooManyLoginAttempts`) | S | Prueba de tabla: cada variable tiene `code` y `message` iguales al spec (31 filas) |
| T1.3 | ✅ `writeJSON` y `writeError`: tabla `code → estado HTTP` en el handler, `500` sin detalles + `slog.Error`, `Retry-After` | S | Prueba con `httptest.NewRecorder`: estado de cada código del catálogo, `Content-Type: application/json; charset=utf-8`, cuerpo `{code, message}` |
| T1.4 | ✅ `decodeObject` v1: `Content-Type`, límite de 16 KB, JSON mal formado, raíz objeto, nada después del objeto (I-06) | S | 11 pruebas en verde (sin Content-Type, otro charset, 17 KB, JSON roto, vacío, raíz arreglo, datos extra…) |
| T1.5 | ✅ `decodeObject` v2: claves duplicadas → error (I-14); claves desconocidas o con otras mayúsculas se ignoran (I-03) | S | 4 pruebas en verde: duplicada → 400; desconocida y `Name` → se ignoran |
| T1.6 | ✅ `decodeObject` v3: lectura explícita `body.String("x")` y `body.Int64("x")`, con `null`/faltantes y enteros (I-01, I-03) | S | Pruebas en verde: `null`, faltante, `1.5`, `"150000"`, `true` → 400; `150000.0`, `1e3` → aceptados; enteros enormes se saturan |
| T1.7 | ✅ Middleware `Logging` (sin cuerpos, query ni tokens) | XS | Prueba: la salida del log no contiene `Authorization`, el token ni el cuerpo |
| T1.8 | ✅ Middleware `JSONNotFound`: 404 y 405 en JSON (I-07) | S | 3 pruebas: ruta inexistente → 404 JSON; método no permitido → 405 JSON con `Allow`; un 404 propio no cambia |
| T1.9 | ✅ `validate.go`: email y nombre (`utf8.RuneCountInString`) | S | Prueba de tabla ≥ 8 casos (límites de longitud, acentos, vacío, espacios Unicode) |
| T1.10 | ✅ `validate.go`: contraseña y número de cuenta (10 dígitos) | XS | Prueba de tabla ≥ 6 casos |
| T1.11 | ✅ `validate.go`: UUID canónico (I-15) y `page` (I-12) | XS | Prueba de tabla ≥ 6 casos (`page=0`, `page=+5`, UUID en mayúsculas → minúsculas, sin guiones → 400) |
| T1.12 | ✅ Formato de fecha `2006-01-02T15:04:05.000Z` en UTC (I-09), en `dto.go` | XS | Prueba: hora con zona `-05:00` se imprime en `Z` con 3 decimales |

**Criterio de salida:** `go test ./internal/... -cover` ≥ 80 % en `model` y en `handler/respond.go`.

**Conceptos de Go:** structs, `errors.Is`/`As`, `encoding/json`, middleware, pruebas de tabla.

---

## I2: Registro y login

**Objetivo:** F-01 y F-02 completos, con límite de intentos.

| ID | Tarea | Est. | Medición |
|---|---|---|---|
| T2.0 | ✅ Ejecutor de migraciones: `migrations/embed.go` (`//go:embed *.sql`), aplica en orden los no aplicados, una transacción por archivo; reemplaza el `CREATE TABLE` de `main.go` | S | Dos arranques seguidos: `select count(*) from schema_migrations` no cambia en el segundo |
| T2.1 | ✅ Migración `001_auth_accounts.sql`: tablas `users` y `refresh_tokens` | S | `\d users` en `psql` muestra los `CHECK` y el `UNIQUE` del spec 3.1 |
| T2.2 | ✅ Misma migración: `login_attempts` y `accounts` con índices y fechas en milisegundos | S | `\d accounts` coincide con spec 3.4; `select now()` insertado tiene 3 decimales |
| T2.3 | ✅ `user_repo`: crear usuario; `UNIQUE` → `ErrEmailAlreadyRegistered` | S | Prueba de integración: el segundo insert del mismo email devuelve ese error |
| T2.4 | ✅ `user_repo`: buscar por email (no existe → `nil`, sin error) | XS | Prueba de integración: encontrado y no encontrado |
| T2.5 | ✅ argon2: generar hash PHC con sal aleatoria | S | Prueba: dos hashes de la misma contraseña son distintos y empiezan con `$argon2id$` |
| T2.6 | ✅ argon2: verificar con `subtle.ConstantTimeCompare` + semáforo de hashes simultáneos | S | Prueba: correcta → true, incorrecta → false; 10 goroutines no superan el límite del semáforo |
| T2.7 | ✅ **F-01** service de registro en el orden de validación del spec 6 | S | Prueba de integración: 1 caso por error de F-01 en el orden del spec |
| T2.8 | ✅ **F-01** handler `POST /auth/register` | XS | `curl` → `201` con el JSON de `openapi.yaml`; repetido → `409` |
| T2.9 | ✅ JWT: emitir (`sub`, `role`, `iat`, `exp` = 15 min) | XS | Prueba: decodificar el token muestra los 4 claims y `exp − iat = 900` |
| T2.10 | ✅ JWT: validar con `WithValidMethods`, `WithExpirationRequired`, `WithIssuedAt`, `WithLeeway(0)` (I-08) | S | 3 pruebas rechazadas: vencido, otro secreto, `alg: none` |
| T2.11 | ✅ Límite de login en el repo: contar fallidos en 15 min (hora de la BD), registrar, limpiar | S | Prueba de integración: 5 fallidos → contador = 5; login correcto → 0 |
| T2.12 | ✅ **F-02** `POST /auth/login` con refresh token (32 bytes `crypto/rand`, se guarda el SHA-256, 7 días) | S | `curl` correcto → `200` con el par de tokens; 6.º fallido → `429` + `Retry-After`; en BD solo hay el hash |

**Criterio de salida:** todos los errores de F-01 y F-02 del catálogo se reproducen con `curl` y coinciden con `openapi.yaml`.

**Conceptos de Go:** `crypto/*`, `crypto/rand`, goroutines y canales como semáforo.

---

## I3: Sesión

**Objetivo:** F-03 y F-04, y la protección de rutas.

| ID | Tarea | Est. | Medición |
|---|---|---|---|
| T3.1 | ✅ `user_repo` (refresh tokens, según `arquitectura-go.md` §3.3): buscar por hash con `FOR UPDATE`, revocar, insertar | S | Prueba de integración: un token revocado no se vuelve a encontrar como válido |
| T3.2 | ✅ **F-03** `POST /auth/refresh`: rotación en una transacción | S | `curl`: el refresh devuelve un par nuevo; reutilizar el anterior → `401` |
| T3.3 | ✅ Middleware `Auth`: usuario en el `context` | S | 3 pruebas: sin token → 401; token inválido → 401; válido → el handler lee el usuario |
| T3.4 | ✅ Función `requireRole` | XS | Prueba: `customer` en ruta de `operator` → 403 con el `code` del catálogo |
| T3.5 | ✅ **F-04** `POST /auth/logout` | XS | `curl`: logout → `204`; refresh con ese token → `401` |
| T3.6 | ✅ `seeds/operators.sql` con contraseña hasheada (idempotente) | XS | `psql "$DATABASE_URL" -f seeds/operators.sql` (sin `psql` local: `docker exec -i accounts-api-db psql -U bank -d bank < seeds/operators.sql`); login del operator → `200` con `role: operator` |
| T3.7 | ✅ Prueba de carrera: dos refresh simultáneos con el mismo token | S | Exactamente uno `200` y uno `401`, 10 ejecuciones seguidas |

**Criterio de salida:** F-03 y F-04 completos; la prueba T3.7 pasa con `go test -race`.

**Conceptos de Go:** `context.WithValue`, transacciones con `defer tx.Rollback()`.

---

## I4: Cuentas

**Objetivo:** F-05 a F-07.

| ID | Tarea | Est. | Medición |
|---|---|---|---|
| T4.1 | `account_repo`: crear con número aleatorio de 10 dígitos; reintento ante `UNIQUE` (máx. 5) | S | Prueba con generador falso que repite número: reintenta y crea; 5 colisiones → error |
| T4.2 | `account_repo`: buscar por id | XS | Prueba de integración: existe / no existe |
| T4.3 | `account_repo`: listar por dueño (orden I-11) | XS | Prueba: 3 cuentas creadas salen en el orden de I-11 |
| T4.4 | **F-05** `POST /accounts` | S | `curl` customer → `201`; operator → `403`; `accountType` inválido → `400` |
| T4.5 | **F-06** `GET /accounts` | XS | `curl` → `200` solo con las cuentas propias |
| T4.6 | **F-07** `GET /accounts/{id}` (ajena → 404, RN-24) | XS | `curl` propia → `200`; ajena → `404`; id no UUID → `400` |
| T4.7 | Prueba de integración del flujo: registro → login → crear → listar → consultar | S | La prueba pasa contra `bank_test` |

**Criterio de salida:** F-01 a F-07 completos; `go test ./...` en verde.

---

## I5: Recarga con idempotencia

**Objetivo:** F-08 sin topes diarios.

| ID | Tarea | Est. | Medición |
|---|---|---|---|
| T5.1 | Migración `002_movements_idempotency.sql`: `movements` | XS | `\d movements` coincide con spec 3.5 |
| T5.2 | Misma migración: `idempotency_keys` | XS | `\d idempotency_keys` coincide con spec 3.6 |
| T5.3 | `model`: `Movement`, `MovementType`, `RechargeType`; `Account.Deposit` con saldo máximo (RN-31) | S | Prueba de tabla: depósito que supera 1e15 → error |
| T5.4 | `movement_repo`: insertar y leer por ids | S | Prueba de integración: lo insertado se lee igual (fecha con 3 decimales) |
| T5.5 | `idempotency_repo`: buscar (24 h) y guardar resultado | S | Prueba: guardado se encuentra; con fecha de hace 25 h no |
| T5.6 | `idempotency_repo`: `TryLock` con `pg_try_advisory_xact_lock` | S | Prueba: dos transacciones con la misma key → la segunda obtiene `false` |
| T5.7 | Hash de la petición: SHA-256 de campos en orden fijo; cuerpo inválido → `"invalid"` | XS | Prueba: mismos campos en otro orden JSON → mismo hash |
| T5.8 | `RechargeService`: validaciones, transacción, savepoint | S | Prueba de integración: 1 caso por error de F-08 (sin topes) |
| T5.9 | **F-08** handler: rol, `Idempotency-Key`, `Idempotent-Replayed: true` | S | `curl` 2 veces con la misma key → mismo id; la 2.ª con `Idempotent-Replayed: true` |
| T5.10 | Pruebas: misma key con otro monto → `422`; key en curso → `409`; `cash` > 300000 → `422` | S | 3 pruebas de integración en verde |

**Criterio de salida:** repetir una recarga no cambia el saldo (`select balance` igual antes y después de la repetición).

**Conceptos de Go:** transacciones, savepoints, encabezados HTTP.

---

## I6: Transferencias

**Objetivo:** F-09 con bloqueos correctos.

| ID | Tarea | Est. | Medición |
|---|---|---|---|
| T6.1 | `Account.Withdraw` con sobregiro por tipo (RN-04, RN-05) | S | Prueba de tabla ≥ 6 casos (límite exacto, 1 centavo de más, por tipo) |
| T6.2 | `account_repo.ResolveNumbers`: números → ids | XS | Prueba: número inexistente → no encontrado |
| T6.3 | `account_repo.LockByIDs`: `FOR UPDATE` por id ascendente | S | Prueba: el SQL generado ordena por id; dos ids invertidos se bloquean en el mismo orden |
| T6.4 | `TransferService`: orden de validación (origen ajeno → 404) | S | Prueba de integración: 1 caso por error de F-09 en el orden del spec |
| T6.5 | `TransferService`: transacción, 2 movimientos, idempotencia | S | Prueba: tras transferir, existen `transferOut` y `transferIn` con el mismo monto |
| T6.6 | **F-09** handler con respuesta `{ transferOut, transferIn }` | XS | `curl` → `201` con el JSON de `openapi.yaml` |
| T6.7 | Prueba de concurrencia: transferencias cruzadas A→B y B→A en paralelo (`sync.WaitGroup`) | S | `go test -race -count=20 -run Concurrency` en verde, sin deadlock |
| T6.8 | Prueba de la invariante RN-17: saldo = suma de movimientos firmados | XS | La consulta de verificación devuelve 0 cuentas con diferencia tras T6.7 |

**Criterio de salida:** T6.7 y T6.8 pasan 20 veces seguidas.

**Conceptos de Go:** goroutines, `sync.WaitGroup`, detector de carreras.

---

## I7: Topes diarios e historial

**Objetivo:** completar F-08 (RN-18 a RN-20) y F-10.

| ID | Tarea | Est. | Medición |
|---|---|---|---|
| T7.1 | Repo: contar y sumar recargas del día UTC (`date_trunc('day', now() AT TIME ZONE 'UTC')`) | S | Prueba: 2 recargas hoy + 1 insertada ayer → cuenta 2 |
| T7.2 | `RechargeService`: pasos de validación de los topes (cantidad y monto) | S | Prueba: 6.ª recarga → `422`; recarga que supera 300000000 acumulado → `422` |
| T7.3 | Prueba: seis recargas simultáneas → solo 5 se aplican | S | `go test -race -count=10` en verde; saldo = 5 recargas |
| T7.4 | `movement_repo`: página de 20 (`LIMIT 20 OFFSET`, orden I-11) | XS | Prueba con 25 movimientos: página 1 = 20, página 2 = 5, página 3 = 0 |
| T7.5 | DTO de movimiento: `movementType` + `details` | S | Prueba: recarga y transferencia serializan igual que los ejemplos de `openapi.yaml` |
| T7.6 | **F-10** `GET /accounts/{id}/movements` | XS | `curl` → `{ page, pageSize: 20, items }`; cuenta ajena → `404` |
| T7.7 | Revisión de I-01 a I-14 sobre todo lo implementado | S | Lista de verificación con 14 filas marcadas, cada una con su prueba o `curl` |

**Criterio de salida:** F-01 a F-10 completos; `go test -race ./...` en verde.

---

## I8: Interfaces y pruebas sin BD

**Objetivo:** probar los services sin BD (DIP, ISP).

| ID | Tarea | Est. | Medición |
|---|---|---|---|
| T8.1 | Interfaces pequeñas en `AuthService` (solo los métodos que usa) | S | `go build ./...`; ningún repo concreto cambió (`git diff --stat repository/` vacío) |
| T8.2 | Interfaces en `AccountService` y `RechargeService` | S | Ídem T8.1 |
| T8.3 | Interfaces en `TransferService` | XS | Ídem T8.1 |
| T8.4 | Fakes en `service/fakes_test.go` | S | Compilan y cumplen las interfaces (`var _ X = (*fakeX)(nil)`) |
| T8.5 | Pruebas unitarias de los services con fakes | S | `go test ./internal/service/...` en verde **sin** la BD encendida (`docker compose stop db`) |

**Criterio de salida:** cobertura de `service` ≥ 70 % sin BD.

**Conceptos de Go:** interfaces implícitas, dobles de prueba.

---

## I9 (opcional): Extensiones

| ID | Tarea | Est. | Medición |
|---|---|---|---|
| T9.1 | Repositorio en memoria (`sync.Mutex`): usuarios y cuentas | S | Pasa las pruebas de T8.5 cambiando los fakes por él |
| T9.2 | Repositorio en memoria: movimientos e idempotencia | S | Ídem |
| T9.3 | Pruebas de contrato contra ambas implementaciones | S | La misma suite pasa con `memory` y `postgres` |
| T9.4 | `BANK_TIMEZONE` para el día de los topes | S | Prueba: con `America/Bogota`, recarga a las 23:30 locales cuenta para ese día |
| T9.5 | Bloqueo de cuentas: primero ampliar contrato y `openapi.yaml` | S | Documentos actualizados y aprobados antes de codificar |

---

## IF: Verificación del contrato

| ID | Tarea | Est. | Medición |
|---|---|---|---|
| TF.1 | Elegir la herramienta de caja negra (por ejemplo, Hurl) y estructurar la suite | S | `hurl --test tests/` corre (aunque vacío) |
| TF.2 | Casos de errores de autenticación (F-01 a F-04) | S | 1 caso por fila del catálogo, en verde |
| TF.3 | Casos de errores de cuentas (F-05 a F-07) | S | Ídem |
| TF.4 | Casos de errores de recarga (F-08) | S | Ídem |
| TF.5 | Casos de errores de transferencia e historial (F-09, F-10) | S | Ídem |
| TF.6 | Matriz de permisos (sin token, `customer`, `operator`) × 10 endpoints | S | 30 casos en verde |
| TF.7 | Casos I-01 a I-14 | S | 14 casos en verde |
| TF.8 | Validar respuestas contra los esquemas de `openapi.yaml` | S | 0 respuestas fuera de esquema |

**Criterio de salida:** la suite pasa completa. Es la misma que se usaría contra una implementación en Rust u otro lenguaje.

---

## Entorno local

```
go_accounts_api/
├─ docker/postgres/
│  └─ init/01-create-test-db.sql    # crea bank_test (solo la 1.ª vez)
├─ docker-compose.yml               # servicios api y db (imagen oficial)
├─ Dockerfile                       # API: compila con golang, ejecuta en alpine
├─ .dockerignore
├─ main.go, env.go                  # arranque y cargador de .env
├─ .env.example
└─ .gitignore
```

| Acción | Comando |
|---|---|
| Preparar variables | `cp .env.example .env` |
| Levantar solo la BD | `docker compose up -d db` |
| Correr la API en local | `go run .` (lee `.env`) |
| Levantar API + BD en Docker | `docker compose up -d --build` |
| Ver estado | `docker compose ps` (debe decir `healthy`) |
| Entrar a la BD | `docker exec -it accounts-api-db psql -U bank -d bank` |
| Detener | `docker compose stop` |
| Borrar todo (incluye datos) | `docker compose down -v` |

---

## Riesgos y pendientes

| Riesgo o pendiente | Mitigación |
|---|---|
| Versiones de librerías sin verificar | T0.5 las fija y registra en `arquitectura-go.md` |
| `decodeObject` es difícil para quien empieza | Dividida en 3 versiones (T1.4 a T1.6), sin `reflect`, cada una con sus pruebas |
| Los documentos dicen `001_init.sql` | Una migración por iteración; actualizar `arquitectura-go.md` en T2.0 |
| `01-create-test-db.sql` solo corre con el volumen vacío | Si `bank_test` no existe: `docker compose down -v` y volver a levantar |
| Bloqueo de cuentas no está en el contrato | T9.5 amplía primero el contrato |

---

## Trazabilidad

| Funcionalidad | Tareas |
|---|---|
| F-01 Registro | T2.1 a T2.8 |
| F-02 Login | T2.9 a T2.12 |
| F-03 Refresh | T3.1, T3.2, T3.7 |
| F-04 Logout | T3.3 a T3.5 |
| F-05 a F-07 Cuentas | I4 |
| F-08 Recarga | I5, T7.1 a T7.3 |
| F-09 Transferencia | I6 |
| F-10 Historial | T7.4 a T7.6 |
| I-01 a I-15 | T1.4 a T1.12, T2.10, T4.3, T7.4, T7.7 |
