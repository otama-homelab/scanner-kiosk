package websession

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

func opaque(prefix string) (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(value[:]), nil
}
func validID(value string) bool {
	if len(value) < 16 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func (h *Handler) createGuest(ctx context.Context, binding, key string) (string, error) {
	if !validID(binding) || !validID(key) {
		return "", ErrSession
	}
	principal, err := opaque("principal_")
	if err != nil {
		return "", err
	}
	return h.records.CreateGuest(ctx, GuestStartRecord{BindingHash: sha256.Sum256([]byte(binding)), KeyHash: sha256.Sum256([]byte(key)), ProposedPrincipalID: principal, Retention: h.receiptLifetime})
}
func (h *Handler) disableGuest(ctx context.Context, principal string) error {
	return h.records.DisableGuest(ctx, principal)
}
