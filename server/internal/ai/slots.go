package ai

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/aiprovider"
	"github.com/jackc/pgx/v5"
)

type slotRow struct {
	slot        string
	usesServer  bool
	providerID  *string
	model       *string
	effort      string
	enabled     bool
	automatic   bool
	autoParts   []string
	consentedBy *string
	consentedAt *time.Time
	updatedBy   *string
	updatedAt   *time.Time
	monthlyCap  *int64
}

func (r slotRow) source() string {
	switch {
	case r.usesServer:
		return SourceServer
	case r.providerID != nil:
		return SourceProvider
	}
	return SourceNone
}

func readSlots(ctx context.Context, tx pgx.Tx, workspaceID string, lock bool) (map[string]slotRow, error) {
	query := `SELECT slot,uses_server,provider_id::text,model,effort,enabled,automatic,auto_parts,consented_by::text,consented_at,updated_by::text,updated_at,
		monthly_cap_micro_usd FROM ai_slots WHERE workspace_id=$1`
	if lock {
		query += ` FOR UPDATE`
	}
	rows, err := tx.Query(ctx, query, workspaceID)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (slotRow, error) {
		var r slotRow
		err := row.Scan(&r.slot, &r.usesServer, &r.providerID, &r.model, &r.effort, &r.enabled, &r.automatic, &r.autoParts, &r.consentedBy, &r.consentedAt, &r.updatedBy, &r.updatedAt,
			&r.monthlyCap)
		return r, err
	})
	if err != nil {
		return nil, err
	}
	slots := map[string]slotRow{}
	for _, r := range list {
		slots[r.slot] = r
	}
	return slots, nil
}

// view turns a row (or its absence: off, nothing chosen) into what the API shows, with whether it
// can run now. A slot on the server's provider stops when the operator removes it or the model.
func (s *Service) view(slot string, row slotRow, found bool) Slot {
	view := Slot{Slot: slot, Source: SourceNone, Effort: string(aiprovider.EffortLow), AutoParts: []string{}}
	if !found {
		return view
	}
	view.Source, view.ProviderID, view.Model, view.Effort, view.Enabled = row.source(), row.providerID, row.model, row.effort, row.enabled
	view.Automatic, view.AutoParts = row.automatic, orderedParts(row.autoParts)
	view.ConsentedBy, view.ConsentedAt, view.UpdatedBy, view.UpdatedAt = row.consentedBy, utcPointer(row.consentedAt), row.updatedBy, utcPointer(row.updatedAt)
	view.MonthlyCap = row.monthlyCap
	view.Active = row.enabled
	if row.enabled && row.usesServer {
		reason := ""
		switch {
		case s.config.Server == nil || len(s.config.Server.ModelsFor(slot)) == 0:
			reason = OffServerProviderMissing
		case row.model != nil && !slices.Contains(s.config.Server.ModelsFor(slot), *row.model):
			reason = OffModelNotOffered
		}
		if reason != "" {
			view.Active, view.OffReason = false, &reason
		}
	}
	return view
}

func utcPointer(at *time.Time) *time.Time {
	if at == nil {
		return nil
	}
	utc := at.UTC()
	return &utc
}

// Settings reads everything Settings → Built-in AI shows.
func (s *Service) Settings(ctx context.Context, workspaceID string) (Settings, error) {
	settings := Settings{Providers: []Provider{}, Slots: []Slot{}, Events: []Event{}, ProviderLimit: MaxProviders,
		CanStoreKeys: s.config.Keys.Configured(), AllowPrivateBaseURLs: s.config.AllowPrivateBaseURLs}
	if server := s.config.Server; server != nil {
		settings.Server = &Server{Kind: string(server.Kind), Label: server.Label, Models: server.Models}
	}
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+providerColumns+` FROM ai_providers WHERE workspace_id=$1 ORDER BY created_at,id`, workspaceID)
		if err != nil {
			return err
		}
		settings.Providers, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Provider, error) { return scanProvider(row) })
		if err != nil {
			return err
		}
		slots, err := readSlots(ctx, tx, workspaceID, false)
		if err != nil {
			return err
		}
		for _, slot := range Slots {
			row, found := slots[slot]
			settings.Slots = append(settings.Slots, s.view(slot, row, found))
			if found && row.providerID != nil {
				for index := range settings.Providers {
					if settings.Providers[index].ID == *row.providerID {
						settings.Providers[index].UsedBy = append(settings.Providers[index].UsedBy, slot)
					}
				}
			}
		}
		if settings.Automatic, _, err = s.automaticLimits(ctx, tx, workspaceID); err != nil {
			return err
		}
		if settings.AutoDraftEstimate, err = s.autoDraftEstimate(ctx, tx, workspaceID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE undone_at IS NOT NULL) FROM ai_triage_suggestions
			WHERE workspace_id=$1 AND auto_applied_at>$2`, workspaceID, s.now().Add(-30*24*time.Hour)).Scan(&settings.AutoTriage.Applied, &settings.AutoTriage.Undone); err != nil {
			return err
		}
		if left, limit, limited, err := s.allowanceLeft(ctx, tx, workspaceID); err != nil {
			return err
		} else if limited {
			if settings.Allowance, err = s.allowanceView(ctx, tx, workspaceID, left, limit, monthStart(s.now()).AddDate(0, 1, 0)); err != nil {
				return err
			}
		}
		events, err := tx.Query(ctx, `SELECT e.id::text,e.kind,e.actor_agent_id::text,COALESCE(NULLIF(a.display_name,''),a.email),e.actor_token_id IS NOT NULL,
				e.provider_id::text,e.provider_label,e.slot,e.detail,e.created_at
			FROM ai_settings_events e LEFT JOIN agents a ON a.workspace_id=e.workspace_id AND a.id=e.actor_agent_id
			WHERE e.workspace_id=$1 ORDER BY e.created_at DESC,e.seq DESC LIMIT $2`, workspaceID, eventsShown)
		if err != nil {
			return err
		}
		settings.Events, err = pgx.CollectRows(events, func(row pgx.CollectableRow) (Event, error) {
			var e Event
			err := row.Scan(&e.ID, &e.Kind, &e.ActorAgentID, &e.ActorName, &e.ViaToken, &e.ProviderID, &e.ProviderLabel, &e.Slot, &e.Detail, &e.CreatedAt)
			e.CreatedAt = e.CreatedAt.UTC()
			return e, err
		})
		return err
	})
	if settings.Providers == nil {
		settings.Providers = []Provider{}
	}
	if settings.Events == nil {
		settings.Events = []Event{}
	}
	return settings, err
}

func (s *Service) withUses(ctx context.Context, workspaceID string, provider Provider) (Provider, error) {
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT slot FROM ai_slots WHERE workspace_id=$1 AND provider_id=$2 ORDER BY slot`, workspaceID, provider.ID)
		if err != nil {
			return err
		}
		uses, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		// Display order, not alphabetical.
		provider.UsedBy = []string{}
		for _, slot := range Slots {
			if slices.Contains(uses, slot) {
				provider.UsedBy = append(provider.UsedBy, slot)
			}
		}
		return nil
	})
	return provider, err
}

// SetSlot sets one feature. Turning it on, or moving it to another provider while it is on, records
// the actor's consent: the feature's data goes to that provider from now on.
func (s *Service) SetSlot(ctx context.Context, workspaceID string, actor Actor, slot string, input SlotInput) (Slot, error) {
	if !slices.Contains(Slots, slot) {
		return Slot{}, ErrNotFound
	}
	effort := aiprovider.Effort(input.Effort)
	if input.Effort == "" {
		effort = aiprovider.EffortLow
	}
	if !effort.Valid() {
		return Slot{}, invalid("effort is low, medium or high.")
	}
	if input.MonthlyCapSet && input.MonthlyCap != nil && (slot != SlotChat || *input.MonthlyCap <= 0) {
		return Slot{}, invalid("Only AI chat takes a monthly limit, of more than zero.")
	}
	now := s.now().UTC()
	var result Slot
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		next := slotRow{slot: slot, effort: string(effort), enabled: input.Enabled}
		label := ""
		switch input.Source {
		case SourceServer:
			models := s.config.Server.ModelsFor(slot)
			if len(models) == 0 {
				return ErrNoServerProvider
			}
			next.usesServer = true
			model := input.Model
			if model == "" {
				model = models[0]
			}
			if !slices.Contains(models, model) {
				return invalid("The server's provider offers %v for this feature.", models)
			}
			next.model = &model
			label = s.config.Server.Label
		case SourceProvider:
			if err := lockProviders(ctx, tx, workspaceID); err != nil {
				return err
			}
			provider, err := loadProvider(ctx, tx, workspaceID, input.ProviderID, false)
			if errors.Is(err, ErrNotFound) {
				return invalid("Pick one of this workspace's providers (provider_id).")
			}
			if err != nil {
				return err
			}
			model := input.Model
			if model == "" {
				model = provider.Models[0]
			}
			if !slices.Contains(provider.Models, model) {
				return invalid("Pick one of the provider's models: %v.", provider.Models)
			}
			next.providerID, next.model = &provider.ID, &model
			label = provider.Label
		case SourceNone, "":
			if input.Enabled {
				return invalid("Pick a provider before turning the feature on.")
			}
		default:
			return invalid("source is server, provider or none.")
		}
		slots, err := readSlots(ctx, tx, workspaceID, true)
		if err != nil {
			return err
		}
		current, found := slots[slot]
		sameRecipient := found && current.usesServer == next.usesServer && equalPointer(current.providerID, next.providerID)
		next.consentedBy, next.consentedAt = current.consentedBy, current.consentedAt
		if next.enabled && !(found && current.enabled && sameRecipient) {
			next.consentedBy, next.consentedAt = stringPointer(actor.AgentID), &now
		}
		if err := nextAutomatic(slot, &next, current, found && sameRecipient, input); err != nil {
			return err
		}
		next.monthlyCap = current.monthlyCap
		if input.MonthlyCapSet {
			next.monthlyCap = input.MonthlyCap
		}
		// A limit counts spend, which the server can compute only for a model it has a price for.
		if input.MonthlyCapSet && next.monthlyCap != nil && next.model != nil && !s.priced(Call{Feature: FeatureChat, BilledTo: BilledToWorkspaceKey}, *next.model) {
			if s.config.Server == nil {
				return invalid("This server has no AI provider of its own, so it has no price for %s and cannot count what AI chat spends: no limit in dollars can be set.", *next.model)
			}
			return invalid("This server has no price for %s, so it cannot count what AI chat spends. Ask its operator to add one to AI_SERVER_PRICES.", *next.model)
		}
		updatedBy := nullable(actor.AgentID)
		var row slotRow
		err = tx.QueryRow(ctx, `INSERT INTO ai_slots(workspace_id,slot,uses_server,provider_id,model,effort,enabled,automatic,auto_parts,consented_by,consented_at,updated_by,updated_at,
				monthly_cap_micro_usd)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
			ON CONFLICT (workspace_id,slot) DO UPDATE SET uses_server=EXCLUDED.uses_server,provider_id=EXCLUDED.provider_id,model=EXCLUDED.model,
				effort=EXCLUDED.effort,enabled=EXCLUDED.enabled,automatic=EXCLUDED.automatic,auto_parts=EXCLUDED.auto_parts,
				consented_by=EXCLUDED.consented_by,consented_at=EXCLUDED.consented_at,updated_by=EXCLUDED.updated_by,updated_at=EXCLUDED.updated_at,
				monthly_cap_micro_usd=EXCLUDED.monthly_cap_micro_usd
			RETURNING slot,uses_server,provider_id::text,model,effort,enabled,automatic,auto_parts,consented_by::text,consented_at,updated_by::text,updated_at,
				monthly_cap_micro_usd`,
			workspaceID, slot, next.usesServer, next.providerID, next.model, next.effort, next.enabled, next.automatic, next.autoParts, next.consentedBy, next.consentedAt, updatedBy, now,
			next.monthlyCap).
			Scan(&row.slot, &row.usesServer, &row.providerID, &row.model, &row.effort, &row.enabled, &row.automatic, &row.autoParts, &row.consentedBy, &row.consentedAt, &row.updatedBy, &row.updatedAt,
				&row.monthlyCap)
		if err != nil {
			return err
		}
		result = s.view(slot, row, true)
		from := map[string]any{"enabled": false, "source": SourceNone}
		if found {
			from = s.slotDetail(ctx, tx, workspaceID, current)
		}
		to := s.slotDetail(ctx, tx, workspaceID, row)
		if found && equalDetail(from, to) {
			return nil
		}
		providerID := ""
		if row.providerID != nil {
			providerID = *row.providerID
		}
		return addEvent(ctx, tx, workspaceID, "slot_changed", actor, providerID, label, slot, map[string]any{"from": from, "to": to}, now)
	})
	return result, err
}

func (s *Service) slotDetail(ctx context.Context, tx pgx.Tx, workspaceID string, row slotRow) map[string]any {
	detail := map[string]any{"enabled": row.enabled, "source": row.source(), "effort": row.effort, "automatic": row.automatic}
	if row.monthlyCap != nil {
		detail["monthly_cap_micro_usd"] = *row.monthlyCap
	}
	if parts := orderedParts(row.autoParts); len(parts) > 0 {
		detail["auto_parts"] = strings.Join(parts, ",")
	}
	if row.model != nil {
		detail["model"] = *row.model
	}
	switch {
	case row.usesServer && s.config.Server != nil:
		detail["provider_label"] = s.config.Server.Label
	case row.providerID != nil:
		var label string
		if tx.QueryRow(ctx, `SELECT label FROM ai_providers WHERE workspace_id=$1 AND id=$2`, workspaceID, *row.providerID).Scan(&label) == nil {
			detail["provider_label"] = label
		}
	}
	return detail
}

// AutoParts are the parts of a triage suggestion AI may apply by itself, in display order.
var AutoParts = []string{"tags", "priority", "assignee"}

// defaultAutoParts is what turning automatic apply on picks when nothing is named: a wrong assignee
// sends a ticket to the wrong person's feed, so it is chosen by hand.
var defaultAutoParts = []string{"tags", "priority"}

func orderedParts(parts []string) []string {
	ordered := []string{}
	for _, part := range AutoParts {
		if slices.Contains(parts, part) {
			ordered = append(ordered, part)
		}
	}
	return ordered
}

// nextAutomatic decides the automatic mode of a slot being set: kept when the request does not name
// it, off with the feature, and off when the feature moves to another provider (its consent is the
// admin's again) unless the same request turns it on.
func nextAutomatic(slot string, next *slotRow, current slotRow, sameRecipient bool, input SlotInput) error {
	next.automatic, next.autoParts = current.automatic && sameRecipient, current.autoParts
	if input.Automatic != nil {
		next.automatic = *input.Automatic
	}
	if input.AutoParts != nil {
		for _, part := range input.AutoParts {
			if !slices.Contains(AutoParts, part) {
				return invalid("auto_parts holds tags, priority and assignee.")
			}
		}
		next.autoParts = orderedParts(input.AutoParts)
	}
	if next.autoParts == nil {
		next.autoParts = []string{}
	}
	if slot != SlotTriage && len(next.autoParts) > 0 {
		return invalid("auto_parts is for triage only.")
	}
	if next.automatic && !next.enabled {
		if input.Automatic != nil {
			return invalid("Turn the feature on before it runs by itself.")
		}
		next.automatic = false
	}
	if !next.enabled {
		next.automatic = false
	}
	if next.automatic && slot == SlotTriage && len(next.autoParts) == 0 {
		if input.AutoParts != nil {
			return invalid("Pick at least one of tags, priority and assignee to apply by itself.")
		}
		next.autoParts = defaultAutoParts
	}
	return nil
}

func equalDetail(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func equalPointer(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// ActiveFeatures says which features can run now in the workspace: what the web app offers on screen.
func (s *Service) ActiveFeatures(ctx context.Context, workspaceID string) (map[string]bool, error) {
	active := map[string]bool{}
	for _, slot := range Slots {
		active[slot] = false
	}
	err := s.db.InWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		slots, err := readSlots(ctx, tx, workspaceID, false)
		for slot, row := range slots {
			active[slot] = s.view(slot, row, true).Active
		}
		return err
	})
	return active, err
}
