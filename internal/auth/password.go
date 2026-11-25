package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Params adalah parameter argon2id. Memory dalam KiB.
type Params struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultParams dipakai aplikasi: m=64 MiB, t=3, p=2.
var DefaultParams = Params{
	Memory:      64 * 1024,
	Iterations:  3,
	Parallelism: 2,
	SaltLength:  16,
	KeyLength:   32,
}

// MaxPasswordLength membatasi panjang input agar tidak disalahgunakan.
const MaxPasswordLength = 1024

var (
	// ErrInvalidHash dikembalikan bila string hash tidak berformat argon2id PHC.
	ErrInvalidHash = errors.New("format hash password tidak valid")
	// ErrIncompatibleVersion dikembalikan bila versi argon2 berbeda.
	ErrIncompatibleVersion = errors.New("versi argon2 tidak kompatibel")
)

// Hasher membuat hash password argon2id dengan parameter tertentu.
type Hasher struct {
	p Params
}

// NewHasher membuat Hasher. Parameter nol diganti nilai DefaultParams.
func NewHasher(p Params) *Hasher {
	if p.Memory == 0 {
		p.Memory = DefaultParams.Memory
	}
	if p.Iterations == 0 {
		p.Iterations = DefaultParams.Iterations
	}
	if p.Parallelism == 0 {
		p.Parallelism = DefaultParams.Parallelism
	}
	if p.SaltLength == 0 {
		p.SaltLength = DefaultParams.SaltLength
	}
	if p.KeyLength == 0 {
		p.KeyLength = DefaultParams.KeyLength
	}
	return &Hasher{p: p}
}

// Params mengembalikan parameter yang dipakai Hasher.
func (h *Hasher) Params() Params { return h.p }

// Hash menghasilkan string PHC: $argon2id$v=19$m=..,t=..,p=..$salt$hash.
func (h *Hasher) Hash(password string) (string, error) {
	if len(password) > MaxPasswordLength {
		return "", errors.New("password terlalu panjang")
	}
	salt := make([]byte, h.p.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("membuat salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, h.p.Iterations, h.p.Memory, h.p.Parallelism, h.p.KeyLength)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, h.p.Memory, h.p.Iterations, h.p.Parallelism,
		b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// Verify membandingkan password dengan hash secara constant-time.
// Parameter dibaca dari hash, sehingga hash lama tetap bisa diverifikasi
// walaupun DefaultParams berubah.
func Verify(password, encoded string) (bool, error) {
	if len(password) > MaxPasswordLength {
		return false, nil
	}
	p, salt, key, err := decode(encoded)
	if err != nil {
		return false, err
	}
	other := argon2.IDKey([]byte(password), salt, p.Iterations, p.Memory, p.Parallelism, p.KeyLength)
	return subtle.ConstantTimeCompare(key, other) == 1, nil
}

func decode(encoded string) (Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return Params{}, nil, nil, ErrInvalidHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return Params{}, nil, nil, ErrInvalidHash
	}
	if version != argon2.Version {
		return Params{}, nil, nil, ErrIncompatibleVersion
	}
	var p Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Iterations, &p.Parallelism); err != nil {
		return Params{}, nil, nil, ErrInvalidHash
	}
	if p.Memory == 0 || p.Iterations == 0 || p.Parallelism == 0 {
		return Params{}, nil, nil, ErrInvalidHash
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return Params{}, nil, nil, ErrInvalidHash
	}
	key, err := b64.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return Params{}, nil, nil, ErrInvalidHash
	}
	p.SaltLength = uint32(len(salt))
	p.KeyLength = uint32(len(key))
	return p, salt, key, nil
}
