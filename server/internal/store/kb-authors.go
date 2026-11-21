package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// errKBAuthorNotOnTeam is the one answer for an author id of another workspace, of a member who left
// and of nobody, so the refusal never tells which ids exist.
var errKBAuthorNotOnTeam = &domain.InvalidKBError{Message: "author_agent_id is not an active member of this team. " +
	"Pick one from GET /api/v1/agents (colliedesk_agents_list) whose active is true."}

// kbAuthorName is the name a team member shows as: the display name, else the email.
const kbAuthorName = `COALESCE(NULLIF(%[1]s.display_name,''),%[1]s.email)`

// setKBArticleAuthor credits the article to agentID, an active member of the team, at once, also on a
// published article, and records it as a version of kind author_change with the author before and
// after. The change moves updated_at, so the list's updated filters find it and an editor holding the
// old updated_at is told the article changed. The same author again changes nothing.
func setKBArticleAuthor(ctx context.Context, tx pgx.Tx, workspaceID string, actor Actor, id, agentID string) error {
	if !savedReplyID.MatchString(agentID) {
		return errKBAuthorNotOnTeam
	}
	var active bool
	err := tx.QueryRow(ctx, `SELECT deactivated_at IS NULL FROM agents WHERE workspace_id=$1 AND id=$2`, workspaceID, agentID).Scan(&active)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !active) {
		return errKBAuthorNotOnTeam
	}
	if err != nil {
		return fmt.Errorf("read knowledge base article author: %w", err)
	}
	var from *string
	if err := tx.QueryRow(ctx, `SELECT author_agent_id::text FROM kb_articles WHERE workspace_id=$1 AND id=$2`, workspaceID, id).Scan(&from); err != nil {
		return fmt.Errorf("read knowledge base article author: %w", err)
	}
	if from != nil && *from == agentID {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE kb_articles SET author_agent_id=$3, updated_at=clock_timestamp() WHERE workspace_id=$1 AND id=$2`, workspaceID, id, agentID); err != nil {
		return fmt.Errorf("change knowledge base article author: %w", err)
	}
	return addKBArticleVersionNaming(ctx, tx, workspaceID, id, actor, domain.KBArticleAuthorChanged, nil, from, &agentID)
}

// fillKBAuthors sets the Author of articles read by scanKBSummary from their AuthorAgentID. One query
// for the whole page.
func fillKBAuthors(ctx context.Context, tx pgx.Tx, workspaceID string, items ...*domain.KBArticleSummary) error {
	ids := []string{}
	for _, item := range items {
		item.Author = nil
		if item.AuthorAgentID != nil {
			ids = append(ids, *item.AuthorAgentID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	authors, err := readKBAuthors(ctx, tx, workspaceID, ids)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.AuthorAgentID != nil {
			item.Author = authors[*item.AuthorAgentID]
		}
	}
	return nil
}

// fillKBAuthorChanges sets the AuthorChange of the versions of kind author_change of an article.
func fillKBAuthorChanges(ctx context.Context, tx pgx.Tx, workspaceID, articleID string, versions []domain.KBArticleVersion) error {
	numbers := []int{}
	for _, version := range versions {
		if version.Kind == domain.KBArticleAuthorChanged {
			numbers = append(numbers, version.Version)
		}
	}
	if len(numbers) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT version, author_from_agent_id::text, author_to_agent_id::text FROM kb_article_versions
		WHERE workspace_id=$1 AND article_id=$2 AND version=ANY($3::int[])`, workspaceID, articleID, numbers)
	if err != nil {
		return fmt.Errorf("read knowledge base author changes: %w", err)
	}
	type change struct{ from, to *string }
	changes := map[int]change{}
	ids := []string{}
	var number int
	var from, to *string
	if _, err := pgx.ForEachRow(rows, []any{&number, &from, &to}, func() error {
		changes[number] = change{from, to}
		for _, id := range []*string{from, to} {
			if id != nil {
				ids = append(ids, *id)
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("read knowledge base author changes: %w", err)
	}
	authors, err := readKBAuthors(ctx, tx, workspaceID, ids)
	if err != nil {
		return err
	}
	pick := func(id *string) *domain.KBArticleAuthor {
		if id == nil {
			return nil
		}
		return authors[*id]
	}
	for i := range versions {
		if found, ok := changes[versions[i].Version]; ok && versions[i].Kind == domain.KBArticleAuthorChanged {
			versions[i].AuthorChange = &domain.KBAuthorChange{From: pick(found.from), To: pick(found.to)}
		}
	}
	return nil
}

// readKBAuthors reads the team members of ids, those who left included, by id.
func readKBAuthors(ctx context.Context, tx pgx.Tx, workspaceID string, ids []string) (map[string]*domain.KBArticleAuthor, error) {
	authors := map[string]*domain.KBArticleAuthor{}
	if len(ids) == 0 {
		return authors, nil
	}
	rows, err := tx.Query(ctx, `SELECT p.id::text, `+fmt.Sprintf(kbAuthorName, "p")+`, p.deactivated_at IS NOT NULL FROM agents p
		WHERE p.workspace_id=$1 AND p.id=ANY($2::uuid[])`, workspaceID, ids)
	if err != nil {
		return nil, fmt.Errorf("read knowledge base authors: %w", err)
	}
	var author domain.KBArticleAuthor
	if _, err := pgx.ForEachRow(rows, []any{&author.AgentID, &author.Name, &author.Left}, func() error {
		found := author
		authors[author.AgentID] = &found
		return nil
	}); err != nil {
		return nil, fmt.Errorf("read knowledge base authors: %w", err)
	}
	return authors, nil
}
