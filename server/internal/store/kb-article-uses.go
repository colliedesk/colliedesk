package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/colliedesk/colliedesk/server/internal/kblinks"
)

// recordKBArticleUses records, inside the reply's transaction, the articles a sent reply used: the
// help center articles its body links to (current addresses on any of the workspace's help center
// hosts, or old Freshdesk addresses of imported articles), and the sources of the AI draft it was sent
// from. Each article counts once per reply and kind. Notes, notices and drafts not sent never reach it.
func recordKBArticleUses(ctx context.Context, tx pgx.Tx, workspaceID, conversationID, messageID, body, draftID string) error {
	site, err := helpCenterSite(ctx, tx, workspaceID)
	if err != nil {
		return err
	}
	links := kblinks.Find(body, site)
	if len(links.Numbers) > 0 || len(links.Freshdesk) > 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO kb_article_uses(workspace_id,kb_article_id,conversation_id,message_id,kind)
			SELECT $1,a.id,$2,$3,'link' FROM kb_articles a
			WHERE a.workspace_id=$1 AND (a.number=ANY($4::bigint[]) OR a.id IN (
				SELECT i.target_id FROM import_items i WHERE i.workspace_id=$1 AND i.entity='kb_article'
					AND i.source_id=ANY($5::text[]) AND i.source LIKE 'freshdesk:%' AND i.status='done'))
			ON CONFLICT DO NOTHING`, workspaceID, conversationID, messageID, links.Numbers, links.Freshdesk); err != nil {
			return fmt.Errorf("record linked articles: %w", err)
		}
	}
	if draftID == "" {
		return nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO kb_article_uses(workspace_id,kb_article_id,conversation_id,message_id,kind)
		SELECT DISTINCT s.workspace_id,s.kb_article_id,$2::uuid,$3::uuid,'ai_source' FROM ai_draft_sources s
		JOIN kb_articles a ON a.workspace_id=s.workspace_id AND a.id=s.kb_article_id
		WHERE s.workspace_id=$1 AND s.draft_id=$4
		ON CONFLICT DO NOTHING`, workspaceID, conversationID, messageID, draftID); err != nil {
		return fmt.Errorf("record draft sources: %w", err)
	}
	return nil
}

// helpCenterSite is what names the workspace's help center in an address: its slug and its claimed
// custom domain.
func helpCenterSite(ctx context.Context, tx pgx.Tx, workspaceID string) (kblinks.Site, error) {
	var site kblinks.Site
	var host *string
	if err := tx.QueryRow(ctx, `SELECT w.slug, d.hostname FROM workspaces w
		LEFT JOIN help_center_domains d ON d.workspace_id=w.id AND d.status='active'
		WHERE w.id=$1`, workspaceID).Scan(&site.Slug, &host); err != nil {
		return kblinks.Site{}, fmt.Errorf("read help center site: %w", err)
	}
	if host != nil {
		site.Hosts = []string{*host}
	}
	return site, nil
}
