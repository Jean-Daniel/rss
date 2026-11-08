// Package auth handles the single user's password and login sessions.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters: OWASP's minimum recommendation (19 MiB, 2 passes).
// The memory cost is kept low on purpose because the server is small, and
// hashing is serialized below so parallel login attempts cannot multiply it.
const (
	argonMemoryKiB = 19 * 1024
	argonTime      = 2
	argonThreads   = 1
	argonKeyLen    = 32
	argonSaltLen   = 16
)

// hashSlot allows one Argon2 computation at a time.
var hashSlot = make(chan struct{}, 1)

// argon2Key runs one Argon2id computation, then hands the 19 MiB work area
// back to the OS right away: logins are rare, and otherwise the Go runtime
// would keep that memory for minutes, more than doubling the server's size.
func argon2Key(password, salt []byte, time, memory uint32, threads uint8, keyLen uint32) []byte {
	hashSlot <- struct{}{}
	defer func() { <-hashSlot }()
	key := argon2.IDKey(password, salt, time, memory, threads, keyLen)
	debug.FreeOSMemory()
	return key
}

// HashPassword returns an encoded Argon2id hash in the usual PHC format.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2Key([]byte(password), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)

	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPassword checks a password against a hash made by HashPassword. The
// parameters stored in the hash are used, so they can change later.
func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("unsupported password hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errors.New("unsupported argon2 version")
	}
	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false, fmt.Errorf("bad argon2 parameters: %w", err)
	}
	b64 := base64.RawStdEncoding
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("bad salt: %w", err)
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("bad hash: %w", err)
	}

	got := argon2Key([]byte(password), salt, time, memory, threads, uint32(len(want)))

	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
