package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Limits of knowledge base article templates. They count characters, not bytes.
const (
	// MaxKBTemplates counts the templates in the trash too, so a create and delete loop cannot grow
	// the trash without bound.
	MaxKBTemplates           = 200
	MaxKBTemplateName        = 200
	MaxKBTemplateDescription = 300
	MaxKBTemplateBody        = 100000
)

// KBTemplate is Markdown a team starts new articles from. Creating an article copies the body once;
// editing the template later never changes the articles made from it. A template is never public.
type KBTemplate struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Body        string    `json:"body"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	// LastChange is the newest version: who made it and through which token.
	LastChange *SavedReplyChange `json:"last_change"`
}

// TrashedKBTemplate is a deleted template, restorable until PurgesAt.
type TrashedKBTemplate struct {
	KBTemplate
	DeletedAt time.Time         `json:"deleted_at"`
	DeletedBy *SavedReplyPerson `json:"deleted_by"`
	PurgesAt  time.Time         `json:"purges_at"`
}

// KBTemplateVersion is one version of a template with the name, description and body it set.
type KBTemplateVersion struct {
	SavedReplyChange
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body"`
}

// KBTemplateChange is an edit of a template; a nil field stays as it is.
type KBTemplateChange struct {
	Name        *string
	Description *string
	Body        *string
}

var (
	// ErrKBTemplateNotFound means the id names no template of the workspace outside the trash.
	ErrKBTemplateNotFound = errors.New("template not found")
	// ErrKBTemplateInTrash means an article or a collection named a template that is in the trash.
	ErrKBTemplateInTrash = errors.New("template is in the trash; undelete it or pick another")
	// ErrKBTemplateDuplicateName means another template outside the trash has the name, ignoring case.
	ErrKBTemplateDuplicateName = errors.New("a template with this name already exists; pick another name")
	// ErrKBTemplateChanged means the template was saved by someone else after the client read it.
	ErrKBTemplateChanged = errors.New("this template was changed by someone else; reload to see their version")
	// ErrKBTemplateLimit means the workspace already holds MaxKBTemplates templates. Nothing deletes a
	// template for good before its 30 days in the trash, so the way on is to edit one.
	ErrKBTemplateLimit = fmt.Errorf("a workspace holds at most %d templates, counting the ones in the trash for 30 days; edit one you no longer need instead", MaxKBTemplates)
	// ErrKBTemplateVersionNotFound means the template has no version with that number.
	ErrKBTemplateVersionNotFound = errors.New("template version not found")
)

// CleanKBTemplateName trims the name and checks its length.
func CleanKBTemplateName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", &InvalidSavedReplyError{"A template needs a name."}
	case utf8.RuneCountInString(name) > MaxKBTemplateName:
		return "", &InvalidSavedReplyError{fmt.Sprintf("A template name holds at most %d characters.", MaxKBTemplateName)}
	}
	return name, nil
}

// CleanKBTemplateDescription trims the description: one line, at most MaxKBTemplateDescription
// characters, empty allowed.
func CleanKBTemplateDescription(description string) (string, error) {
	description = strings.TrimSpace(description)
	switch {
	case strings.ContainsAny(description, "\r\n"):
		return "", &InvalidSavedReplyError{"A template description is one line."}
	case utf8.RuneCountInString(description) > MaxKBTemplateDescription:
		return "", &InvalidSavedReplyError{fmt.Sprintf("A template description holds at most %d characters.", MaxKBTemplateDescription)}
	}
	return description, nil
}

// CheckKBTemplateBody refuses an empty body or one that is too long. The body is stored as written.
func CheckKBTemplateBody(body string) error {
	switch {
	case strings.TrimSpace(body) == "":
		return &InvalidSavedReplyError{"A template needs text."}
	case utf8.RuneCountInString(body) > MaxKBTemplateBody:
		return &InvalidSavedReplyError{fmt.Sprintf("A template text holds at most %d characters.", MaxKBTemplateBody)}
	}
	return nil
}
