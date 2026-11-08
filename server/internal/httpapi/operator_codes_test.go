package httpapi

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/configerr"
)

// codeExamples are the params of one example of each message code, for the console's table of codes.
var codeExamples = map[string]configerr.Params{
	"ai_server_half_configured":     {"missing": "AI_SERVER_API_KEY, AI_SERVER_MODELS"},
	"ai_server_label_too_long":      {"max": 80},
	"ai_server_allowance_unpriced":  {"models": "claude-haiku-5-5, gpt-5"},
	"ai_server_models_twice":        {"slot": "triage"},
	"ai_server_models_bad_model":    {"slot": "draft"},
	"ai_server_prices_not_number":   {"model": "claude-haiku-5-5"},
	"ai_server_prices_twice":        {"model": "claude-haiku-5-5"},
	"ai_server_allowance_unknown":   {"key": "seat"},
	"secret_not_valid":              {"keys": "ai.server.api_key, smtp.password"},
	"test_reenter_secret":           {"secret": "smtp.password", "key": "smtp.host"},
	"test_ai_refused":               {"model": "claude-haiku-5-5", "detail": "401 invalid x-api-key [secret]"},
	"test_ai_ok":                    {"model": "claude-haiku-5-5"},
	"test_ai_models_refused":        {"check_code": "auth"},
	"test_ai_models_failed":         {"detail": "connection refused"},
	"test_ai_models_ok":             {"count": 2},
	"internal_signup_email_invalid": {"entry": "colliedesk.com"},
	"network_entry_invalid":         {"name": "TRUSTED_PROXIES", "entry": "10.0.0.0/33"},
	"help_center_domain_invalid":    {"domain": "localhost"},
	"test_jwks_failed":              {"detail": "GET https://auth.example.com/.well-known/jwks.json: 404"},
	"test_jwks_ok":                  {"count": 2},
}

const codesTable = "../../../web/src/operator/codes.en.json"

// The console translates a message by its code (docs/internals/operator-console.md › Message codes). Its table
// of codes in English is written from the server's templates, with an example each: the console renders the
// example as the server does (web/src/operator/codes.test.ts), and the contract lists every code. Run with
// UPDATE_CODES=1 to write the table and the contract's list after adding a code.
func TestTheConsoleTableOfCodesIsTheServersEnglish(t *testing.T) {
	type entry struct {
		Text    string           `json:"text"`
		Params  configerr.Params `json:"params,omitempty"`
		Message string           `json:"message"`
	}
	table := map[string]entry{}
	codes := make([]string, 0, len(configerr.Templates))
	for code, template := range configerr.Templates {
		codes = append(codes, code)
		params := codeExamples[code]
		if _, listed := codeExamples[code]; !listed && strings.Contains(template, "{") {
			t.Errorf("%s: no example in codeExamples", code)
		}
		said := configerr.Say(code, params)
		if _, isConfig := configerr.Config[code]; !isConfig {
			table[code] = entry{Text: template, Params: params, Message: said.Text}
			continue
		}
		text, _, fieldCodes := friendlyConfigError(said)
		var coded configerr.Coded
		for _, item := range fieldCodes {
			coded = item
		}
		if coded.Code != code {
			t.Errorf("%s: the console's API lost its code: %+v", code, fieldCodes)
		}
		if configerr.Render(friendly(template), coded.Params) != text {
			t.Errorf("%s: the setting names of the text and of the params differ: %q", code, text)
		}
		table[code] = entry{Text: friendly(template), Params: coded.Params, Message: text}
	}
	slices.Sort(codes)
	written, err := json.MarshalIndent(table, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	written = append(written, '\n')
	contract, err := os.ReadFile("../../../contracts/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	head := "\n    OperatorMessageCode:\n"
	start := bytes.Index(contract, []byte(head))
	listAt := start + bytes.Index(contract[start:], []byte("      enum:\n")) + len("      enum:\n")
	end := listAt
	for bytes.HasPrefix(contract[end:], []byte("        - ")) {
		end += bytes.IndexByte(contract[end:], '\n') + 1
	}
	var list strings.Builder
	for _, code := range codes {
		list.WriteString("        - " + code + "\n")
	}
	wantContract := append(append(append([]byte{}, contract[:listAt]...), list.String()...), contract[end:]...)
	if os.Getenv("UPDATE_CODES") == "1" {
		if err := os.WriteFile(codesTable, written, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile("../../../contracts/openapi.yaml", wantContract, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if current, _ := os.ReadFile(codesTable); !bytes.Equal(current, written) {
		t.Errorf("%s is not the server's table of codes: run this test with UPDATE_CODES=1", codesTable)
	}
	if !bytes.Equal(contract, wantContract) {
		t.Errorf("OperatorMessageCode in contracts/openapi.yaml is not the server's list of codes: run this test with UPDATE_CODES=1")
	}
}
