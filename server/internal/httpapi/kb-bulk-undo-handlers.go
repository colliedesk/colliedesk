package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// kbBulkUndoItem is one article's line in the answer to an undo: undone, or not undone and why.
type kbBulkUndoItem struct {
	ID      string             `json:"id"`
	Number  *int64             `json:"number,omitempty"`
	Title   string             `json:"title,omitempty"`
	Outcome string             `json:"outcome"`
	Error   *kbBulkResultError `json:"error,omitempty"`
}

// undoKBBulkOperation puts back what a bulk action changed, within ten minutes and once. An article
// changed since the action is left as it is and listed as not undone, so undo never overwrites newer
// work; each article put back gets its own history entry, as a change by hand would.
func (a *API) undoKBBulkOperation(response http.ResponseWriter, request *http.Request) {
	who := principal(request)
	if a.refuseUnlessHeld(response, request, who, auth.PermKBArticlesUpdate, nil) {
		return
	}
	id := request.PathValue("id")
	a.runOnce(response, request, http.StatusOK, kbBulkUndoOperation, []string{id}, func(ctx context.Context) (any, error) {
		ctx = context.WithoutCancel(ctx)
		operation, err := a.store.ClaimKBBulkUndo(ctx, who.Workspace.ID, savedReplyActor(ctx, who), id)
		if err != nil {
			writeKBError(response, err)
			return nil, errAnswered
		}
		results := make([]kbBulkUndoItem, 0, len(operation.Entries))
		counts := map[string]int{"undone": 0, "not_undone": 0}
		for _, entry := range operation.Entries {
			item := kbBulkUndoItem{ID: entry.ArticleID, Outcome: "undone"}
			if err := a.kbUndoOne(ctx, who, entry); err != nil {
				item.Outcome, item.Error = "not_undone", a.kbBulkError(request, who, err)
			}
			if article, err := a.store.GetKBArticle(ctx, who.Workspace.ID, entry.ArticleID); err == nil {
				number := article.Number
				item.Number, item.Title = &number, article.Title
			}
			counts[item.Outcome]++
			results = append(results, item)
		}
		return map[string]any{
			"operation": map[string]any{"id": operation.ID, "action": operation.Action, "undone_at": operation.UndoneAt.UTC().Format(time.RFC3339)},
			"counts":    counts, "results": results,
		}, nil
	})
}

// kbUndoOne puts one article back as entry says, refusing what the caller may not do by hand and an
// article whose token moved since the bulk action.
func (a *API) kbUndoOne(ctx context.Context, who auth.Principal, entry store.KBBulkEntry) error {
	ws, actor, id := who.Workspace.ID, savedReplyActor(ctx, who), entry.ArticleID
	var seen time.Time
	if entry.AfterToken != nil {
		seen = *entry.AfterToken
	}
	var before kbBulkBefore
	if len(entry.Before) > 0 {
		if err := json.Unmarshal(entry.Before, &before); err != nil {
			return err
		}
	}
	need := func(permission string) error {
		if !who.Can(permission) {
			return kbBulkPermissionError{permission}
		}
		return nil
	}
	setStatus := func(status string) error {
		if err := need(auth.PermKBArticlesPublish); err != nil {
			return err
		}
		_, err := a.store.UpdateKBArticle(ctx, ws, actor, id, seen, store.KBArticleChange{Status: &status})
		return err
	}
	switch entry.Undo {
	case "update":
		if before.Visibility != nil {
			if err := a.kbBulkMayRaise(ctx, who, id, *before.Visibility); err != nil {
				return err
			}
		}
		_, err := a.store.UpdateKBArticle(ctx, ws, actor, id, seen, store.KBArticleChange{CollectionID: before.CollectionID,
			ExtraCollectionIDs: before.ExtraCollectionIDs, Position: before.Position, Visibility: before.Visibility,
			AuthorAgentID: before.AuthorAgentID, Tags: before.Tags})
		return err
	case "discard_draft":
		_, err := a.store.DiscardKBArticleDraft(ctx, ws, actor, id, seen)
		return err
	case "withdraw_review":
		_, err := a.store.DecideKBReview(ctx, ws, actor, entry.Ref,
			store.KBReviewDecision{State: domain.KBReviewWithdrawn, WithdrawAny: who.Can(auth.PermKBArticlesPublish)})
		return err
	case "unpublish":
		return setStatus(domain.KBDraft)
	case "republish":
		return setStatus(domain.KBPublished)
	case "restore":
		if err := need(auth.PermKBArticlesDelete); err != nil {
			return err
		}
		if before.Status != nil && *before.Status == domain.KBPublished {
			if err := need(auth.PermKBArticlesPublish); err != nil {
				return err
			}
		}
		_, err := a.store.UndeleteKBArticle(ctx, ws, actor, id, seen)
		return err
	}
	return nil
}
