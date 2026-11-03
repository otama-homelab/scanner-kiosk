// Package oidcidentity maps verified issuer/subject to durable application identity.
// It never links accounts by email or trusts browser-supplied identity claims.
package oidcidentity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"scanner-kiosk/internal/oidcprovider"
)

var (
	ErrDenied    = errors.New("OIDC identity or browser session unavailable")
	ErrStorage   = errors.New("OIDC identity storage unavailable")
	ErrUncertain = errors.New("OIDC identity commit uncertain")
)

// Records resolves one verified identity and rechecks its still-active browser
// in the same atomic command. Disabled identities never reactivate/remap. No ID
// may be returned after an uncertain commit. Implementations never issue a
// session Cookie or login grant; those remain in the session boundary.
type Records interface {
	Resolve(context.Context, ResolveRecord) (string, error)
}
type ResolveRecord struct {
	Issuer, Subject     string
	BrowserHash         [32]byte
	ProposedPrincipalID string
	CreatedAtMS         int64
}

func (ResolveRecord) String() string               { return "[redacted]" }
func (ResolveRecord) GoString() string             { return "[redacted]" }
func (ResolveRecord) MarshalJSON() ([]byte, error) { return []byte(`"[redacted]"`), nil }

type Store struct {
	records Records
	now     func() time.Time
}
type Principal struct {
	ID            string
	CanAdminister bool
}

func NewWithRecords(records Records, now func() time.Time) (*Store, error) {
	if records == nil || now == nil {
		return nil, ErrStorage
	}
	return &Store{records: records, now: now}, nil
}
func principalID(id string) bool {
	if len(id) < 16 || len(id) > 96 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func (s *Store) Resolve(ctx context.Context, identity oidcprovider.Verified, browserToken string) (Principal, error) {
	raw, err := base64.RawURLEncoding.DecodeString(browserToken)
	if identity.Issuer() == "" || identity.Subject() == "" || err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != browserToken {
		return Principal{}, ErrDenied
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err = ctx.Err(); err != nil {
		return Principal{}, err
	}
	var nonce [32]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return Principal{}, ErrStorage
	}
	command := ResolveRecord{Issuer: identity.Issuer(), Subject: identity.Subject(), BrowserHash: sha256.Sum256([]byte(browserToken)), ProposedPrincipalID: "principal_" + base64.RawURLEncoding.EncodeToString(nonce[:]), CreatedAtMS: s.now().UnixMilli()}
	if command.CreatedAtMS < 0 {
		return Principal{}, ErrStorage
	}
	id, err := s.records.Resolve(ctx, command)
	if err != nil {
		return Principal{}, err
	}
	if err = ctx.Err(); err != nil {
		return Principal{}, err
	}
	if !principalID(id) {
		return Principal{}, ErrStorage
	}
	return Principal{ID: id, CanAdminister: identity.CanAdminister()}, nil
}
