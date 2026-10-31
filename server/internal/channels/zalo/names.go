package zalo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/jackc/pgx/v5"
)

// A delivery names its sender by id only. Once it is taken, the sender's name is read from Zalo while it is
// not known, at most once an hour per sender (zalo_users.looked_up_at): their profile as the OA sees it,
// or for a group member who does not follow the OA, the group's member list. The name is kept in
// zalo_users, which the next messages are parsed with, and given to what was stored already: the
// contact (while it is still called by the id), the identity and the chat's items. A lookup that fails
// changes nothing.

// lookupEvery is how often one sender's name, or one group's member list, is asked for while Zalo does not
// give it.
const lookupEvery = time.Hour

// maxMemberPages bounds a group's member list read at once (50 members a page).
const maxMemberPages = 20

// maxLookups is how many lookups run at once; a delivery that finds them all busy looks up nothing.
const maxLookups = 4

// zaloError is an answer of Zalo's with an error code.
type zaloError struct{ code int }

func (e *zaloError) Error() string { return fmt.Sprintf("Zalo answered error %d", e.code) }

// retryable says a lookup failed for a reason of the moment (a refused token the next send refreshes,
// the network, Zalo's own error): its claim is given back so the next message tries again.
func retryable(err error) bool {
	var refused *zaloError
	if errors.As(err, &refused) {
		return tokenRefused[refused.code]
	}
	return err != nil
}

// lookupName reads the name of a delivery's customer from Zalo when it is not known yet.
func (a *Adapter) lookupName(ctx context.Context, kit channels.RouteKit, channel channels.Channel, e event) {
	if !e.fromCustomer() || e.Sender.ID == "" {
		return
	}
	select {
	case a.lookups <- struct{}{}:
		defer func() { <-a.lookups }()
	default:
		return
	}
	user := e.Sender.ID
	claimed, err := a.claim(ctx, kit, channel, user, true)
	if err != nil || !claimed {
		return
	}
	token, found, err := kit.Credential(ctx, channel, CredentialAccessToken)
	if err != nil || !found {
		a.release(ctx, kit, channel, user)
		return
	}
	name, err := a.profileName(ctx, token, user)
	switch {
	case retryable(err):
		slog.Warn("zalo name lookup", "channel", channel.ID, "error", err)
		a.release(ctx, kit, channel, user)
		return
	case (err != nil || name == "") && e.inGroup():
		// No name from the profile (not a follower, which Zalo is said to answer -213 for, unverified; another answer; or an empty name): the group's member list names
		// them, and every other member read with them; it is read at most once an hour a group.
		a.readMembers(ctx, kit, channel, token, e.Recipient.ID)
		return
	case err != nil || name == "":
		return
	}
	a.keepNames(ctx, kit, channel, map[string]string{user: name})
}

// claim takes the right to ask Zalo about key (a user id, or "group:<id>") for the next hour, unless its
// name is known (for a user) or another asked within the hour. A team member is never asked about.
func (a *Adapter) claim(ctx context.Context, kit channels.RouteKit, channel channels.Channel, key string, user bool) (bool, error) {
	claimed := false
	now := a.config.Now().UTC()
	err := kit.InWorkspace(ctx, channel, func(tx pgx.Tx) error {
		if user {
			var staff bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM zalo_channels WHERE workspace_id=$1 AND channel_id=$2 AND $3=ANY(staff_user_ids))`,
				channel.WorkspaceID, channel.ID, key).Scan(&staff); err != nil || staff {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `INSERT INTO zalo_users(workspace_id,channel_id,user_id,looked_up_at) VALUES($1,$2,$3,$4)
			ON CONFLICT (workspace_id,channel_id,user_id) DO UPDATE SET looked_up_at=EXCLUDED.looked_up_at
			WHERE zalo_users.name IS NULL AND zalo_users.looked_up_at<EXCLUDED.looked_up_at-make_interval(secs => $5)`,
			channel.WorkspaceID, channel.ID, key, now, lookupEvery.Seconds())
		claimed = tag.RowsAffected() == 1
		return err
	})
	return claimed, err
}

// release gives a claim back, so the next message asks again.
func (a *Adapter) release(ctx context.Context, kit channels.RouteKit, channel channels.Channel, key string) {
	_ = kit.InWorkspace(ctx, channel, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE zalo_users SET looked_up_at=looked_up_at-make_interval(secs => $4) WHERE workspace_id=$1 AND channel_id=$2 AND user_id=$3 AND name IS NULL`,
			channel.WorkspaceID, channel.ID, key, 2*lookupEvery.Seconds())
		return err
	})
}

// readMembers reads a group's member list, at most once an hour, and keeps every name in it.
func (a *Adapter) readMembers(ctx context.Context, kit channels.RouteKit, channel channels.Channel, token, group string) {
	key := "group:" + group
	claimed, err := a.claim(ctx, kit, channel, key, false)
	if err != nil || !claimed {
		return
	}
	names, err := a.members(ctx, token, group)
	if retryable(err) {
		slog.Warn("zalo group members", "channel", channel.ID, "error", err)
		a.release(ctx, kit, channel, key)
		return
	}
	if len(names) > 0 {
		a.keepNames(ctx, kit, channel, names)
	}
}

// keepNames keeps names Zalo gave (user id to name) and gives them to what was stored of those people
// while they were known by their id; a name already kept is not replaced.
func (a *Adapter) keepNames(ctx context.Context, kit channels.RouteKit, channel channels.Channel, names map[string]string) {
	users, given := make([]string, 0, len(names)), make([]string, 0, len(names))
	for user, name := range names {
		if name = clip(name, 200); name != "" && len(user) <= 100 {
			users, given = append(users, user), append(given, name)
		}
	}
	now := a.config.Now().UTC()
	if err := kit.InWorkspace(ctx, channel, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO zalo_users(workspace_id,channel_id,user_id,name,looked_up_at) SELECT $1,$2,u,n,$5 FROM unnest($3::text[],$4::text[]) AS x(u,n)
			ON CONFLICT (workspace_id,channel_id,user_id) DO UPDATE SET name=EXCLUDED.name WHERE zalo_users.name IS NULL`,
			channel.WorkspaceID, channel.ID, users, given, now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE contact_identities i SET display_name=x.n,updated_at=now() FROM unnest($4::text[],$5::text[]) AS x(u,n)
			WHERE i.workspace_id=$1 AND i.channel_id=$2 AND i.kind=$3 AND i.value=x.u AND i.display_name IS NULL`,
			channel.WorkspaceID, channel.ID, IdentityUser, users, given); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE contacts c SET name=x.n FROM contact_identities i, unnest($4::text[],$5::text[]) AS x(u,n)
			WHERE i.workspace_id=$1 AND i.channel_id=$2 AND i.kind=$3 AND i.value=x.u AND c.workspace_id=i.workspace_id AND c.id=i.contact_id AND c.name=x.u`,
			channel.WorkspaceID, channel.ID, IdentityUser, users, given); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE channel_thread_items i SET sender_name=x.n FROM unnest($4::text[],$5::text[]) AS x(u,n)
			WHERE i.workspace_id=$1 AND i.channel_id=$2 AND i.sender_kind=$3 AND i.sender_value=x.u AND i.sender_name IS NULL`,
			channel.WorkspaceID, channel.ID, IdentityUser, users, given)
		return err
	}); err != nil {
		slog.Warn("zalo name lookup: keep the names", "channel", channel.ID, "error", err)
	}
}

func clip(value string, size int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) > size {
		runes = runes[:size]
	}
	return string(runes)
}

// get reads one of Zalo's GET answers into into; an answer with an error code is a *zaloError.
func (a *Adapter) get(ctx context.Context, token, path string, query url.Values, into any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, a.config.MessageBase+path+"?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	request.Header.Set("access_token", token)
	response, err := a.config.HTTP.Do(request)
	if err != nil {
		return fmt.Errorf("Zalo did not answer: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 500 {
		return fmt.Errorf("Zalo answered %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	var envelope struct {
		Error int             `json:"error"`
		Data  json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("Zalo answered %d with no JSON", response.StatusCode)
	}
	if envelope.Error != 0 {
		return &zaloError{code: envelope.Error}
	}
	return json.Unmarshal(envelope.Data, into)
}

// profileName is a follower's name as the OA sees it.
func (a *Adapter) profileName(ctx context.Context, token, user string) (string, error) {
	data, _ := json.Marshal(map[string]string{"user_id": user})
	var profile struct {
		DisplayName string `json:"display_name"`
	}
	err := a.get(ctx, token, "/v3.0/oa/user/detail", url.Values{"data": {string(data)}}, &profile)
	return profile.DisplayName, err
}

// members is a group's member list, user id to name.
func (a *Adapter) members(ctx context.Context, token, group string) (map[string]string, error) {
	names := map[string]string{}
	for page, offset := 0, 0; page < maxMemberPages; page++ {
		var list struct {
			Total   int `json:"total"`
			Members []struct {
				UserID string `json:"user_id"`
				Name   string `json:"name"`
			} `json:"members"`
		}
		if err := a.get(ctx, token, "/v3.0/oa/group/listmember", url.Values{"group_id": {group}, "offset": {strconv.Itoa(offset)}, "count": {"50"}}, &list); err != nil {
			return names, err
		}
		for _, member := range list.Members {
			if member.UserID != "" && strings.TrimSpace(member.Name) != "" {
				names[member.UserID] = member.Name
			}
		}
		offset += len(list.Members)
		if len(list.Members) == 0 || offset >= list.Total {
			break
		}
	}
	return names, nil
}
