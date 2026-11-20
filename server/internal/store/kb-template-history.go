package store

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const kbTemplateColumns = `id::text,name,description,body,created_at,updated_at`

// kbTemplateChangeColumns reads a template version v with its person a, in the order changeRow scans.
// Templates never had agent requests, so that column is always NULL.
const kbTemplateChangeColumns = `v.version,v.kind,v.restored_from_version,v.changed_by_agent_id::text,COALESCE(NULLIF(a.display_name,''),a.email),
	v.token_id::text,v.token_label,NULL::text,v.created_at`

// kbTemplateListSelect reads templates t with their newest version; trash adds who deleted them.
func kbTemplateListSelect(trash bool) string {
	extra, join := ``, ``
	if trash {
		extra = `,t.deleted_at,t.deleted_by_agent_id::text,COALESCE(NULLIF(d.display_name,''),d.email)`
		join = ` LEFT JOIN agents d ON d.workspace_id=t.workspace_id AND d.id=t.deleted_by_agent_id`
	}
	return `SELECT t.id::text,t.name,t.description,t.body,t.created_at,t.updated_at,` + kbTemplateChangeColumns + extra + `
	FROM kb_templates t
	LEFT JOIN LATERAL (SELECT * FROM kb_template_versions x WHERE x.workspace_id=t.workspace_id AND x.template_id=t.id ORDER BY x.version DESC LIMIT 1) v ON true
	LEFT JOIN agents a ON a.workspace_id=v.workspace_id AND a.id=v.changed_by_agent_id` + join
}

func scanKBTemplate(row pgx.Row) (domain.KBTemplate, error) {
	var template domain.KBTemplate
	err := row.Scan(&template.ID, &template.Name, &template.Description, &template.Body, &template.CreatedAt, &template.UpdatedAt)
	template.CreatedAt, template.UpdatedAt = domain.UTC(template.CreatedAt), domain.UTC(template.UpdatedAt)
	return template, err
}

// scanListedKBTemplate reads a row of kbTemplateListSelect; trash says the trash columns follow.
func scanListedKBTemplate(row pgx.Row, trash bool) (domain.TrashedKBTemplate, error) {
	var item domain.TrashedKBTemplate
	var change changeRow
	targets := append([]any{&item.ID, &item.Name, &item.Description, &item.Body, &item.CreatedAt, &item.UpdatedAt}, change.targets()...)
	var deleterID, deleterName *string
	if trash {
		targets = append(targets, &item.DeletedAt, &deleterID, &deleterName)
	}
	if err := row.Scan(targets...); err != nil {
		return item, err
	}
	item.CreatedAt, item.UpdatedAt = domain.UTC(item.CreatedAt), domain.UTC(item.UpdatedAt)
	item.LastChange = change.change()
	if trash {
		item.DeletedAt = domain.UTC(item.DeletedAt)
		item.DeletedBy = savedReplyPerson(deleterID, deleterName)
		item.PurgesAt = item.DeletedAt.Add(domain.KBTrashRetention)
	}
	return item, nil
}

func isDuplicateKBTemplateName(err error) bool {
	var violation *pgconn.PgError
	return errors.As(err, &violation) && violation.Code == uniqueViolation && violation.ConstraintName == "kb_templates_name_idx"
}

// addKBTemplateVersion records the template's current name, description and body as its next
// version. The caller changed the template row in tx first, so the row lock orders its versions.
func addKBTemplateVersion(ctx context.Context, tx pgx.Tx, workspaceID, id string, actor Actor, kind string, restoredFrom *int) (*domain.SavedReplyChange, error) {
	var change changeRow
	err := tx.QueryRow(ctx, `WITH v AS (
		INSERT INTO kb_template_versions(workspace_id,template_id,version,kind,restored_from_version,name,description,body,changed_by_agent_id,token_id,token_label,created_at)
		SELECT t.workspace_id,t.id,COALESCE((SELECT max(version) FROM kb_template_versions WHERE workspace_id=$1 AND template_id=$2),0)+1,
			$3,$4,t.name,t.description,t.body,$5,s.id,s.label,clock_timestamp()
		FROM kb_templates t LEFT JOIN sessions s ON s.workspace_id=t.workspace_id AND s.id=$6
		WHERE t.workspace_id=$1 AND t.id=$2
		RETURNING *)
		SELECT `+kbTemplateChangeColumns+` FROM v LEFT JOIN agents a ON a.workspace_id=v.workspace_id AND a.id=v.changed_by_agent_id`,
		workspaceID, id, kind, restoredFrom, optionalID(actor.AgentID), optionalID(actor.TokenID)).Scan(change.targets()...)
	if err != nil {
		return nil, fmt.Errorf("record template version: %w", err)
	}
	return change.change(), nil
}

// ListTrashedKBTemplates returns one page of the workspace's templates in the trash, most recently
// deleted first, and the cursor of the next page ("" on the last one).
func (s *Store) ListTrashedKBTemplates(ctx context.Context, workspaceID string, limit int, cursor string) ([]domain.TrashedKBTemplate, string, error) {
	query := kbTemplateListSelect(true) + ` WHERE t.workspace_id=$1 AND t.deleted_at IS NOT NULL`
	args := []any{workspaceID, limit + 1}
	if cursor != "" {
		at, id, err := decodeTrashCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		query += ` AND (t.deleted_at, t.id) < ($3, $4)`
		args = append(args, at, id)
	}
	items, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.TrashedKBTemplate, error) {
		rows, err := tx.Query(ctx, query+` ORDER BY t.deleted_at DESC, t.id DESC LIMIT $2`, args...)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.TrashedKBTemplate, error) {
			return scanListedKBTemplate(row, true)
		})
	})
	if err != nil {
		return nil, "", fmt.Errorf("list template trash: %w", err)
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1]
		next = base64.RawURLEncoding.EncodeToString([]byte(last.DeletedAt.Format(time.RFC3339Nano) + " " + last.ID))
	}
	return items, next, nil
}

// KBTemplateVersions returns one page of the versions of a template, in or out of the trash, newest
// first: at most limit versions older than before (0 means from the newest), and the version to pass
// as before for the next page (0 on the last one).
func (s *Store) KBTemplateVersions(ctx context.Context, workspaceID, id string, limit, before int) ([]domain.KBTemplateVersion, int, error) {
	if !savedReplyID.MatchString(id) {
		return nil, 0, domain.ErrKBTemplateNotFound
	}
	if before <= 0 {
		before = math.MaxInt32
	}
	versions, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.KBTemplateVersion, error) {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM kb_templates WHERE workspace_id=$1 AND id=$2)`, workspaceID, id).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, domain.ErrKBTemplateNotFound
		}
		rows, err := tx.Query(ctx, `SELECT `+kbTemplateChangeColumns+`,v.name,v.description,v.body FROM kb_template_versions v
			LEFT JOIN agents a ON a.workspace_id=v.workspace_id AND a.id=v.changed_by_agent_id
			WHERE v.workspace_id=$1 AND v.template_id=$2 AND v.version<$3 ORDER BY v.version DESC LIMIT $4`, workspaceID, id, before, limit+1)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.KBTemplateVersion, error) {
			var change changeRow
			var version domain.KBTemplateVersion
			if err := row.Scan(append(change.targets(), &version.Name, &version.Description, &version.Body)...); err != nil {
				return version, err
			}
			version.SavedReplyChange = *change.change()
			return version, nil
		})
	})
	if errors.Is(err, domain.ErrKBTemplateNotFound) {
		return nil, 0, err
	}
	if err != nil {
		return nil, 0, fmt.Errorf("list template versions: %w", err)
	}
	next := 0
	if len(versions) > limit {
		versions = versions[:limit]
		next = versions[limit-1].Version
	}
	return versions, next, nil
}
