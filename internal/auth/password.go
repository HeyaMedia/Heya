package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

const (
	// NIST SP 800-63B requires at least 15 characters for passwords used as a
	// single authentication factor. Heya deliberately does not impose brittle
	// composition rules; long passphrases are welcome.
	MinPasswordLength = 15
	MaxPasswordLength = 256

	argonMemory  uint32 = 19 * 1024
	argonTime    uint32 = 2
	argonThreads uint8  = 1
	argonSaltLen        = 16
	argonKeyLen         = 32
)

var ErrPasswordPolicy = errors.New("password does not meet policy")

func ValidateNewPassword(password string) error {
	length := utf8.RuneCountInString(password)
	if length < MinPasswordLength {
		return fmt.Errorf("%w: use at least %d characters", ErrPasswordPolicy, MinPasswordLength)
	}
	if length > MaxPasswordLength {
		return fmt.Errorf("%w: use at most %d characters", ErrPasswordPolicy, MaxPasswordLength)
	}
	return nil
}

// HashPassword produces a PHC-encoded Argon2id hash using OWASP's balanced
// baseline (19 MiB, 2 iterations, one lane). New-password policy is enforced
// by service entry points rather than here so a legacy short password can be
// transparently rehashed after a successful login.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// CheckDummyPassword pays the same Argon2 cost as a real current account so
// an unknown username does not retain a cheap timing oracle.
func CheckDummyPassword(password string) {
	dummy := argon2.IDKey([]byte(password), []byte("heya-auth-dummy!"), argonTime, argonMemory, argonThreads, argonKeyLen)
	_ = subtle.ConstantTimeCompare(dummy, make([]byte, argonKeyLen))
}

func CheckPassword(hash, password string) bool {
	if strings.HasPrefix(hash, "$argon2id$") {
		params, salt, expected, ok := parseArgon2Hash(hash)
		if !ok {
			return false
		}
		actual := argon2.IDKey([]byte(password), salt, params.time, params.memory, params.threads, uint32(len(expected)))
		return subtle.ConstantTimeCompare(actual, expected) == 1
	}
	// Existing bcrypt accounts remain valid and are upgraded to Argon2id after
	// their next successful full-password login.
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func NeedsPasswordRehash(hash string) bool {
	if !strings.HasPrefix(hash, "$argon2id$") {
		return true
	}
	params, salt, key, ok := parseArgon2Hash(hash)
	return !ok || params.memory != argonMemory || params.time != argonTime || params.threads != argonThreads ||
		len(salt) != argonSaltLen || len(key) != argonKeyLen
}

type argon2Params struct {
	memory  uint32
	time    uint32
	threads uint8
}

func parseArgon2Hash(encoded string) (argon2Params, []byte, []byte, bool) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return argon2Params{}, nil, nil, false
	}
	var version int
	if n, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || n != 1 || version != argon2.Version {
		return argon2Params{}, nil, nil, false
	}
	var params argon2Params
	if n, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &params.memory, &params.time, &params.threads); err != nil || n != 3 {
		return argon2Params{}, nil, nil, false
	}
	// Keep even a corrupted database value from requesting unreasonable work.
	if params.memory < 8*1024 || params.memory > 64*1024 || params.time < 1 || params.time > 10 || params.threads < 1 || params.threads > 4 {
		return argon2Params{}, nil, nil, false
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return argon2Params{}, nil, nil, false
	}
	key, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil || len(key) < 16 || len(key) > 64 {
		return argon2Params{}, nil, nil, false
	}
	return params, salt, key, true
}
