package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// kbBulkBefore is what undo puts back on an article: only the fields the bulk action changed are set.
type kbBulkBefore struct {
	CollectionID       *string   `json:"collection_id,omitempty"`
	ExtraCollectionIDs *[]string `json:"extra_collection_ids,omitempty"`
	Position           *int      `json:"position,omitempty"`
	Visibility         *string   `json:"visibility,omitempty"`
	AuthorAgentID      *string   `json:"author_agent_id,omitempty"`
	Tags               *[]string `json:"tags,omitempty"`
	Status             *string   `json:"status,omitempty"`
}

// kbBulkRefusal is a refusal that has no store error of its own.
type kbBulkRefusal struct{ code, message string }

func (e kbBulkRefusal) Error() string { return e.message }

// kbBulkOne applies the action to one article, as the call for it alone would, and returns its line of
// the answer and, when undo can put it back, what undo needs.
func (a *API) kbBulkOne(ctx context.Context, request *http.Request, who auth.Principal, input kbBulkInput, id string) (kbBulkItem, *store.KBBulkEntry) {
	item := kbBulkItem{ID: id, Outcome: "refused"}
	current, err := a.store.GetKBArticle(ctx, who.Workspace.ID, id)
	if err != nil {
		item.Error = a.kbBulkError(request, who, err)
		return item, nil
	}
	number := current.Number
	item.Number, item.Title = &number, current.Title
	entry, changed, err := a.kbBulkApply(ctx, who, input, current)
	switch {
	case err != nil:
		item.Error = a.kbBulkError(request, who, err)
		return item, nil
	case !changed:
		item.Outcome = "unchanged"
		return item, nil
	}
	item.Outcome = "done"
	if entry != nil {
		entry.ArticleID = id
	}
	return item, entry
}

// kbBulkApply changes current; changed is false when it already was as asked. A nil entry with changed
// set is a change undo cannot put back (an approval, a published draft).
func (a *API) kbBulkApply(ctx context.Context, who auth.Principal, input kbBulkInput, current domain.KBArticle) (*store.KBBulkEntry, bool, error) {
	ws, actor, drafted := who.Workspace.ID, savedReplyActor(ctx, who), current.AsDrafted()
	// A change to a published article's text, collections or tags goes to its draft; when the bulk action
	// made that draft, undo throws it away.
	newDraft := current.Status == domain.KBPublished && !current.HasDraft
	update := func(change store.KBArticleChange, before kbBulkBefore, toDraft bool) (*store.KBBulkEntry, bool, error) {
		article, err := a.store.UpdateKBArticle(ctx, ws, actor, current.ID, current.UpdatedAt, change)
		if err != nil {
			return nil, false, err
		}
		entry := &store.KBBulkEntry{AfterToken: &article.UpdatedAt, Undo: "update"}
		if toDraft && newDraft {
			entry.Undo = "discard_draft"
		} else {
			entry.Before, _ = json.Marshal(before)
		}
		return entry, true, nil
	}
	switch input.Action {
	case "move":
		if drafted.CollectionID == input.CollectionID {
			return nil, false, nil
		}
		was, kept := []string{}, []string{}
		for _, collection := range drafted.CollectionIDs {
			if collection != drafted.CollectionID {
				was = append(was, collection)
				if collection != input.CollectionID {
					kept = append(kept, collection)
				}
			}
		}
		return update(store.KBArticleChange{CollectionID: &input.CollectionID, ExtraCollectionIDs: &kept},
			kbBulkBefore{CollectionID: &drafted.CollectionID, ExtraCollectionIDs: &was, Position: &current.Position}, true)
	case "tags_add", "tags_remove":
		tags := slices.DeleteFunc(slices.Clone(drafted.Tags), func(tag string) bool { return slices.Contains(input.Tags, tag) })
		if input.Action == "tags_add" {
			tags = append(tags, input.Tags...)
		}
		slices.Sort(tags)
		was := append([]string{}, drafted.Tags...)
		if slices.Equal(tags, was) {
			return nil, false, nil
		}
		return update(store.KBArticleChange{Tags: &tags}, kbBulkBefore{Tags: &was}, true)
	case "visibility":
		if current.Visibility == input.Visibility {
			return nil, false, nil
		}
		if err := a.kbBulkMayRaise(ctx, who, current.ID, input.Visibility); err != nil {
			return nil, false, err
		}
		return update(store.KBArticleChange{Visibility: &input.Visibility}, kbBulkBefore{Visibility: &current.Visibility}, false)
	case "author":
		if current.AuthorAgentID != nil && *current.AuthorAgentID == input.AuthorAgentID {
			return nil, false, nil
		}
		entry, changed, err := update(store.KBArticleChange{AuthorAgentID: &input.AuthorAgentID}, kbBulkBefore{AuthorAgentID: current.AuthorAgentID}, false)
		if current.AuthorAgentID == nil {
			entry = nil // credited to nobody before: there is no one to credit it back to
		}
		return entry, changed, err
	case "publish":
		article, err := a.publishOneKBArticle(ctx, who, current.ID)
		if err != nil || article == nil {
			return nil, false, err
		}
		if current.Status == domain.KBPublished {
			return nil, true, nil // its draft replaced the published text; the history keeps the old one
		}
		return &store.KBBulkEntry{AfterToken: &article.UpdatedAt, Undo: "unpublish"}, true, nil
	case "unpublish":
		if current.Status != domain.KBPublished {
			return nil, false, nil
		}
		status := domain.KBDraft
		article, err := a.store.UpdateKBArticle(ctx, ws, actor, current.ID, current.UpdatedAt, store.KBArticleChange{Status: &status})
		if err != nil || current.HasDraft {
			return nil, err == nil, err // its draft became its text: publishing again is a new decision
		}
		return &store.KBBulkEntry{AfterToken: &article.UpdatedAt, Undo: "republish"}, true, nil
	case "trash":
		return a.kbBulkTrash(ctx, who, current)
	case "send_for_review":
		review, err := a.store.RequestKBReview(ctx, ws, actor, current.ID, store.KBReviewRequest{ReviewerIDs: input.ReviewerIDs, Note: input.Note})
		if err != nil {
			return nil, false, err
		}
		return &store.KBBulkEntry{Undo: "withdraw_review", Ref: review.ID}, true, nil
	case "approve":
		reviews, err := a.store.ListKBReviews(ctx, ws, domain.KBReviewFilter{State: domain.KBReviewOpen, ArticleID: current.ID})
		if err != nil {
			return nil, false, err
		}
		if len(reviews) == 0 {
			return nil, false, kbBulkRefusal{"review_state", "This article has no open review to approve: send it for review first."}
		}
		_, err = a.store.DecideKBReview(ctx, ws, actor, reviews[0].ID, store.KBReviewDecision{State: domain.KBReviewApproved, Note: input.Note})
		return nil, err == nil, err
	}
	return nil, false, nil
}

// kbBulkTrash moves current to the trash; a published article leaves the help center, which needs publish.
func (a *API) kbBulkTrash(ctx context.Context, who auth.Principal, current domain.KBArticle) (*store.KBBulkEntry, bool, error) {
	if current.Status == domain.KBPublished && !who.Can(auth.PermKBArticlesPublish) {
		return nil, false, kbBulkPermissionError{auth.PermKBArticlesPublish}
	}
	if err := a.store.DeleteKBArticle(ctx, who.Workspace.ID, savedReplyActor(ctx, who), current.ID, current.UpdatedAt); err != nil {
		return nil, false, err
	}
	token, err := a.store.KBTrashedArticleToken(ctx, who.Workspace.ID, current.ID)
	if err != nil {
		return nil, true, nil
	}
	before, _ := json.Marshal(kbBulkBefore{Status: &current.Status})
	return &store.KBBulkEntry{Before: before, AfterToken: &token, Undo: "restore"}, true, nil
}

// kbBulkMayRaise refuses a visibility that shows the article to more people when the caller may not
// publish, as PATCH /api/v1/kb/articles/{id} does.
func (a *API) kbBulkMayRaise(ctx context.Context, who auth.Principal, id, visibility string) error {
	if who.Can(auth.PermKBArticlesPublish) {
		return nil
	}
	raises, err := a.store.KBArticleVisibilityRaises(ctx, who.Workspace.ID, id, visibility)
	if err != nil {
		return err
	}
	if raises {
		return kbBulkPermissionError{auth.PermKBArticlesPublish}
	}
	return nil
}
