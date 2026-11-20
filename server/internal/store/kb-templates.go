package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// ListKBTemplates returns the workspace's templates outside the trash, ordered by name ignoring case,
// each with its newest version.
func (s *Store) ListKBTemplates(ctx context.Context, workspaceID string) ([]domain.KBTemplate, error) {
	templates, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) ([]domain.KBTemplate, error) {
		rows, err := tx.Query(ctx, kbTemplateListSelect(false)+` WHERE t.workspace_id=$1 AND t.deleted_at IS NULL ORDER BY lower(t.name), t.id`, workspaceID)
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.KBTemplate, error) {
			item, err := scanListedKBTemplate(row, false)
			return item.KBTemplate, err
		})
	})
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	return templates, nil
}

// GetKBTemplate returns a template outside the trash with its newest version.
func (s *Store) GetKBTemplate(ctx context.Context, workspaceID, id string) (domain.KBTemplate, error) {
	if !savedReplyID.MatchString(id) {
		return domain.KBTemplate{}, domain.ErrKBTemplateNotFound
	}
	template, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBTemplate, error) {
		item, err := scanListedKBTemplate(tx.QueryRow(ctx, kbTemplateListSelect(false)+` WHERE t.workspace_id=$1 AND t.id=$2 AND t.deleted_at IS NULL`, workspaceID, id), false)
		return item.KBTemplate, err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.KBTemplate{}, domain.ErrKBTemplateNotFound
	}
	if err != nil {
		return domain.KBTemplate{}, fmt.Errorf("get template: %w", err)
	}
	return template, nil
}

// CreateKBTemplate adds a template written by actor, with version 1 in its history.
func (s *Store) CreateKBTemplate(ctx context.Context, workspaceID string, actor Actor, name, description, body string) (domain.KBTemplate, error) {
	name, err := domain.CleanKBTemplateName(name)
	if err != nil {
		return domain.KBTemplate{}, err
	}
	if description, err = domain.CleanKBTemplateDescription(description); err != nil {
		return domain.KBTemplate{}, err
	}
	if err := domain.CheckKBTemplateBody(body); err != nil {
		return domain.KBTemplate{}, err
	}
	template, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBTemplate, error) {
		// The lock keeps two concurrent creates from passing the count together.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('kb-templates|' || $1::text, 0))`, workspaceID); err != nil {
			return domain.KBTemplate{}, err
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM kb_templates WHERE workspace_id=$1`, workspaceID).Scan(&count); err != nil {
			return domain.KBTemplate{}, err
		}
		if count >= domain.MaxKBTemplates {
			return domain.KBTemplate{}, domain.ErrKBTemplateLimit
		}
		template, err := scanKBTemplate(tx.QueryRow(ctx, `INSERT INTO kb_templates(workspace_id,name,description,body,created_by_agent_id,updated_by_agent_id)
			VALUES($1,$2,$3,$4,$5,$5) RETURNING `+kbTemplateColumns, workspaceID, name, description, body, optionalID(actor.AgentID)))
		if err != nil {
			return domain.KBTemplate{}, err
		}
		template.LastChange, err = addKBTemplateVersion(ctx, tx, workspaceID, template.ID, actor, domain.SavedReplyCreated, nil)
		return template, err
	})
	return template, kbTemplateError("create template", err)
}

// UpdateKBTemplate changes the name, the description, the body or several, and records the version.
// seen is the updated_at the client read: when the template was saved since, it answers
// domain.ErrKBTemplateChanged. Articles made from the template keep their text.
func (s *Store) UpdateKBTemplate(ctx context.Context, workspaceID string, actor Actor, id string, seen time.Time, change domain.KBTemplateChange) (domain.KBTemplate, error) {
	if change.Name != nil {
		name, err := domain.CleanKBTemplateName(*change.Name)
		if err != nil {
			return domain.KBTemplate{}, err
		}
		change.Name = &name
	}
	if change.Description != nil {
		description, err := domain.CleanKBTemplateDescription(*change.Description)
		if err != nil {
			return domain.KBTemplate{}, err
		}
		change.Description = &description
	}
	if change.Body != nil {
		if err := domain.CheckKBTemplateBody(*change.Body); err != nil {
			return domain.KBTemplate{}, err
		}
	}
	return s.changeKBTemplate(ctx, workspaceID, actor, id, true, domain.SavedReplyEdited, nil, func(tx pgx.Tx) pgx.Row {
		return tx.QueryRow(ctx, `UPDATE kb_templates SET name=COALESCE($3,name), description=COALESCE($4,description), body=COALESCE($5,body),
			updated_by_agent_id=$6, updated_at=clock_timestamp()
			WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL AND updated_at=$7 RETURNING `+kbTemplateColumns,
			workspaceID, id, change.Name, change.Description, change.Body, optionalID(actor.AgentID), seen)
	})
}

// RestoreKBTemplateVersion makes version the current text of the template as a new version; the
// history stays as it is. seen works as in UpdateKBTemplate.
func (s *Store) RestoreKBTemplateVersion(ctx context.Context, workspaceID string, actor Actor, id string, version int, seen time.Time) (domain.KBTemplate, error) {
	return s.changeKBTemplate(ctx, workspaceID, actor, id, true, domain.SavedReplyRestored, &version, func(tx pgx.Tx) pgx.Row {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM kb_template_versions WHERE workspace_id=$1 AND template_id=$2 AND version=$3)`,
			workspaceID, id, version).Scan(&exists); err == nil && !exists {
			return errorRow{domain.ErrKBTemplateVersionNotFound}
		}
		return tx.QueryRow(ctx, `UPDATE kb_templates t SET name=v.name, description=v.description, body=v.body, updated_by_agent_id=$5, updated_at=clock_timestamp()
			FROM kb_template_versions v
			WHERE t.workspace_id=$1 AND t.id=$2 AND t.deleted_at IS NULL AND t.updated_at=$4
			AND v.workspace_id=t.workspace_id AND v.template_id=t.id AND v.version=$3
			RETURNING t.id::text,t.name,t.description,t.body,t.created_at,t.updated_at`, workspaceID, id, version, seen, optionalID(actor.AgentID))
	})
}

// DeleteKBTemplate moves a template to the trash and, in the same statement, clears it as the default
// template of every collection. seen works as in UpdateKBTemplate.
func (s *Store) DeleteKBTemplate(ctx context.Context, workspaceID string, actor Actor, id string, seen time.Time) error {
	_, err := s.changeKBTemplate(ctx, workspaceID, actor, id, true, domain.SavedReplyDeleted, nil, func(tx pgx.Tx) pgx.Row {
		return tx.QueryRow(ctx, `WITH t AS (
			UPDATE kb_templates SET deleted_at=clock_timestamp(), deleted_by_agent_id=$3, updated_at=clock_timestamp()
			WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL AND updated_at=$4
			RETURNING id,name,description,body,created_at,updated_at
		), cleared AS (
			UPDATE kb_collections SET default_template_id=NULL WHERE workspace_id=$1 AND default_template_id IN (SELECT id FROM t)
		)
		SELECT id::text,name,description,body,created_at,updated_at FROM t`, workspaceID, id, optionalID(actor.AgentID), seen)
	})
	return err
}

// UndeleteKBTemplate takes a template out of the trash as it was. seen is the updated_at read in the
// trash. A name another template took meanwhile is domain.ErrKBTemplateDuplicateName.
func (s *Store) UndeleteKBTemplate(ctx context.Context, workspaceID string, actor Actor, id string, seen time.Time) (domain.KBTemplate, error) {
	return s.changeKBTemplate(ctx, workspaceID, actor, id, false, domain.SavedReplyUndeleted, nil, func(tx pgx.Tx) pgx.Row {
		return tx.QueryRow(ctx, `UPDATE kb_templates SET deleted_at=NULL, deleted_by_agent_id=NULL, updated_at=clock_timestamp()
			WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NOT NULL AND updated_at=$3 RETURNING `+kbTemplateColumns, workspaceID, id, seen)
	})
}

// changeKBTemplate runs an optimistic write of a template, outside the trash or (live false) in it,
// and records it as a version of kind. No row means the template is not there (not found) or was
// saved since (changed).
func (s *Store) changeKBTemplate(ctx context.Context, workspaceID string, actor Actor, id string, live bool, kind string, restoredFrom *int, write func(pgx.Tx) pgx.Row) (domain.KBTemplate, error) {
	if !savedReplyID.MatchString(id) {
		return domain.KBTemplate{}, domain.ErrKBTemplateNotFound
	}
	template, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.KBTemplate, error) {
		template, err := scanKBTemplate(write(tx))
		if errors.Is(err, pgx.ErrNoRows) {
			var there bool
			if checkErr := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM kb_templates WHERE workspace_id=$1 AND id=$2 AND (deleted_at IS NULL)=$3)`,
				workspaceID, id, live).Scan(&there); checkErr == nil && there {
				return domain.KBTemplate{}, domain.ErrKBTemplateChanged
			}
			return domain.KBTemplate{}, domain.ErrKBTemplateNotFound
		}
		if err != nil {
			return domain.KBTemplate{}, err
		}
		template.LastChange, err = addKBTemplateVersion(ctx, tx, workspaceID, id, actor, kind, restoredFrom)
		return template, err
	})
	return template, kbTemplateError("change template", err)
}

// kbTemplateError keeps the domain errors and names what failed otherwise.
func kbTemplateError(what string, err error) error {
	var invalid *domain.InvalidSavedReplyError
	switch {
	case err == nil:
		return nil
	case isDuplicateKBTemplateName(err):
		return domain.ErrKBTemplateDuplicateName
	case errors.As(err, &invalid), errors.Is(err, domain.ErrKBTemplateNotFound), errors.Is(err, domain.ErrKBTemplateChanged),
		errors.Is(err, domain.ErrKBTemplateLimit), errors.Is(err, domain.ErrKBTemplateVersionNotFound):
		return err
	}
	return fmt.Errorf("%s: %w", what, err)
}

// kbTemplateBody returns the body of a template of the workspace for a new article:
// domain.ErrKBTemplateNotFound when there is none, domain.ErrKBTemplateInTrash when it is in the trash.
func kbTemplateBody(ctx context.Context, tx pgx.Tx, workspaceID, id string) (string, error) {
	if !savedReplyID.MatchString(id) {
		return "", domain.ErrKBTemplateNotFound
	}
	var body string
	var trashed bool
	err := tx.QueryRow(ctx, `SELECT body, deleted_at IS NOT NULL FROM kb_templates WHERE workspace_id=$1 AND id=$2`, workspaceID, id).Scan(&body, &trashed)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", domain.ErrKBTemplateNotFound
	case err != nil:
		return "", err
	case trashed:
		return "", domain.ErrKBTemplateInTrash
	}
	return body, nil
}

// checkKBDefaultTemplate refuses a collection default that names no template of the workspace or one
// in the trash.
func checkKBDefaultTemplate(ctx context.Context, tx pgx.Tx, workspaceID string, id *string) error {
	if id == nil {
		return nil
	}
	_, err := kbTemplateBody(ctx, tx, workspaceID, *id)
	return err
}

// kbArticleStartingBody is the text a new article starts with: its BodyMD, the body of TemplateID, or
// with BodyFromCollection the body of its home collection's default template (empty without one).
func kbArticleStartingBody(ctx context.Context, tx pgx.Tx, workspaceID string, input KBArticleInput) (string, error) {
	id := input.TemplateID
	if input.BodyFromCollection {
		var fallback *string
		if err := tx.QueryRow(ctx, `SELECT default_template_id::text FROM kb_collections WHERE workspace_id=$1 AND id=$2`,
			workspaceID, input.CollectionID).Scan(&fallback); err != nil {
			return "", err
		}
		if fallback == nil {
			return input.BodyMD, nil
		}
		id = *fallback
	}
	if id == "" {
		return input.BodyMD, nil
	}
	body, err := kbTemplateBody(ctx, tx, workspaceID, id)
	if err != nil {
		return "", err
	}
	return body, domain.CheckKBBody(body)
}
