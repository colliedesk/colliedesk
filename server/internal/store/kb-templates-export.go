package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/export"
)

// exportKBTemplates writes the workspace's article templates, the trashed ones too, then every version.
func exportKBTemplates(ctx context.Context, tx pgx.Tx, workspaceID string, sink export.Sink) error {
	rows, err := tx.Query(ctx, `SELECT id::text,name,description,body,created_by_agent_id::text,updated_by_agent_id::text,created_at,updated_at,deleted_at,deleted_by_agent_id::text
		FROM kb_templates WHERE workspace_id=$1 ORDER BY id`, workspaceID)
	if err != nil {
		return fmt.Errorf("export knowledge base templates: %w", err)
	}
	for rows.Next() {
		var record export.KBTemplate
		if err := rows.Scan(&record.ID, &record.Name, &record.Description, &record.Body, &record.CreatedByAgentID, &record.UpdatedByAgentID,
			&record.CreatedAt, &record.UpdatedAt, &record.DeletedAt, &record.DeletedByAgentID); err != nil {
			rows.Close()
			return fmt.Errorf("export knowledge base templates: %w", err)
		}
		record.CreatedAt, record.UpdatedAt, record.DeletedAt = domain.UTC(record.CreatedAt), domain.UTC(record.UpdatedAt), domain.UTCOptional(record.DeletedAt)
		if err := sink.KBTemplate(record); err != nil {
			rows.Close()
			return err
		}
	}
	if err := rowsError("export knowledge base templates", rows); err != nil {
		return err
	}

	rows, err = tx.Query(ctx, `SELECT template_id::text,version,kind,restored_from_version,name,description,body,changed_by_agent_id::text,token_id::text,token_label,created_at
		FROM kb_template_versions WHERE workspace_id=$1 ORDER BY template_id,version`, workspaceID)
	if err != nil {
		return fmt.Errorf("export knowledge base template versions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var record export.KBTemplateVersion
		if err := rows.Scan(&record.TemplateID, &record.Version, &record.Kind, &record.RestoredFromVersion, &record.Name, &record.Description, &record.Body,
			&record.ChangedByAgentID, &record.TokenID, &record.TokenLabel, &record.CreatedAt); err != nil {
			return fmt.Errorf("export knowledge base template versions: %w", err)
		}
		record.CreatedAt = domain.UTC(record.CreatedAt)
		if err := sink.KBTemplateVersion(record); err != nil {
			return err
		}
	}
	return rowsError("export knowledge base template versions", rows)
}
