package store

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/colliedesk/colliedesk/server/internal/kblinks"
	"github.com/colliedesk/colliedesk/server/internal/workspacehost"
)

// WithHelpCenterHosts returns a store that reads a sent reply's article links on this server's help
// center hosts: origin (APP_ORIGIN) and <slug>.<helpCenterDomain>. Without it only custom domains and
// old Freshdesk hosts are read.
func (s *Store) WithHelpCenterHosts(origin, helpCenterDomain string) *Store {
	copy := *s
	copy.appHost, copy.helpCenterDomain = "", strings.ToLower(helpCenterDomain)
	if parsed, err := url.Parse(origin); err == nil {
		copy.appHost = strings.ToLower(parsed.Hostname())
	}
	return &copy
}

// recordKBArticleUses records, inside the reply's transaction, the articles a sent reply used: the
// help center articles its body links to (current addresses on the workspace's own help center hosts,
// or old Freshdesk addresses of imported articles on those or on the Freshdesk hosts it imported from), and the sources of the AI draft it was sent
// from. Each article counts once per reply and kind. Notes, notices and drafts not sent never reach it.
func (s *Store) recordKBArticleUses(ctx context.Context, tx pgx.Tx, workspaceID, conversationID, messageID, body, draftID string) error {
	site, err := s.helpCenterSite(ctx, tx, workspaceID)
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

// helpCenterSite is what names the workspace's help center in an address: its slug, this server's
// hosts, its verified custom domain and the Freshdesk hosts of its imports (the account's host and the
// old help center domain the import was given).
func (s *Store) helpCenterSite(ctx context.Context, tx pgx.Tx, workspaceID string) (kblinks.Site, error) {
	site := kblinks.Site{AppHost: s.appHost}
	if err := tx.QueryRow(ctx, `SELECT w.slug,
			COALESCE((SELECT array_agg(d.hostname) FROM help_center_domains d WHERE d.workspace_id=w.id AND d.status='active'), '{}'),
			COALESCE((SELECT array_agg(DISTINCT lower(h.host)) FROM import_jobs j
				CROSS JOIN LATERAL (VALUES (j.domain), (j.options->>'old_help_center_domain')) h(host)
				WHERE j.workspace_id=w.id AND j.kind='import' AND j.source='freshdesk' AND COALESCE(h.host,'')<>''), '{}')
		FROM workspaces w WHERE w.id=$1`, workspaceID).Scan(&site.Slug, &site.Hosts, &site.FreshdeskHosts); err != nil {
		return kblinks.Site{}, fmt.Errorf("read help center site: %w", err)
	}
	site.SlugHost = workspacehost.Host(s.helpCenterDomain, site.Slug)
	return site, nil
}
