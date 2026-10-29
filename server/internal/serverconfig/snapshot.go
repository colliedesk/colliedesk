package serverconfig

import (
	"maps"
	"slices"
	"strings"
)

// ProductRow is one row of polar_products.
type ProductRow struct {
	ProductID string `json:"product_id"`
	Plan      string `json:"plan"`
	Period    string `json:"period"`
	OnSale    bool   `json:"on_sale"`
}

// Snapshot is one version of the configuration, secrets opened, as the parsers read it. Its Getenv answers
// with the old variable names, so every parser that took a getenv (polar.ConfigFromEnv, email.ConfigFromEnv,
// …) reads the database unchanged.
type Snapshot struct {
	Version  int64
	values   map[string]string
	products []ProductRow
	// signupHashKey comes from server_secrets, which is not versioned: it never changes.
	signupHashKey string
	// environ reads the process environment: the bootstrap variables, the telemetry opt-out, and the
	// secrets of a server without a key ring (D8: they cannot be sealed, so they stay in the environment).
	environ func(string) string
	ring    bool
}

// NewSnapshot assembles a snapshot. values holds plaintext by dotted name; environ is os.Getenv (nil reads
// nothing); ring says whether MAILBOX_TOKEN_KEYS is set.
func NewSnapshot(version int64, values map[string]string, products []ProductRow, signupHashKey string, environ func(string) string, ring bool) Snapshot {
	if environ == nil {
		environ = func(string) string { return "" }
	}
	return Snapshot{Version: version, values: maps.Clone(values), products: slices.Clone(products), signupHashKey: signupHashKey, environ: environ, ring: ring}
}

// Get returns a key's value and whether it is set.
func (s Snapshot) Get(name string) (string, bool) {
	key, ok := Lookup(name)
	if !ok {
		return "", false
	}
	if key.Secret && !s.ring {
		value := s.environ(key.Env)
		return value, value != ""
	}
	value, ok := s.values[key.Name]
	return value, ok
}

// Values returns every key set, secrets included: for validation and the CLI export only.
func (s Snapshot) Values() map[string]string { return maps.Clone(s.values) }

// Products returns the rows of polar_products.
func (s Snapshot) Products() []ProductRow { return slices.Clone(s.products) }

// Ring reports whether the server has a key ring to seal secrets with.
func (s Snapshot) Ring() bool { return s.ring }

// With returns the snapshot with keys set and unset, for checking a change before it is saved.
func (s Snapshot) With(set map[string]string, unset []string) Snapshot {
	next := s
	next.values = maps.Clone(s.values)
	if next.values == nil {
		next.values = map[string]string{}
	}
	for name, value := range set {
		if key, ok := Lookup(name); ok {
			next.values[key.Name] = value
		}
	}
	for _, name := range unset {
		if key, ok := Lookup(name); ok {
			delete(next.values, key.Name)
		}
	}
	return next
}

// WithProducts returns the snapshot with another product list.
func (s Snapshot) WithProducts(products []ProductRow) Snapshot {
	next := s
	next.products = slices.Clone(products)
	return next
}

// Getenv answers with the old variable names: a moved variable from the database, a bootstrap variable
// and the telemetry opt-out from the environment. A moved variable left in the environment is never read.
// A setting that never was a variable (operator.require_totp) answers to its dotted name.
func (s Snapshot) Getenv(name string) string {
	if key, ok := Lookup(name); ok && key.Env == "" && key.Name == name {
		value, _ := s.Get(name)
		return value
	}
	if key, ok := Lookup(name); ok && key.Env == name {
		value, _ := s.Get(name)
		return value
	}
	for _, product := range ProductEnvs {
		if product.Env == name {
			for _, row := range s.products {
				if row.OnSale && row.Plan == product.Plan && row.Period == product.Period {
					return row.ProductID
				}
			}
			return ""
		}
	}
	switch name {
	case SignupHashKeyEnv:
		return s.signupHashKey
	case "DISABLE_TELEMETRY":
		if value := s.environ(name); value != "" {
			return value
		}
		if strings.EqualFold(strings.TrimSpace(s.values["telemetry.enabled"]), "false") {
			return "true"
		}
		return ""
	}
	return s.environ(name)
}

// Retired maps every product id ever sold that is no longer on sale to its plan, so a subscription bought as
// one still belongs to its plan.
func (s Snapshot) Retired() map[string]string {
	retired := map[string]string{}
	for _, row := range s.products {
		if !row.OnSale {
			retired[row.ProductID] = row.Plan
		}
	}
	return retired
}
