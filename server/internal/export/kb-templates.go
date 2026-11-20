package export

import "time"

// KBTemplate is a Markdown article template, also in the trash. A template is never public.
type KBTemplate struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Description      string     `json:"description"`
	Body             string     `json:"body"`
	CreatedByAgentID *string    `json:"created_by_agent_id"`
	UpdatedByAgentID *string    `json:"updated_by_agent_id"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	DeletedAt        *time.Time `json:"deleted_at"`
	DeletedByAgentID *string    `json:"deleted_by_agent_id"`
}

// KBTemplateVersion is one change of a template: the template as it was after the change.
type KBTemplateVersion struct {
	TemplateID          string    `json:"template_id"`
	Version             int       `json:"version"`
	Kind                string    `json:"kind"`
	RestoredFromVersion *int      `json:"restored_from_version"`
	Name                string    `json:"name"`
	Description         string    `json:"description"`
	Body                string    `json:"body"`
	ChangedByAgentID    *string   `json:"changed_by_agent_id"`
	TokenID             *string   `json:"token_id"`
	TokenLabel          *string   `json:"token_label"`
	CreatedAt           time.Time `json:"created_at"`
}

func (w *Writer) KBTemplate(record KBTemplate) error {
	return w.record("kb-templates.ndjson", record)
}

func (w *Writer) KBTemplateVersion(record KBTemplateVersion) error {
	return w.record("kb-template-versions.ndjson", record)
}
