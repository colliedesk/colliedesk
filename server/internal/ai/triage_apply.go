package ai

import (
	"context"
	"errors"
	"slices"

	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// customerChannels are how a customer starts a conversation. Only there may AI set the priority by
// itself: a teammate who starts one sets its priority on purpose, normal included.
var customerChannels = []string{"email", "web_form", "problem_report"}

// conversationNow is what a suggestion is applied against, read under the conversation's lock.
type conversationNow struct {
	priority string
	assignee *string
	channel  string
	tagIDs   []string
}

func lockConversation(ctx context.Context, tx pgx.Tx, workspaceID, conversationID string) (conversationNow, error) {
	var c conversationNow
	err := tx.QueryRow(ctx, `SELECT priority,assignee_id::text,channel,
			COALESCE((SELECT array_agg(tag_id::text) FROM conversation_tags ct WHERE ct.workspace_id=cv.workspace_id AND ct.conversation_id=cv.id),'{}')
		FROM conversations cv WHERE workspace_id=$1 AND id=$2 FOR NO KEY UPDATE`, workspaceID, conversationID).Scan(&c.priority, &c.assignee, &c.channel, &c.tagIDs)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrConversationNotFound
	}
	return c, err
}

// applyParts makes the changes of a suggestion on its conversation as a person's change does (actor
// empty for AI: an assignment it makes records no one). Tags are added one by one.
func applyParts(ctx context.Context, tx pgx.Tx, workspaceID, conversationID, actorID string, set TriageSet) error {
	for _, tag := range set.TagIDs {
		if _, err := store.SetConversationTagIn(ctx, tx, workspaceID, conversationID, tag, true); err != nil {
			return err
		}
	}
	update := store.ConversationUpdate{Priority: set.Priority, ActorID: actorID}
	if set.AssigneeAgentID != nil {
		update.AssigneeID = &set.AssigneeAgentID
	}
	if update.Priority == nil && update.AssigneeID == nil {
		return nil
	}
	_, err := store.UpdateConversationIn(ctx, tx, workspaceID, conversationID, update)
	return err
}

func readBase(ctx context.Context, tx pgx.Tx, workspaceID, id string) (TriageStale, error) {
	var base TriageStale
	err := tx.QueryRow(ctx, `SELECT base FROM ai_triage_suggestions WHERE workspace_id=$1 AND id=$2`, workspaceID, id).Scan(&base)
	return base, err
}

func merge(set TriageSet, part TriageSet) TriageSet {
	if set.TagIDs == nil {
		set.TagIDs = []string{}
	}
	for _, tag := range part.TagIDs {
		if !slices.Contains(set.TagIDs, tag) {
			set.TagIDs = append(set.TagIDs, tag)
		}
	}
	if part.Priority != nil {
		set.Priority = part.Priority
	}
	if part.AssigneeAgentID != nil {
		set.AssigneeAgentID = part.AssigneeAgentID
	}
	return set
}

// without is set less the parts of part.
func without(set TriageSet, part TriageSet) TriageSet {
	out := TriageSet{TagIDs: []string{}, Priority: set.Priority, AssigneeAgentID: set.AssigneeAgentID}
	for _, tag := range set.TagIDs {
		if !slices.Contains(part.TagIDs, tag) {
			out.TagIDs = append(out.TagIDs, tag)
		}
	}
	if part.Priority != nil {
		out.Priority = nil
	}
	if part.AssigneeAgentID != nil {
		out.AssigneeAgentID = nil
	}
	return out
}

func (t TriageSet) empty() bool {
	return len(t.TagIDs) == 0 && t.Priority == nil && t.AssigneeAgentID == nil
}

// open is what of the suggestion nobody applied yet.
func (t Triage) open() TriageSet {
	set := TriageSet{TagIDs: []string{}}
	for _, tag := range t.TagIDs {
		if !slices.Contains(t.Applied.TagIDs, tag) {
			set.TagIDs = append(set.TagIDs, tag)
		}
	}
	if t.Priority != nil && t.Applied.Priority == nil {
		set.Priority = t.Priority
	}
	if t.AssigneeAgentID != nil && t.Applied.AssigneeAgentID == nil {
		set.AssigneeAgentID = t.AssigneeAgentID
	}
	return set
}

// AcceptTriage applies what a person accepts of a suggestion (want nil or naming nothing: the rest of
// it) to the conversation and records it, in one transaction under the suggestion's lock. Each part
// must be the suggestion's own; a part applied already is not applied again. After an undo, accepting
// is "Apply again" (applyAgain).
func (s *Service) AcceptTriage(ctx context.Context, workspaceID string, actor Actor, conversationID, id string, want *TriageSet) (Triage, error) {
	if !isUUID(conversationID) || !isUUID(id) {
		return Triage{}, ErrTriageNotFound
	}
	var triage Triage
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		t, err := scanTriage(tx.QueryRow(ctx, `SELECT `+triageColumns+` WHERE s.workspace_id=$1 AND s.conversation_id=$2 AND s.id=$3 FOR UPDATE OF s`, workspaceID, conversationID, id))
		if err != nil {
			return err
		}
		if t.Status != DraftReady {
			return ErrTriageDecided
		}
		if t.Outcome != nil {
			switch *t.Outcome {
			case "accepted":
				triage = t // accepting all of it again changes nothing
				return nil
			case "undone":
				triage, err = s.applyAgain(ctx, tx, workspaceID, actor, conversationID, t)
				return err
			case "auto_applied":
			default:
				return ErrTriageDecided
			}
		}
		rest := t.open()
		if rest.empty() {
			if len(t.TagIDs) == 0 && t.Priority == nil && t.AssigneeAgentID == nil {
				return ErrTriageNothing
			}
			triage = t
			return nil
		}
		set := rest
		if want != nil && !want.empty() {
			set = TriageSet{TagIDs: []string{}}
			for _, tag := range want.TagIDs {
				if !slices.Contains(t.TagIDs, tag) {
					return invalid("Accept only the tags the suggestion names.")
				}
				if slices.Contains(rest.TagIDs, tag) && !slices.Contains(set.TagIDs, tag) {
					set.TagIDs = append(set.TagIDs, tag)
				}
			}
			if want.Priority != nil {
				if t.Priority == nil || *want.Priority != *t.Priority {
					return invalid("Accept only the priority the suggestion names.")
				}
				set.Priority = rest.Priority
			}
			if want.AssigneeAgentID != nil {
				if t.AssigneeAgentID == nil || *want.AssigneeAgentID != *t.AssigneeAgentID {
					return invalid("Accept only the assignee the suggestion names.")
				}
				set.AssigneeAgentID = rest.AssigneeAgentID
			}
		}
		if _, err := lockConversation(ctx, tx, workspaceID, conversationID); err != nil {
			return err
		}
		if err := applyParts(ctx, tx, workspaceID, conversationID, actor.AgentID, set); err != nil {
			return err
		}
		t.Applied = merge(t.Applied, set)
		var outcome, decidedAt, decidedBy any
		switch {
		case t.open().empty():
			outcome, decidedAt, decidedBy = "accepted", s.now().UTC(), nullable(actor.AgentID)
		case t.Outcome != nil:
			// A part applied again after AI's or after an undo: the rest stays as it was.
			outcome, decidedAt, decidedBy = "partly_accepted", s.now().UTC(), nullable(actor.AgentID)
		}
		if _, err := tx.Exec(ctx, `UPDATE ai_triage_suggestions SET applied=$3,outcome=$4,decided_at=$5,decided_by_agent_id=$6 WHERE workspace_id=$1 AND id=$2`,
			workspaceID, id, t.Applied, outcome, decidedAt, decidedBy); err != nil {
			return err
		}
		triage, err = scanTriage(tx.QueryRow(ctx, `SELECT `+triageColumns+` WHERE s.workspace_id=$1 AND s.id=$2`, workspaceID, id))
		return err
	})
	return triage, err
}

// applyAgain applies once more what an undo took back ("Apply again"), filling only what is still
// empty, and records it as AI's again so it shows with its own Undo: the tags; the priority while the
// conversation still has the one it had before AI; the assignee while nobody is assigned. What a
// person set since is left, and the suggestion says why.
func (s *Service) applyAgain(ctx context.Context, tx pgx.Tx, workspaceID string, actor Actor, conversationID string, t Triage) (Triage, error) {
	now, err := lockConversation(ctx, tx, workspaceID, conversationID)
	if err != nil {
		return Triage{}, err
	}
	base, err := readBase(ctx, tx, workspaceID, t.ID)
	if err != nil {
		return Triage{}, err
	}
	set, skipped := TriageSet{TagIDs: []string{}}, map[string]TriageSkip{}
	for _, tag := range t.UndoneParts.TagIDs {
		if slices.Contains(t.TagIDs, tag) { // a tag deleted since is gone from the suggestion
			set.TagIDs = append(set.TagIDs, tag)
		}
	}
	if t.UndoneParts.Priority != nil {
		priority := now.priority
		if base.Priority != nil && now.priority == *base.Priority {
			set.Priority = t.UndoneParts.Priority
		} else {
			skipped["priority"] = TriageSkip{Reason: "set_by_person", Value: &priority}
		}
	}
	if t.UndoneParts.AssigneeAgentID != nil {
		if now.assignee == nil && t.AssigneeAgentID != nil {
			set.AssigneeAgentID = t.UndoneParts.AssigneeAgentID
		} else if now.assignee != nil {
			skipped["assignee"] = TriageSkip{Reason: "already_assigned", Value: now.assignee}
		}
	}
	// What a person kept at the undo is theirs: it says so as a part AI left.
	if t.Kept.Priority != nil && set.Priority == nil && skipped["priority"].Reason == "" {
		skipped["priority"] = TriageSkip{Reason: "set_by_person", Value: t.Kept.Priority}
	}
	if t.Kept.AssigneeAgentID != nil && set.AssigneeAgentID == nil && skipped["assignee"].Reason == "" {
		skipped["assignee"] = TriageSkip{Reason: "already_assigned", Value: t.Kept.AssigneeAgentID}
	}
	if err := applyParts(ctx, tx, workspaceID, conversationID, actor.AgentID, set); err != nil {
		return Triage{}, err
	}
	at := s.now().UTC()
	priority, assignee := t.Priority, t.AssigneeAgentID
	if _, ok := skipped["priority"]; ok {
		priority = nil
	}
	if _, ok := skipped["assignee"]; ok {
		assignee = nil
	}
	applied := merge(t.Applied, set)
	open := Triage{TagIDs: t.TagIDs, Priority: priority, AssigneeAgentID: assignee, Applied: applied}.open()
	var outcome, decidedAt, autoAt any
	if !set.empty() {
		autoAt = at
	}
	if open.empty() {
		outcome, decidedAt = "auto_applied", at
		if set.empty() {
			outcome = "undone" // nothing could be applied again: it stays undone
			decidedAt = t.DecidedAt
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE ai_triage_suggestions SET priority=$3,assignee_agent_id=$4,applied=$5,auto_applied=$6,auto_applied_at=COALESCE($7,auto_applied_at),
			skipped=$8,kept='{"priority":null,"assignee_agent_id":null}',outcome=$9,decided_at=$10,decided_by_agent_id=$11 WHERE workspace_id=$1 AND id=$2`,
		workspaceID, t.ID, priority, assignee, applied, set, autoAt, skipped, outcome, decidedAt, nullable(actor.AgentID)); err != nil {
		return Triage{}, err
	}
	return scanTriage(tx.QueryRow(ctx, `SELECT `+triageColumns+` WHERE s.workspace_id=$1 AND s.id=$2`, workspaceID, t.ID))
}

// UndoTriage takes back what AI applied by itself: parts names tags, priority or assignee (none: all
// of it). Each part goes back only while it is still what AI set: a tag AI added is removed if it is
// still there, the priority returns to what it was before AI, the assignee is unassigned. A part a
// person changed since is kept, and the suggestion says so. Undoing again changes nothing; accepting
// again applies it once more.
func (s *Service) UndoTriage(ctx context.Context, workspaceID string, actor Actor, conversationID, id string, parts []string) (Triage, error) {
	if !isUUID(conversationID) || !isUUID(id) {
		return Triage{}, ErrTriageNotFound
	}
	for _, part := range parts {
		if !slices.Contains(AutoParts, part) {
			return Triage{}, invalid("parts holds tags, priority and assignee.")
		}
	}
	var triage Triage
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		t, err := scanTriage(tx.QueryRow(ctx, `SELECT `+triageColumns+` WHERE s.workspace_id=$1 AND s.conversation_id=$2 AND s.id=$3 FOR UPDATE OF s`, workspaceID, conversationID, id))
		if err != nil {
			return err
		}
		if t.AutoAppliedAt == nil {
			return ErrTriageNotAutoApplied
		}
		take := func(part string) bool { return len(parts) == 0 || slices.Contains(parts, part) }
		if t.AutoApplied.empty() {
			triage = t
			return nil
		}
		now, err := lockConversation(ctx, tx, workspaceID, conversationID)
		if err != nil {
			return err
		}
		base, err := readBase(ctx, tx, workspaceID, id)
		if err != nil {
			return err
		}
		undone, gone := TriageSet{TagIDs: []string{}}, TriageSet{TagIDs: []string{}}
		if take("tags") {
			for _, tag := range t.AutoApplied.TagIDs {
				gone.TagIDs = append(gone.TagIDs, tag)
				if !slices.Contains(now.tagIDs, tag) {
					continue // a person removed it already
				}
				if _, err := store.SetConversationTagIn(ctx, tx, workspaceID, conversationID, tag, false); err != nil {
					return err
				}
				undone.TagIDs = append(undone.TagIDs, tag)
			}
		}
		update := store.ConversationUpdate{ActorID: actor.AgentID}
		if take("priority") && t.AutoApplied.Priority != nil {
			gone.Priority = t.AutoApplied.Priority
			if now.priority == *t.AutoApplied.Priority && base.Priority != nil {
				update.Priority, undone.Priority = base.Priority, t.AutoApplied.Priority
			} else {
				priority := now.priority
				t.Kept.Priority = &priority
			}
		}
		if take("assignee") && t.AutoApplied.AssigneeAgentID != nil {
			gone.AssigneeAgentID = t.AutoApplied.AssigneeAgentID
			if now.assignee != nil && *now.assignee == *t.AutoApplied.AssigneeAgentID {
				var nobody *string
				update.AssigneeID, undone.AssigneeAgentID = &nobody, t.AutoApplied.AssigneeAgentID
			} else {
				t.Kept.AssigneeAgentID = now.assignee
			}
		}
		if update.Priority != nil || update.AssigneeID != nil {
			if _, err := store.UpdateConversationIn(ctx, tx, workspaceID, conversationID, update); err != nil {
				return err
			}
		}
		autoApplied := without(t.AutoApplied, gone)
		applied := without(t.Applied, gone)
		var outcome, decidedAt, decidedBy any = t.Outcome, t.DecidedAt, t.DecidedByAgentID
		if autoApplied.empty() && (t.Outcome == nil || *t.Outcome == "auto_applied") {
			outcome, decidedAt, decidedBy = "undone", s.now().UTC(), nullable(actor.AgentID)
		}
		if _, err := tx.Exec(ctx, `UPDATE ai_triage_suggestions SET auto_applied=$3,applied=$4,undone_parts=$5,kept=$6,undone_at=$7,undone_by_agent_id=$8,
				outcome=$9,decided_at=$10,decided_by_agent_id=$11 WHERE workspace_id=$1 AND id=$2`,
			workspaceID, id, autoApplied, applied, merge(t.UndoneParts, undone), t.Kept, s.now().UTC(), nullable(actor.AgentID), outcome, decidedAt, decidedBy); err != nil {
			return err
		}
		triage, err = scanTriage(tx.QueryRow(ctx, `SELECT `+triageColumns+` WHERE s.workspace_id=$1 AND s.id=$2`, workspaceID, id))
		return err
	})
	return triage, err
}

// autoApply fills what nobody set from a suggestion that just came, inside the transaction that
// records it, when the workspace lets triage apply by itself. Only a suggestion a new ticket queued
// applies by itself (one a person asked for, that person decides). It returns the suggestion's parts
// less what it applied or skipped, so what is left stays open to accept.
func (s *Service) autoApply(ctx context.Context, tx pgx.Tx, workspaceID, conversationID, id string, base TriageStale, set TriageSet) (applied TriageSet, skipped map[string]TriageSkip, left TriageSet, err error) {
	skipped, left = map[string]TriageSkip{}, set
	applied = TriageSet{TagIDs: []string{}}
	var on bool
	var parts []string
	err = tx.QueryRow(ctx, `SELECT enabled AND automatic, auto_parts FROM ai_slots WHERE workspace_id=$1 AND slot='triage'`, workspaceID).Scan(&on, &parts)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !on) {
		return applied, skipped, left, nil
	}
	if err != nil {
		return applied, skipped, left, err
	}
	now, err := lockConversation(ctx, tx, workspaceID, conversationID)
	if err != nil {
		return applied, skipped, left, err
	}
	if slices.Contains(parts, "tags") {
		applied.TagIDs = set.TagIDs // set holds only tags that exist and the conversation lacks
	}
	if slices.Contains(parts, "priority") && set.Priority != nil {
		priority := now.priority
		switch {
		case !slices.Contains(customerChannels, now.channel):
			skipped["priority"] = TriageSkip{Reason: "agent_conversation", Value: &priority}
		case now.priority != "normal" || base.Priority == nil || *base.Priority != now.priority:
			skipped["priority"] = TriageSkip{Reason: "set_by_person", Value: &priority}
		default:
			applied.Priority = set.Priority
		}
	}
	if slices.Contains(parts, "assignee") && set.AssigneeAgentID != nil {
		if now.assignee != nil {
			skipped["assignee"] = TriageSkip{Reason: "already_assigned", Value: now.assignee}
		} else {
			applied.AssigneeAgentID = set.AssigneeAgentID
		}
	}
	if err := applyParts(ctx, tx, workspaceID, conversationID, "", applied); err != nil {
		return applied, skipped, left, err
	}
	left = without(set, applied)
	if _, ok := skipped["priority"]; ok {
		left.Priority = nil
	}
	if _, ok := skipped["assignee"]; ok {
		left.AssigneeAgentID = nil
	}
	return applied, skipped, left, nil
}
