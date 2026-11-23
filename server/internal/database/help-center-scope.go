package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrNoHelpCenter means a help-center-scoped transaction was requested without a valid help center id.
var ErrNoHelpCenter = errors.New("help-center-scoped query needs a help center id")

// ReaderRole returns the role every help-center-scoped transaction switches to. The migration creates
// it per database (colliedesk_reader_<database>) and binds the help_center_isolation policies to it;
// the name is read back from the policy on kb_articles, so the migration stays the single source.
// The role owns nothing, cannot log in, has no BYPASSRLS and may read only the public help center's
// tables: what a visitor of one help center may see.
func (d *DB) ReaderRole(ctx context.Context) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.readerRole != "" {
		return d.readerRole, nil
	}
	var role string
	err := d.pool.QueryRow(ctx, `SELECT roles[1]::text FROM pg_policies
		WHERE schemaname=current_schema() AND tablename='kb_articles' AND policyname='help_center_isolation'`).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errors.New("help center row-level security is not set up in this schema; run `collie migrate` first")
	}
	if err != nil {
		return "", fmt.Errorf("read the reader role: %w", err)
	}
	d.readerRole = role
	return role, nil
}

// BeginHelpCenter starts a transaction as the reader role with app.workspace_id and app.help_center_id
// set for this transaction only. Row-level security then limits every statement to the rows that help
// center of that workspace shows, and the reader has no grant on the inbox at all.
func (d *DB) BeginHelpCenter(ctx context.Context, workspaceID, helpCenterID string) (pgx.Tx, error) {
	return d.BeginHelpCenterTx(ctx, workspaceID, helpCenterID, pgx.TxOptions{})
}

// BeginHelpCenterTx is BeginHelpCenter with transaction options, for example a read-only snapshot.
func (d *DB) BeginHelpCenterTx(ctx context.Context, workspaceID, helpCenterID string, options pgx.TxOptions) (pgx.Tx, error) {
	return d.beginReader(ctx, workspaceID, helpCenterID, options, "off")
}

// beginReader starts a reader transaction with app.help_chat set to helpChat, in one statement.
func (d *DB) beginReader(ctx context.Context, workspaceID, helpCenterID string, options pgx.TxOptions, helpChat string) (pgx.Tx, error) {
	if !uuidPattern.MatchString(workspaceID) {
		return nil, ErrNoWorkspace
	}
	if !uuidPattern.MatchString(helpCenterID) {
		return nil, ErrNoHelpCenter
	}
	role, err := d.ReaderRole(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := d.request.BeginTx(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("begin help center transaction: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('role',$1,true),set_config('app.workspace_id',$2,true),
		set_config('app.help_center_id',$3,true),set_config('app.help_chat',$4,true)`, role, workspaceID, helpCenterID, helpChat); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("scope transaction to help center: %w", err)
	}
	return tx, nil
}

// InHelpCenter runs fn in a help-center-scoped transaction and commits when fn returns nil.
func (d *DB) InHelpCenter(ctx context.Context, workspaceID, helpCenterID string, fn func(pgx.Tx) error) error {
	tx, err := d.BeginHelpCenter(ctx, workspaceID, helpCenterID)
	if err != nil {
		return err
	}
	return finish(ctx, tx, fn)
}

// BeginHelpChatTx is BeginHelpCenterTx for help chat: app.help_chat is on for this transaction, so
// kb_help_center_chat_articles and kb_help_center_chat_translations return what help chat on that help
// center may cite, the articles kept for help chat included. A public page never sets it.
func (d *DB) BeginHelpChatTx(ctx context.Context, workspaceID, helpCenterID string, options pgx.TxOptions) (pgx.Tx, error) {
	return d.beginReader(ctx, workspaceID, helpCenterID, options, "on")
}

// InHelpChat runs fn in a help chat transaction (BeginHelpChatTx) and commits when fn returns nil.
func (d *DB) InHelpChat(ctx context.Context, workspaceID, helpCenterID string, fn func(pgx.Tx) error) error {
	tx, err := d.BeginHelpChatTx(ctx, workspaceID, helpCenterID, pgx.TxOptions{})
	if err != nil {
		return err
	}
	return finish(ctx, tx, fn)
}

// verifyReader refuses to serve when help center isolation could be skipped or would hide the staff's
// own rows: the reader must not be a superuser or BYPASSRLS, the request connection must be able to
// switch to it, and neither the app role nor a separate request login may inherit it, because its
// RESTRICTIVE policies would then bind them too.
func (d *DB) verifyReader(ctx context.Context, app string) error {
	reader, err := d.ReaderRole(ctx)
	if err != nil {
		return err
	}
	var super, bypass, appInherits bool
	err = d.pool.QueryRow(ctx, `SELECT rolsuper,rolbypassrls,pg_has_role($2,oid,'USAGE') FROM pg_roles WHERE rolname=$1`,
		reader, app).Scan(&super, &bypass, &appInherits)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("database role %s is missing; run `collie migrate` first", reader)
	}
	if err != nil {
		return fmt.Errorf("check database role %s: %w", reader, err)
	}
	if super || bypass {
		return fmt.Errorf("database role %s must not be SUPERUSER or BYPASSRLS; help center isolation would be skipped", reader)
	}
	if appInherits {
		return fmt.Errorf("database role %s must not inherit %s; run `GRANT %s TO %s WITH INHERIT FALSE, SET TRUE`",
			app, reader, reader, app)
	}
	var user string
	var canSet, inherits bool
	if err := d.request.QueryRow(ctx, `SELECT session_user, pg_has_role(session_user,$1,'SET'),
		pg_has_role(session_user,$1,'USAGE')`, reader).Scan(&user, &canSet, &inherits); err != nil {
		return fmt.Errorf("check the request login: %w", err)
	}
	if !canSet {
		return fmt.Errorf("database role %s cannot SET ROLE %s; run `GRANT %s TO %s WITH INHERIT FALSE, SET TRUE` as a superuser",
			user, reader, reader, app)
	}
	if d.request != d.pool && inherits {
		return fmt.Errorf("the request login %s must not inherit %s", user, reader)
	}
	return d.verifyHelpCenterViews(ctx, app)
}

// helpCenterViews decide which articles a help center shows. They are not security_invoker: they read
// their tables as their owner, which must be the app role so workspace isolation still binds them.
var helpCenterViews = []string{"kb_help_center_collection_ranks", "kb_help_center_public_articles", "kb_help_center_public_translations",
	"kb_help_center_chat_articles", "kb_help_center_chat_translations"}

// verifyHelpCenterViews refuses to serve when a help center view is missing or owned by another role,
// as after a restore with --no-owner: owned by a superuser it would read every workspace's rows.
func (d *DB) verifyHelpCenterViews(ctx context.Context, app string) error {
	for _, view := range helpCenterViews {
		var owner string
		err := d.pool.QueryRow(ctx, `SELECT pg_get_userbyid(relowner) FROM pg_class
			WHERE relnamespace=current_schema()::regnamespace AND relname=$1 AND relkind='v'`, view).Scan(&owner)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("view %s is missing; run `collie migrate` first", view)
		}
		if err != nil {
			return fmt.Errorf("check view %s: %w", view, err)
		}
		if owner != app {
			return fmt.Errorf("view %s is owned by %s and must be owned by %s; run `ALTER VIEW %s OWNER TO %s`",
				view, owner, app, view, app)
		}
	}
	return nil
}
