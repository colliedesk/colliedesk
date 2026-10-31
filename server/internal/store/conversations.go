package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/jackc/pgx/v5"
)

// ConversationFilter narrows the inbox list. Empty fields do not narrow; a list matches any of its
// values. The HTTP layer validates every value, so the store trusts them.
type ConversationFilter struct {
	Statuses   []string
	Priorities []string
	// Assignees holds "me", "unassigned", "anyone" or agent ids; AgentID is who "me" is.
	Assignees   []string
	TagIDs      []string
	TagMatchAll bool
	ContactID   string
	// Organization matches the contact's organization ignoring case.
	Organization string
	// Awaiting is "team" (the customer wrote last) or "customer" (the team did); notes do not count.
	Awaiting        string
	HasPendingDraft *bool
	DeliveryFailed  *bool
	// Source is "imported" or "native".
	Source   string
	Channels []string
	// ChannelIDs keep the conversations that came through one of the channels or whose replies leave
	// from one of them; removed channels count.
	ChannelIDs    []string
	CreatedAfter  *time.Time
	CreatedBefore *time.Time
	UpdatedAfter  *time.Time
	UpdatedBefore *time.Time
	// Ref keeps the conversations whose code (display_ref) is the one a person typed, read by the
	// formats of the workspace's series: another case or padding matches, another year does not.
	Ref string
	// refIDs are the conversations Ref (or Search) names, found before the list is read.
	refIDs []string
	// SLA keeps the conversations a running response target makes "overdue" or "due_soon".
	SLA []string
	// slaOn says the workspace's response targets are on (read before the list).
	slaOn bool
	// Sort is one of the ConversationSort* values; empty is ConversationSortUpdatedDesc.
	Sort   string
	Search string
	// ResultSnapshotID and ResultMetric keep the conversations one number of the results page counts
	// (results_members); a snapshot no longer kept is ErrResultsRefreshed.
	ResultSnapshotID string
	ResultMetric     string
	AgentID          string
	Page             int
	PageSize         int
}

// Orders of the inbox list. Every order ends with the id, so a page never repeats or skips a row.
const (
	ConversationSortUpdatedDesc    = "updated_desc"
	ConversationSortUpdatedAsc     = "updated_asc"
	ConversationSortCreatedDesc    = "created_desc"
	ConversationSortCreatedAsc     = "created_asc"
	ConversationSortPriority       = "priority"
	ConversationSortWaitingLongest = "waiting_longest"
	ConversationSortDue            = "due"
)

var conversationOrders = map[string]string{
	ConversationSortUpdatedDesc: `c.updated_at DESC,c.id DESC`,
	ConversationSortUpdatedAsc:  `c.updated_at ASC,c.id ASC`,
	ConversationSortCreatedDesc: `c.created_at DESC,c.id DESC`,
	ConversationSortCreatedAsc:  `c.created_at ASC,c.id ASC`,
	ConversationSortPriority:    `CASE c.priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'normal' THEN 2 ELSE 3 END,c.updated_at DESC,c.id DESC`,
	// Conversations awaiting the team, the oldest customer message first, then the rest by last update.
	ConversationSortWaitingLongest: `CASE WHEN c.last_public_kind='incoming' THEN c.last_public_at END ASC NULLS LAST,c.updated_at DESC,c.id DESC`,
	// The earliest running response target first, then the rest as the inbox shows them.
	ConversationSortDue: `c.sla_due_at ASC NULLS LAST,c.updated_at DESC,c.id DESC`,
}

type ConversationPage struct {
	Items []domain.Conversation `json:"items"`
	Page  int                   `json:"page"`
	Size  int                   `json:"page_size"`
}

// CreateConversation opens an empty conversation; channelID, when given, is the channel its replies
// leave from (else the first reply takes the default one).
func (s *Store) CreateConversation(ctx context.Context, workspaceID, contactID, subject, channelID string) (domain.Conversation, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.Conversation, error) {
		if err := checkContactNotMerged(ctx, tx, workspaceID, contactID); err != nil {
			return domain.Conversation{}, err
		}
		var replyChannel *string
		if channelID != "" {
			carrier, err := s.startingChannel(ctx, tx, workspaceID, channelID)
			if err != nil {
				return domain.Conversation{}, err
			}
			replyChannel = &carrier.ID
		}
		var id string
		err := tx.QueryRow(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject,channel,reply_channel_id)
			SELECT $1,c.id,$3,$4,$5::uuid FROM contacts c WHERE c.workspace_id=$1 AND c.id=$2 RETURNING id::text`,
			workspaceID, contactID, subject, domain.ChannelAgent, replyChannel).Scan(&id)
		if err != nil {
			return domain.Conversation{}, fmt.Errorf("create conversation: %w", err)
		}
		return s.withChannelRef(ctx, tx, workspaceID)(getConversation(ctx, tx, workspaceID, id))
	})
}

// NewConversation is a conversation the team starts, with a contact of the workspace (ContactID) or a
// customer added with it (NewContact), in the same transaction.
type NewConversation struct {
	ContactID  string
	NewContact *NewContact
	Subject    string
	// ChannelID is the channel the conversation is started from; empty for its type's default.
	ChannelID string
}

// startingChannel is the channel a conversation the team starts leaves from: the one chosen (a live
// channel of the type that carries such replies), else that type's default. It must be on: the first
// reply leaves now.
func (s *Store) startingChannel(ctx context.Context, tx pgx.Tx, workspaceID, channelID string) (channels.Channel, error) {
	if channelID == "" {
		carrier, err := s.otherRepliesCarrier(ctx, tx, workspaceID)
		if err != nil {
			return channels.Channel{}, err
		}
		if carrier == nil {
			return channels.Channel{}, domain.ErrEmailChannelMissing
		}
		if !carrier.Enabled {
			return channels.Channel{}, channels.ErrChannelOff
		}
		return *carrier, nil
	}
	channel, err := s.channelByID(ctx, tx, workspaceID, channelID)
	if err != nil {
		return channels.Channel{}, err
	}
	if !channel.Live() {
		return channels.Channel{}, channels.ErrNotFound
	}
	if kind, ok := s.channels.ReplyType(""); !ok || channel.Type != kind {
		return channels.Channel{}, channels.ErrChannelCannotSend
	}
	if !channel.Enabled {
		return channels.Channel{}, channels.ErrChannelOff
	}
	return channel, nil
}

// NewContact is a customer added while starting a conversation.
type NewContact struct {
	Name, Email string
}

// StartConversation opens a conversation with its first reply in one transaction, so a reply the email
// rules refuse (trial, Cc, storage) leaves no empty conversation behind. The reply is emailed through the
// outbox like any other; a workspace without an email channel is refused, because the reply would be
// stored and never sent.
func (s *Store) StartConversation(ctx context.Context, workspaceID string, in NewConversation, actor Actor, body string, cc []string, files ...NewAttachment) (domain.Conversation, error) {
	if len(cc) == 0 {
		cc = nil
	}
	tx, err := s.db.BeginWorkspace(ctx, workspaceID)
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("begin conversation: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	contactID := in.ContactID
	if in.NewContact != nil {
		added, err := insertContact(ctx, tx, workspaceID, in.NewContact.Name, in.NewContact.Email)
		if err != nil {
			return domain.Conversation{}, err
		}
		contactID = added.ID
	} else if err := checkContactNotMerged(ctx, tx, workspaceID, contactID); err != nil {
		return domain.Conversation{}, err
	}
	// The reply goes out through the channel chosen, or the default one of the type that carries a team
	// member's conversations; the conversation keeps it.
	carrier, err := s.startingChannel(ctx, tx, workspaceID, in.ChannelID)
	if err != nil {
		return domain.Conversation{}, err
	}
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject,channel,reply_channel_id)
		SELECT $1,c.id,$3,$4,$5::uuid FROM contacts c WHERE c.workspace_id=$1 AND c.id=$2 RETURNING id::text`,
		workspaceID, contactID, in.Subject, domain.ChannelAgent, carrier.ID).Scan(&id)
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("create conversation: %w", err)
	}
	if _, err := s.createMessage(ctx, tx, workspaceID, id, actor, "outgoing", body, cc, files); err != nil {
		return domain.Conversation{}, err
	}
	conversation, err := s.withChannelRef(ctx, tx, workspaceID)(getConversation(ctx, tx, workspaceID, id))
	if err != nil {
		return domain.Conversation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Conversation{}, fmt.Errorf("commit conversation: %w", err)
	}
	return conversation, nil
}

func (s *Store) GetConversation(ctx context.Context, workspaceID, id string) (domain.Conversation, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (domain.Conversation, error) {
		item, err := s.withChannelRef(ctx, tx, workspaceID)(getConversation(ctx, tx, workspaceID, id))
		if err != nil {
			return item, err
		}
		// Which statuses would tell the customer is read for one conversation, not for a list.
		return item, s.attachStatusNotices(ctx, tx, workspaceID, &item)
	})
}

// withChannelRef returns a function that gives a conversation read in tx its channel_ref.
func (s *Store) withChannelRef(ctx context.Context, tx pgx.Tx, workspaceID string) func(domain.Conversation, error) (domain.Conversation, error) {
	return func(conversation domain.Conversation, err error) (domain.Conversation, error) {
		if err != nil {
			return conversation, err
		}
		items := []domain.Conversation{conversation}
		if err := s.attachChannelRefs(ctx, tx, workspaceID, items); err != nil {
			return domain.Conversation{}, err
		}
		if err := s.attachSLA(ctx, tx, workspaceID, items); err != nil {
			return domain.Conversation{}, err
		}
		if err := attachStatusChanges(ctx, tx, workspaceID, items); err != nil {
			return domain.Conversation{}, err
		}
		return items[0], nil
	}
}

func getConversation(ctx context.Context, tx pgx.Tx, workspaceID, id string) (domain.Conversation, error) {
	var item domain.Conversation
	err := tx.QueryRow(ctx, `SELECT c.id::text,c.contact_id::text,k.name,c.subject,c.status,c.priority,c.assignee_id::text,c.channel,c.channel_id::text,c.reply_channel_id::text,c.origin,c.created_at,c.updated_at,(SELECT coalesce(max(m.reply_number),0) FROM messages m WHERE m.workspace_id=c.workspace_id AND m.conversation_id=c.id AND m.reply_number IS NOT NULL),
		COALESCE(c.number,0),c.number_series,COALESCE(c.display_ref,'')
		FROM conversations c JOIN contacts k ON k.workspace_id=c.workspace_id AND k.id=c.contact_id WHERE c.workspace_id=$1 AND c.id=$2`, workspaceID, id).
		Scan(&item.ID, &item.ContactID, &item.ContactName, &item.Subject, &item.Status, &item.Priority, &item.AssigneeID, &item.Channel, &item.ChannelID, &item.ReplyChannelID, &item.Origin, &item.CreatedAt, &item.UpdatedAt, &item.LatestReplyNumber,
			&item.Number, &item.NumberSeries, &item.DisplayRef)
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("get conversation: %w", err)
	}
	item.CreatedAt, item.UpdatedAt = domain.UTC(item.CreatedAt), domain.UTC(item.UpdatedAt)
	tags, err := conversationTags(ctx, tx, workspaceID, id)
	if err != nil {
		return domain.Conversation{}, err
	}
	item.Tags = tags
	return item, nil
}

func (s *Store) ListConversations(ctx context.Context, workspaceID string, filter ConversationFilter) (ConversationPage, error) {
	return scoped(ctx, s, workspaceID, func(tx pgx.Tx) (ConversationPage, error) {
		page, err := listConversations(ctx, tx, workspaceID, filter, s.now())
		if err != nil {
			return page, err
		}
		if err := s.attachChannelRefs(ctx, tx, workspaceID, page.Items); err != nil {
			return page, err
		}
		if err := attachStatusChanges(ctx, tx, workspaceID, page.Items); err != nil {
			return page, err
		}
		return page, s.attachSLA(ctx, tx, workspaceID, page.Items)
	})
}

// conversationQuery builds the WHERE clause of the inbox list. Every value is a parameter; only
// fixed SQL text is concatenated.
type conversationQuery struct {
	where []string
	// searchRef is true for the conversation whose code the search names, which comes first.
	searchRef string
	args      []any
}

func (q *conversationQuery) arg(value any) string {
	q.args = append(q.args, value)
	return fmt.Sprintf("$%d", len(q.args))
}

func (q *conversationQuery) and(condition string) { q.where = append(q.where, condition) }

func conversationConditions(workspaceID string, filter ConversationFilter, now time.Time) *conversationQuery {
	q := &conversationQuery{}
	q.and("c.workspace_id=" + q.arg(workspaceID))
	if len(filter.Statuses) > 0 {
		q.and("c.status=ANY(" + q.arg(filter.Statuses) + "::text[])")
	}
	if len(filter.Priorities) > 0 {
		q.and("c.priority=ANY(" + q.arg(filter.Priorities) + "::text[])")
	}
	if len(filter.Assignees) > 0 {
		var anyOf, ids []string
		for _, assignee := range filter.Assignees {
			switch assignee {
			case "unassigned":
				anyOf = append(anyOf, "c.assignee_id IS NULL")
			case "anyone":
				anyOf = append(anyOf, "c.assignee_id IS NOT NULL")
			case "me":
				ids = append(ids, filter.AgentID)
			default:
				ids = append(ids, assignee)
			}
		}
		if len(ids) > 0 {
			anyOf = append(anyOf, "c.assignee_id=ANY("+q.arg(ids)+"::uuid[])")
		}
		q.and("(" + strings.Join(anyOf, " OR ") + ")")
	}
	if len(filter.TagIDs) > 0 {
		tags := q.arg(filter.TagIDs)
		if filter.TagMatchAll {
			q.and(`(SELECT count(DISTINCT ct.tag_id) FROM conversation_tags ct WHERE ct.workspace_id=c.workspace_id AND ct.conversation_id=c.id AND ct.tag_id=ANY(` +
				tags + `::uuid[]))=cardinality(ARRAY(SELECT DISTINCT unnest(` + tags + `::uuid[])))`)
		} else {
			q.and(`EXISTS(SELECT 1 FROM conversation_tags ct WHERE ct.workspace_id=c.workspace_id AND ct.conversation_id=c.id AND ct.tag_id=ANY(` + tags + `::uuid[]))`)
		}
	}
	if filter.ContactID != "" {
		q.and("c.contact_id=" + q.arg(filter.ContactID) + "::uuid")
	}
	if len(filter.SLA) > 0 {
		// Off, no conversation has targets; a stale row is a round of the worker behind.
		anyOf := []string{"false"}
		if filter.slaOn {
			at := q.arg(now)
			for _, state := range filter.SLA {
				switch state {
				case "overdue":
					anyOf = append(anyOf, "c.sla_due_at<="+at)
				case "due_soon":
					anyOf = append(anyOf, "(c.sla_warn_at<="+at+" AND c.sla_due_at>"+at+")")
				}
			}
		}
		q.and("c.sla_applies AND (" + strings.Join(anyOf, " OR ") + ")")
	}
	if filter.Ref != "" {
		q.and("c.id=ANY(" + q.arg(filter.refIDs) + "::uuid[])")
	}
	if filter.Organization != "" {
		q.and(`EXISTS(SELECT 1 FROM contacts k WHERE k.workspace_id=c.workspace_id AND k.id=c.contact_id AND lower(k.organization)=lower(` + q.arg(filter.Organization) + `))`)
	}
	switch filter.Awaiting {
	case "team":
		q.and("c.last_public_kind='incoming'")
	case "customer":
		q.and("c.last_public_kind='outgoing'")
	}
	if filter.HasPendingDraft != nil {
		// Expiry is applied lazily, so a pending row past its expiry no longer waits.
		draft := `EXISTS(SELECT 1 FROM agent_requests r WHERE r.workspace_id=c.workspace_id AND r.conversation_id=c.id
			AND r.kind='reply_draft' AND r.status='pending' AND r.expires_at>` + q.arg(now) + `)`
		q.and(negateUnless(*filter.HasPendingDraft, draft))
	}
	if filter.DeliveryFailed != nil {
		failed := `EXISTS(SELECT 1 FROM channel_outbox o JOIN messages m ON m.workspace_id=o.workspace_id AND m.id=o.message_id
			WHERE o.workspace_id=c.workspace_id AND o.status='failed' AND m.conversation_id=c.id)`
		q.and(negateUnless(*filter.DeliveryFailed, failed))
	}
	switch filter.Source {
	case "imported":
		q.and("c.origin IS NOT NULL")
	case "native":
		q.and("c.origin IS NULL")
	}
	if len(filter.ChannelIDs) > 0 {
		ids := q.arg(filter.ChannelIDs)
		q.and("(c.channel_id=ANY(" + ids + "::uuid[]) OR c.reply_channel_id=ANY(" + ids + "::uuid[]))")
	}
	if len(filter.Channels) > 0 {
		q.and("c.channel=ANY(" + q.arg(filter.Channels) + "::text[])")
	}
	for _, bound := range []struct {
		value *time.Time
		sql   string
	}{
		{filter.CreatedAfter, "c.created_at>="}, {filter.CreatedBefore, "c.created_at<"},
		{filter.UpdatedAfter, "c.updated_at>="}, {filter.UpdatedBefore, "c.updated_at<"},
	} {
		if bound.value != nil {
			q.and(bound.sql + q.arg(bound.value.UTC()))
		}
	}
	if filter.Search != "" {
		pattern := q.arg(escapeLikePattern(filter.Search))
		like := `ILIKE '%'||` + pattern + `||'%' ESCAPE '\'`
		// The words may be a conversation's code, read as ?ref= reads it; only then do they change the order.
		if len(filter.refIDs) > 0 {
			q.searchRef = "c.id=ANY(" + q.arg(filter.refIDs) + "::uuid[])"
		}
		byCode := ""
		if q.searchRef != "" {
			byCode = q.searchRef + ` OR `
		}
		q.and(`(` + byCode + `c.subject ` + like +
			` OR EXISTS(SELECT 1 FROM messages m WHERE m.workspace_id=c.workspace_id AND m.conversation_id=c.id AND m.detached_at IS NULL AND m.body ` + like + `)` +
			` OR EXISTS(SELECT 1 FROM contacts k WHERE k.workspace_id=c.workspace_id AND k.id=c.contact_id AND (k.name ` + like + ` OR k.email ` + like + `)))`)
	}
	if filter.ResultSnapshotID != "" {
		q.and(`EXISTS(SELECT 1 FROM results_members rm WHERE rm.workspace_id=c.workspace_id AND rm.snapshot_id=` + q.arg(filter.ResultSnapshotID) +
			`::uuid AND rm.metric=` + q.arg(filter.ResultMetric) + ` AND rm.conversation_id=c.id)`)
	}
	return q
}

// conversationsByCode are the conversations whose code is the one a person typed: read by every format
// a series has had (conversation_ref_read), each matching only the conversations made with that format,
// so a code given before a format change stays its conversation's. Another case or padding matches;
// another year or series does not. Words that cannot be a code (no digit, spaces inside, too long) match none.
func conversationsByCode(ctx context.Context, tx pgx.Tx, workspaceID, code string) ([]string, error) {
	code = strings.TrimSpace(code)
	// A code holds its number: words without a digit are none.
	if code == "" || len(code) > 60 || strings.ContainsAny(code, " \t\r\n") || !strings.ContainsAny(code, "0123456789") {
		return []string{}, nil
	}
	// Each (series, number) the code may be is looked up by the number's index, never by the workspace:
	// OFFSET 0 keeps the planner from reading the workspace's conversations first and the code for each.
	rows, err := tx.Query(ctx, `SELECT c.id FROM conversation_number_formats f
		CROSS JOIN LATERAL conversation_ref_read(f.format, $2) r
		CROSS JOIN LATERAL (SELECT c.id::text AS id FROM conversations c WHERE c.workspace_id=$1 AND c.number_series=f.series AND c.number=r.n
			AND c.number_format=f.format AND (r.year IS NULL OR c.number_year=r.year) OFFSET 0) c
		WHERE f.workspace_id=$1`, workspaceID, code)
	if err != nil {
		return nil, fmt.Errorf("find conversations by code: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func negateUnless(keep bool, condition string) string {
	if keep {
		return condition
	}
	return "NOT " + condition
}

func listConversations(ctx context.Context, tx pgx.Tx, workspaceID string, filter ConversationFilter, now time.Time) (ConversationPage, error) {
	order, ok := conversationOrders[filter.Sort]
	if !ok {
		order = conversationOrders[ConversationSortUpdatedDesc]
	}
	if filter.ResultSnapshotID != "" {
		var kept bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM results_snapshots WHERE workspace_id=$1 AND id=$2::uuid)`, workspaceID, filter.ResultSnapshotID).Scan(&kept); err != nil {
			return ConversationPage{}, fmt.Errorf("read results snapshot: %w", err)
		}
		if !kept {
			return ConversationPage{}, domain.ErrResultsRefreshed
		}
	}
	if len(filter.SLA) > 0 || filter.Sort == ConversationSortDue {
		if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT enabled FROM response_targets WHERE workspace_id=$1),false)`, workspaceID).Scan(&filter.slaOn); err != nil {
			return ConversationPage{}, fmt.Errorf("read response targets: %w", err)
		}
		if !filter.slaOn && filter.Sort == ConversationSortDue {
			filter.Sort = ConversationSortUpdatedDesc
		}
	}
	if filter.Ref != "" || filter.Search != "" {
		code := filter.Ref
		if code == "" {
			code = filter.Search
		}
		ids, err := conversationsByCode(ctx, tx, workspaceID, code)
		if err != nil {
			return ConversationPage{}, err
		}
		filter.refIDs = ids
	}
	q := conversationConditions(workspaceID, filter, now)
	limit, offset := q.arg(filter.PageSize), q.arg((filter.Page-1)*filter.PageSize)
	// A conversation whose code the search names comes first.
	if q.searchRef != "" {
		order = q.searchRef + ` DESC,` + order
	}
	idRows, err := tx.Query(ctx, `SELECT c.id::text FROM conversations c WHERE `+strings.Join(q.where, " AND ")+
		` ORDER BY `+order+` LIMIT `+limit+` OFFSET `+offset, q.args...)
	if err != nil {
		return ConversationPage{}, fmt.Errorf("list conversation ids: %w", err)
	}
	ids, err := pgx.CollectRows(idRows, pgx.RowTo[string])
	if err != nil {
		return ConversationPage{}, fmt.Errorf("scan conversation ids: %w", err)
	}
	if len(ids) == 0 {
		return ConversationPage{Items: []domain.Conversation{}, Page: filter.Page, Size: filter.PageSize}, nil
	}

	// The rows come back in the order of ids, so the two queries can never disagree on the order.
	rows, err := tx.Query(ctx, `SELECT c.id::text,c.contact_id::text,k.name,c.subject,c.status,c.priority,
		c.assignee_id::text,c.channel,c.channel_id::text,c.reply_channel_id::text,c.origin,c.created_at,c.updated_at,(SELECT coalesce(max(m.reply_number),0) FROM messages m WHERE m.workspace_id=c.workspace_id AND m.conversation_id=c.id AND m.reply_number IS NOT NULL),COALESCE(tag_data.tags,'[]'::jsonb),
		COALESCE(c.number,0),c.number_series,COALESCE(c.display_ref,'')
		FROM unnest($2::uuid[]) WITH ORDINALITY AS listed(id,position)
		JOIN conversations c ON c.workspace_id=$1 AND c.id=listed.id
		JOIN contacts k ON k.workspace_id=c.workspace_id AND k.id=c.contact_id
		LEFT JOIN LATERAL (
			SELECT jsonb_agg(jsonb_build_object('id',t.id::text,'name',t.name,'color',t.color) ORDER BY t.name) AS tags
			FROM conversation_tags ct JOIN tags t ON t.id=ct.tag_id AND t.workspace_id=ct.workspace_id
			WHERE ct.workspace_id=c.workspace_id AND ct.conversation_id=c.id
		) tag_data ON true
		ORDER BY listed.position`, workspaceID, ids)
	if err != nil {
		return ConversationPage{}, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()
	items := make([]domain.Conversation, 0, len(ids))
	for rows.Next() {
		var item domain.Conversation
		var tagsJSON []byte
		if err := rows.Scan(&item.ID, &item.ContactID, &item.ContactName, &item.Subject, &item.Status, &item.Priority,
			&item.AssigneeID, &item.Channel, &item.ChannelID, &item.ReplyChannelID, &item.Origin, &item.CreatedAt, &item.UpdatedAt, &item.LatestReplyNumber, &tagsJSON,
			&item.Number, &item.NumberSeries, &item.DisplayRef); err != nil {
			return ConversationPage{}, fmt.Errorf("scan conversation: %w", err)
		}
		if err := json.Unmarshal(tagsJSON, &item.Tags); err != nil {
			return ConversationPage{}, fmt.Errorf("decode conversation tags: %w", err)
		}
		item.CreatedAt, item.UpdatedAt = domain.UTC(item.CreatedAt), domain.UTC(item.UpdatedAt)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ConversationPage{}, fmt.Errorf("iterate conversations: %w", err)
	}
	return ConversationPage{Items: items, Page: filter.Page, Size: filter.PageSize}, nil
}

func conversationTags(ctx context.Context, tx pgx.Tx, workspaceID, conversationID string) ([]domain.Tag, error) {
	rows, err := tx.Query(ctx, `SELECT t.id::text,t.name,t.color FROM conversation_tags ct JOIN tags t ON t.id=ct.tag_id AND t.workspace_id=ct.workspace_id
		WHERE ct.workspace_id=$1 AND ct.conversation_id=$2 ORDER BY t.name`, workspaceID, conversationID)
	if err != nil {
		return nil, fmt.Errorf("list conversation tags: %w", err)
	}
	defer rows.Close()
	tags := make([]domain.Tag, 0)
	for rows.Next() {
		var tag domain.Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Color); err != nil {
			return nil, fmt.Errorf("scan conversation tag: %w", err)
		}
		tags = append(tags, tag)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversation tags: %w", err)
	}
	return tags, nil
}
