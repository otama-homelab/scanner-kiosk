// Package oidcprovider exchanges codes and verifies ID tokens with official
// libraries. Browser state consumption, session grant and identity storage are
// separate application boundaries.
package oidcprovider

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/oidcattempt"
)

var (
	ErrInvalid     = errors.New("invalid OIDC provider configuration")
	ErrUnavailable = errors.New("OIDC provider unavailable")
	ErrRejected    = errors.New("OIDC authentication rejected")
)

type Options struct {
	Issuer, ClientID, ClientAuth, RedirectURL string
	ClientSecret                              config.Secret
	Scopes, AdminGroups                       []string
	Timeout                                   time.Duration
	MaxResponseBytes                          int64
	Roots                                     *x509.CertPool
}
type Provider struct {
	issuer, clientID string
	oauth            oauth2.Config
	verifier         *oidc.IDTokenVerifier
	clientContext    func(context.Context) context.Context
	groups           map[string]bool
	timeout          time.Duration
}

// Verified can only be populated by successful signature/claims/nonce checks.
// Implicit formatting/serialization redacts identity data.
type Verified struct {
	issuer, subject string
	admin           bool
}

func (v Verified) Issuer() string             { return v.issuer }
func (v Verified) Subject() string            { return v.subject }
func (v Verified) CanAdminister() bool        { return v.admin }
func (Verified) String() string               { return "[redacted]" }
func (Verified) GoString() string             { return "[redacted]" }
func (Verified) MarshalJSON() ([]byte, error) { return []byte(`"[redacted]"`), nil }

func New(ctx context.Context, options Options) (*Provider, error) {
	if !secureURL(options.Issuer) || !secureURL(options.RedirectURL) || !secretString(options.ClientID, 1024) || options.Timeout < time.Second || options.Timeout > 30*time.Second || options.MaxResponseBytes < 1024 || options.MaxResponseBytes > 1<<20 {
		return nil, ErrInvalid
	}
	style := oauth2.AuthStyleInParams
	clientSecret := string(options.ClientSecret)
	switch options.ClientAuth {
	case "client_secret_basic":
		style = oauth2.AuthStyleInHeader
	case "client_secret_post":
	case "none":
		clientSecret = ""
	default:
		return nil, ErrInvalid
	}
	if options.ClientAuth != "none" && options.ClientSecret == "" {
		return nil, ErrInvalid
	}
	seen := map[string]bool{}
	for _, scope := range options.Scopes {
		if !secretString(scope, 256) || strings.TrimSpace(scope) != scope || seen[scope] {
			return nil, ErrInvalid
		}
		seen[scope] = true
	}
	if !seen[oidc.ScopeOpenID] {
		return nil, ErrInvalid
	}
	groups := map[string]bool{}
	for _, group := range options.AdminGroups {
		if !secretString(group, 512) || groups[group] {
			return nil, ErrInvalid
		}
		groups[group] = true
	}
	client, transport := newClient(options.Timeout, options.MaxResponseBytes, options.Roots)
	discoveryURL := strings.TrimSuffix(options.Issuer, "/") + "/.well-known/openid-configuration"
	transport.allow(discoveryURL, "GET")
	discovery, cancel := context.WithTimeout(oidc.ClientContext(ctx, client), options.Timeout)
	defer cancel()
	upstream, err := oidc.NewProvider(discovery, options.Issuer)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrUnavailable
	}
	var metadata struct {
		JWKS       string   `json:"jwks_uri"`
		Algorithms []string `json:"id_token_signing_alg_values_supported"`
	}
	if upstream.Claims(&metadata) != nil || !secureURL(metadata.JWKS) {
		return nil, ErrInvalid
	}
	endpoint := upstream.Endpoint()
	if !secureURL(endpoint.AuthURL) || !secureURL(endpoint.TokenURL) {
		return nil, ErrInvalid
	}
	endpoint.AuthStyle = style
	if metadata.JWKS == endpoint.TokenURL || discoveryURL == endpoint.TokenURL {
		return nil, ErrInvalid
	}
	transport.allow(metadata.JWKS, "GET")
	transport.allow(endpoint.TokenURL, "POST")
	safe := map[string]bool{"RS256": true, "RS384": true, "RS512": true, "PS256": true, "PS384": true, "PS512": true, "ES256": true, "ES384": true, "ES512": true, "EdDSA": true}
	var algorithms []string
	for _, algorithm := range metadata.Algorithms {
		if safe[algorithm] {
			algorithms = append(algorithms, algorithm)
		}
	}
	if len(algorithms) == 0 {
		return nil, ErrInvalid
	}
	verifier := upstream.VerifierContext(oidc.ClientContext(context.Background(), client), &oidc.Config{ClientID: options.ClientID, SupportedSigningAlgs: algorithms})
	return &Provider{issuer: options.Issuer, clientID: options.ClientID, oauth: oauth2.Config{ClientID: options.ClientID, ClientSecret: clientSecret, Endpoint: endpoint, RedirectURL: options.RedirectURL, Scopes: append([]string(nil), options.Scopes...)}, verifier: verifier, clientContext: func(ctx context.Context) context.Context { return oidc.ClientContext(ctx, client) }, groups: groups, timeout: options.Timeout}, nil
}
func (p *Provider) AuthorizationURL(material oidcattempt.Material) (string, error) {
	if material.Consumed() || material.State() == "" || material.Nonce() == "" || material.Verifier() == "" {
		return "", ErrRejected
	}
	return p.oauth.AuthCodeURL(material.State(), oidc.Nonce(material.Nonce()), oauth2.S256ChallengeOption(material.Verifier())), nil
}

// Exchange requires the material returned by a confirmed one-time consume. It
// never retries token exchange or returns/persists access/refresh/ID tokens.
func (p *Provider) Exchange(ctx context.Context, code string, material oidcattempt.Material) (Verified, error) {
	if !material.Consumed() || !secretString(code, 4096) || material.Nonce() == "" || material.Verifier() == "" {
		return Verified{}, ErrRejected
	}
	ctx, cancel := context.WithTimeout(p.clientContext(ctx), p.timeout)
	defer cancel()
	token, err := p.oauth.Exchange(ctx, code, oauth2.VerifierOption(material.Verifier()))
	if err != nil {
		if ctx.Err() != nil {
			return Verified{}, ctx.Err()
		}
		return Verified{}, ErrUnavailable
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || len(raw) == 0 || len(raw) > 1<<20 {
		return Verified{}, ErrRejected
	}
	identity, err := p.verifier.Verify(ctx, raw)
	if err != nil {
		if ctx.Err() != nil {
			return Verified{}, ctx.Err()
		}
		return Verified{}, ErrRejected
	}
	if identity.Issuer != p.issuer || identity.Subject == "" || len(identity.Subject) > 255 || subtle.ConstantTimeCompare([]byte(identity.Nonce), []byte(material.Nonce())) != 1 {
		return Verified{}, ErrRejected
	}
	for _, r := range identity.Subject {
		if r < 32 || r > 126 {
			return Verified{}, ErrRejected
		}
	}
	if !identity.Expiry.After(time.Now()) || identity.IssuedAt.After(time.Now().Add(time.Minute)) {
		return Verified{}, ErrRejected
	}
	if identity.AccessTokenHash != "" && identity.VerifyAccessToken(token.AccessToken) != nil {
		return Verified{}, ErrRejected
	}
	var claims struct {
		AuthorizedParty string          `json:"azp"`
		Groups          json.RawMessage `json:"groups"`
	}
	if identity.Claims(&claims) != nil || claims.AuthorizedParty != "" && claims.AuthorizedParty != p.clientID || len(identity.Audience) > 1 && claims.AuthorizedParty != p.clientID {
		return Verified{}, ErrRejected
	}
	admin := false
	if len(claims.Groups) > 0 && len(claims.Groups) <= 16<<10 {
		var groups []string
		if json.Unmarshal(claims.Groups, &groups) == nil && len(groups) <= 256 {
			for _, group := range groups {
				if p.groups[group] {
					admin = true
				}
			}
		}
	}
	if ctx.Err() != nil {
		return Verified{}, ctx.Err()
	}
	return Verified{issuer: identity.Issuer, subject: identity.Subject, admin: admin}, nil
}

func (*Provider) String() string   { return "[redacted]" }
func (*Provider) GoString() string { return "[redacted]" }

func (p *Provider) Issuer() string { return p.issuer }
func (p *Provider) Fingerprint() [32]byte {
	bytes, _ := json.Marshal([]any{p.issuer, p.clientID, p.oauth.RedirectURL, p.oauth.Endpoint.AuthURL, p.oauth.Scopes})
	return sha256.Sum256(bytes)
}
