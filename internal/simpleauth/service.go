package simpleauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"

	"scanner-kiosk/internal/authport"
)

type Credential struct {
	PrincipalID string
	Version     int64
	Hash        PasswordHash
	Disabled    bool
}

// Records commands are backend-neutral. Password hashing never holds a database
// transaction. Current rechecks a successful calculation against reset/disable.
type Records interface {
	Lookup(context.Context, string) (Credential, bool, error)
	Create(context.Context, string, string, PasswordHash) (Credential, error)
	Replace(context.Context, string, PasswordHash) (Credential, error)
	Current(context.Context, string, int64) (bool, error)
}

type Service struct {
	records Records
	gate    chan struct{}
	dummy   PasswordHash
}

var _ authport.Password = (*Service)(nil)

func New(records Records) (*Service, error) {
	if records == nil {
		return nil, authport.ErrUnavailable
	}
	dummy, err := dummyHash()
	if err != nil {
		return nil, err
	}
	return &Service{records: records, gate: make(chan struct{}, 1), dummy: dummy}, nil
}

func (s *Service) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case s.gate <- struct{}{}:
		return nil
	default:
		return authport.ErrLimit
	}
}
func (s *Service) hash(ctx context.Context, password string) (PasswordHash, error) {
	if !validPassword(password, true) {
		return PasswordHash{}, authport.ErrDenied
	}
	if err := s.acquire(ctx); err != nil {
		return PasswordHash{}, err
	}
	defer func() { <-s.gate }()
	hash, err := makeHash(password)
	if ctx.Err() != nil {
		return PasswordHash{}, ctx.Err()
	}
	return hash, err
}

// Authenticate returns no identity until verification AND a current enabled
// credential-version read succeed. It never creates or enables an account.
func (s *Service) Authenticate(ctx context.Context, username, password string) (authport.PasswordResult, error) {
	name, err := NormalizeUsername(username)
	if err != nil || !validPassword(password, false) {
		return authport.PasswordResult{}, authport.ErrDenied
	}
	if err := s.acquire(ctx); err != nil {
		return authport.PasswordResult{}, err
	}
	defer func() { <-s.gate }()
	credential, found, err := s.records.Lookup(ctx, name)
	if err != nil {
		return authport.PasswordResult{}, authport.ErrUnavailable
	}
	hash := s.dummy
	if found {
		hash = credential.Hash
	}
	matched, err := verifyHash(password, hash)
	if ctx.Err() != nil {
		return authport.PasswordResult{}, ctx.Err()
	}
	if err != nil {
		return authport.PasswordResult{}, err
	}
	if !found || !matched || credential.Disabled || credential.PrincipalID == "" || credential.Version < 1 {
		return authport.PasswordResult{}, authport.ErrDenied
	}
	current, err := s.records.Current(ctx, credential.PrincipalID, credential.Version)
	if err != nil {
		return authport.PasswordResult{}, authport.ErrUnavailable
	}
	if !current {
		return authport.PasswordResult{}, authport.ErrDenied
	}
	if ctx.Err() != nil {
		return authport.PasswordResult{}, ctx.Err()
	}
	return authport.PasswordResult{Principal: authport.Principal{ID: credential.PrincipalID}, CredentialVersion: credential.Version}, nil
}

func (s *Service) Create(ctx context.Context, username, password string) (authport.PasswordResult, error) {
	name, err := NormalizeUsername(username)
	if err != nil {
		return authport.PasswordResult{}, err
	}
	hash, err := s.hash(ctx, password)
	if err != nil {
		return authport.PasswordResult{}, err
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return authport.PasswordResult{}, authport.ErrUnavailable
	}
	credential, err := s.records.Create(ctx, name, "principal_"+base64.RawURLEncoding.EncodeToString(nonce[:]), hash)
	if err != nil {
		return authport.PasswordResult{}, err
	}
	return authport.PasswordResult{Principal: authport.Principal{ID: credential.PrincipalID}, CredentialVersion: credential.Version}, nil
}

func (s *Service) SetPassword(ctx context.Context, username, password string) (authport.PasswordResult, error) {
	name, err := NormalizeUsername(username)
	if err != nil {
		return authport.PasswordResult{}, err
	}
	hash, err := s.hash(ctx, password)
	if err != nil {
		return authport.PasswordResult{}, err
	}
	credential, err := s.records.Replace(ctx, name, hash)
	if err != nil {
		return authport.PasswordResult{}, err
	}
	return authport.PasswordResult{Principal: authport.Principal{ID: credential.PrincipalID}, CredentialVersion: credential.Version}, nil
}
