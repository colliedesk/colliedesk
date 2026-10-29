package serverconfig

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

// composeDefaults are the values the installer's compose file passed when .env said nothing, which equal
// the server's own defaults: alone they import nothing. TRUSTED_PROXIES is not one of them: its compose
// default trusted the local proxy, the server's trusts none, so it is imported like any value.
var composeDefaults = map[string]string{
	"SMTP_PORT":              "587",
	"SMTP_STARTTLS":          "true",
	"EMAIL_FROM_POLICY":      "verified",
	"MICROSOFT_OAUTH_TENANT": "common",
}

// GroupCheck validates one import group as the server would start with it: the parsers of that group read
// getenv. It returns an error that names variables, never values.
type GroupCheck func(group string, getenv func(string) string) error

// ImportResult says what the import did, per group.
type ImportResult struct {
	Imported []string          // groups written, each as its own version
	Skipped  []string          // groups the database already had
	Invalid  map[string]string // groups whose variables do not make a valid setting: nothing written
	Secrets  []string          // secrets imported later, once a key ring was added
	// SignupKey is "imported", "generated", "kept" or "too-short" (left out: sign-up stays off).
	SignupKey string
}

// ImportOptions tune an import.
type ImportOptions struct {
	// Replace writes every group that has a variable set, even when the database has it (collie config import --replace).
	Replace bool
	// Hosted sets operator.require_totp when the import leaves cloud sign-in and billing both on.
	Hosted func(Snapshot) bool
	// SkipSignupKey leaves the sign-up key alone (a file import that does not mention it).
	SkipSignupKey bool
}

// ImportEnv moves the settings still in getenv into the database, a group at a time, under the lock every
// process takes, so overlapping deploys import once (docs/internals/operator-console.md › First import):
//  1. a group the database holds, or held once (the operator may have unset it since), is skipped: the
//     database wins;
//  2. a group with at least one variable set (and not only the compose file's defaults) is validated whole by
//     its parsers and written as one version, or not at all;
//  3. a secret the database never held is imported once a key ring exists, even into a group already held;
//  4. the sign-up hash key comes from SIGNUP_HASH_KEY when set, and is generated only when it is not.
func (s *Store) ImportEnv(ctx context.Context, getenv func(string) string, check GroupCheck, by Actor, options ImportOptions) (ImportResult, error) {
	result := ImportResult{Invalid: map[string]string{}}
	value := func(name string) string { return strings.TrimSpace(getenv(name)) }
	err := s.privileged(ctx, func(tx pgx.Tx) error {
		if err := lock(ctx, tx); err != nil {
			return err
		}
		// Every key any version ever held: a group the operator set once and unset later stays unset.
		ever := map[string]bool{}
		saved, queryErr := tx.Query(ctx, `SELECT DISTINCT key FROM server_config_values`)
		if queryErr != nil {
			return fmt.Errorf("read saved settings: %w", queryErr)
		}
		names, collectErr := pgx.CollectRows(saved, pgx.RowTo[string])
		if collectErr != nil {
			return fmt.Errorf("read saved settings: %w", collectErr)
		}
		for _, name := range names {
			ever[name] = true
		}
		for _, group := range ImportGroups() {
			state, err := readState(ctx, tx)
			if err != nil {
				return err
			}
			raw, err := readValues(ctx, tx, state.Desired)
			if err != nil {
				return err
			}
			products, err := readProducts(ctx, tx)
			if err != nil {
				return err
			}
			values, _ := s.open(raw)
			current := NewSnapshot(state.Desired, values, products, "", s.environ, s.keys.Configured())
			keys := GroupKeys(group)
			held := slices.ContainsFunc(keys, func(key Key) bool { return ever[key.Name] })
			if group == "polar" && len(products) > 0 {
				held = true
			}
			set := map[string]string{}
			chosen := false
			for _, key := range keys {
				if key.Secret && !s.keys.Configured() {
					continue // D8: stays in the environment until a ring exists
				}
				if v := value(key.Env); v != "" {
					set[key.Name] = v
					if composeDefaults[key.Env] != v {
						chosen = true
					}
				}
			}
			var rows []ProductRow
			if group == "polar" {
				for _, product := range ProductEnvs {
					if v := value(product.Env); v != "" {
						rows = append(rows, ProductRow{ProductID: v, Plan: product.Plan, Period: product.Period, OnSale: true})
						chosen = true
					}
				}
			}
			if held && !options.Replace {
				// Secrets the database lacks, set while the server had no ring (D8), arrive now.
				late := map[string]string{}
				for _, key := range keys {
					if !ever[key.Name] && key.Secret && s.keys.Configured() && value(key.Env) != "" {
						late[key.Name] = value(key.Env)
					}
				}
				if len(late) == 0 {
					result.Skipped = append(result.Skipped, group)
					continue
				}
				candidate := current.With(late, nil)
				if err := check(group, candidate.Getenv); err != nil {
					result.Invalid[group] = err.Error()
					continue
				}
				if _, err := s.save(ctx, tx, Change{Set: late, Source: "env-import", Note: "secrets kept in the environment until the key ring existed", Actor: by}); err != nil {
					return err
				}
				for name := range late {
					result.Secrets = append(result.Secrets, name)
				}
				continue
			}
			if !chosen {
				continue
			}
			candidate := current.With(set, nil)
			if len(rows) > 0 {
				candidate = candidate.WithProducts(rows)
			}
			if err := check(group, candidate.Getenv); err != nil {
				result.Invalid[group] = err.Error()
				continue
			}
			for _, row := range rows {
				var exists bool
				if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM polar_products WHERE product_id=$1)`, row.ProductID).Scan(&exists); err != nil {
					return err
				}
				if !exists {
					if err := addProduct(ctx, tx, row, by); err != nil {
						return err
					}
				}
			}
			unset := []string{}
			if options.Replace {
				for _, key := range keys {
					// A secret the file leaves out is kept (an export leaves every secret out), as apply does.
					if _, ok := set[key.Name]; !ok && !key.Secret {
						unset = append(unset, key.Name)
					}
				}
			}
			if len(set) > 0 || len(unset) > 0 {
				if _, err := s.save(ctx, tx, Change{Set: set, Unset: unset, Source: "env-import", Note: "imported from the environment: " + group, Actor: by, AllowNoChange: true}); err != nil {
					return err
				}
			}
			result.Imported = append(result.Imported, group)
		}
		if options.Hosted != nil {
			if err := s.requireTOTPWhenHosted(ctx, tx, options.Hosted, by); err != nil {
				return err
			}
		}
		if options.SkipSignupKey {
			return nil
		}
		var err error
		result.SignupKey, err = importSignupKey(ctx, tx, value(SignupHashKeyEnv), by)
		return err
	})
	return result, err
}

func (s *Store) requireTOTPWhenHosted(ctx context.Context, tx pgx.Tx, hosted func(Snapshot) bool, by Actor) error {
	state, err := readState(ctx, tx)
	if err != nil {
		return err
	}
	raw, err := readValues(ctx, tx, state.Desired)
	if err != nil {
		return err
	}
	if _, ok := raw["operator.require_totp"]; ok {
		return nil
	}
	products, err := readProducts(ctx, tx)
	if err != nil {
		return err
	}
	values, _ := s.open(raw)
	if !hosted(NewSnapshot(state.Desired, values, products, "", s.environ, s.keys.Configured())) {
		return nil
	}
	_, err = s.save(ctx, tx, Change{Set: map[string]string{"operator.require_totp": "true"}, Source: "env-import", Note: "the hosted cloud requires an authenticator for operators", Actor: by})
	return err
}

// minSignupKeyBytes is the shortest sign-up hash key the server accepts.
const minSignupKeyBytes = 32

func importSignupKey(ctx context.Context, tx pgx.Tx, fromEnv string, by Actor) (string, error) {
	existing, err := readSignupKey(ctx, tx)
	if err != nil {
		return "", err
	}
	if existing != "" {
		return "kept", nil
	}
	outcome := "imported"
	key := fromEnv
	switch {
	case fromEnv != "" && len(fromEnv) < minSignupKeyBytes:
		// A key that short kept sign-up off; a new one would not be the operator's. Leave it for them.
		return "too-short", nil
	case fromEnv == "":
		if key, err = newSignupKey(); err != nil {
			return "", fmt.Errorf("make the sign-up hash key: %w", err)
		}
		outcome = "generated"
	}
	if _, err := tx.Exec(ctx, `INSERT INTO server_secrets(name, value) VALUES($1,$2) ON CONFLICT (name) DO NOTHING`, SignupHashKeySecret, []byte(key)); err != nil {
		return "", fmt.Errorf("store the sign-up hash key: %w", err)
	}
	action := "signup_key.import"
	if outcome == "generated" {
		action = "signup_key.generate"
	}
	return outcome, appendAudit(ctx, tx, by, action, nil, nil, nil)
}
