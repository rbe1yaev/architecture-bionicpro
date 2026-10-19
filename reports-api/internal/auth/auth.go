// Package auth проверяет access token Keycloak и извлекает из него идентичность пользователя.
package auth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	// ErrUnauthenticated — токена нет или он невалиден (HTTP 401).
	ErrUnauthenticated = errors.New("unauthenticated")
	// ErrForbidden — токен валиден, но у пользователя нет нужной роли (HTTP 403).
	ErrForbidden = errors.New("forbidden")
)

// Identity — пользователь, подтверждённый проверенным токеном.
type Identity struct {
	Username string
	Roles    []string
}

type claims struct {
	jwt.RegisteredClaims
	Typ               string `json:"typ"`
	AZP               string `json:"azp"`
	PreferredUsername string `json:"preferred_username"`
	RealmAccess       struct {
		Roles []string `json:"roles"`
	} `json:"realm_access"`
}

type Verifier struct {
	keyfunc      jwt.Keyfunc
	issuer       string
	allowedAZP   []string
	requiredRole string
}

func NewVerifier(keyfunc jwt.Keyfunc, issuer string, allowedAZP []string, requiredRole string) *Verifier {
	return &Verifier{keyfunc: keyfunc, issuer: issuer, allowedAZP: allowedAZP, requiredRole: requiredRole}
}

// Verify проверяет подпись (только RS256), iss, exp/nbf, typ и azp, затем наличие роли.
func (v *Verifier) Verify(raw string) (Identity, error) {
	var c claims
	_, err := jwt.ParseWithClaims(raw, &c, v.keyfunc,
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(v.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(30*time.Second),
	)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %v", ErrUnauthenticated, err)
	}
	// typ=Bearer отличает access token от ID token (typ=ID)
	if c.Typ != "Bearer" {
		return Identity{}, fmt.Errorf("%w: typ %q", ErrUnauthenticated, c.Typ)
	}
	if !slices.Contains(v.allowedAZP, c.AZP) {
		return Identity{}, fmt.Errorf("%w: azp %q не разрешён", ErrUnauthenticated, c.AZP)
	}
	if c.PreferredUsername == "" {
		return Identity{}, fmt.Errorf("%w: нет preferred_username", ErrUnauthenticated)
	}

	id := Identity{Username: c.PreferredUsername, Roles: c.RealmAccess.Roles}
	if !slices.Contains(id.Roles, v.requiredRole) {
		return id, fmt.Errorf("%w: нет роли %q", ErrForbidden, v.requiredRole)
	}
	return id, nil
}

type ctxKey struct{}

func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

func FromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id, ok
}
