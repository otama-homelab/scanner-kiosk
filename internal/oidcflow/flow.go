// Package oidcflow joins one-time state, provider verification and identity
// mapping. It does not issue Cookies or trust browser-supplied identity claims.
package oidcflow

import (
	"context"
	"errors"
	"scanner-kiosk/internal/authport"
	"scanner-kiosk/internal/oidcattempt"
	"scanner-kiosk/internal/oidcidentity"
	"scanner-kiosk/internal/oidcprovider"
)

// Atomic attempt and identity ports allow another storage implementation without
// SQL, driver types, or concrete-store assertions in this protocol coordinator.
type Attempts interface {
	BeginRequest(context.Context, string, string, [32]byte) (oidcattempt.Material, error)
	ConsumeRequest(context.Context, string, string, [32]byte) (oidcattempt.Material, error)
}
type Provider interface {
	Issuer() string
	Fingerprint() [32]byte
	AuthorizationURL(oidcattempt.Material) (string, error)
	Exchange(context.Context, string, oidcattempt.Material) (oidcprovider.Verified, error)
}
type Identities interface {
	Resolve(context.Context, oidcprovider.Verified, string) (oidcidentity.Principal, error)
}
type Flow struct {
	attempts   Attempts
	provider   Provider
	identities Identities
}

func New(attempts Attempts, provider Provider, identities Identities) (*Flow, error) {
	if attempts == nil || provider == nil || identities == nil {
		return nil, authport.ErrUnavailable
	}
	return &Flow{attempts: attempts, provider: provider, identities: identities}, nil
}

func (f *Flow) Issuer() string { return f.provider.Issuer() }
func classify(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, oidcattempt.ErrGone):
		return authport.ErrGone
	case errors.Is(err, oidcattempt.ErrConflict):
		return authport.ErrConflict
	case errors.Is(err, oidcattempt.ErrLimit):
		return authport.ErrLimit
	case errors.Is(err, oidcattempt.ErrDenied) || errors.Is(err, oidcattempt.ErrInvalid) || errors.Is(err, oidcprovider.ErrRejected) || errors.Is(err, oidcidentity.ErrDenied):
		return authport.ErrDenied
	default:
		return authport.ErrUnavailable
	}
}
func (f *Flow) Start(ctx context.Context, browser, key string) (string, error) {
	material, err := f.attempts.BeginRequest(ctx, browser, key, f.provider.Fingerprint())
	if err != nil {
		return "", classify(err)
	}
	target, err := f.provider.AuthorizationURL(material)
	return target, classify(err)
}
func (f *Flow) Finish(ctx context.Context, browser, state, code string) (authport.Principal, error) {
	material, err := f.attempts.ConsumeRequest(ctx, state, browser, f.provider.Fingerprint())
	if err != nil {
		return authport.Principal{}, classify(err)
	}
	verified, err := f.provider.Exchange(ctx, code, material)
	if err != nil {
		return authport.Principal{}, classify(err)
	}
	principal, err := f.identities.Resolve(ctx, verified, browser)
	if err != nil {
		return authport.Principal{}, classify(err)
	}
	return authport.Principal{ID: principal.ID, CanAdminister: principal.CanAdminister}, nil
}
func (f *Flow) Reject(ctx context.Context, browser, state string) error {
	_, err := f.attempts.ConsumeRequest(ctx, state, browser, f.provider.Fingerprint())
	return classify(err)
}

var _ authport.OIDC = (*Flow)(nil)
