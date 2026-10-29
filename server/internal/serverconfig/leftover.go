package serverconfig

import (
	"slices"
	"strings"
)

// Leftover is a moved variable still set in the environment. The server never reads it.
type Leftover struct {
	Env string `json:"env_name"`
	Key string `json:"key"`
	// Same says the database holds the same value: the variable can go from .env and compose.yaml.
	Same bool `json:"same"`
}

// Leftovers lists the moved variables still set in environ, and whether each still matches the database.
// The secrets of a server without a key ring are not leftovers: they are still read from the environment (D8).
func Leftovers(snap Snapshot, environ func(string) string) []Leftover {
	var found []Leftover
	for _, key := range keys {
		if key.Env == "" {
			continue
		}
		value := strings.TrimSpace(environ(key.Env))
		if value == "" || (key.Secret && !snap.ring) {
			continue
		}
		current, set := snap.values[key.Name]
		same := set && current == value
		if !set {
			same = composeDefaults[key.Env] == value
		}
		found = append(found, Leftover{Env: key.Env, Key: key.Name, Same: same})
	}
	for _, product := range ProductEnvs {
		value := strings.TrimSpace(environ(product.Env))
		if value == "" {
			continue
		}
		known := slices.ContainsFunc(snap.products, func(row ProductRow) bool { return row.ProductID == value && row.Plan == product.Plan })
		found = append(found, Leftover{Env: product.Env, Key: "polar_products", Same: known})
	}
	if value := environ(SignupHashKeyEnv); value != "" {
		found = append(found, Leftover{Env: SignupHashKeyEnv, Key: "server_secrets." + SignupHashKeySecret, Same: value == snap.signupHashKey})
	}
	return found
}

// Dead lists the variables set in environ that no release reads.
func Dead(environ func(string) string) []string {
	var found []string
	for _, name := range DeadEnvs {
		if environ(name) != "" {
			found = append(found, name)
		}
	}
	return found
}
