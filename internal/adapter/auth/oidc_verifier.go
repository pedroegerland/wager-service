package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

var ErrInvalidToken = errors.New("invalid token")

type Config struct {
	Issuer string

	JWKSURL  string
	Audience string
}

type Verifier struct {
	v *oidc.IDTokenVerifier
}

func NewVerifier(ctx context.Context, cfg Config) (*Verifier, error) {
	if cfg.Issuer == "" || cfg.JWKSURL == "" || cfg.Audience == "" {
		return nil, errors.New("auth: issuer, jwks url and audience are required")
	}
	keys := oidc.NewRemoteKeySet(ctx, cfg.JWKSURL)
	v := oidc.NewVerifier(cfg.Issuer, keys, &oidc.Config{ClientID: cfg.Audience})
	return &Verifier{v: v}, nil
}

type tokenClaims struct {
	Azp         string `json:"azp"`
	ClientID    string `json:"client_id"`
	ProviderID  string `json:"provider_id"`
	RealmAccess struct {
		Roles []string `json:"roles"`
	} `json:"realm_access"`
}

func (vf *Verifier) Verify(ctx context.Context, raw string) (Principal, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tok, err := vf.v.Verify(ctx, raw)
	if err != nil {
		return Principal{}, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	var c tokenClaims
	if err := tok.Claims(&c); err != nil {
		return Principal{}, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	clientID := c.Azp
	if clientID == "" {
		clientID = c.ClientID
	}
	return Principal{
		Subject:    tok.Subject,
		ClientID:   clientID,
		ProviderID: c.ProviderID,
		Roles:      c.RealmAccess.Roles,
	}, nil
}
