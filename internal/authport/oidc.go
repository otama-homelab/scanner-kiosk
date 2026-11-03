// Package authport defines the authentication protocol boundary used by HTTP.
package authport

import (
	"context"
	"errors"
)

var (
	ErrDenied      = errors.New("authentication rejected")
	ErrGone        = errors.New("authentication request expired")
	ErrConflict    = errors.New("authentication request conflicts")
	ErrLimit       = errors.New("authentication pending limit")
	ErrUnavailable = errors.New("authentication unavailable")
)

type Principal struct {
	ID            string
	CanAdminister bool
}
type OIDC interface {
	Issuer() string
	Start(context.Context, string, string) (string, error)
	Finish(context.Context, string, string, string) (Principal, error)
	Reject(context.Context, string, string) error
}
