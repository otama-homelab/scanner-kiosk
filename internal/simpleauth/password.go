// Package simpleauth verifies local credentials independently of HTTP and SQL.
package simpleauth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
	"scanner-kiosk/internal/authport"
)

// Initial supported PHC profile is deliberately fixed and bounded. An encoded
// database value cannot select arbitrary CPU/memory costs during verification.
const hashPrefix = "$argon2id$v=19$m=19456,t=2,p=1$"
const maxPasswordBytes = 1024

type PasswordHash struct{ encoded string }

func (PasswordHash) String() string               { return "[redacted]" }
func (PasswordHash) GoString() string             { return "[redacted]" }
func (PasswordHash) MarshalJSON() ([]byte, error) { return json.Marshal("[redacted]") }

// Encoded is explicitly for persistence, never responses or logging.
func (p PasswordHash) Encoded() string { return p.encoded }

func ParsePasswordHash(encoded string) (PasswordHash, error) {
	if len(encoded) > 256 || !strings.HasPrefix(encoded, hashPrefix) {
		return PasswordHash{}, authport.ErrUnavailable
	}
	parts := strings.Split(strings.TrimPrefix(encoded, hashPrefix), "$")
	if len(parts) != 2 {
		return PasswordHash{}, authport.ErrUnavailable
	}
	for i, text := range parts {
		value, err := base64.RawStdEncoding.DecodeString(text)
		wanted := 16
		if i == 1 {
			wanted = 32
		}
		if err != nil || len(value) != wanted || base64.RawStdEncoding.EncodeToString(value) != text {
			return PasswordHash{}, authport.ErrUnavailable
		}
	}
	return PasswordHash{encoded}, nil
}

// Names are NFC/case-folded identifiers, not OIDC aliases or display names.
// No trimming, punctuation substitution or account linking is performed.
func NormalizeUsername(input string) (string, error) {
	if !utf8.ValidString(input) || len(input) > 256 {
		return "", authport.ErrDenied
	}
	name := norm.NFC.String(cases.Fold().String(norm.NFC.String(input)))
	if len(name) > 256 || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 64 {
		return "", authport.ErrDenied
	}
	for i, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		if i > 0 && (r == '_' || r == '.' || r == '-') {
			continue
		}
		return "", authport.ErrDenied
	}
	return name, nil
}

func validPassword(password string, create bool) bool {
	if len(password) == 0 || len(password) > maxPasswordBytes || !utf8.ValidString(password) || strings.ContainsAny(password, "\x00\r\n") {
		return false
	}
	return !create || utf8.RuneCountInString(password) >= 15
}

func makeHash(password string) (PasswordHash, error) {
	var salt [16]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return PasswordHash{}, authport.ErrUnavailable
	}
	key := argon2.IDKey([]byte(password), salt[:], 2, 19456, 1, 32)
	defer clear(key)
	return PasswordHash{hashPrefix + base64.RawStdEncoding.EncodeToString(salt[:]) + "$" + base64.RawStdEncoding.EncodeToString(key)}, nil
}

func verifyHash(password string, hash PasswordHash) (bool, error) {
	if _, err := ParsePasswordHash(hash.encoded); err != nil {
		return false, err
	}
	parts := strings.Split(strings.TrimPrefix(hash.encoded, hashPrefix), "$")
	salt, _ := base64.RawStdEncoding.DecodeString(parts[0])
	expected, _ := base64.RawStdEncoding.DecodeString(parts[1])
	actual := argon2.IDKey([]byte(password), salt, 2, 19456, 1, 32)
	defer clear(actual)
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func dummyHash() (PasswordHash, error) {
	var salt [16]byte
	var key [32]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return PasswordHash{}, authport.ErrUnavailable
	}
	if _, err := rand.Read(key[:]); err != nil {
		return PasswordHash{}, authport.ErrUnavailable
	}
	return PasswordHash{hashPrefix + base64.RawStdEncoding.EncodeToString(salt[:]) + "$" + base64.RawStdEncoding.EncodeToString(key[:])}, nil
}
