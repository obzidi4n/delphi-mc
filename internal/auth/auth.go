// Package auth hashes passwords and tokens and maps roles to scopes.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonMemory  = 64 * 1024
	argonTime    = 3
	argonThreads = 2
	argonKeyLen  = 32
	saltLen      = 16
)

// HashPassword returns an encoded argon2id hash.
func HashPassword(password string) (string, error) {
	if len(password) < 8 {
		return "", errors.New("password must be at least 8 characters")
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash)), nil
}

// VerifyPassword reports whether password matches an encoded hash.
func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var memory uint32
	var timeCost uint32
	var threads uint8
	for _, kv := range strings.Split(parts[3], ",") {
		key, value, ok := strings.Cut(kv, "=")
		if !ok {
			return false
		}
		n, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return false
		}
		switch key {
		case "m":
			memory = uint32(n)
		case "t":
			timeCost = uint32(n)
		case "p":
			threads = uint8(n)
		}
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, timeCost, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// NewSecret returns a 32-byte URL-safe token.
func NewSecret() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic("auth: crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// HashSecret is what the panel stores for sessions and API keys.
func HashSecret(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// Allows reports whether the scope set grants want. "*" is everything, and a
// "*" segment matches any value in that position. A trailing two-segment
// "resource:*" grants every verb on that resource.
func Allows(scopes []string, want string) bool {
	wantParts := strings.Split(want, ":")
	for _, scope := range scopes {
		if scope == "*" || scope == want {
			return true
		}
		parts := strings.Split(scope, ":")
		if len(parts) == 2 && parts[1] == "*" && len(wantParts) >= 2 && wantParts[0] == parts[0] {
			return true
		}
		if len(parts) != len(wantParts) {
			continue
		}
		match := true
		for i := range parts {
			if parts[i] != "*" && parts[i] != wantParts[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// ValidRole reports whether role is a role a user can hold.
func ValidRole(role string) bool {
	switch role {
	case "owner", "admin", "developer", "viewer":
		return true
	}
	return false
}

// ScopesForRole maps an org-level role to the scope set it grants. Org roles
// carry wildcards across the org's servers; a server binding narrows them.
func ScopesForRole(role, orgID string) []string {
	switch role {
	case "owner":
		return []string{"*"}
	case "admin":
		return []string{
			"org:" + orgID + ":read",
			"org:" + orgID + ":admin",
			"org:" + orgID + ":server:create",
			"org:" + orgID + ":user:read",
			"org:" + orgID + ":user:admin",
			"server:*:*",
		}
	case "developer":
		return []string{
			"org:" + orgID + ":read",
			"org:" + orgID + ":server:create",
			"server:*:read", "server:*:update", "server:*:power",
		}
	case "viewer":
		return []string{"org:" + orgID + ":read", "server:*:read"}
	}
	return nil
}

// ScopesForServer maps a role bound to one server to explicit server scopes.
func ScopesForServer(role, serverID string) []string {
	scope := func(verb string) string { return "server:" + serverID + ":" + verb }
	switch role {
	case "owner", "admin":
		return []string{scope("*")}
	case "developer":
		return []string{scope("read"), scope("update"), scope("power")}
	case "viewer":
		return []string{scope("read")}
	}
	return nil
}
