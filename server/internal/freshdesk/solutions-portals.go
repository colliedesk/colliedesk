package freshdesk

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// entityKBHelpCenter is the help center a Freshdesk portal became, source id the portal id. The portal
// that maps to the primary help center has a row too (not created), so a later run keeps the mapping.
const entityKBHelpCenter = "kb_help_center"

// What became of a portal, in KBPortalHelpCenter.Status.
const (
	portalPrimary   = "primary"    // shown on the workspace's primary help center
	portalCreated   = "created"    // this run made its help center
	portalExisting  = "existing"   // an earlier run made its help center
	portalPlanned   = "planned"    // a dry run: the import would make its help center
	portalOverLimit = "over_limit" // the workspace has the most help centers it may have; on the primary
	portalTrashed   = "trashed"    // its help center was deleted in Colliedesk; on the primary
	portalFailed    = "failed"     // its help center could not be made; on the primary, see the failure
)

// KBPortalHelpCenter is the help center one Freshdesk portal became.
type KBPortalHelpCenter struct {
	PortalID   int64  `json:"portal_id"`
	PortalName string `json:"portal_name"`
	// NameFromFreshdesk is false when Freshdesk gave no name, so the portal is called "Portal <id>".
	NameFromFreshdesk bool `json:"name_from_freshdesk"`
	// Categories counts the categories the portal shows.
	Categories int `json:"categories"`
	// HelpCenterID, HelpCenterName and Handle name the help center the portal's categories are on;
	// empty in a dry run for a help center not made yet. Handle is empty for the primary.
	HelpCenterID   string `json:"help_center_id,omitempty"`
	HelpCenterName string `json:"help_center_name,omitempty"`
	Handle         string `json:"handle,omitempty"`
	Primary        bool   `json:"primary"`
	Status         string `json:"status"`
}

// kbPortal is one portal the categories name.
type kbPortal struct {
	id         int64
	name       string
	named      bool
	categories int
}

// fdTicketForm is the part of a ticket form that names portals: the only place Freshdesk's API gives a
// portal's name.
type fdTicketForm struct {
	Portals []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"portals"`
}

// readPortalNames reads the portals' names from the ticket forms. A failure only costs the names.
func (r *run) readPortalNames(ctx context.Context, kb *knowledgeBase) error {
	if len(kb.portals()) == 0 {
		return nil
	}
	names := map[int64]string{}
	err := listAll(ctx, r.client, r.endpoint("/api/v2/ticket-forms", pageQuery()), func(form fdTicketForm) error {
		for _, portal := range form.Portals {
			if name := strings.TrimSpace(portal.Name); name != "" && names[portal.ID] == "" {
				names[portal.ID] = name
			}
		}
		return nil
	})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		kb.portalNamesNote = "The portals' names could not be read from the ticket forms (" + err.Error() + "); their help centers are named Portal <id>. Rename them in Knowledge base → Settings → Help centers."
	}
	kb.portalNames = names
	return nil
}

// portalPlan lists the portals the categories name, the one with the most categories first (the
// lowest id on a tie): that one is shown on the primary help center unless an earlier run chose another.
func (kb *knowledgeBase) portalPlan() []kbPortal {
	ids := kb.portals()
	if len(ids) == 0 {
		return nil
	}
	plan := make([]kbPortal, 0, len(ids))
	for _, id := range ids {
		portal := kbPortal{id: id, name: kb.portalNames[id]}
		portal.named = portal.name != ""
		if !portal.named {
			portal.name = "Portal " + strconv.FormatInt(id, 10)
		}
		for _, category := range kb.categories {
			if category.VisibleInPortals != nil && slices.Contains(*category.VisibleInPortals, id) {
				portal.categories++
			}
		}
		plan = append(plan, portal)
	}
	slices.SortStableFunc(plan, func(a, b kbPortal) int {
		if a.categories != b.categories {
			return b.categories - a.categories
		}
		return int(a.id - b.id)
	})
	return plan
}

// unnamedPortalsNote says which portals Freshdesk gave no name, or "".
func unnamedPortalsNote(plan []kbPortal) string {
	var ids []string
	for _, portal := range plan {
		if !portal.named {
			ids = append(ids, strconv.FormatInt(portal.id, 10))
		}
	}
	if len(ids) == 0 {
		return ""
	}
	return "Freshdesk gave no name for portal " + strings.Join(ids, ", ") + ", so its help center is named Portal <id>. Rename it in Knowledge base → Settings → Help centers."
}

// plannedPortalHelpCenters is the mapping a dry run reports: the first portal on the primary, every
// other one on a help center of its own.
func plannedPortalHelpCenters(plan []kbPortal) []KBPortalHelpCenter {
	out := make([]KBPortalHelpCenter, 0, len(plan))
	for index, portal := range plan {
		entry := portalEntry(portal)
		entry.Primary, entry.Status = index == 0, portalPlanned
		if entry.Primary {
			entry.Status = portalPrimary
		}
		out = append(out, entry)
	}
	return out
}

func portalEntry(portal kbPortal) KBPortalHelpCenter {
	return KBPortalHelpCenter{PortalID: portal.id, PortalName: portal.name, NameFromFreshdesk: portal.named, Categories: portal.categories}
}

// ensurePortalHelpCenters maps every portal to a help center: the primary one for the portal an earlier
// run put there (else the one with the most categories), a help center of its own for each other,
// made here on the first run.
func (r *run) ensurePortalHelpCenters(ctx context.Context) error {
	kb, state := r.kb, r.kbWriteState()
	plan := kb.portalPlan()
	if len(plan) == 0 {
		return nil
	}
	primaryID, primaryName, err := r.primaryHelpCenter(ctx)
	if err != nil {
		return err
	}
	type known struct {
		target string
		found  bool
	}
	earlier := make([]known, len(plan))
	primaryIndex := 0
	for index, portal := range plan {
		target, done, err := r.ledger.done(ctx, entityKBHelpCenter, strconv.FormatInt(portal.id, 10))
		if err != nil {
			return err
		}
		earlier[index] = known{target, done}
		if done && target == primaryID {
			primaryIndex = index
		}
	}
	// The portal an earlier run put on the primary stays there, listed first, even when another portal
	// has more categories now.
	if primaryIndex > 0 {
		plan = slices.Concat(plan[primaryIndex:primaryIndex+1], plan[:primaryIndex], plan[primaryIndex+1:])
		earlier = slices.Concat(earlier[primaryIndex:primaryIndex+1], earlier[:primaryIndex], earlier[primaryIndex+1:])
		primaryIndex = 0
	}
	state.portalHelpCenters = map[int64]string{}
	state.createdHelpCenters = map[string]bool{}
	for index, portal := range plan {
		key := strconv.FormatInt(portal.id, 10)
		entry := portalEntry(portal)
		if index == primaryIndex {
			entry.Primary, entry.Status, entry.HelpCenterID, entry.HelpCenterName = true, portalPrimary, primaryID, primaryName
			state.portalHelpCenters[portal.id] = primaryID
			if !earlier[index].found {
				if err := r.ledger.mark(ctx, nil, ledgerMark{entity: entityKBHelpCenter, sourceID: key, targetID: &primaryID, status: ledgerDone}); err != nil {
					return err
				}
			}
			state.helpCenters = append(state.helpCenters, entry)
			continue
		}
		if earlier[index].found && earlier[index].target != primaryID {
			name, handle, live, err := r.helpCenterNamed(ctx, earlier[index].target)
			if err != nil {
				return err
			}
			if live {
				entry.Status, entry.HelpCenterID, entry.HelpCenterName, entry.Handle = portalExisting, earlier[index].target, name, handle
				state.portalHelpCenters[portal.id] = earlier[index].target
			} else {
				entry.Status, entry.HelpCenterID, entry.HelpCenterName = portalTrashed, primaryID, primaryName
				state.portalHelpCenters[portal.id] = primaryID
				r.warn("kb_portal_help_center_deleted", "The help center an earlier import made for this Freshdesk portal was deleted in Colliedesk, so the portal's categories were left on the primary help center.", key)
			}
			state.helpCenters = append(state.helpCenters, entry)
			continue
		}
		id, name, handle, err := r.createPortalHelpCenter(ctx, portal)
		switch {
		case errors.Is(err, domain.ErrHelpCenterLimit):
			entry.Status, entry.HelpCenterID, entry.HelpCenterName = portalOverLimit, primaryID, primaryName
			state.portalHelpCenters[portal.id] = primaryID
			r.warn("kb_portal_over_help_center_limit", fmt.Sprintf("A workspace has at most %d help centers, so this Freshdesk portal's categories were put on the primary help center.", domain.MaxHelpCenters), key)
		case err != nil:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			entry.Status, entry.HelpCenterID, entry.HelpCenterName = portalFailed, primaryID, primaryName
			state.portalHelpCenters[portal.id] = primaryID
			r.itemFailed(ctx, entityKBHelpCenter, key, "", err)
			if r.internalErr != nil {
				return r.internalErr
			}
		default:
			entry.Status, entry.HelpCenterID, entry.HelpCenterName, entry.Handle = portalCreated, id, name, handle
			state.portalHelpCenters[portal.id] = id
			state.createdHelpCenters[id] = true
		}
		state.helpCenters = append(state.helpCenters, entry)
	}
	return nil
}

func (r *run) primaryHelpCenter(ctx context.Context) (id, name string, err error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return "", "", fmt.Errorf("begin help center read: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if id, err = store.ImportPrimaryHelpCenter(ctx, tx, r.workspaceID); err != nil {
		return "", "", fmt.Errorf("read the primary help center: %w", err)
	}
	if err := tx.QueryRow(ctx, `SELECT name FROM help_centers WHERE workspace_id=$1 AND id=$2`, r.workspaceID, id).Scan(&name); err != nil {
		return "", "", fmt.Errorf("read the primary help center: %w", err)
	}
	return id, name, tx.Commit(ctx)
}

// helpCenterNamed reads a help center an earlier run made; live is false once it was deleted.
func (r *run) helpCenterNamed(ctx context.Context, id string) (name, handle string, live bool, err error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return "", "", false, fmt.Errorf("begin help center read: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var h *string
	if err := tx.QueryRow(ctx, `SELECT name, handle, deleted_at IS NULL FROM help_centers WHERE workspace_id=$1 AND id=$2`, r.workspaceID, id).
		Scan(&name, &h, &live); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", false, nil
		}
		return "", "", false, fmt.Errorf("read help center: %w", err)
	}
	if h != nil {
		handle = *h
	}
	return name, handle, live, nil
}

func (r *run) createPortalHelpCenter(ctx context.Context, portal kbPortal) (id, name, handle string, err error) {
	key := strconv.FormatInt(portal.id, 10)
	tx, err := r.begin(ctx)
	if err != nil {
		return "", "", "", fmt.Errorf("begin help center import: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if id, handle, err = store.ImportHelpCenter(ctx, tx, r.workspaceID, portal.name, "portal-"+key); err != nil {
		return "", "", "", err
	}
	if err := tx.QueryRow(ctx, `SELECT name FROM help_centers WHERE workspace_id=$1 AND id=$2`, r.workspaceID, id).Scan(&name); err != nil {
		return "", "", "", err
	}
	if err := r.ledger.mark(ctx, tx, ledgerMark{entity: entityKBHelpCenter, sourceID: key, targetID: &id, status: ledgerDone, meta: &ticketMeta{Created: true}}); err != nil {
		return "", "", "", err
	}
	return id, name, handle, tx.Commit(ctx)
}

// categoryHelpCenters returns the help centers a category is shown on, or nil for the primary alone
// (where a new collection goes by itself).
func (state *kbState) categoryHelpCenters(category fdSolutionCategory) []string {
	if category.VisibleInPortals == nil || len(state.portalHelpCenters) == 0 {
		return nil
	}
	var ids []string
	primaryOnly := true
	for _, portal := range *category.VisibleInPortals {
		id, ok := state.portalHelpCenters[portal]
		if !ok || slices.Contains(ids, id) {
			continue
		}
		ids = append(ids, id)
		if state.createdHelpCenters[id] || !state.isPrimary(id) {
			primaryOnly = false
		}
	}
	if primaryOnly {
		return nil
	}
	return ids
}

func (state *kbState) isPrimary(id string) bool {
	for _, entry := range state.helpCenters {
		if entry.Primary {
			return entry.HelpCenterID == id
		}
	}
	return false
}

// createdIn keeps the help centers of ids this run made.
func (state *kbState) createdIn(ids []string) []string {
	var out []string
	for _, id := range ids {
		if state.createdHelpCenters[id] {
			out = append(out, id)
		}
	}
	return out
}
