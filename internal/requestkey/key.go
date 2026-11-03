// Package requestkey defines aged idempotency keys, not authentication tokens.
package requestkey

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

const Lifetime = 24 * time.Hour
const FutureSkew = 30 * time.Second

var ErrInvalid = errors.New("invalid request key")
var ErrExpired = errors.New("request key expired")
var ErrClock = errors.New("request clock behind retirement fence")

// Expiry recognizes rq1_<13 decimal milliseconds>_<32 lowercase hex random>.
// Legacy opaque keys remain unaged and must retain their tombstones indefinitely.
// Neither format grants authority. Changing a key is always a new user intent.
func Expiry(key string) (int64, bool, error) {
	if !strings.HasPrefix(key, "rq1_") {
		return 0, false, nil
	}
	if len(key) != 50 || key[17] != '_' {
		return 0, true, ErrInvalid
	}
	for _, c := range key[4:17] {
		if c < '0' || c > '9' {
			return 0, true, ErrInvalid
		}
	}
	for _, c := range key[18:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return 0, true, ErrInvalid
		}
	}
	issued, err := strconv.ParseInt(key[4:17], 10, 64)
	if err != nil {
		return 0, true, ErrInvalid
	}
	return issued + Lifetime.Milliseconds(), true, nil
}

// ValidateNew runs only after exact receipt lookup in the admission transaction.
// A receipt replay can outlive the new-key window; a missing aged key cannot.
func ValidateNew(key string, now, floor int64) (int64, bool, error) {
	expiry, aged, err := Expiry(key)
	if err != nil || !aged {
		return expiry, aged, err
	}
	if now < 0 || floor < 0 {
		return 0, true, ErrInvalid
	}
	if now < floor {
		return 0, true, ErrClock
	}
	if expiry <= now {
		return 0, true, ErrExpired
	}
	if expiry-Lifetime.Milliseconds() > now+FutureSkew.Milliseconds() {
		return 0, true, ErrInvalid
	}
	return expiry, true, nil
}
