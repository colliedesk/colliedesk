package httpapi_test

import (
	"net/http"
	"testing"
)

func TestMeSaysWhatAnAgentTokenWasGivenAndIsNullForAPerson(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Me Desk", "admin@me.example.com")

	tokenOf := func(token string) (map[string]any, []any) {
		t.Helper()
		response := request(t, api, http.MethodGet, "/api/v1/auth/me", nil, token, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("GET /auth/me status=%d body=%s", response.Code, response.Body.String())
		}
		var me map[string]any
		decode(t, response, &me)
		value, present := me["token"]
		if !present {
			t.Fatalf("GET /auth/me has no token field: %s", response.Body.String())
		}
		grant, _ := value.(map[string]any)
		permissions, _ := me["permissions"].([]any)
		return grant, permissions
	}

	if grant, permissions := tokenOf(admin); grant != nil || len(permissions) == 0 {
		t.Errorf("a person's session: token=%v permissions=%v, want null and the person's permissions", grant, permissions)
	}
	for _, tier := range []string{"read", "send", "admin"} {
		token := issueToken(t, api, admin, "me "+tier, tier).Token
		grant, permissions := tokenOf(token)
		want := "custom"
		if tier == "admin" {
			want = "role"
		}
		if grant == nil || grant["kind"] != want || len(permissions) == 0 {
			t.Errorf("%s token: token=%v permissions=%d, want kind %s and its permissions", tier, grant, len(permissions), want)
		}
	}
}
