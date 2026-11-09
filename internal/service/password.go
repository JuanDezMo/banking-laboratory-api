// Hash de contraseñas con argon2id (RN-25), en formato PHC.
package service

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Parámetros de argon2id: los mismos valores por defecto del crate argon2 de Rust
// (recomendación de OWASP), para que un hash sirva en cualquiera de las dos implementaciones.
const (
	argonMemoryKiB = 19456 // 19 MiB
	argonTime      = 2     // iteraciones
	argonThreads   = 1
	argonSaltLen   = 16
	argonKeyLen    = 32
)

// PasswordHasher genera y verifica hashes. Como cada hash usa unos 19 MiB de memoria,
// un semáforo limita cuántos se calculan a la vez.
type PasswordHasher struct {
	slots     chan struct{} // semáforo: un lugar por hash simultáneo
	dummyHash string        // para igualar el tiempo cuando el email no existe
}

// NewPasswordHasher crea el hasher con un máximo de maxConcurrent hashes simultáneos.
func NewPasswordHasher(maxConcurrent int) *PasswordHasher {
	h := &PasswordHasher{slots: make(chan struct{}, maxConcurrent)}
	h.dummyHash = h.Hash("dummy password used to equalize timing")
	return h
}

// Hash devuelve el hash en formato PHC: $argon2id$v=19$m=19456,t=2,p=1$<sal>$<hash>
func (h *PasswordHasher) Hash(password string) string {
	h.slots <- struct{}{}        // ocupa un lugar (espera si no hay)
	defer func() { <-h.slots }() // lo libera al terminar

	salt := make([]byte, argonSaltLen)
	rand.Read(salt) // desde Go 1.24, crypto/rand.Read nunca devuelve error
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)

	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key))
}

// Verify comprueba la contraseña contra un hash PHC, usando los parámetros guardados en él.
func (h *PasswordHasher) Verify(password, encoded string) (bool, error) {
	params, salt, key, err := parsePHC(encoded)
	if err != nil {
		return false, err
	}

	h.slots <- struct{}{}
	defer func() { <-h.slots }()

	candidate := argon2.IDKey([]byte(password), salt, params.time, params.memory, params.threads, uint32(len(key)))
	// Comparación en tiempo constante: no revela cuántos bytes coinciden.
	return subtle.ConstantTimeCompare(candidate, key) == 1, nil
}

// VerifyDummy hace el mismo trabajo que Verify contra un hash fijo. Se usa cuando el email
// no existe, para que la respuesta tarde lo mismo y no revele qué emails están registrados.
func (h *PasswordHasher) VerifyDummy(password string) {
	h.Verify(password, h.dummyHash)
}

type argonParams struct {
	memory  uint32
	time    uint32
	threads uint8
}

// parsePHC separa un hash "$argon2id$v=19$m=..,t=..,p=..$sal$hash" en sus partes.
func parsePHC(encoded string) (argonParams, []byte, []byte, error) {
	var p argonParams
	parts := strings.Split(encoded, "$") // ["", "argon2id", "v=19", "m=..,t=..,p=..", sal, hash]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return p, nil, nil, errors.New("unsupported password hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return p, nil, nil, errors.New("unsupported argon2 version")
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.time, &p.threads); err != nil {
		return p, nil, nil, fmt.Errorf("invalid argon2 parameters: %w", err)
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return p, nil, nil, fmt.Errorf("invalid salt: %w", err)
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return p, nil, nil, fmt.Errorf("invalid hash: %w", err)
	}
	return p, salt, key, nil
}
