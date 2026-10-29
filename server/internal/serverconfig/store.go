package serverconfig

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
	"github.com/jackc/pgx/v5"
)

// lockKey serializes every write to the configuration (saves, the first import, the sign-up key) across
// processes: a deploy that overlaps two servers imports once.
const lockKey = "colliedesk-server-config"

// Errors a save returns; none ever carries a value.
var (
	// ErrConflict: the change was made on a version that is no longer the current one.
	ErrConflict = errors.New("the configuration changed since you read it")
	// ErrNoRing: a secret cannot be stored without MAILBOX_TOKEN_KEYS.
	ErrNoRing = errors.New("secrets can be stored only with MAILBOX_TOKEN_KEYS set (a key ring)")
	// ErrNoChange: the change sets every key to the value it already has.
	ErrNoChange = errors.New("nothing changes")
	// ErrUnknownKey: the name is not a setting.
	ErrUnknownKey = errors.New("unknown setting")
)

// stored is one key of one version as the database holds it.
type stored struct {
	Value  *string
	Sealed []byte
	KeyID  string
}

// Version describes a saved version, without values.
type Version struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
	Source    string    `json:"source"`
	ParentID  *int64    `json:"parent_id,omitempty"`
	Note      string    `json:"note"`
	Status    string    `json:"status"`
	Rejected  string    `json:"rejected_reason,omitempty"`
	// Changed lists the keys whose value differs from the version before it (secrets by name only).
	Changed []string `json:"changed"`
	// Changes says how: a setting's value before and after; a secret only that it changed.
	Changes []ChangeItem `json:"changes"`
}

// ChangeItem is one key a version changed; Before and After are nil for a secret and when not set.
type ChangeItem struct {
	Key    string  `json:"key"`
	Secret bool    `json:"secret"`
	Before *string `json:"before"`
	After  *string `json:"after"`
}

// State is which version should run and which last ran without an error.
type State struct {
	Desired  int64
	LastGood int64
}

// Actor is who changes the configuration, for the audit log.
type Actor struct {
	Kind string // operator, cli, server, token
	Name string
	IP   string
}

// Change is one save.
type Change struct {
	// Base is the version the change was made on, checked when CheckBase (the console; 0 is "no version
	// yet"). The CLI checks it itself with 0 meaning "skip"; imports never.
	Base      int64
	CheckBase bool
	Set       map[string]string
	Unset     []string
	Source    string
	Note      string
	Actor     Actor
	// AllowNoChange saves nothing and returns the current version when the change changes nothing.
	AllowNoChange bool
	// RequestID is the operator request an approved change came from.
	RequestID string
}

// Store reads and writes the configuration on the owner connection.
type Store struct {
	db      *database.DB
	keys    tokenseal.Keys
	environ func(string) string
	now     func() time.Time
}

// NewStore opens the configuration with the key ring that seals its secrets; environ is os.Getenv.
func NewStore(db *database.DB, keys tokenseal.Keys, environ func(string) string) *Store {
	return &Store{db: db, keys: keys, environ: environ, now: time.Now}
}

// Keys is the key ring the secrets are sealed with.
func (s *Store) Keys() tokenseal.Keys { return s.keys }

func sealContext(key string) string { return "server-config:" + key }

func (s *Store) privileged(ctx context.Context, fn func(pgx.Tx) error) error {
	return s.db.Privileged(ctx, database.PrivilegeServerConfig, fn)
}

func lock(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, lockKey)
	return err
}

func readState(ctx context.Context, tx pgx.Tx) (State, error) {
	var desired, good *int64
	if err := tx.QueryRow(ctx, `SELECT desired_version, last_good_version FROM server_config_state`).Scan(&desired, &good); err != nil {
		return State{}, fmt.Errorf("read configuration state: %w", err)
	}
	var state State
	if desired != nil {
		state.Desired = *desired
	}
	if good != nil {
		state.LastGood = *good
	}
	return state, nil
}

func readValues(ctx context.Context, tx pgx.Tx, version int64) (map[string]stored, error) {
	values := map[string]stored{}
	if version == 0 {
		return values, nil
	}
	rows, err := tx.Query(ctx, `SELECT key, value, sealed, seal_key_id FROM server_config_values WHERE version_id=$1`, version)
	if err != nil {
		return nil, fmt.Errorf("read configuration: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var item stored
		var keyID *string
		if err := rows.Scan(&key, &item.Value, &item.Sealed, &keyID); err != nil {
			return nil, err
		}
		if keyID != nil {
			item.KeyID = *keyID
		}
		values[key] = item
	}
	return values, rows.Err()
}

func readProducts(ctx context.Context, tx pgx.Tx) ([]ProductRow, error) {
	rows, err := tx.Query(ctx, `SELECT product_id, plan, period, on_sale FROM polar_products ORDER BY created_at, product_id`)
	if err != nil {
		return nil, fmt.Errorf("read products: %w", err)
	}
	defer rows.Close()
	var products []ProductRow
	for rows.Next() {
		var row ProductRow
		if err := rows.Scan(&row.ProductID, &row.Plan, &row.Period, &row.OnSale); err != nil {
			return nil, err
		}
		products = append(products, row)
	}
	return products, rows.Err()
}

func readSignupKey(ctx context.Context, tx pgx.Tx) (string, error) {
	var value []byte
	err := tx.QueryRow(ctx, `SELECT value FROM server_secrets WHERE name=$1`, SignupHashKeySecret).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return string(value), err
}

// Loaded is a snapshot with what could not be opened.
type Loaded struct {
	Snapshot
	State State
	// Unopened names the secrets sealed with a key the ring no longer has: they read as unset.
	Unopened []string
	// Meta says who set each key and when, from the version that last changed it.
	Meta map[string]KeyMeta
}

// KeyMeta is where a key's current value came from.
type KeyMeta struct {
	Version int64     `json:"version"`
	Source  string    `json:"source"`
	By      string    `json:"by"`
	At      time.Time `json:"at"`
	KeyID   string    `json:"key_id,omitempty"`
}

// Current loads the version every process should run.
func (s *Store) Current(ctx context.Context) (Loaded, error) {
	return s.load(ctx, -1)
}

// Load loads one version.
func (s *Store) Load(ctx context.Context, version int64) (Loaded, error) {
	return s.load(ctx, version)
}

func (s *Store) load(ctx context.Context, version int64) (Loaded, error) {
	var loaded Loaded
	err := s.privileged(ctx, func(tx pgx.Tx) error {
		state, err := readState(ctx, tx)
		if err != nil {
			return err
		}
		if version < 0 {
			version = state.Desired
		}
		raw, err := readValues(ctx, tx, version)
		if err != nil {
			return err
		}
		products, err := readProducts(ctx, tx)
		if err != nil {
			return err
		}
		signupKey, err := readSignupKey(ctx, tx)
		if err != nil {
			return err
		}
		values, unopened := s.open(raw)
		loaded = Loaded{Snapshot: NewSnapshot(version, values, products, signupKey, s.environ, s.keys.Configured()), State: state, Unopened: unopened}
		loaded.Meta, err = keyMeta(ctx, tx, version, raw)
		return err
	})
	return loaded, err
}

func (s *Store) open(raw map[string]stored) (map[string]string, []string) {
	values := map[string]string{}
	var unopened []string
	for key, item := range raw {
		if item.Value != nil {
			values[key] = *item.Value
			continue
		}
		plain, err := s.keys.Open(item.KeyID, item.Sealed, sealContext(key))
		if err != nil {
			unopened = append(unopened, key)
			continue
		}
		values[key] = string(plain)
	}
	slices.Sort(unopened)
	return values, unopened
}

// keyMeta finds, for each key set in version, the newest version up to it that changed the key.
func keyMeta(ctx context.Context, tx pgx.Tx, version int64, raw map[string]stored) (map[string]KeyMeta, error) {
	meta := map[string]KeyMeta{}
	if version == 0 {
		return meta, nil
	}
	rows, err := tx.Query(ctx, `
		WITH chain AS (
			SELECT v.id, v.source, v.created_by, v.created_at, x.key, x.value, x.sealed, x.seal_key_id,
				lag(coalesce(x.value, encode(x.sealed, 'base64'))) OVER (PARTITION BY x.key ORDER BY v.id) AS before,
				lag(v.id) OVER (PARTITION BY x.key ORDER BY v.id) AS before_id
			FROM server_config_versions v JOIN server_config_values x ON x.version_id = v.id
			WHERE v.id <= $1
		)
		SELECT DISTINCT ON (key) key, id, source, created_by, created_at, coalesce(seal_key_id, '')
		FROM chain
		WHERE before IS NULL OR before IS DISTINCT FROM coalesce(value, encode(sealed, 'base64'))
		ORDER BY key, id DESC`, version)
	if err != nil {
		return nil, fmt.Errorf("read who set the configuration: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var item KeyMeta
		if err := rows.Scan(&key, &item.Version, &item.Source, &item.By, &item.At, &item.KeyID); err != nil {
			return nil, err
		}
		if _, ok := raw[key]; ok {
			meta[key] = item
		}
	}
	return meta, rows.Err()
}

// Save writes a new version and makes it the one to run. Secrets are sealed; the audit names keys, never
// values.
func (s *Store) Save(ctx context.Context, change Change) (int64, error) {
	var version int64
	err := s.privileged(ctx, func(tx pgx.Tx) error {
		if err := lock(ctx, tx); err != nil {
			return err
		}
		var err error
		version, err = s.save(ctx, tx, change)
		return err
	})
	return version, err
}

func (s *Store) save(ctx context.Context, tx pgx.Tx, change Change) (int64, error) {
	state, err := readState(ctx, tx)
	if err != nil {
		return 0, err
	}
	if (change.CheckBase || change.Base != 0) && change.Base != state.Desired {
		return 0, ErrConflict
	}
	current, err := readValues(ctx, tx, state.Desired)
	if err != nil {
		return 0, err
	}
	next := maps.Clone(current)
	for name, value := range change.Set {
		key, ok := Lookup(name)
		if !ok {
			return 0, fmt.Errorf("%w: %s", ErrUnknownKey, name)
		}
		if key.Secret {
			if !s.keys.Configured() {
				return 0, fmt.Errorf("%s: %w", key.Name, ErrNoRing)
			}
			if old, ok := current[key.Name]; ok && old.Value == nil {
				if plain, err := s.keys.Open(old.KeyID, old.Sealed, sealContext(key.Name)); err == nil && string(plain) == value && old.KeyID == s.keys.Current() {
					continue
				}
			}
			id, sealed, err := s.keys.Seal([]byte(value), sealContext(key.Name))
			if err != nil {
				return 0, fmt.Errorf("seal %s: %w", key.Name, err)
			}
			next[key.Name] = stored{Sealed: sealed, KeyID: id}
			continue
		}
		text := value
		next[key.Name] = stored{Value: &text}
	}
	for _, name := range change.Unset {
		key, ok := Lookup(name)
		if !ok {
			return 0, fmt.Errorf("%w: %s", ErrUnknownKey, name)
		}
		delete(next, key.Name)
	}
	changed := changedKeys(current, next)
	if len(changed) == 0 {
		if change.AllowNoChange {
			return state.Desired, nil
		}
		return 0, ErrNoChange
	}
	return s.insertVersion(ctx, tx, state.Desired, next, change, changed)
}

func (s *Store) insertVersion(ctx context.Context, tx pgx.Tx, parent int64, values map[string]stored, change Change, changed []string) (int64, error) {
	var parentID *int64
	if parent != 0 {
		parentID = &parent
	}
	by := change.Actor.Name
	if by == "" {
		by = "unknown"
	}
	var version int64
	var requestID *string
	if change.RequestID != "" {
		requestID = &change.RequestID
	}
	if err := tx.QueryRow(ctx, `INSERT INTO server_config_versions(created_by, source, parent_id, note, request_id) VALUES($1,$2,$3,$4,$5) RETURNING id`,
		by, change.Source, parentID, change.Note, requestID).Scan(&version); err != nil {
		return 0, fmt.Errorf("save configuration: %w", err)
	}
	for key, item := range values {
		var keyID *string
		if item.KeyID != "" {
			keyID = &item.KeyID
		}
		if _, err := tx.Exec(ctx, `INSERT INTO server_config_values(version_id, key, value, sealed, seal_key_id) VALUES($1,$2,$3,$4,$5)`,
			version, key, item.Value, item.Sealed, keyID); err != nil {
			return 0, fmt.Errorf("save %s: %w", key, err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE server_config_state SET desired_version=$1, updated_at=now()`, version); err != nil {
		return 0, fmt.Errorf("point at the new configuration: %w", err)
	}
	action := "config.save"
	if change.Source == "env-import" {
		action = "env.import"
	}
	if change.Source == "restore" {
		action = "config.restore"
	}
	if err := appendAudit(ctx, tx, change.Actor, action, &version, changed, nil); err != nil {
		return 0, err
	}
	_, err := tx.Exec(ctx, `SELECT pg_notify('colliedesk_config', $1)`, fmt.Sprint(version))
	return version, err
}

// changedKeys lists the keys set, unset or changed between two versions.
func changedKeys(before, after map[string]stored) []string {
	var changed []string
	for key, item := range after {
		old, ok := before[key]
		if !ok || !sameStored(old, item) {
			changed = append(changed, key)
		}
	}
	for key := range before {
		if _, ok := after[key]; !ok {
			changed = append(changed, key)
		}
	}
	slices.Sort(changed)
	return changed
}

func sameStored(a, b stored) bool {
	if (a.Value == nil) != (b.Value == nil) {
		return false
	}
	if a.Value != nil {
		return *a.Value == *b.Value
	}
	return a.KeyID == b.KeyID && string(a.Sealed) == string(b.Sealed)
}

func appendAudit(ctx context.Context, tx pgx.Tx, actor Actor, action string, version *int64, keys []string, detail map[string]any) error {
	if keys == nil {
		keys = []string{}
	}
	if detail == nil {
		detail = map[string]any{}
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	kind, name := actor.Kind, actor.Name
	if kind == "" {
		kind = "cli"
	}
	if name == "" {
		name = "unknown"
	}
	var ip *string
	if actor.IP != "" {
		ip = &actor.IP
	}
	if _, err := tx.Exec(ctx, `INSERT INTO operator_audit(actor_kind, actor, action, version_id, keys, detail, ip) VALUES($1,$2,$3,$4,$5,$6,$7::inet)`,
		kind, name, action, version, keys, encoded, ip); err != nil {
		return fmt.Errorf("write the audit log: %w", err)
	}
	return nil
}

// Audit appends one line to the audit log. detail must never hold a secret's value.
func (s *Store) Audit(ctx context.Context, actor Actor, action string, version int64, keys []string, detail map[string]any) error {
	var versionID *int64
	if version != 0 {
		versionID = &version
	}
	return s.privileged(ctx, func(tx pgx.Tx) error { return appendAudit(ctx, tx, actor, action, versionID, keys, detail) })
}

// Restore saves an older version again as the newest one; secrets are copied sealed.
func (s *Store) Restore(ctx context.Context, from int64, base int64, actor Actor, note string) (int64, error) {
	var version int64
	err := s.privileged(ctx, func(tx pgx.Tx) error {
		if err := lock(ctx, tx); err != nil {
			return err
		}
		state, err := readState(ctx, tx)
		if err != nil {
			return err
		}
		if base != 0 && base != state.Desired {
			return ErrConflict
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM server_config_versions WHERE id=$1)`, from).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("version %d does not exist", from)
		}
		old, err := readValues(ctx, tx, from)
		if err != nil {
			return err
		}
		current, err := readValues(ctx, tx, state.Desired)
		if err != nil {
			return err
		}
		changed := changedKeys(current, old)
		if len(changed) == 0 {
			return fmt.Errorf("version %d is what runs already: %w", from, ErrNoChange)
		}
		if note == "" {
			note = fmt.Sprintf("restores v%d", from)
		}
		version, err = s.insertVersion(ctx, tx, state.Desired, old, Change{Source: "restore", Note: note, Actor: actor}, changed)
		return err
	})
	return version, err
}

// History lists versions, newest first, each with the keys it changed from its parent, in one query.
func (s *Store) History(ctx context.Context, limit int) ([]Version, error) {
	if limit <= 0 {
		limit = 100
	}
	var versions []Version
	err := s.privileged(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			WITH page AS (
				SELECT id, created_at, created_by, source, parent_id, note, status, coalesce(rejected_reason,'') AS rejected
				FROM server_config_versions ORDER BY id DESC LIMIT $1
			), pairs AS (
				SELECT p.id, coalesce(c.key, b.key) AS key, (c.sealed IS NOT NULL OR b.sealed IS NOT NULL) AS secret,
					CASE WHEN c.sealed IS NULL AND b.sealed IS NULL THEN b.value END AS before,
					CASE WHEN c.sealed IS NULL AND b.sealed IS NULL THEN c.value END AS after
				FROM page p
				JOIN server_config_values c ON c.version_id = p.id
				FULL JOIN (SELECT p2.id AS id, v.key, v.value, v.sealed, v.seal_key_id FROM page p2 JOIN server_config_values v ON v.version_id = p2.parent_id) b
					ON b.id = c.version_id AND b.key = c.key
				WHERE c.version_id IS NULL OR b.id IS NULL
					OR c.value IS DISTINCT FROM b.value OR c.sealed IS DISTINCT FROM b.sealed OR c.seal_key_id IS DISTINCT FROM b.seal_key_id
			)
			SELECT p.id, p.created_at, p.created_by, p.source, p.parent_id, p.note, p.status, p.rejected,
				coalesce((SELECT array_agg(DISTINCT x.key ORDER BY x.key) FROM pairs x WHERE x.id = p.id), '{}'),
				coalesce((SELECT json_agg(json_build_object('key', x.key, 'secret', x.secret, 'before', x.before, 'after', x.after) ORDER BY x.key) FROM pairs x WHERE x.id = p.id), '[]')
			FROM page p ORDER BY p.id DESC`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item Version
			if err := rows.Scan(&item.ID, &item.CreatedAt, &item.CreatedBy, &item.Source, &item.ParentID, &item.Note, &item.Status, &item.Rejected, &item.Changed, &item.Changes); err != nil {
				return err
			}
			versions = append(versions, item)
		}
		return rows.Err()
	})
	return versions, err
}

// Reject records that a process could not run a version; the reason names keys, never values.
func (s *Store) Reject(ctx context.Context, version int64, reason string, actor Actor) error {
	return s.privileged(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE server_config_versions SET status='rejected', rejected_reason=$2 WHERE id=$1 AND status <> 'rejected'`, version, truncate(reason, 2000))
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		return appendAudit(ctx, tx, actor, "config.rejected", &version, nil, map[string]any{"reason": truncate(reason, 500)})
	})
}

// MarkGood records that a process runs a version without an error.
func (s *Store) MarkGood(ctx context.Context, version int64) error {
	if version == 0 {
		return nil
	}
	return s.privileged(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE server_config_state SET last_good_version=$1, updated_at=now() WHERE last_good_version IS DISTINCT FROM $1`, version)
		return err
	})
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit]
}

// AuditEntry is one line of the audit log.
type AuditEntry struct {
	ID        int64          `json:"id"`
	At        time.Time      `json:"at"`
	ActorKind string         `json:"actor_kind"`
	Actor     string         `json:"actor"`
	Action    string         `json:"action"`
	VersionID *int64         `json:"version_id,omitempty"`
	Keys      []string       `json:"keys"`
	Detail    map[string]any `json:"detail"`
	IP        string         `json:"ip,omitempty"`
}

// AuditLog reads the audit log, newest first; key filters on a key name, actor on who.
func (s *Store) AuditLog(ctx context.Context, key, actor string, since time.Time, limit int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var entries []AuditEntry
	err := s.privileged(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id, at, actor_kind, actor, action, version_id, keys, detail, coalesce(host(ip),'')
			FROM operator_audit
			WHERE ($1 = '' OR $1 = ANY(keys)) AND ($2 = '' OR actor = $2) AND at >= $3
			ORDER BY id DESC LIMIT $4`, key, actor, since, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var entry AuditEntry
			if err := rows.Scan(&entry.ID, &entry.At, &entry.ActorKind, &entry.Actor, &entry.Action, &entry.VersionID, &entry.Keys, &entry.Detail, &entry.IP); err != nil {
				return err
			}
			entries = append(entries, entry)
		}
		return rows.Err()
	})
	return entries, err
}

// Products lists polar_products.
func (s *Store) Products(ctx context.Context) ([]ProductRow, error) {
	var products []ProductRow
	err := s.privileged(ctx, func(tx pgx.Tx) error {
		var err error
		products, err = readProducts(ctx, tx)
		return err
	})
	return products, err
}

// AddProduct adds a product. On sale, it takes the product of the same plan and period off sale in the same
// transaction (one product per plan and period is sold).
func (s *Store) AddProduct(ctx context.Context, row ProductRow, actor Actor) error {
	return s.privileged(ctx, func(tx pgx.Tx) error {
		if err := lock(ctx, tx); err != nil {
			return err
		}
		return addProduct(ctx, tx, row, actor)
	})
}

func addProduct(ctx context.Context, tx pgx.Tx, row ProductRow, actor Actor) error {
	row.ProductID = strings.TrimSpace(row.ProductID)
	if row.ProductID == "" {
		return errors.New("a product needs its Polar id")
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM polar_products WHERE product_id=$1)`, row.ProductID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("product %s is already listed; products are never changed, only taken off sale", row.ProductID)
	}
	if row.OnSale {
		if _, err := tx.Exec(ctx, `UPDATE polar_products SET on_sale=false WHERE plan=$1 AND period=$2 AND on_sale`, row.Plan, row.Period); err != nil {
			return err
		}
	}
	by := actor.Name
	if by == "" {
		by = "unknown"
	}
	if _, err := tx.Exec(ctx, `INSERT INTO polar_products(product_id, plan, period, on_sale, created_by) VALUES($1,$2,$3,$4,$5)`,
		row.ProductID, row.Plan, row.Period, row.OnSale, by); err != nil {
		return fmt.Errorf("add product: %w", err)
	}
	if err := appendAudit(ctx, tx, actor, "product.add", nil, nil, map[string]any{"product_id": row.ProductID, "plan": row.Plan, "period": row.Period, "on_sale": row.OnSale}); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `SELECT pg_notify('colliedesk_config', 'products')`)
	return err
}

// SetOnSale puts a product on sale (taking the other of its plan and period off) or off sale.
func (s *Store) SetOnSale(ctx context.Context, productID string, onSale bool, actor Actor) error {
	return s.privileged(ctx, func(tx pgx.Tx) error {
		if err := lock(ctx, tx); err != nil {
			return err
		}
		var plan, period string
		err := tx.QueryRow(ctx, `SELECT plan, period FROM polar_products WHERE product_id=$1`, productID).Scan(&plan, &period)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("product %s is not listed", productID)
		}
		if err != nil {
			return err
		}
		if onSale {
			if _, err := tx.Exec(ctx, `UPDATE polar_products SET on_sale=false WHERE plan=$1 AND period=$2 AND on_sale AND product_id<>$3`, plan, period, productID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE polar_products SET on_sale=$2 WHERE product_id=$1`, productID, onSale); err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, actor, "product.on_sale", nil, nil, map[string]any{"product_id": productID, "on_sale": onSale}); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `SELECT pg_notify('colliedesk_config', 'products')`)
		return err
	})
}

// Reseal seals every secret of the current version again with the ring's current key, as a new version.
func (s *Store) Reseal(ctx context.Context, actor Actor) (int64, []string, error) {
	var version int64
	var resealed []string
	err := s.privileged(ctx, func(tx pgx.Tx) error {
		if err := lock(ctx, tx); err != nil {
			return err
		}
		state, err := readState(ctx, tx)
		if err != nil {
			return err
		}
		current, err := readValues(ctx, tx, state.Desired)
		if err != nil {
			return err
		}
		next := maps.Clone(current)
		for key, item := range current {
			if item.Value != nil || item.KeyID == s.keys.Current() {
				continue
			}
			plain, err := s.keys.Open(item.KeyID, item.Sealed, sealContext(key))
			if err != nil {
				return fmt.Errorf("%s is sealed with key %q, which the ring no longer has: set it again", key, item.KeyID)
			}
			id, sealed, err := s.keys.Seal(plain, sealContext(key))
			if err != nil {
				return err
			}
			next[key] = stored{Sealed: sealed, KeyID: id}
			resealed = append(resealed, key)
		}
		if len(resealed) == 0 {
			version = state.Desired
			return nil
		}
		slices.Sort(resealed)
		version, err = s.insertVersion(ctx, tx, state.Desired, next, Change{Source: "cli", Note: "sealed again with key " + s.keys.Current(), Actor: actor}, resealed)
		return err
	})
	return version, resealed, err
}

// newSignupKey is 32 random bytes as text, the shape SIGNUP_HASH_KEY had.
func newSignupKey() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// Process is one running `collie serve` as server_processes records it.
type Process struct {
	InstanceID     string    `json:"instance_id"`
	Hostname       string    `json:"hostname"`
	BinaryVersion  string    `json:"binary_version"`
	RunningVersion int64     `json:"running_version"`
	StartedAt      time.Time `json:"started_at"`
	HeartbeatAt    time.Time `json:"heartbeat_at"`
	LastError      string    `json:"last_error,omitempty"`
	WaitingFor     string    `json:"waiting_for,omitempty"`
}

// processExpiry is how long a process without a heartbeat stays listed.
const processExpiry = 10 * time.Minute

// Heartbeat records what this process runs, and forgets the processes silent for 10 minutes.
func (s *Store) Heartbeat(ctx context.Context, process Process) error {
	return s.privileged(ctx, func(tx pgx.Tx) error {
		var running *int64
		if process.RunningVersion != 0 {
			running = &process.RunningVersion
		}
		if _, err := tx.Exec(ctx, `INSERT INTO server_processes(instance_id, hostname, binary_version, running_version, started_at, heartbeat_at, last_error, waiting_for)
			VALUES($1,$2,$3,$4,$5,now(),nullif($6,''),nullif($7,''))
			ON CONFLICT (instance_id) DO UPDATE SET running_version=excluded.running_version, heartbeat_at=now(),
				last_error=excluded.last_error, waiting_for=excluded.waiting_for`,
			process.InstanceID, process.Hostname, process.BinaryVersion, running, process.StartedAt, truncate(process.LastError, 2000), truncate(process.WaitingFor, 500)); err != nil {
			return fmt.Errorf("record this process: %w", err)
		}
		_, err := tx.Exec(ctx, `DELETE FROM server_processes WHERE heartbeat_at < now() - $1::interval`, fmt.Sprintf("%d seconds", int(processExpiry.Seconds())))
		return err
	})
}

// Leave removes this process from the list at a graceful stop.
func (s *Store) Leave(ctx context.Context, instanceID string) error {
	return s.privileged(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM server_processes WHERE instance_id=$1`, instanceID)
		return err
	})
}

// Processes lists the processes with a heartbeat in the last 10 minutes.
func (s *Store) Processes(ctx context.Context) ([]Process, error) {
	var processes []Process
	err := s.privileged(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT instance_id, hostname, binary_version, coalesce(running_version,0), started_at, heartbeat_at, coalesce(last_error,''), coalesce(waiting_for,'')
			FROM server_processes ORDER BY started_at, instance_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var process Process
			if err := rows.Scan(&process.InstanceID, &process.Hostname, &process.BinaryVersion, &process.RunningVersion, &process.StartedAt, &process.HeartbeatAt, &process.LastError, &process.WaitingFor); err != nil {
				return err
			}
			processes = append(processes, process)
		}
		return rows.Err()
	})
	return processes, err
}

// Listen wakes notify whenever the configuration or the products change (pg_notify colliedesk_config).
func (s *Store) Listen(ctx context.Context, notify chan<- struct{}) error {
	return s.db.Listen(ctx, "colliedesk_config", notify)
}

// Fingerprint names the polar_products rows, so a poll sees a product change that made no version.
func Fingerprint(products []ProductRow) string {
	var text strings.Builder
	for _, row := range products {
		fmt.Fprintf(&text, "%s/%s/%s/%v;", row.ProductID, row.Plan, row.Period, row.OnSale)
	}
	return text.String()
}

// LatestMigration names the newest migration applied.
func (s *Store) LatestMigration(ctx context.Context) (string, error) {
	var name string
	err := s.privileged(ctx, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT coalesce(max(name), '') FROM schema_migrations`).Scan(&name)
	})
	return name, err
}
