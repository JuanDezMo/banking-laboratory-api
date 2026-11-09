package service

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPasswordHashFormat(t *testing.T) {
	h := NewPasswordHasher(2)
	first, second := h.Hash("S3gura2026"), h.Hash("S3gura2026")

	const prefix = "$argon2id$v=19$m=19456,t=2,p=1$"
	if !strings.HasPrefix(first, prefix) {
		t.Errorf("hash %q does not start with %q", first, prefix)
	}
	if first == second {
		t.Error("two hashes of the same password must differ (random salt)")
	}
}

func TestPasswordVerify(t *testing.T) {
	h := NewPasswordHasher(2)
	hash := h.Hash("S3gura2026")

	if ok, err := h.Verify("S3gura2026", hash); err != nil || !ok {
		t.Errorf("correct password: ok=%v err=%v", ok, err)
	}
	if ok, err := h.Verify("s3gura2026", hash); err != nil || ok {
		t.Errorf("wrong password: ok=%v err=%v", ok, err)
	}
}

// Un hash generado por el crate argon2 de Rust (mismos parámetros) también se verifica.
func TestPasswordVerifyPHCFromOtherImplementation(t *testing.T) {
	h := NewPasswordHasher(1)
	// Generado con Argon2::default().hash_password(b"S3gura2026") del crate argon2 0.6.0 (Rust).
	const phc = "$argon2id$v=19$m=19456,t=2,p=1$x/vysEhAOmzsWFYVbUvGiw$177EOjV0mDh8avqj2biQOnBeZNYZuZG9uCNOIV3yKRI"

	if ok, err := h.Verify("S3gura2026", phc); err != nil || !ok {
		t.Errorf("correct password: ok=%v err=%v", ok, err)
	}
	if ok, _ := h.Verify("otra-clave", phc); ok {
		t.Error("wrong password should not match")
	}
}

func TestPasswordVerifyInvalidHash(t *testing.T) {
	h := NewPasswordHasher(1)
	for _, bad := range []string{
		"",
		"$2b$10$bcrypt-hash",
		"$argon2i$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=16$m=19456,t=2,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=x,t=2,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=19456,t=2,p=1$!!!$aGFzaA",
		"$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$!!!",
	} {
		if _, err := h.Verify("password", bad); err == nil {
			t.Errorf("hash %q should be rejected", bad)
		}
	}
}

// El semáforo nunca deja calcular más hashes simultáneos que el límite.
func TestPasswordHasherConcurrencyLimit(t *testing.T) {
	const limit = 2
	h := NewPasswordHasher(limit)

	var maxSeen atomic.Int64
	stop := make(chan struct{})
	go func() { // observa cuántos lugares del semáforo están ocupados
		for {
			select {
			case <-stop:
				return
			default:
				if n := int64(len(h.slots)); n > maxSeen.Load() {
					maxSeen.Store(n)
				}
				time.Sleep(time.Millisecond)
			}
		}
	}()

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.Hash("S3gura2026")
		}()
	}
	wg.Wait()
	close(stop)

	if got := maxSeen.Load(); got > limit {
		t.Errorf("saw %d concurrent hashes, limit is %d", got, limit)
	}
}
