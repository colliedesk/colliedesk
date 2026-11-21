# Writing a migration

A migration is read far more often than it is written: in review, when a self-hosted install fails
to upgrade, and when someone needs to know why a column exists. Write it so a stranger understands
it in one pass. `migration-check` enforces the rules marked **(checked)**; the rest are for review.

## Create

1. Run `go run ./cmd/migration-new --name <domain-change>` in `server/`. It writes an empty up and
   down pair named for what changes (`add-contact-tags`, not `fix-1` or an issue number).
2. Write the up file, then the down file that undoes it.
3. Run `go run ./cmd/migration-check --repo=..` in `server/`.

## Layout

1. **(checked)** The up file opens with a `--` comment: what changes, why, and what an older image
   does with the new schema when that matters for a rollback.
2. **(checked)** No line is longer than 100 characters. Wrap comments as paragraphs. Wrap SQL at a
   comma, an `AND`/`OR`, or a clause keyword, and indent continuation lines by 4 spaces.
3. One column per line in `CREATE TABLE`. A long `CHECK` or `FOREIGN KEY` goes on its own lines.
4. A comment sits directly above the statement or column it explains, and says why, not what.
5. Split a long string literal across lines with SQL's adjacent literals: `'first part '`, a line
   break, then `'second part'`. Never change the text itself.

## Security of a new table

**(checked)** Never write row-level security or app-role privileges by hand. Call one helper after
`CREATE TABLE` (`docs/internals/workspace-isolation.md`, "Securing a new table"):

```sql
SELECT secure_workspace_table('notes');                       -- workspace data
SELECT secure_server_table('server_jobs');                    -- server-wide data, privileged path only
SELECT limit_app_role('note_versions', 'SELECT', 'INSERT');   -- add-only history
```

`CREATE`/`ALTER`/`DROP POLICY`, `ROW LEVEL SECURITY`, `pg_policies` and `app.privileged` in an up
migration fail the check. Only the two migrations that define the helpers and set up row-level
security (`20261014135949-table-security-helpers`, `20261014135950-workspace-row-level-security`)
spell it out. When a statement needs the app role's name, use `app_role()`.

Use a `DO $$ ... $$` block only for logic that needs PL/pgSQL: a loop over data, or an error with a
message. Never use one to build policies or grants.

## Applied migrations

The server records an applied migration by its file name only (`schema_migrations`). An edit to an
applied file never runs again on a database that applied it; it changes what a new install runs.

1. Never change what an applied migration does. Fix a mistake with a new migration.
2. You may edit an applied migration to make it easier to read: comments, layout, helper calls in
   place of hand-written security. The edit must build the same schema.
3. **(checked)** Prove it on a real PostgreSQL:

   ```sh
   go run ./cmd/migration-check --repo=.. --base=origin/main --database="$TEST_DATABASE_URL"
   ```

   It runs the base chain and your chain into two new schemas. After every up migration, and after
   every down migration on the way back, it compares tables, columns, constraints, indexes,
   policies, grants, functions, triggers, views, types, comments and rows. It names the first
   migration where the two differ. Without `--database`, an edited applied migration is refused.
4. Keep a `CREATE FUNCTION` body in an applied migration exactly as written. PostgreSQL stores the
   body text, so a reflowed body is a different function on new installs. Long lines in those bodies
   are allowed up to `storedFunctionBodies` in `server/internal/migrationcheck/style.go`; a new
   migration wraps its function bodies too.
5. Never delete or rename an applied migration.

The comparison cannot see what a data change does to rows that only exist on a live install. An
edit that changes anything but comments, layout or the security helper calls needs a reviewer who
reads every changed statement.
