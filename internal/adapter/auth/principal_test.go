package auth

import (
	"context"
	"testing"
)

func TestOwnsProvider(t *testing.T) {
	cases := []struct {
		name string
		p    Principal
		id   string
		want bool
	}{
		{"provider owns itself", Principal{Roles: []string{RoleProvider}, ProviderID: "a"}, "a", true},
		{"provider does not own others", Principal{Roles: []string{RoleProvider}, ProviderID: "a"}, "b", false},
		{"provider role without id", Principal{Roles: []string{RoleProvider}}, "", false},
		{"internal owns everyone", Principal{Roles: []string{RoleInternal}}, "a", true},
		{"no role owns nothing", Principal{ProviderID: "a"}, "a", false},
	}
	for _, c := range cases {
		if got := c.p.OwnsProvider(c.id); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

func TestPrincipalContext(t *testing.T) {
	if _, ok := PrincipalFrom(context.Background()); ok {
		t.Fatal("empty context must not carry a principal")
	}
	ctx := WithPrincipal(context.Background(), Principal{Subject: "s", Roles: []string{RoleInternal}})
	p, ok := PrincipalFrom(ctx)
	if !ok || p.Subject != "s" || !p.IsInternal() {
		t.Errorf("got %+v %v", p, ok)
	}
}
