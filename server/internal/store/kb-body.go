package store

import (
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/kbformat"
)

// cleanKBBody writes the formatting tags of a text a person or an agent writes into an article in
// their canonical spelling (kbformat.Normalize), so the visual editor reads back what it wrote, then
// checks its length. Imports keep their source's bytes: their reconcile compares them.
func cleanKBBody(body *string) error {
	*body = kbformat.Normalize(*body)
	return domain.CheckKBBody(*body)
}

// cleanKBTemplateBody is cleanKBBody for a template, whose body starts new articles.
func cleanKBTemplateBody(body *string) error {
	*body = kbformat.Normalize(*body)
	return domain.CheckKBTemplateBody(*body)
}
