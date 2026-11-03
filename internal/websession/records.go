package websession

import (
	"context"
	"time"
)

// Records is the persistence boundary for sessions and their principal grants.
// Each mutation is one atomic command, not a sequence of public CRUD calls.
// Implementations must prevent revoked/stale session resurrection and preserve
// login rotation, logout-successor cancellation and enabled-principal fencing.
// Token arguments are hashes, never browser bearer tokens. No SQL types escape.
type Records interface {
	FindSession(context.Context, string) ([]byte, bool, error)
	CommitSession(context.Context, SessionData) error
	RotateLogin(context.Context, LoginGrantRecord, time.Duration) error
	CommitLogin(context.Context, SessionData, LoginGrantRecord) error
	RevokeSession(context.Context, string, time.Duration) error
	KnownSession(context.Context, string) (bool, error)
	PrincipalKind(context.Context, string) (string, error)
	CreateGuest(context.Context, GuestStartRecord) (string, error)
	DisableGuest(context.Context, string) error
	AuthorizeSession(context.Context, string, string, string) error
}

type SessionData struct {
	TokenHash string
	Data      []byte
	Expiry    time.Time
}

type LoginGrantRecord struct {
	SourceHash, Nonce, PrincipalID string
	Kind                           string
	CredentialVersion              int64
}

type GuestStartRecord struct {
	BindingHash, KeyHash [32]byte
	ProposedPrincipalID  string
	Retention            time.Duration
}
