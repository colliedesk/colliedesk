package workspacehost

import (
	"context"
	"testing"
)

func TestOrigin(t *testing.T) {
	cases := []struct{ origin, domain, slug, want string }{
		{"https://app.colliedesk.com", "colliedesk.com", "acme", "https://acme.colliedesk.com"},
		{"https://staging.colliedesk.com/", "staging.colliedesk.com", "acme", "https://acme.staging.colliedesk.com"},
		{"http://127.0.0.1:8080", "desk.test", "acme", "http://acme.desk.test:8080"},
		{"https://desk.example.com", "", "acme", "https://desk.example.com"},
		{"https://app.colliedesk.com", "colliedesk.com", "help-staging", "https://app.colliedesk.com"},
	}
	for _, c := range cases {
		if got := Origin(c.origin, c.domain, c.slug); got != c.want {
			t.Errorf("Origin(%q, %q, %q) = %q, want %q", c.origin, c.domain, c.slug, got, c.want)
		}
	}
	if _, ok := Slug(context.Background()); ok {
		t.Fatal("a plain context names a workspace host")
	}
	if slug, ok := Slug(With(context.Background(), "acme")); !ok || slug != "acme" {
		t.Fatalf("Slug = %q, %v", slug, ok)
	}
}
