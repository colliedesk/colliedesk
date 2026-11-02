package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// allPermissions is every permission, in the matrix order; the built-in Admin role holds all of them.
// auth sets it at start-up (auth.AllPermissions), so this package needs no copy of the catalog.
var allPermissions []string

// SetPermissionCatalog records every permission, in the matrix order. auth calls it once.
func SetPermissionCatalog(names []string) { allPermissions = slices.Clone(names) }

// agentRoleDefaults is what the built-in Agent role holds until a workspace changes it, set by auth like
// the catalog; a role shows it so Reset to default can say what it would change before it does.
var agentRoleDefaults []string

// SetAgentRoleDefaults records the Agent role's built-in permissions (auth.AgentRolePermissions).
func SetAgentRoleDefaults(names []string) { agentRoleDefaults = slices.Clone(names) }

// Caller is who changes roles: the person (AgentID) and what this call may do (Permissions, the
// effective ones: an agent token's, limited to its person's).
type Caller struct {
	AgentID     string
	Permissions []string
}

func (c Caller) holds(name string) bool { return slices.Contains(c.Permissions, name) }

// missing lists the names the caller does not hold, in the matrix order.
func (c Caller) missing(names []string) []string {
	var missing []string
	for _, name := range allPermissions {
		if slices.Contains(names, name) && !c.holds(name) {
			missing = append(missing, name)
		}
	}
	return missing
}

const permManageRoles = "team:manage_roles"

// memberName is a person's name for messages and lists: the display name, or the email.
const memberName = `COALESCE(NULLIF(btrim(a.display_name),''),a.email)`

// AgentRolesColumn selects, for a query on agents a, the person's roles as a JSON list of RoleRef:
// Admin, Agent, then by name.
const AgentRolesColumn = `COALESCE((SELECT json_agg(json_build_object('id',r.id,'name',r.name,'system',r.system)
		ORDER BY CASE r.system WHEN 'admin' THEN 0 WHEN 'agent' THEN 1 ELSE 2 END, lower(r.name), r.id)
	FROM agent_roles ar JOIN roles r ON r.workspace_id=ar.workspace_id AND r.id=ar.role_id
	WHERE ar.workspace_id=a.workspace_id AND ar.agent_id=a.id AND r.deleted_at IS NULL),'[]'::json)`

// HoldsPermission is a SQL condition true when the person a (agents a) holds the permission bound at
// the parameter param ($n): through the Admin role or a role listing it.
func HoldsPermission(param string) string {
	return `EXISTS (SELECT 1 FROM agent_roles hr JOIN roles hp ON hp.workspace_id=hr.workspace_id AND hp.id=hr.role_id
		WHERE hr.workspace_id=a.workspace_id AND hr.agent_id=a.id AND hp.deleted_at IS NULL
		AND (hp.system='admin' OR ` + param + `=ANY(hp.permissions)))`
}

// HoldsAdminRole is a SQL condition true when the person a holds the built-in Admin role.
const HoldsAdminRole = `EXISTS (SELECT 1 FROM agent_roles hr JOIN roles hp ON hp.workspace_id=hr.workspace_id AND hp.id=hr.role_id
	WHERE hr.workspace_id=a.workspace_id AND hr.agent_id=a.id AND hp.system='admin')`

// NamesHolding lists the active members who hold permission, by name, sorted.
func NamesHolding(ctx context.Context, tx pgx.Tx, workspaceID, permission string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT `+memberName+` FROM agents a WHERE a.workspace_id=$1 AND a.deactivated_at IS NULL AND `+HoldsPermission("$2")+`
		ORDER BY (COALESCE(btrim(a.display_name),'')=''), lower(a.display_name), a.email`, workspaceID, permission)
	if err != nil {
		return nil, fmt.Errorf("list members holding %s: %w", permission, err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("list members holding %s: %w", permission, err)
	}
	if names == nil {
		names = []string{}
	}
	return names, nil
}

// NamesHolding lists the active members who hold permission, by name.
func (s *Store) NamesHolding(ctx context.Context, workspaceID, permission string) ([]string, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]string, error) { return NamesHolding(ctx, tx, workspaceID, permission) })
}

// roleRecord is a role row.
type roleRecord struct {
	ID          string
	Name        string
	Description string
	System      *string
	Permissions []string
	UpdatedBy   *string
	DeletedAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (r roleRecord) ref() domain.RoleRef {
	return domain.RoleRef{ID: r.ID, Name: r.Name, System: r.System}
}

func (r roleRecord) isAdmin() bool { return r.System != nil && *r.System == domain.SystemRoleAdmin }

// permissions of the role: every one for Admin.
func (r roleRecord) permissions() []string {
	if r.isAdmin() {
		return slices.Clone(allPermissions)
	}
	var names []string
	for _, name := range allPermissions {
		if slices.Contains(r.Permissions, name) {
			names = append(names, name)
		}
	}
	if names == nil {
		names = []string{}
	}
	return names
}

const roleColumns = `r.id::text,r.name,r.description,r.system,r.permissions,
	(SELECT ` + memberName + ` FROM agents a WHERE a.workspace_id=r.workspace_id AND a.id=r.updated_by_agent_id),
	r.deleted_at,r.created_at,r.updated_at`

func scanRole(row pgx.Row) (roleRecord, error) {
	var role roleRecord
	err := row.Scan(&role.ID, &role.Name, &role.Description, &role.System, &role.Permissions, &role.UpdatedBy, &role.DeletedAt, &role.CreatedAt, &role.UpdatedAt)
	role.CreatedAt, role.UpdatedAt, role.DeletedAt = domain.UTC(role.CreatedAt), domain.UTC(role.UpdatedAt), domain.UTCOptional(role.DeletedAt)
	return role, err
}

// roleOrder: Admin, Agent, then by name.
const roleOrder = `CASE r.system WHEN 'admin' THEN 0 WHEN 'agent' THEN 1 ELSE 2 END, lower(r.name), r.id`

// findRole finds a role of the workspace by id or by name (without regard to case); a key shaped like a
// uuid is always an id, so a role named like another's id never answers for it. A deleted role is
// found only with deleted.
func findRole(ctx context.Context, tx pgx.Tx, workspaceID, key string, deleted bool, lock bool) (roleRecord, error) {
	query := `SELECT ` + roleColumns + ` FROM roles r WHERE r.workspace_id=$1 AND CASE WHEN $2 ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
		THEN r.id::text=lower($2) ELSE lower(btrim(r.name))=lower(btrim($2)) END`
	if !deleted {
		query += ` AND r.deleted_at IS NULL`
	}
	query += ` ORDER BY r.deleted_at IS NULL DESC, r.deleted_at DESC LIMIT 1`
	if lock {
		query += ` FOR UPDATE OF r`
	}
	role, err := scanRole(tx.QueryRow(ctx, query, workspaceID, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return roleRecord{}, domain.ErrRoleNotFound
	}
	if err != nil {
		return roleRecord{}, fmt.Errorf("find role: %w", err)
	}
	return role, nil
}

// ResolveRoles finds the roles named by ids or by names (exactly one of the two lists is used): an id
// of no role of the workspace is domain.ErrRoleNotFound; an unknown name a *domain.UnknownRoleNamesError
// that lists the valid ones.
func ResolveRoles(ctx context.Context, tx pgx.Tx, workspaceID string, ids, names []string) ([]domain.RoleRef, error) {
	records, err := resolveRoles(ctx, tx, workspaceID, ids, names)
	if err != nil {
		return nil, err
	}
	refs := make([]domain.RoleRef, len(records))
	for index, record := range records {
		refs[index] = record.ref()
	}
	return refs, nil
}

func resolveRoles(ctx context.Context, tx pgx.Tx, workspaceID string, ids, names []string) ([]roleRecord, error) {
	rows, err := tx.Query(ctx, `SELECT `+roleColumns+` FROM roles r WHERE r.workspace_id=$1 AND r.deleted_at IS NULL ORDER BY `+roleOrder, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	all, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (roleRecord, error) { return scanRole(row) })
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	var picked []roleRecord
	add := func(role roleRecord) {
		if !slices.ContainsFunc(picked, func(other roleRecord) bool { return other.ID == role.ID }) {
			picked = append(picked, role)
		}
	}
	for _, id := range ids {
		index := slices.IndexFunc(all, func(role roleRecord) bool { return role.ID == strings.TrimSpace(id) })
		if index < 0 {
			return nil, domain.ErrRoleNotFound
		}
		add(all[index])
	}
	var unknown []string
	for _, name := range names {
		index := slices.IndexFunc(all, func(role roleRecord) bool {
			return strings.EqualFold(strings.TrimSpace(role.Name), strings.TrimSpace(name))
		})
		if index < 0 {
			unknown = append(unknown, name)
			continue
		}
		add(all[index])
	}
	if unknown != nil {
		valid := make([]string, len(all))
		for index, role := range all {
			valid[index] = role.Name
		}
		return nil, &domain.UnknownRoleNamesError{Unknown: unknown, Valid: valid}
	}
	return picked, nil
}

// agentRoleRecords lists the roles a person holds.
func agentRoleRecords(ctx context.Context, tx pgx.Tx, workspaceID, agentID string) ([]roleRecord, error) {
	rows, err := tx.Query(ctx, `SELECT `+roleColumns+` FROM roles r JOIN agent_roles ar ON ar.workspace_id=r.workspace_id AND ar.role_id=r.id
		WHERE r.workspace_id=$1 AND ar.agent_id=$2 AND r.deleted_at IS NULL ORDER BY `+roleOrder, workspaceID, agentID)
	if err != nil {
		return nil, fmt.Errorf("list agent roles: %w", err)
	}
	records, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (roleRecord, error) { return scanRole(row) })
	if err != nil {
		return nil, fmt.Errorf("list agent roles: %w", err)
	}
	return records, nil
}

// heldBy is every permission of the roles.
func heldBy(roles []roleRecord) []string {
	var held []string
	for _, name := range allPermissions {
		if slices.ContainsFunc(roles, func(role roleRecord) bool { return slices.Contains(role.permissions(), name) }) {
			held = append(held, name)
		}
	}
	return held
}

// lockRoles serializes every change to roles, members' roles, seats and the last-admin rule of a
// workspace (the agent-seats lock), so two changes at once cannot both slip past a rule.
func lockRoles(ctx context.Context, tx pgx.Tx, workspaceID string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "agent-seats:"+workspaceID); err != nil {
		return fmt.Errorf("lock roles: %w", err)
	}
	return nil
}

// requireOutranks refuses a change to member when they hold a permission the caller lacks.
func requireOutranks(ctx context.Context, tx pgx.Tx, workspaceID string, caller Caller, memberID string) error {
	roles, err := agentRoleRecords(ctx, tx, workspaceID, memberID)
	if err != nil {
		return err
	}
	if missing := caller.missing(heldBy(roles)); len(missing) > 0 {
		var name string
		if err := tx.QueryRow(ctx, `SELECT `+memberName+` FROM agents a WHERE a.workspace_id=$1 AND a.id=$2`, workspaceID, memberID).Scan(&name); err != nil {
			return fmt.Errorf("load member: %w", err)
		}
		return &domain.ExceedsYoursError{Missing: missing, Member: name}
	}
	return nil
}

// RequireOutranks refuses a change to member (their sessions, name, state) when they hold a permission
// the caller lacks.
func (s *Store) RequireOutranks(ctx context.Context, workspaceID string, caller Caller, memberID string) error {
	return s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error { return requireOutranks(ctx, tx, workspaceID, caller, memberID) })
}

// requireAnotherActiveAdmin refuses a change that would leave no active member holding the Admin role
// besides agentID.
func requireAnotherActiveAdmin(ctx context.Context, tx pgx.Tx, workspaceID, agentID string) error {
	var others int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM agents a WHERE a.workspace_id=$1 AND a.id<>$2 AND a.deactivated_at IS NULL AND `+HoldsAdminRole,
		workspaceID, agentID).Scan(&others); err != nil {
		return fmt.Errorf("count admins: %w", err)
	}
	if others == 0 {
		return domain.ErrLastAdmin
	}
	return nil
}

// syncLegacyRole keeps agents.role (kept for a rollback, read by nothing) equal to whether the person
// holds the Admin role.
func syncLegacyRole(ctx context.Context, tx pgx.Tx, workspaceID, agentID string) error {
	if _, err := tx.Exec(ctx, `UPDATE agents a SET role=CASE WHEN `+HoldsAdminRole+` THEN 'admin' ELSE 'agent' END
		WHERE a.workspace_id=$1 AND a.id=$2`, workspaceID, agentID); err != nil {
		return fmt.Errorf("sync agent role: %w", err)
	}
	return nil
}

// SetAgentRoles gives a member exactly roles (one at least). The caller must outrank the member and hold
// every permission of each role added or taken; taking Admin from the last active member who holds it
// is domain.ErrLastAdmin. Tokens of the member that follow a removed role stop (they read the role on
// every call); unused invites to the member's address that would give a removed role back are revoked.
func (s *Store) SetAgentRoles(ctx context.Context, workspaceID string, caller Caller, agentID string, roleIDs, roleNames []string) (domain.TeamMember, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.TeamMember, error) {
		// The member's unused invites go first, before any other lock: a redeem in flight holds its link and
		// then waits for the seat lock or the member's row, so taking those first would deadlock with it.
		if _, err := tx.Exec(ctx, `SELECT p.id FROM pairing_links p JOIN agents a ON a.workspace_id=p.workspace_id AND p.email=a.email
			WHERE p.workspace_id=$1 AND a.id=$2 AND p.kind='invite' AND p.used_at IS NULL AND p.revoked_at IS NULL
			ORDER BY p.id FOR UPDATE OF p`, workspaceID, agentID); err != nil {
			return domain.TeamMember{}, fmt.Errorf("lock invites: %w", err)
		}
		if err := lockRoles(ctx, tx, workspaceID); err != nil {
			return domain.TeamMember{}, err
		}
		var email string
		var active bool
		err := tx.QueryRow(ctx, `SELECT email,deactivated_at IS NULL FROM agents WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspaceID, agentID).Scan(&email, &active)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.TeamMember{}, ErrAgentNotFound
		}
		if err != nil {
			return domain.TeamMember{}, fmt.Errorf("load agent: %w", err)
		}
		wanted, err := resolveRoles(ctx, tx, workspaceID, roleIDs, roleNames)
		if err != nil {
			return domain.TeamMember{}, err
		}
		if len(wanted) == 0 {
			return domain.TeamMember{}, domain.ErrNoRoles
		}
		if err := requireOutranks(ctx, tx, workspaceID, caller, agentID); err != nil {
			return domain.TeamMember{}, err
		}
		current, err := agentRoleRecords(ctx, tx, workspaceID, agentID)
		if err != nil {
			return domain.TeamMember{}, err
		}
		has := func(list []roleRecord, id string) bool {
			return slices.ContainsFunc(list, func(role roleRecord) bool { return role.ID == id })
		}
		var changed []roleRecord
		var removed []string
		for _, role := range wanted {
			if !has(current, role.ID) {
				changed = append(changed, role)
			}
		}
		for _, role := range current {
			if !has(wanted, role.ID) {
				changed = append(changed, role)
				removed = append(removed, role.ID)
			}
		}
		if missing := caller.missing(heldBy(changed)); len(missing) > 0 {
			return domain.TeamMember{}, &domain.ExceedsYoursError{Missing: missing}
		}
		for _, role := range current {
			if role.isAdmin() && !has(wanted, role.ID) && active {
				if err := requireAnotherActiveAdmin(ctx, tx, workspaceID, agentID); err != nil {
					return domain.TeamMember{}, err
				}
			}
		}
		if len(removed) > 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM agent_roles WHERE workspace_id=$1 AND agent_id=$2 AND role_id=ANY($3::uuid[])`, workspaceID, agentID, removed); err != nil {
				return domain.TeamMember{}, fmt.Errorf("take roles: %w", err)
			}
			// An unused invite to this address that gives a role taken now would hand it back.
			if _, err := tx.Exec(ctx, `UPDATE pairing_links p SET revoked_at=$4 WHERE p.workspace_id=$1 AND p.kind='invite' AND p.email=lower($2)
				AND p.used_at IS NULL AND p.revoked_at IS NULL
				AND EXISTS (SELECT 1 FROM invite_roles i WHERE i.workspace_id=p.workspace_id AND i.link_id=p.id AND i.role_id=ANY($3::uuid[]))`,
				workspaceID, email, removed, s.now()); err != nil {
				return domain.TeamMember{}, fmt.Errorf("revoke invites: %w", err)
			}
		}
		for _, role := range wanted {
			if _, err := tx.Exec(ctx, `INSERT INTO agent_roles(workspace_id,agent_id,role_id,granted_by_agent_id,granted_at) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5)
				ON CONFLICT DO NOTHING`, workspaceID, agentID, role.ID, caller.AgentID, s.now()); err != nil {
				return domain.TeamMember{}, fmt.Errorf("give role: %w", err)
			}
		}
		if err := syncLegacyRole(ctx, tx, workspaceID, agentID); err != nil {
			return domain.TeamMember{}, err
		}
		return loadTeamMember(ctx, tx, workspaceID, caller, agentID)
	})
}

// ErrAgentNotFound: no member of the workspace has this id.
var ErrAgentNotFound = errors.New("agent not found")

// GiveRolesOnJoin adds roles to a person who redeemed an invite. A person new to the workspace (created)
// holds only them: the starting role the database gave on insert goes, but only once a role of the
// invite was given, so no one joins without a role. An invite none of whose roles remain is
// ErrInviteRolesGone.
func GiveRolesOnJoin(ctx context.Context, tx pgx.Tx, workspaceID, agentID string, roleIDs []string, created bool, grantedBy *string) error {
	if len(roleIDs) == 0 {
		return nil
	}
	var given []string
	rows, err := tx.Query(ctx, `INSERT INTO agent_roles(workspace_id,agent_id,role_id,granted_by_agent_id)
		SELECT $1,$2,r.id,$4 FROM roles r WHERE r.workspace_id=$1 AND r.id=ANY($3::uuid[]) AND r.deleted_at IS NULL
		ON CONFLICT (workspace_id,agent_id,role_id) DO UPDATE SET role_id=EXCLUDED.role_id
		RETURNING role_id::text`, workspaceID, agentID, roleIDs, grantedBy)
	if err != nil {
		return fmt.Errorf("give invite roles: %w", err)
	}
	if given, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		return fmt.Errorf("give invite roles: %w", err)
	}
	if len(given) == 0 {
		return ErrInviteRolesGone
	}
	if created {
		if _, err := tx.Exec(ctx, `DELETE FROM agent_roles WHERE workspace_id=$1 AND agent_id=$2 AND role_id<>ALL($3::uuid[])`, workspaceID, agentID, given); err != nil {
			return fmt.Errorf("replace starting role: %w", err)
		}
	}
	return syncLegacyRole(ctx, tx, workspaceID, agentID)
}

// ErrInviteRolesGone: every role an invite gives was deleted.
var ErrInviteRolesGone = errors.New("every role of the invite was deleted")

// RoleInvalidError is a role field the API refuses with 400.
type RoleInvalidError struct{ Field, Message string }

func (e *RoleInvalidError) Error() string { return e.Message }

// RoleInput is a new role, or a change to one (nil fields stay).
type RoleInput struct {
	Name        *string
	Description *string
	Permissions []string
	// ExpectedUpdatedAt makes the change fail with *domain.RoleChangedError when the role changed since.
	ExpectedUpdatedAt *time.Time
}

func checkRoleName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 60 {
		return "", &RoleInvalidError{Field: "name", Message: "A role needs a name of 1 to 60 characters."}
	}
	return name, nil
}

func checkRoleDescription(description string) (string, error) {
	description = strings.TrimSpace(description)
	if len([]rune(description)) > 300 {
		return "", &RoleInvalidError{Field: "description", Message: "A description is at most 300 characters."}
	}
	return description, nil
}

// CreateRole makes a role; its permissions must all be ones the caller holds.
func (s *Store) CreateRole(ctx context.Context, workspaceID string, caller Caller, input RoleInput) (domain.Role, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.Role, error) {
		if input.Name == nil {
			return domain.Role{}, &RoleInvalidError{Field: "name", Message: "A role needs a name of 1 to 60 characters."}
		}
		name, err := checkRoleName(*input.Name)
		if err != nil {
			return domain.Role{}, err
		}
		description := ""
		if input.Description != nil {
			if description, err = checkRoleDescription(*input.Description); err != nil {
				return domain.Role{}, err
			}
		}
		if input.Permissions == nil {
			input.Permissions = []string{}
		}
		if missing := caller.missing(input.Permissions); len(missing) > 0 {
			return domain.Role{}, &domain.ExceedsYoursError{Missing: missing}
		}
		if err := lockRoles(ctx, tx, workspaceID); err != nil {
			return domain.Role{}, err
		}
		var id string
		err = tx.QueryRow(ctx, `INSERT INTO roles(workspace_id,name,description,permissions,created_by_agent_id,updated_by_agent_id,created_at,updated_at)
			VALUES($1,$2,$3,$4,NULLIF($5,'')::uuid,NULLIF($5,'')::uuid,$6,$6) RETURNING id::text`,
			workspaceID, name, description, input.Permissions, caller.AgentID, s.now()).Scan(&id)
		if isUniqueViolation(err) {
			return domain.Role{}, domain.ErrRoleNameTaken
		}
		if err != nil {
			return domain.Role{}, fmt.Errorf("create role: %w", err)
		}
		return loadRole(ctx, tx, workspaceID, caller, id, false)
	})
}

// UpdateRole renames a role, changes its description or its permissions. Admin is locked; the caller
// must hold every permission the role has and will have.
func (s *Store) UpdateRole(ctx context.Context, workspaceID string, caller Caller, key string, input RoleInput) (domain.Role, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.Role, error) {
		if err := lockRoles(ctx, tx, workspaceID); err != nil {
			return domain.Role{}, err
		}
		role, err := findRole(ctx, tx, workspaceID, key, false, true)
		if err != nil {
			return domain.Role{}, err
		}
		return s.updateRole(ctx, tx, workspaceID, caller, role, input)
	})
}

func (s *Store) updateRole(ctx context.Context, tx pgx.Tx, workspaceID string, caller Caller, role roleRecord, input RoleInput) (domain.Role, error) {
	if role.isAdmin() {
		return domain.Role{}, domain.ErrSystemRoleLocked
	}
	if input.ExpectedUpdatedAt != nil && !input.ExpectedUpdatedAt.Equal(role.UpdatedAt) {
		return domain.Role{}, &domain.RoleChangedError{UpdatedBy: role.UpdatedBy}
	}
	name, description, permissions := role.Name, role.Description, role.permissions()
	var err error
	if input.Name != nil {
		if name, err = checkRoleName(*input.Name); err != nil {
			return domain.Role{}, err
		}
	}
	if input.Description != nil {
		if description, err = checkRoleDescription(*input.Description); err != nil {
			return domain.Role{}, err
		}
	}
	if input.Permissions != nil {
		permissions = input.Permissions
	}
	if missing := caller.missing(append(role.permissions(), permissions...)); len(missing) > 0 {
		return domain.Role{}, &domain.ExceedsYoursError{Missing: missing}
	}
	_, err = tx.Exec(ctx, `UPDATE roles SET name=$3,description=$4,permissions=$5,updated_by_agent_id=NULLIF($6,'')::uuid,updated_at=$7
		WHERE workspace_id=$1 AND id=$2`, workspaceID, role.ID, name, description, permissions, caller.AgentID, s.now())
	if isUniqueViolation(err) {
		return domain.Role{}, domain.ErrRoleNameTaken
	}
	if err != nil {
		return domain.Role{}, fmt.Errorf("update role: %w", err)
	}
	return loadRole(ctx, tx, workspaceID, caller, role.ID, false)
}

// ResetRole puts the built-in Agent role back to permissions (what it starts with).
func (s *Store) ResetRole(ctx context.Context, workspaceID string, caller Caller, key string, permissions []string) (domain.Role, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.Role, error) {
		if err := lockRoles(ctx, tx, workspaceID); err != nil {
			return domain.Role{}, err
		}
		role, err := findRole(ctx, tx, workspaceID, key, false, true)
		if err != nil {
			return domain.Role{}, err
		}
		if role.System == nil || *role.System != domain.SystemRoleAgent {
			return domain.Role{}, domain.ErrSystemRoleLocked
		}
		return s.updateRole(ctx, tx, workspaceID, caller, role, RoleInput{Permissions: permissions})
	})
}

// DeleteRole deletes a role nothing uses (members, pending invites, live tokens); the built-in roles
// cannot go. It can be restored.
func (s *Store) DeleteRole(ctx context.Context, workspaceID string, caller Caller, key string) error {
	return s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		if err := lockRoles(ctx, tx, workspaceID); err != nil {
			return err
		}
		role, err := findRole(ctx, tx, workspaceID, key, false, true)
		if err != nil {
			return err
		}
		if role.System != nil {
			return domain.ErrSystemRoleLocked
		}
		if missing := caller.missing(role.permissions()); len(missing) > 0 {
			return &domain.ExceedsYoursError{Missing: missing}
		}
		inUse, err := roleUse(ctx, tx, workspaceID, role.ID, s.now())
		if err != nil {
			return err
		}
		if len(inUse.Members)+len(inUse.Invites)+len(inUse.Tokens) > 0 {
			return inUse
		}
		if _, err := tx.Exec(ctx, `UPDATE roles SET deleted_at=$3,deleted_by_agent_id=NULLIF($4,'')::uuid WHERE workspace_id=$1 AND id=$2`,
			workspaceID, role.ID, s.now(), caller.AgentID); err != nil {
			return fmt.Errorf("delete role: %w", err)
		}
		return nil
	})
}

// RestoreRole brings a deleted role back (nothing changes for one that is not deleted).
func (s *Store) RestoreRole(ctx context.Context, workspaceID string, caller Caller, key string) (domain.Role, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.Role, error) {
		if err := lockRoles(ctx, tx, workspaceID); err != nil {
			return domain.Role{}, err
		}
		role, err := findRole(ctx, tx, workspaceID, key, true, true)
		if err != nil {
			return domain.Role{}, err
		}
		if missing := caller.missing(role.permissions()); len(missing) > 0 {
			return domain.Role{}, &domain.ExceedsYoursError{Missing: missing}
		}
		if role.DeletedAt != nil {
			_, err := tx.Exec(ctx, `UPDATE roles SET deleted_at=NULL,deleted_by_agent_id=NULL,updated_by_agent_id=NULLIF($3,'')::uuid,updated_at=$4
				WHERE workspace_id=$1 AND id=$2`, workspaceID, role.ID, caller.AgentID, s.now())
			if isUniqueViolation(err) {
				return domain.Role{}, domain.ErrRoleNameTaken
			}
			if err != nil {
				return domain.Role{}, fmt.Errorf("restore role: %w", err)
			}
		}
		return loadRole(ctx, tx, workspaceID, caller, role.ID, false)
	})
}

// roleUse names what still uses a role: members holding it, pending invites giving it, live tokens
// following it.
func roleUse(ctx context.Context, tx pgx.Tx, workspaceID, roleID string, now time.Time) (*domain.RoleInUseError, error) {
	use := &domain.RoleInUseError{Members: []string{}, Invites: []string{}, Tokens: []string{}}
	queries := []struct {
		into  *[]string
		query string
	}{
		{&use.Members, `SELECT ` + memberName + ` FROM agent_roles ar JOIN agents a ON a.workspace_id=ar.workspace_id AND a.id=ar.agent_id
			WHERE ar.workspace_id=$1 AND ar.role_id=$2 ORDER BY 1`},
		{&use.Invites, `SELECT p.email FROM invite_roles i JOIN pairing_links p ON p.workspace_id=i.workspace_id AND p.id=i.link_id
			WHERE i.workspace_id=$1 AND i.role_id=$2 AND p.used_at IS NULL AND p.revoked_at IS NULL AND p.expires_at>$3 ORDER BY 1`},
		{&use.Tokens, `SELECT s.label FROM sessions s WHERE s.workspace_id=$1 AND s.role_id=$2 AND s.revoked_at IS NULL AND s.expires_at>$3 ORDER BY 1`},
	}
	for _, item := range queries {
		args := []any{workspaceID, roleID}
		if strings.Contains(item.query, "$3") {
			args = append(args, now)
		}
		rows, err := tx.Query(ctx, item.query, args...)
		if err != nil {
			return nil, fmt.Errorf("role use: %w", err)
		}
		names, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return nil, fmt.Errorf("role use: %w", err)
		}
		if names != nil {
			*item.into = names
		}
	}
	return use, nil
}

// ListRoles lists the roles (Admin, Agent, then by name), or with deleted only the deleted ones.
func (s *Store) ListRoles(ctx context.Context, workspaceID string, caller Caller, deleted bool) ([]domain.Role, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.Role, error) {
		condition := `r.deleted_at IS NULL`
		if deleted {
			condition = `r.deleted_at IS NOT NULL`
		}
		rows, err := tx.Query(ctx, `SELECT r.id::text FROM roles r WHERE r.workspace_id=$1 AND `+condition+` ORDER BY `+roleOrder, workspaceID)
		if err != nil {
			return nil, fmt.Errorf("list roles: %w", err)
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return nil, fmt.Errorf("list roles: %w", err)
		}
		roles := make([]domain.Role, 0, len(ids))
		for _, id := range ids {
			role, err := loadRole(ctx, tx, workspaceID, caller, id, deleted)
			if err != nil {
				return nil, err
			}
			roles = append(roles, role)
		}
		return roles, nil
	})
}

// GetRole finds one role by id or name.
func (s *Store) GetRole(ctx context.Context, workspaceID string, caller Caller, key string) (domain.Role, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.Role, error) {
		role, err := findRole(ctx, tx, workspaceID, key, false, false)
		if err != nil {
			return domain.Role{}, err
		}
		return loadRole(ctx, tx, workspaceID, caller, role.ID, false)
	})
}

// loadRole reads a role with what uses it and what the caller may do with it.
func loadRole(ctx context.Context, tx pgx.Tx, workspaceID string, caller Caller, id string, deleted bool) (domain.Role, error) {
	record, err := findRole(ctx, tx, workspaceID, id, deleted, false)
	if err != nil {
		return domain.Role{}, err
	}
	role := domain.Role{ID: record.ID, Name: record.Name, Description: record.Description, System: record.System,
		Permissions: record.permissions(), UpdatedBy: record.UpdatedBy, DeletedAt: record.DeletedAt,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt, Members: []domain.RoleMember{}, Tokens: []domain.RoleToken{}}
	if record.System != nil && *record.System == domain.SystemRoleAgent {
		role.DefaultPermissions = slices.Clone(agentRoleDefaults)
	}
	rows, err := tx.Query(ctx, `SELECT a.id::text,`+memberName+`,a.deactivated_at IS NULL FROM agent_roles ar
		JOIN agents a ON a.workspace_id=ar.workspace_id AND a.id=ar.agent_id
		WHERE ar.workspace_id=$1 AND ar.role_id=$2 ORDER BY a.deactivated_at IS NOT NULL, lower(`+memberName+`)`, workspaceID, record.ID)
	if err != nil {
		return domain.Role{}, fmt.Errorf("role members: %w", err)
	}
	members, err := pgx.CollectRows(rows, pgx.RowToStructByPos[domain.RoleMember])
	if err != nil {
		return domain.Role{}, fmt.Errorf("role members: %w", err)
	}
	if members != nil {
		role.Members = members
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM invite_roles i JOIN pairing_links p ON p.workspace_id=i.workspace_id AND p.id=i.link_id
		WHERE i.workspace_id=$1 AND i.role_id=$2 AND p.used_at IS NULL AND p.revoked_at IS NULL AND p.expires_at>now()`, workspaceID, record.ID).Scan(&role.Invites); err != nil {
		return domain.Role{}, fmt.Errorf("role invites: %w", err)
	}
	rows, err = tx.Query(ctx, `SELECT s.id::text,s.label,`+memberName+`,a.id::text FROM sessions s JOIN agents a ON a.workspace_id=s.workspace_id AND a.id=s.agent_id
		WHERE s.workspace_id=$1 AND s.role_id=$2 AND s.scope='role' AND s.revoked_at IS NULL AND s.expires_at>now() ORDER BY lower(s.label)`, workspaceID, record.ID)
	if err != nil {
		return domain.Role{}, fmt.Errorf("role tokens: %w", err)
	}
	tokens, err := pgx.CollectRows(rows, pgx.RowToStructByPos[domain.RoleToken])
	if err != nil {
		return domain.Role{}, fmt.Errorf("role tokens: %w", err)
	}
	if tokens != nil {
		role.Tokens = tokens
	}
	manages := caller.holds(permManageRoles)
	beyond := len(caller.missing(role.Permissions)) > 0
	role.Editable = manages && !record.isAdmin() && !beyond && record.DeletedAt == nil
	role.Assignable = manages && !beyond && record.DeletedAt == nil
	role.Deletable = role.Editable && record.System == nil && len(role.Members) == 0 && role.Invites == 0 && len(role.Tokens) == 0
	return role, nil
}

// HeldPermissionsColumns selects, for a query on agents a, whether the person holds the Admin role and
// the permissions their other roles list.
const HeldPermissionsColumns = HoldsAdminRole + `,ARRAY(SELECT DISTINCT p FROM agent_roles hr JOIN roles hp ON hp.workspace_id=hr.workspace_id AND hp.id=hr.role_id
	CROSS JOIN LATERAL unnest(COALESCE(hp.permissions,'{}'::text[])) p WHERE hr.workspace_id=a.workspace_id AND hr.agent_id=a.id AND hp.deleted_at IS NULL)`

// loadTeamMember reads one member as the caller sees them.
func loadTeamMember(ctx context.Context, tx pgx.Tx, workspaceID string, caller Caller, agentID string) (domain.TeamMember, error) {
	return scanTeamMember(tx.QueryRow(ctx, teamMemberSelect+` WHERE a.workspace_id=$1 AND a.id=$2`, workspaceID, agentID), caller)
}

// RequireOutranksTx is RequireOutranks inside a transaction of the caller's.
func RequireOutranksTx(ctx context.Context, tx pgx.Tx, workspaceID string, caller Caller, memberID string) error {
	return requireOutranks(ctx, tx, workspaceID, caller, memberID)
}
