package configerr

// Templates is every message code (Config and Messages) of the operator console and its English (Render). A code is stable:
// rename one only with the contract (OperatorMessageCode in contracts/openapi.yaml) and the console's tables.
// The configuration's codes name variables as the parsers do; the console's API names settings instead
// (httpapi friendlyConfigError rewrites both the text and the params).
var Templates = map[string]string{}

func init() {
	for _, list := range []map[string]string{Config, Messages} {
		for code, template := range list {
			Templates[code] = template
		}
	}
}

// Config are the codes of the configuration's parsers: their text names variables (AI_SERVER_PROVIDER) and the
// console's API rewrites it to name settings (ai.server.provider), params too.
var Config = map[string]string{
	// The server's AI provider (ai.ServerProviderFromEnv).
	"ai_server_provider_invalid":     "AI_SERVER_PROVIDER must be anthropic or openai_compatible",
	"ai_server_half_configured":      "the server AI provider is half configured: set {missing}, or unset every AI_SERVER_ variable",
	"ai_server_base_url_invalid":     "AI_SERVER_BASE_URL must be an http or https URL without credentials",
	"ai_server_label_too_long":       "AI_SERVER_LABEL must be at most {max} characters",
	"ai_server_allowance_unpriced":   "AI_SERVER_MONTHLY_ALLOWANCE needs a price in AI_SERVER_PRICES for {models}",
	"ai_server_models_format":        "AI_SERVER_MODELS must be written triage=model,draft=model (a slot may list more models separated by |)",
	"ai_server_models_twice":         "AI_SERVER_MODELS lists {slot} twice",
	"ai_server_models_bad_model":     "AI_SERVER_MODELS has an empty or too long model for {slot}",
	"ai_server_prices_format":        "AI_SERVER_PRICES must be written model=input:output[:cache_read[:cache_write]] in US dollars per million tokens, separated by commas",
	"ai_server_prices_not_number":    "AI_SERVER_PRICES has a price for {model} that is not a number of dollars",
	"ai_server_prices_twice":         "AI_SERVER_PRICES lists {model} twice",
	"ai_server_allowance_not_number": "AI_SERVER_MONTHLY_ALLOWANCE must be a number of dollars",
	"ai_server_allowance_format":     "AI_SERVER_MONTHLY_ALLOWANCE must be dollars per workspace (10) or per agent with a trial amount (agent=3,trial=2)",
	"ai_server_allowance_unknown":    `AI_SERVER_MONTHLY_ALLOWANCE knows workspace, agent and trial, not {"key"}`,
	// Any setting: a refusal that would have repeated a secret names the secrets instead.
	"secret_not_valid": "{keys}: not valid",
}

// Messages are the codes of everything else the console shows from the server: live tests, what a change does
// and why it is refused, refused requests. Their text is sent as written.
var Messages = map[string]string{
	// Live tests (POST /operator/config/check with test).
	"test_reenter_secret":           "Enter {secret} again: the test goes to the new {key}.",
	"test_ai_needs_settings":        "Set the provider, its key or address and the models first.",
	"test_ai_refused":               "The provider refused {model}: {detail}",
	"test_ai_ok":                    "The provider answered with {model}. Nothing was saved.",
	"test_ai_models_needs_provider": "Pick a provider first.",
	"test_ai_models_needs_base_url": "Enter the base URL first.",
	"test_ai_models_needs_key":      "Enter the API key first.",
	"test_ai_models_refused":        "The provider refused to list its models ({check_code}). Check the key and the address, or type the model names.",
	"test_ai_models_failed":         "The models could not be listed: {detail}",
	"test_ai_models_ok":             "The provider offers {count} {count?model|models}. Nothing was saved.",
}
