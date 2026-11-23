package store

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// KBEmailSender is what a workspace knows about mail sent to its knowledge base address: the active
// member whose address sent it, whether that member may write articles, the collection a draft goes
// to, and the draft the same Message-ID already made.
type KBEmailSender struct {
	// AgentID is the active member with the From address; "" when there is none.
	AgentID string
	// Permitted is true when that member holds the permission asked about.
	Permitted bool
	// CollectionID is the first live collection, the order the knowledge base lists them in; "" when
	// the workspace has none.
	CollectionID string
	// ArticleID is the draft this Message-ID made before; "" when it is new.
	ArticleID string
}

// KBEmailSenderOf reads, in one query, who sent mail from the address from with messageID to the
// workspace's knowledge base address, whether they hold permission, and where a draft of it would go.
func (s *Store) KBEmailSenderOf(ctx context.Context, workspaceID, from, messageID, permission string) (KBEmailSender, error) {
	sender, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (KBEmailSender, error) {
		var agentID, collectionID, articleID *string
		var permitted *bool
		err := tx.QueryRow(ctx, `SELECT m.id, m.permitted, c.id, e.article_id::text
			FROM (SELECT 1) one
			LEFT JOIN LATERAL (SELECT a.id::text AS id, `+HoldsPermission("$3")+` AS permitted FROM agents a
				WHERE a.workspace_id=$1 AND lower(a.email)=lower($2) AND a.deactivated_at IS NULL ORDER BY a.id LIMIT 1) m ON true
			LEFT JOIN LATERAL (SELECT c.id::text AS id FROM kb_collections c WHERE c.workspace_id=$1 AND c.deleted_at IS NULL
				ORDER BY c.parent_id NULLS FIRST, c.position, lower(c.name), c.id LIMIT 1) c ON true
			LEFT JOIN kb_email_articles e ON e.workspace_id=$1 AND e.message_id=$4`,
			workspaceID, from, permission, messageID).Scan(&agentID, &permitted, &collectionID, &articleID)
		return KBEmailSender{AgentID: deref(agentID), Permitted: permitted != nil && *permitted,
			CollectionID: deref(collectionID), ArticleID: deref(articleID)}, err
	})
	if err != nil {
		return KBEmailSender{}, fmt.Errorf("read knowledge base mail sender: %w", err)
	}
	return sender, nil
}

// KBEmailFile is an attachment of mail to the knowledge base address, already written to storage and
// checked: its name, type and searchable text.
type KBEmailFile struct {
	Name, ContentType, Text string
	Object                  attachments.Object
}

// KBEmailDraft is the draft article mail to the knowledge base address makes.
type KBEmailDraft struct {
	// MessageID is the mail's Message-ID; the same one never makes a second draft.
	MessageID string
	Article   KBArticleInput
	Files     []KBEmailFile
}

// KBEmailResult is what CreateKBEmailDraft did: the article, false in Created when the Message-ID had
// already made it, and the names of the files it could not list.
type KBEmailResult struct {
	ArticleID string
	Created   bool
	Skipped   []string
}

var errKBEmailSeen = errors.New("the mail already made a draft")

// CreateKBEmailDraft writes the draft a member's mail makes, with its files in the media library and
// listed on it, in one transaction, so a failure leaves nothing. Mail whose Message-ID made a draft
// before answers that draft. A file whose bytes are an image or video of the library, or that the
// library has no room for, is left out and named in Skipped. When it fails, the caller owns the
// stored bytes; on success the bytes of skipped files and of a duplicate are the caller's to discard.
func (s *Store) CreateKBEmailDraft(ctx context.Context, workspaceID string, actor Actor, draft KBEmailDraft) (KBEmailResult, error) {
	title, err := prepareKBArticle(&draft.Article)
	if err != nil {
		return KBEmailResult{}, err
	}
	result, err := scoped(ctx, s, workspaceID, func(tx pgx.Tx) (KBEmailResult, error) {
		article, err := insertKBArticle(ctx, tx, workspaceID, actor, title, draft.Article)
		if err != nil {
			return KBEmailResult{}, err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO kb_email_articles(workspace_id,message_id,article_id) VALUES($1,$2,$3)
			ON CONFLICT (workspace_id, message_id) DO NOTHING`, workspaceID, draft.MessageID, article.ID)
		if err != nil {
			return KBEmailResult{}, err
		}
		if tag.RowsAffected() == 0 {
			return KBEmailResult{}, errKBEmailSeen
		}
		ids, skipped, err := s.addKBEmailFiles(ctx, tx, workspaceID, actor, draft.Files)
		if err != nil {
			return KBEmailResult{}, err
		}
		if len(ids) > 0 {
			if err := ImportKBAttachments(ctx, tx, workspaceID, article.ID, nil, ids); err != nil {
				return KBEmailResult{}, err
			}
		}
		return KBEmailResult{ArticleID: article.ID, Created: true, Skipped: skipped}, nil
	})
	if errors.Is(err, errKBEmailSeen) {
		// A delivery of the same mail committed while this one ran; answer its draft.
		sender, err := s.KBEmailSenderOf(ctx, workspaceID, "", draft.MessageID, "")
		if err != nil {
			return KBEmailResult{}, err
		}
		return KBEmailResult{ArticleID: sender.ArticleID}, nil
	}
	return result, kbError("create knowledge base draft from mail", err)
}

// addKBEmailFiles puts files in the media library, reusing the item that already holds the same
// bytes, and returns the ids to list on the draft and the names left out.
func (s *Store) addKBEmailFiles(ctx context.Context, tx pgx.Tx, workspaceID string, actor Actor, files []KBEmailFile) ([]string, []string, error) {
	var ids, skipped []string
	var fresh []KBEmailFile
	var freshBytes int64
	for _, file := range files {
		if len(ids)+len(fresh) >= domain.MaxKBAttachments {
			skipped = append(skipped, file.Name)
			continue
		}
		upload, err := cleanKBImageUpload(domain.KBImageUpload{Name: file.Name})
		if err != nil {
			skipped = append(skipped, file.Name)
			continue
		}
		existing, err := touchKBImage(ctx, tx, workspaceID, file.Object.SHA256, upload)
		switch {
		case err == nil && existing.Kind == domain.KBMediaFile:
			ids = append(ids, existing.ID)
		case err == nil:
			skipped = append(skipped, file.Name)
		case errors.Is(err, pgx.ErrNoRows):
			file.Name = upload.Name
			fresh = append(fresh, file)
			freshBytes += file.Object.Size
		default:
			return nil, nil, err
		}
	}
	if len(fresh) == 0 {
		return ids, skipped, nil
	}
	if err := s.CheckStorage(ctx, tx, workspaceID, freshBytes, s.now()); err != nil {
		return nil, nil, err
	}
	if err := lockKB(ctx, tx, workspaceID); err != nil {
		return nil, nil, err
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM kb_media WHERE workspace_id=$1`, workspaceID).Scan(&count); err != nil {
		return nil, nil, err
	}
	for _, file := range fresh {
		if count >= domain.MaxKBMedia {
			skipped = append(skipped, file.Name)
			continue
		}
		if err := VerifyObjects(ctx, tx, s.objects, file.Object.Key); err != nil {
			return nil, nil, err
		}
		var text any
		if file.Text != "" {
			text = file.Text
		}
		var id string
		err := tx.QueryRow(ctx, `INSERT INTO kb_media(workspace_id,kind,storage_key,sha256,size,content_type,created_by_agent_id,
				updated_by_agent_id,name,text,inspected_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$7,$8,$9,clock_timestamp()) ON CONFLICT (workspace_id, sha256) DO NOTHING RETURNING id::text`,
			workspaceID, domain.KBMediaFile, file.Object.Key, file.Object.SHA256, file.Object.Size, file.ContentType, optionalID(actor.AgentID),
			file.Name, text).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			// Two attachments of this mail hold the same bytes; the first one's item serves both.
			existing, err := touchKBImage(ctx, tx, workspaceID, file.Object.SHA256, domain.KBImageUpload{})
			if err != nil {
				return nil, nil, err
			}
			if existing.Kind != domain.KBMediaFile || slices.Contains(ids, existing.ID) {
				skipped = append(skipped, file.Name)
				continue
			}
			id = existing.ID
		} else if err != nil {
			return nil, nil, err
		} else {
			count++
		}
		ids = append(ids, id)
	}
	return ids, skipped, nil
}
