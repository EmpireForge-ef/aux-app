// Package oidcauth wraps an OpenID Connect provider (e.g. Keycloak) for the
// authorization-code login flow: it builds the authorization URL, exchanges
// the returned code, verifies the ID token (signature, issuer, audience,
// expiry, nonce), and applies an optional email allowlist.
package oidcauth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Config is the resolved OIDC configuration.
type Config struct {
	IssuerURL     string
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	Scopes        []string
	AllowedEmails []string
}

// Identity is the subset of ID-token claims the app uses.
type Identity struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
}

// DisplayName returns the friendliest available label for the user.
func (i Identity) DisplayName() string {
	switch {
	case i.Name != "":
		return i.Name
	case i.Email != "":
		return i.Email
	default:
		return i.Subject
	}
}

// Authenticator performs the OIDC flow. Provider discovery is lazy and
// cached, so a temporarily unreachable IdP does not block application start;
// it is retried on the next login attempt.
type Authenticator struct {
	cfg Config

	mu       sync.Mutex
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
}

// New builds an Authenticator. It does not contact the provider yet.
func New(cfg Config) *Authenticator {
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}
	return &Authenticator{cfg: cfg}
}

// ensure performs (and caches) provider discovery.
func (a *Authenticator) ensure(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.oauth != nil {
		return nil
	}
	provider, err := oidc.NewProvider(ctx, a.cfg.IssuerURL)
	if err != nil {
		return fmt.Errorf("oidc discovery for %s: %w", a.cfg.IssuerURL, err)
	}
	a.oauth = &oauth2.Config{
		ClientID:     a.cfg.ClientID,
		ClientSecret: a.cfg.ClientSecret,
		RedirectURL:  a.cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       a.cfg.Scopes,
	}
	a.verifier = provider.Verifier(&oidc.Config{ClientID: a.cfg.ClientID})
	return nil
}

// AuthCodeURL returns the provider URL to redirect the user to. state guards
// against CSRF; nonce binds the ID token to this login attempt.
func (a *Authenticator) AuthCodeURL(ctx context.Context, state, nonce string) (string, error) {
	if err := a.ensure(ctx); err != nil {
		return "", err
	}
	return a.oauth.AuthCodeURL(state, oidc.Nonce(nonce)), nil
}

// Verify exchanges the authorization code and validates the resulting ID
// token, including that its nonce matches expectedNonce.
func (a *Authenticator) Verify(ctx context.Context, code, expectedNonce string) (*Identity, error) {
	if err := a.ensure(ctx); err != nil {
		return nil, err
	}
	token, err := a.oauth.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("code exchange: %w", err)
	}
	rawID, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, errors.New("no id_token in provider response")
	}
	idToken, err := a.verifier.Verify(ctx, rawID)
	if err != nil {
		return nil, fmt.Errorf("verify id token: %w", err)
	}
	if idToken.Nonce != expectedNonce {
		return nil, errors.New("id token nonce mismatch")
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		Username      string `json:"preferred_username"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("parse id token claims: %w", err)
	}
	name := claims.Name
	if name == "" {
		name = claims.Username
	}
	return &Identity{
		Subject:       idToken.Subject,
		Email:         claims.Email,
		EmailVerified: claims.EmailVerified,
		Name:          name,
	}, nil
}

// Allowed reports whether the identity may sign in. With no allowlist any
// authenticated user is accepted; with an allowlist the email must be present
// (case-insensitively) and verified.
func (a *Authenticator) Allowed(id *Identity) bool {
	if len(a.cfg.AllowedEmails) == 0 {
		return true
	}
	if id.Email == "" || !id.EmailVerified {
		return false
	}
	for _, allowed := range a.cfg.AllowedEmails {
		if strings.EqualFold(strings.TrimSpace(allowed), id.Email) {
			return true
		}
	}
	return false
}
