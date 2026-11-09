# Verificación de interoperabilidad (T7.7)

Revisión de las reglas I-01 a I-15 (`../funcionalidades-y-errores.md`, sección 7) sobre F-01 a F-10.
Cada fila indica la prueba automatizada que la cubre. Todas pasan con:

```
set -a; source .env; set +a
go test -race ./...
```

Revisado el 2026-11-09 (fin de I7). La suite de caja negra contra `openapi.yaml` queda para la iteración IF.

| ✔ | Regla | Qué se comprueba | Pruebas |
|---|---|---|---|
| ✅ | **I-01** `amount` entero | `150000.0` y `1e3` se aceptan; `150000.5`, `"150000"`, `true`, `null` → 400 `Invalid request body`; enteros enormes se saturan → `Amount exceeds maximum` | `handler/respond_test.go` `TestDecodeObjectFields`; `handler/operation_handler_test.go` `TestRechargeEndpointErrors` (casos "8 decimal amount", "9 huge amount"); `TestRechargeEndpoint` (`150000.0` repite la misma petición) |
| ✅ | **I-02** Saldo máximo | Ninguna suma supera 1e15: recarga y destino de transferencia → `BALANCE_LIMIT_EXCEEDED`; el límite exacto se permite | `model/account_test.go` `TestDeposit`; `service/recharge_service_test.go` `TestRechargeValidationOrder` (paso 14); `service/transfer_service_test.go` `TestTransferValidationOrder` (paso 13) |
| ✅ | **I-03** Campos del cuerpo | Desconocidos se ignoran; ausente o `null` → `Invalid request body`; tipo JSON incorrecto → `Invalid request body`; texto fuera de la lista → error específico | `handler/respond_test.go` `TestDecodeObjectKeys`, `TestDecodeObjectFields`, `TestDecodeObjectStringType`; `handler/account_handler_test.go` `TestCreateAccountEndpoint` (`accountType: 1` vs `"Savings"`); `TestRechargeEndpointErrors` (paso 7) |
| ✅ | **I-04** Números de cuenta | Texto de 10 dígitos ASCII, sin recortar; como número JSON → `Invalid request body`; otro formato → `Invalid account number` | `service/validate_test.go` `TestValidateAccountNumber`; `TestRechargeEndpointErrors` (casos "6 …"); `TestRechargeValidationOrder` ("6 number with spaces"); `TestTransferValidationOrder` (paso 6) |
| ✅ | **I-05** Textos del registro | Longitudes en code points; `name` recortado 1–100; `email` recortado, en minúsculas, ≤ 254 y con formato; `password` sin recortar 8–128 | `service/validate_test.go` `TestNormalizeName`, `TestNormalizeEmail`, `TestValidatePassword`; `service/auth_service_test.go` `TestRegisterNormalizesAndHashes` |
| ✅ | **I-06** Formato de la petición | `Content-Type` JSON (con o sin `charset=utf-8`), cuerpo vacío, JSON roto, > 16 KB, raíz no objeto, datos después del objeto → 400 | `handler/respond_test.go` `TestDecodeObjectRequestFormat` |
| ✅ | **I-07** Rutas y métodos | Ruta inexistente → 404 `ROUTE_NOT_FOUND`; método no soportado → 405 `METHOD_NOT_ALLOWED` con `Allow`; siempre `{code, message}` | `handler/middleware_test.go` `TestJSONNotFound`, `TestJSONNotFoundKeepsAllowHeader`; `handler/auth_handler_test.go` `TestAuthRoutesMethodNotAllowed` |
| ✅ | **I-08** Access token | Solo HS256 (rechaza `none` y otro secreto); `sub`, `role`, `iat`, `exp` obligatorias; sin tolerancia; `iat` futuro rechazado; `Bearer` sin distinguir mayúsculas | `service/tokens_test.go` `TestIssueClaims`, `TestValidateAccepts`, `TestValidateRejects`; `handler/middleware_test.go` `TestAuthRejects`, `TestAuthValidToken` |
| ✅ | **I-09** Formato de fechas | UTC, 3 decimales, sufijo `Z`, aunque la hora venga en otra zona | `handler/dto_test.go` `TestFormatTime`, `TestMovementResponseJSON`; `createdAt` validado con `isoMillis` en las pruebas de endpoints |
| ✅ | **I-10** Fuente de la hora | `createdAt`, vencimiento de refresh tokens y día de los topes usan `now()` de la BD; `exp` del JWT usa la hora del servidor | `repository/user_repo_test.go` `TestCreateUser`, `TestCreateRefreshToken`, `TestLockActiveRefreshToken` (vencido); `repository/movement_repo_test.go` `TestDailyRecharges` (medianoche UTC − 1 ms); `service/tokens_test.go` `TestValidateRejects` (reloj inyectado) |
| ✅ | **I-11** Orden de las listas | Cuentas: `createdAt` ↑, empate por `id` ↑. Historial: `createdAt` ↓, empate por `id` ↓ | `repository/account_repo_test.go` `TestListAccountsByOwner`; `service/account_service_test.go` `TestListAccounts`; `repository/movement_repo_test.go` `TestListMovementPage` (fechas repetidas) |
| ✅ | **I-12** Parámetro `page` | Ausente → 1; `0`, `1.5`, `-1`, `+5`, `abc`, vacío, > 10000 → `Invalid pagination`; página posterior a la última → `items: []` | `service/validate_test.go` `TestParsePage`; `service/account_service_test.go` `TestAccountMovements`; `handler/account_handler_test.go` `TestListMovementsEndpoint` |
| ✅ | **I-13** Codificación | Respuestas con `Content-Type: application/json; charset=utf-8`; la 204 sin cuerpo | `handler/respond_test.go` `TestWriteJSON` y `assertErrorBody` (en todas las pruebas de error); `handler/auth_handler_test.go` `TestLogoutEndpoint` (204 sin cuerpo) |
| ✅ | **I-14** Nombres y claves duplicadas | Claves con otras mayúsculas se ignoran (falta el campo → 400); clave repetida → 400 | `handler/respond_test.go` `TestDecodeObjectKeys` |
| ✅ | **I-15** Formato de UUID | Solo 8-4-4-4-12; mayúsculas aceptadas y normalizadas (id de cuenta y `Idempotency-Key`); sin guiones → 400 | `service/validate_test.go` `TestNormalizeUUID`; `service/account_service_test.go` `TestGetAccount`; `handler/account_handler_test.go` `TestGetAccountEndpoint`; `handler/operation_handler_test.go` `TestRechargeEndpoint` (key en mayúsculas = misma key), `TestRechargeEndpointErrors` (key sin guiones) |
