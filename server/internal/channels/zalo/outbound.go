package zalo

import (
	"bytes"
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
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5"
)

// PartRunes is the most a message to Zalo holds; a longer reply goes in parts, in order.
const PartRunes = 2000

// Delivery codes the web app explains.
const (
	CodeNotConnected = "zalo_not_connected"
	CodeRefused      = "zalo_refused"
	CodeNoFiles      = "zalo_no_files"
)

// tokenRefused are the codes with which Zalo refuses an access token that expired or was replaced.
var tokenRefused = map[int]bool{-216: true, -220: true, -124: true, -14: true}

type outbound struct{ a *Adapter }

func (outbound) Prepare(context.Context, pgx.Tx, channels.PrepareRequest) (channels.Prepared, error) {
	return channels.Prepared{Status: channels.DeliveryQueued, Recipients: 1}, nil
}

// answer is what Zalo answers a call.
type answer struct {
	Error   int    `json:"error"`
	Message string `json:"message"`
	Data    struct {
		MessageID string `json:"message_id"`
	} `json:"data"`
}

// Send delivers a reply or a notice: to the group it belongs to, or to the customer alone (quoting their
// last message, once). A long text goes in parts of PartRunes; a retry sends only the parts that did not
// go. An access token Zalo refuses is refreshed once and the part sent again.
func (o outbound) Send(ctx context.Context, out channels.Outbound, kit channels.SendKit) channels.SendResult {
	if len(out.Message.Files) > 0 {
		return channels.SendResult{Outcome: channels.OutcomeFailed, Code: CodeNoFiles,
			Reason: "Zalo replies cannot carry files yet: send the text without them."}
	}
	path, recipient := "/v3.0/oa/message/cs", map[string]string{}
	quote := ""
	switch {
	case out.Place != nil && out.Place.Kind == channels.PlaceGroup:
		path, recipient["group_id"] = "/v3.0/oa/group/message", strings.TrimPrefix(out.Place.ExternalID, "group:")
	case out.Place != nil && strings.HasPrefix(out.Place.ExternalID, "user:"):
		recipient["user_id"], quote = strings.TrimPrefix(out.Place.ExternalID, "user:"), out.LastIncoming
	default:
		for _, identity := range out.Recipient.Identities {
			if identity.Kind == IdentityUser {
				recipient["user_id"] = identity.Value
			}
		}
		quote = out.LastIncoming
	}
	if recipient["user_id"] == "" && recipient["group_id"] == "" {
		return channels.Failed("This customer has no Zalo id on this OA, so the reply cannot be sent there.")
	}
	token, _, found, err := kit.Credential(ctx, CredentialAccessToken)
	if err != nil {
		return channels.Retry(err.Error())
	}
	if !found {
		return channels.SendResult{Outcome: channels.OutcomeFailed, Code: CodeNotConnected,
			Reason: "The OA has no access token yet: add it in the channel's settings, then retry."}
	}
	parts := split(out.Message.Body, PartRunes)
	var progress struct {
		Sent  int    `json:"sent"`
		First string `json:"first,omitempty"`
	}
	_ = json.Unmarshal(out.Prepared, &progress)
	var ids []string
	if progress.First != "" {
		ids = append(ids, progress.First)
	}
	refreshed := false
	// send delivers one part: a refused token is refreshed once for the whole reply, and a quote Zalo
	// cannot make (a message too old, deleted) is dropped rather than saying nothing, after a refresh too.
	send := func(message map[string]string) (answer, error) {
		for {
			got, err := o.post(ctx, token, path, recipient, message)
			switch {
			case err != nil:
				return got, err
			case tokenRefused[got.Error] && !refreshed:
				refreshed = true
				if token, err = o.refresh(ctx, out, kit); err != nil {
					return got, err
				}
			case got.Error != 0 && !tokenRefused[got.Error] && message["quote_message_id"] != "":
				delete(message, "quote_message_id")
			default:
				return got, nil
			}
		}
	}
	result := func(r channels.SendResult, sent int) channels.SendResult {
		first := progress.First
		if first == "" && len(ids) > 0 {
			first = ids[0]
		}
		meta, _ := json.Marshal(map[string]any{"sent": sent, "first": first})
		r.Meta, r.ExternalIDs, r.ExternalID = meta, ids, first
		return r
	}
	for i := progress.Sent; i < len(parts); i++ {
		message := map[string]string{"text": parts[i]}
		if i == 0 && quote != "" {
			message["quote_message_id"] = quote
		}
		got, err := send(message)
		switch {
		case err != nil:
			return result(channels.Retry(err.Error()), i)
		case got.Error != 0:
			return result(channels.SendResult{Outcome: channels.OutcomeFailed, Code: CodeRefused,
				Reason: fmt.Sprintf("Zalo refused the message (%d: %s).", got.Error, got.Message)}, i)
		}
		if got.Data.MessageID != "" {
			ids = append(ids, got.Data.MessageID)
		}
	}
	return result(channels.Sent(""), len(parts))
}

func (o outbound) post(ctx context.Context, token, path string, recipient, message map[string]string) (answer, error) {
	body, _ := json.Marshal(map[string]any{"recipient": recipient, "message": message})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, o.a.config.MessageBase+path, bytes.NewReader(body))
	if err != nil {
		return answer{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("access_token", token)
	return o.call(request)
}

func (o outbound) call(request *http.Request) (answer, error) {
	response, err := o.a.config.HTTP.Do(request)
	if err != nil {
		return answer{}, fmt.Errorf("Zalo did not answer: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return answer{}, fmt.Errorf("read Zalo's answer: %w", err)
	}
	if response.StatusCode >= 500 {
		return answer{}, fmt.Errorf("Zalo answered %d", response.StatusCode)
	}
	var got answer
	if err := json.Unmarshal(data, &got); err != nil {
		return answer{}, fmt.Errorf("Zalo answered %d with no JSON", response.StatusCode)
	}
	return got, nil
}

// refresh exchanges the refresh token for a new pair, one sender at a time: the refresh token is locked
// while Zalo is asked, the new access token stored, then the new refresh token. A refresh token is used
// once; a sender that finds it refreshed by another meanwhile reads the access token that one stored.
func (o outbound) refresh(ctx context.Context, out channels.Outbound, kit channels.SendKit) (string, error) {
	_, seen, found, err := kit.Credential(ctx, CredentialRefreshToken)
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("the OA has no refresh token: add the tokens again in the channel's settings")
	}
	appSecret, _, found, err := kit.Credential(ctx, CredentialAppSecret)
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("the channel has no app secret key to refresh its token with")
	}
	appID, err := o.appID(ctx, out, kit)
	if err != nil {
		return "", err
	}
	_, accessVersion, _, err := kit.Credential(ctx, CredentialAccessToken)
	if err != nil {
		return "", err
	}
	if _, _, err := kit.RefreshCredential(ctx, CredentialRefreshToken, seen, func(ctx context.Context, current string) (string, *time.Time, error) {
		form := url.Values{"app_id": {appID}, "refresh_token": {current}, "grant_type": {"refresh_token"}}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, o.a.config.OAuthBase+"/v4/oa/access_token", strings.NewReader(form.Encode()))
		if err != nil {
			return "", nil, err
		}
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("secret_key", appSecret)
		response, err := o.a.config.HTTP.Do(request)
		if err != nil {
			return "", nil, fmt.Errorf("Zalo's token server did not answer: %w", err)
		}
		defer response.Body.Close()
		var tokens struct {
			AccessToken  string          `json:"access_token"`
			RefreshToken string          `json:"refresh_token"`
			ExpiresIn    json.RawMessage `json:"expires_in"`
			Error        int             `json:"error"`
		}
		if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&tokens); err != nil || tokens.AccessToken == "" || tokens.RefreshToken == "" {
			return "", nil, fmt.Errorf("Zalo did not refresh the token (status %d, error %d)", response.StatusCode, tokens.Error)
		}
		var expires *time.Time
		if seconds, err := strconv.ParseInt(strings.Trim(string(tokens.ExpiresIn), `"`), 10, 64); err == nil && seconds > 0 {
			at := o.a.config.Now().UTC().Add(time.Duration(seconds) * time.Second)
			expires = &at
		}
		if _, err := kit.ReplaceCredential(ctx, CredentialAccessToken, tokens.AccessToken, accessVersion, expires); err != nil {
			// Zalo spent the old refresh token: the new one must be kept even so.
			slog.Error("zalo access token refreshed but not stored", "channel", out.Channel.ID, "error", err)
		}
		return tokens.RefreshToken, nil, nil
	}); err != nil {
		slog.Error("zalo token refresh failed", "channel", out.Channel.ID, "error", err)
		return "", err
	}
	token, _, _, err := kit.Credential(ctx, CredentialAccessToken)
	return token, err
}

// appID is the Zalo app the channel is reached through, as its configuration says.
func (o outbound) appID(ctx context.Context, out channels.Outbound, kit channels.SendKit) (string, error) {
	host, ok := kit.Host().(*store.Store)
	if !ok {
		return "", errors.New("no store to read the channel's app from")
	}
	var appID string
	err := host.InWorkspace(ctx, out.WorkspaceID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT app_id FROM zalo_channels WHERE workspace_id=$1 AND channel_id=$2`, out.WorkspaceID, out.Channel.ID).Scan(&appID)
	})
	return appID, err
}

// split cuts text into parts of at most size runes, at a line break or a space when there is one.
func split(text string, size int) []string {
	runes := []rune(text)
	var parts []string
	for len(runes) > size {
		cut := size
		for i := size; i > size/2; i-- {
			if runes[i] == '\n' || runes[i] == ' ' {
				cut = i
				break
			}
		}
		parts = append(parts, strings.TrimRight(string(runes[:cut]), " \n"))
		runes = []rune(strings.TrimLeft(string(runes[cut:]), " \n"))
	}
	if len(runes) > 0 || len(parts) == 0 {
		parts = append(parts, string(runes))
	}
	return parts
}
