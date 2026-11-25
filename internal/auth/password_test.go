package auth

import (
	"errors"
	"strings"
	"testing"
)

// testParams sengaja ringan agar test cepat. Aplikasi memakai DefaultParams.
var testParams = Params{Memory: 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}

func TestDefaultParams(t *testing.T) {
	if DefaultParams.Memory != 64*1024 || DefaultParams.Iterations != 3 || DefaultParams.Parallelism != 2 {
		t.Fatalf("DefaultParams harus m=64MiB, t=3, p=2, dapat %+v", DefaultParams)
	}
	if got := NewHasher(Params{}).Params(); got != DefaultParams {
		t.Fatalf("parameter nol harus jatuh ke default, dapat %+v", got)
	}
}

func TestHashAndVerify(t *testing.T) {
	h := NewHasher(testParams)
	const pw = "kata-sandi-demo-123"

	encoded, err := h.Hash(pw)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=1024,t=1,p=1$") {
		t.Fatalf("format hash tidak sesuai: %s", encoded)
	}
	if strings.Contains(encoded, pw) {
		t.Fatal("hash tidak boleh memuat password asli")
	}

	ok, err := Verify(pw, encoded)
	if err != nil || !ok {
		t.Fatalf("password benar harus lolos: ok=%v err=%v", ok, err)
	}
	ok, err = Verify("salah", encoded)
	if err != nil || ok {
		t.Fatalf("password salah harus ditolak: ok=%v err=%v", ok, err)
	}
	ok, _ = Verify(strings.Repeat("a", MaxPasswordLength+1), encoded)
	if ok {
		t.Fatal("password terlalu panjang harus ditolak")
	}
}

func TestHashUsesRandomSalt(t *testing.T) {
	h := NewHasher(testParams)
	a, _ := h.Hash("sama")
	b, _ := h.Hash("sama")
	if a == b {
		t.Fatal("dua hash untuk password sama harus berbeda (salt acak)")
	}
}

func TestVerifyUsesParamsFromHash(t *testing.T) {
	encoded, _ := NewHasher(Params{Memory: 2048, Iterations: 2, Parallelism: 1}).Hash("pw")
	if ok, err := Verify("pw", encoded); err != nil || !ok {
		t.Fatalf("verify harus memakai parameter dari hash: ok=%v err=%v", ok, err)
	}
}

func TestVerifyInvalidHash(t *testing.T) {
	bad := []string{
		"",
		"plain",
		"$argon2i$v=19$m=1024,t=1,p=1$c2FsdA$a2V5",
		"$argon2id$v=19$m=x,t=1,p=1$c2FsdA$a2V5",
		"$argon2id$v=19$m=0,t=1,p=1$c2FsdA$a2V5",
		"$argon2id$v=19$m=1024,t=1,p=1$!!!$a2V5",
		"$argon2id$v=19$m=1024,t=1,p=1$c2FsdA$",
	}
	for _, b := range bad {
		if ok, err := Verify("pw", b); ok || !errors.Is(err, ErrInvalidHash) {
			t.Errorf("hash %q harus ErrInvalidHash, dapat ok=%v err=%v", b, ok, err)
		}
	}
	if _, err := Verify("pw", "$argon2id$v=16$m=1024,t=1,p=1$c2FsdA$a2V5"); !errors.Is(err, ErrIncompatibleVersion) {
		t.Errorf("versi lain harus ErrIncompatibleVersion, dapat %v", err)
	}
}
