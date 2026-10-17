package httpapi_test

import (
	"net/http"
	"testing"
)

func TestMeNamesTheTierOfAnAgentTokenAndIsNullForAPerson(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Me Desk", "admin@me.example.com")

	scopeOf := func(token string) any {
		t.Helper()
		response := request(t, api, http.MethodGet, "/api/v1/auth/me", nil, token, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("GET /auth/me status=%d body=%s", response.Code, response.Body.String())
		}
		var me map[string]any
		decode(t, response, &me)
		value, present := me["scope"]
		if !present {
			t.Fatalf("GET /auth/me has no scope field: %s", response.Body.String())
		}
		return value
	}

	if got := scopeOf(admin); got != nil {
		t.Errorf("person session scope=%v, want null", got)
	}
	for _, tier := range []string{"read", "draft", "send"} {
		token := issueToken(t, api, admin, "me "+tier, tier).Token
		if got := scopeOf(token); got != tier {
			t.Errorf("%s token scope=%v, want %q", tier, got, tier)
		}
	}
}
