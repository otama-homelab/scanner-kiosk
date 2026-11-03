// Package oidcattempt persists one-time, browser-bound OIDC authorization attempts.
// It does not exchange authorization codes or establish authenticated identity.
package oidcattempt

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"golang.org/x/oauth2"
	"math"
	"time"
)

var (
	ErrGone      = errors.New("authorization attempt expired or consumed")
	ErrConflict  = errors.New("authorization request conflicts with original configuration")
	ErrInvalid   = errors.New("invalid authorization attempt configuration")
	ErrDenied    = errors.New("authorization attempt unavailable")
	ErrLimit     = errors.New("too many pending authorization attempts")
	ErrStorage   = errors.New("authorization state storage unavailable")
	ErrUncertain = errors.New("authorization attempt commit uncertain")
)

type Options struct {
	Lifetime   time.Duration
	MaxPending int
	Now        func() time.Time
}
type Store struct {
	records Records
	options Options
}

// Records commands are atomic, including active-browser/revocation checks. Only
// confirmed commits return protocol records. Implementations preserve request
// tombstones, fixed configuration/TTL and exactly one successful consume.
// No raw browser token, request key or callback state crosses this boundary.
type Records interface {
	Begin(context.Context, BeginRecord) (StoredMaterial, error)
	Consume(context.Context, ConsumeRecord) (StoredMaterial, error)
}
type StoredMaterial struct {
	StateHash                [32]byte
	Nonce, Verifier          string
	CreatedAtMS, ExpiresAtMS int64
}
type BeginRecord struct {
	BrowserHash, KeyHash, ConfigurationHash [32]byte
	RecordRequest                           bool
	NowMS                                   int64
	MaxPending                              int
	Proposed                                StoredMaterial
}
type ConsumeRecord struct {
	BrowserHash, StateHash, ConfigurationHash [32]byte
	RequireRequest                            bool
	NowMS                                     int64
}

func (StoredMaterial) String() string               { return "[redacted]" }
func (StoredMaterial) GoString() string             { return "[redacted]" }
func (StoredMaterial) MarshalJSON() ([]byte, error) { return []byte(`"[redacted]"`), nil }
func (BeginRecord) String() string                  { return "[redacted]" }
func (BeginRecord) GoString() string                { return "[redacted]" }
func (BeginRecord) MarshalJSON() ([]byte, error)    { return []byte(`"[redacted]"`), nil }
func (ConsumeRecord) String() string                { return "[redacted]" }
func (ConsumeRecord) GoString() string              { return "[redacted]" }
func (ConsumeRecord) MarshalJSON() ([]byte, error)  { return []byte(`"[redacted]"`), nil }
func NewWithRecords(records Records, options Options) (*Store, error) {
	if records == nil || options.Now == nil || options.Lifetime < time.Second || options.Lifetime > 10*time.Minute || options.MaxPending < 1 || options.MaxPending > 16 {
		return nil, ErrInvalid
	}
	return &Store{records: records, options: options}, nil
}

// Material deliberately redacts implicit formatting/JSON. Protocol code must
// explicitly select a value. None of these values belongs in logs or app state.
type Material struct {
	state, nonce, verifier string
	consumed               bool
}

func (Material) String() string               { return "[redacted]" }
func (Material) GoString() string             { return "[redacted]" }
func (Material) MarshalJSON() ([]byte, error) { return []byte(`"[redacted]"`), nil }
func (m Material) Consumed() bool             { return m.consumed }
func (m Material) State() string              { return m.state }
func (m Material) Nonce() string              { return m.nonce }
func (m Material) Verifier() string           { return m.verifier }
func (m Material) Challenge() string          { return oauth2.S256ChallengeFromVerifier(m.verifier) }
func random() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", ErrStorage
	}
	return base64.RawURLEncoding.EncodeToString(bytes[:]), nil
}
func token(value string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(value) == 43 && len(decoded) == 32 && base64.RawURLEncoding.EncodeToString(decoded) == value
}
func sessionHash(value string) [32]byte { return sha256.Sum256([]byte(value)) }

// Begin requires an already-persisted browser session, rechecked by Records.
func (s *Store) Begin(ctx context.Context, browserToken string) (Material, error) {
	return s.begin(ctx, browserToken, "", [32]byte{}, false)
}
func (s *Store) BeginRequest(ctx context.Context, browserToken, key string, configuration [32]byte) (Material, error) {
	if len(key) < 16 || len(key) > 128 {
		return Material{}, ErrInvalid
	}
	for _, r := range key {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return Material{}, ErrInvalid
		}
	}
	return s.begin(ctx, browserToken, key, configuration, true)
}
func requestState(verifier string, binding, key, configuration [32]byte) string {
	mac := hmac.New(sha256.New, []byte(verifier))
	mac.Write([]byte("scanner-kiosk/oidc-start/v1\x00"))
	mac.Write(binding[:])
	mac.Write(key[:])
	mac.Write(configuration[:])
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func validStored(record StoredMaterial, now int64) bool {
	return now >= 0 && record.CreatedAtMS >= 0 && record.CreatedAtMS <= now && record.ExpiresAtMS > now && record.ExpiresAtMS-record.CreatedAtMS <= 600000 && token(record.Nonce) && token(record.Verifier)
}
func (s *Store) begin(ctx context.Context, browserToken, key string, configuration [32]byte, recordRequest bool) (Material, error) {
	if !token(browserToken) {
		return Material{}, ErrDenied
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return Material{}, err
	}
	now := s.options.Now().UnixMilli()
	life := s.options.Lifetime.Milliseconds()
	if now < 0 || now > math.MaxInt64-life {
		return Material{}, ErrInvalid
	}
	state, err := random()
	if err != nil {
		return Material{}, err
	}
	nonce, err := random()
	if err != nil {
		return Material{}, err
	}
	verifier := oauth2.GenerateVerifier()
	binding := sessionHash(browserToken)
	keyHash := sha256.Sum256([]byte(key))
	if recordRequest {
		state = requestState(verifier, binding, keyHash, configuration)
	}
	proposed := StoredMaterial{StateHash: sha256.Sum256([]byte(state)), Nonce: nonce, Verifier: verifier, CreatedAtMS: now, ExpiresAtMS: now + life}
	record, err := s.records.Begin(ctx, BeginRecord{BrowserHash: binding, KeyHash: keyHash, ConfigurationHash: configuration, RecordRequest: recordRequest, NowMS: now, MaxPending: s.options.MaxPending, Proposed: proposed})
	if err != nil {
		return Material{}, err
	}
	if err = ctx.Err(); err != nil {
		return Material{}, err
	}
	if !validStored(record, s.options.Now().UnixMilli()) {
		return Material{}, ErrGone
	}
	if recordRequest {
		state = requestState(record.Verifier, binding, keyHash, configuration)
	} else if record.Nonce != proposed.Nonce || record.Verifier != proposed.Verifier {
		return Material{}, ErrDenied
	}
	digest := sha256.Sum256([]byte(state))
	if subtle.ConstantTimeCompare(digest[:], record.StateHash[:]) != 1 {
		return Material{}, ErrDenied
	}
	return Material{state: state, nonce: record.Nonce, verifier: record.Verifier}, nil
}

// Consume releases nonce/PKCE material only after a confirmed atomic consume.
func (s *Store) Consume(ctx context.Context, state, browserToken string) (Material, error) {
	return s.consume(ctx, state, browserToken, [32]byte{}, false)
}
func (s *Store) ConsumeRequest(ctx context.Context, state, browserToken string, configuration [32]byte) (Material, error) {
	return s.consume(ctx, state, browserToken, configuration, true)
}
func (s *Store) consume(ctx context.Context, state, browserToken string, configuration [32]byte, requireRequest bool) (Material, error) {
	if !token(state) || !token(browserToken) {
		return Material{}, ErrDenied
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return Material{}, err
	}
	now := s.options.Now().UnixMilli()
	if now < 0 {
		return Material{}, ErrInvalid
	}
	digest := sha256.Sum256([]byte(state))
	record, err := s.records.Consume(ctx, ConsumeRecord{BrowserHash: sessionHash(browserToken), StateHash: digest, ConfigurationHash: configuration, RequireRequest: requireRequest, NowMS: now})
	if err != nil {
		return Material{}, err
	}
	if err = ctx.Err(); err != nil {
		return Material{}, err
	}
	if !validStored(record, s.options.Now().UnixMilli()) || subtle.ConstantTimeCompare(digest[:], record.StateHash[:]) != 1 {
		return Material{}, ErrDenied
	}
	return Material{state: state, nonce: record.Nonce, verifier: record.Verifier, consumed: true}, nil
}
