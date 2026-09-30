package auth

import "context"

const (
	RoleProvider = "provider"
	RoleInternal = "internal"
)

type Principal struct {
	Subject    string
	ClientID   string
	ProviderID string
	Roles      []string
}

func (p Principal) HasRole(role string) bool {
	for _, r := range p.Roles {
		if r == role {
			return true
		}
	}
	return false
}

func (p Principal) IsInternal() bool { return p.HasRole(RoleInternal) }

func (p Principal) OwnsProvider(id string) bool {
	if p.IsInternal() {
		return true
	}
	return p.HasRole(RoleProvider) && p.ProviderID != "" && p.ProviderID == id
}

type ctxKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}
