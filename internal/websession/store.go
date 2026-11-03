package websession

import (
	"context"
	"time"
)

// sessionStore bridges SCS to atomic backend commands. Context-local grants stay
// in the session layer; persistence implementations receive immutable values.
type sessionStore struct {
	records   Records
	retention time.Duration
}

func (s *sessionStore) Find(token string) ([]byte, bool, error) {
	return s.FindCtx(context.Background(), token)
}
func (s *sessionStore) Commit(token string, data []byte, expiry time.Time) error {
	return s.CommitCtx(context.Background(), token, data, expiry)
}
func (s *sessionStore) Delete(token string) error { return s.DeleteCtx(context.Background(), token) }
func (s *sessionStore) FindCtx(ctx context.Context, token string) ([]byte, bool, error) {
	if len(token) != 43 {
		return nil, false, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.records.FindSession(ctx, token)
}
func (s *sessionStore) CommitCtx(ctx context.Context, token string, data []byte, expiry time.Time) error {
	if len(token) != 43 || len(data) > 64<<10 || !expiry.After(time.Now()) {
		return ErrSession
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	record := SessionData{TokenHash: token, Data: data, Expiry: expiry}
	if grant, ok := ctx.Value(grantKey{}).(*loginGrant); ok {
		if !grant.rotated || token == grant.source || !validLoginKind(grant.kind, grant.credentialVersion) {
			return ErrSession
		}
		return s.records.CommitLogin(ctx, record, LoginGrantRecord{SourceHash: grant.source, Nonce: grant.nonce, PrincipalID: grant.principal, Kind: grant.kind, CredentialVersion: grant.credentialVersion})
	}
	return s.records.CommitSession(ctx, record)
}
func (s *sessionStore) DeleteCtx(ctx context.Context, token string) error {
	if len(token) != 43 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if grant, ok := ctx.Value(grantKey{}).(*loginGrant); ok {
		if token != grant.source || !validLoginKind(grant.kind, grant.credentialVersion) {
			return ErrSession
		}
		err := s.records.RotateLogin(ctx, LoginGrantRecord{SourceHash: grant.source, Nonce: grant.nonce, PrincipalID: grant.principal, Kind: grant.kind, CredentialVersion: grant.credentialVersion}, s.retention)
		if err == nil {
			grant.rotated = true
		}
		return err
	}
	return s.records.RevokeSession(ctx, token, s.retention)
}
