package httpapi_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestContactsCanBeFetchedByIDWithinOneWorkspace(t *testing.T) {
	api, service := testAPI(t)
	tokenA := createSession(t, api, service, "Ids Alpha", "admin@ids-alpha.example.com")
	tokenB := createSession(t, api, service, "Ids Bravo", "admin@ids-bravo.example.com")
	ada := create(t, api, tokenA, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@ids.example.com"})
	bob := create(t, api, tokenA, "/api/v1/contacts", map[string]any{"name": "Bob", "email": "bob@ids.example.com"})
	create(t, api, tokenA, "/api/v1/contacts", map[string]any{"name": "Cy", "email": "cy@ids.example.com"})
	foreign := create(t, api, tokenB, "/api/v1/contacts", map[string]any{"name": "Zed", "email": "zed@ids.example.com"})
	absent := "00000000-0000-4000-8000-000000000000"

	names := func(query string, want int) []string {
		t.Helper()
		response := request(t, api, http.MethodGet, "/api/v1/contacts?"+query, nil, tokenA, nil)
		if response.Code != want {
			t.Fatalf("GET /contacts?%s status=%d body=%s, want %d", query, response.Code, response.Body.String(), want)
		}
		if want != http.StatusOK {
			return nil
		}
		var body struct {
			Items []struct {
				Name string `json:"name"`
			} `json:"items"`
		}
		decode(t, response, &body)
		got := make([]string, 0, len(body.Items))
		for _, item := range body.Items {
			got = append(got, item.Name)
		}
		return got
	}
	same := func(got []string, want ...string) bool { return strings.Join(got, ",") == strings.Join(want, ",") }

	if got := names("ids="+url.QueryEscape(bob+","+ada), http.StatusOK); !same(got, "Ada", "Bob") {
		t.Errorf("ids of Ada and Bob returned %v, want [Ada Bob] ordered by name", got)
	}
	if got := names("ids="+url.QueryEscape(ada+","+absent), http.StatusOK); !same(got, "Ada") {
		t.Errorf("an unknown id must be skipped, got %v", got)
	}
	if got := names("ids="+url.QueryEscape(foreign), http.StatusOK); !same(got) {
		t.Errorf("the id of another workspace's contact returned %v, want nothing", got)
	}
	if got := names("ids="+url.QueryEscape(ada)+"&search=bob", http.StatusOK); !same(got) {
		t.Errorf("ids and search must both apply, got %v", got)
	}
	names("ids="+url.QueryEscape("not-a-uuid"), http.StatusBadRequest)
	names("ids="+url.QueryEscape(ada+",,"+bob), http.StatusBadRequest)
	many := make([]string, 101)
	for index := range many {
		many[index] = fmt.Sprintf("00000000-0000-4000-8000-%012d", index)
	}
	names("ids="+url.QueryEscape(strings.Join(many, ",")), http.StatusBadRequest)
}

func TestContactIDsFilterExplainsWhatIsWrong(t *testing.T) {
	api, service := testAPI(t)
	token := createSession(t, api, service, "Ids Message", "admin@ids-message.example.com")
	ada := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@ids-message.example.com"})
	tooMany := make([]string, 101)
	for index := range tooMany {
		tooMany[index] = fmt.Sprintf("00000000-0000-4000-8000-%012d", index)
	}
	cases := []struct {
		name, ids string
		want      []string
	}{
		{"not an id", "not-a-uuid", []string{`"not-a-uuid"`, "UUID"}},
		{"one bad id among good ones", ada + ",12345", []string{`"12345"`, "UUID"}},
		{"empty entry", ada + ",," + ada, []string{"empty", "ids"}},
		{"too many", strings.Join(tooMany, ","), []string{"at most 100"}},
	}
	for _, c := range cases {
		response := request(t, api, http.MethodGet, "/api/v1/contacts?ids="+url.QueryEscape(c.ids), nil, token, nil)
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s: status=%d body=%s, want 400", c.name, response.Code, response.Body.String())
			continue
		}
		var body struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		decode(t, response, &body)
		for _, want := range c.want {
			if !strings.Contains(body.Error.Message, want) {
				t.Errorf("%s: message %q lacks %q", c.name, body.Error.Message, want)
			}
		}
	}
}
