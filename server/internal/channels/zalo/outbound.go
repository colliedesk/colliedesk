package zalo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
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
	// CodeNoFiles: a reply carries a file that is no picture; CodeFileTooLarge: a picture over MaxPicture.
	CodeNoFiles      = "zalo_no_files"
	CodeFileTooLarge = "zalo_file_too_large"
)

// MaxPicture is the largest picture a reply carries to Zalo.
const MaxPicture = 1 << 20

// pictureTypes are the pictures a reply carries: Zalo's image upload takes JPEG and PNG (a GIF has an
// upload of its own, not used yet).
var pictureTypes = map[string]bool{"image/jpeg": true, "image/png": true}

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
		MessageID    string `json:"message_id"`
		AttachmentID string `json:"attachment_id"`
	} `json:"data"`
}

// Send delivers a reply or a notice: to the group it belongs to, or to the customer alone (quoting their
// last message, once). A long text goes in parts of PartRunes, then each picture as a message of its own;
// a retry sends only the parts that did not go. An access token Zalo refuses is refreshed once and the
// part sent again. A file that is no picture, or a picture over MaxPicture, is refused before anything
// goes.
func (o outbound) Send(ctx context.Context, out channels.Outbound, kit channels.SendKit) channels.SendResult {
	for _, file := range out.Message.Files {
		switch {
		case !pictureTypes[strings.ToLower(file.ContentType)]:
			return channels.SendResult{Outcome: channels.OutcomeFailed, Code: CodeNoFiles,
				Reason: file.Name + " is no picture: Zalo replies carry pictures only. Send it another way."}
		case file.Size > MaxPicture:
			return channels.SendResult{Outcome: channels.OutcomeFailed, Code: CodeFileTooLarge,
				Reason: fmt.Sprintf("%s is over 1 MB, the largest picture a Zalo reply carries.", file.Name)}
		}
		// What the bytes are, not what the file was called.
		reader, err := kit.OpenFile(ctx, file.StorageKey)
		if err != nil {
			return channels.Retry(err.Error())
		}
		head := make([]byte, 512)
		n, _ := io.ReadFull(reader, head)
		reader.Close()
		if kind := http.DetectContentType(head[:n]); !pictureTypes[kind] {
			return channels.SendResult{Outcome: channels.OutcomeFailed, Code: CodeNoFiles,
				Reason: file.Name + " is no JPEG or PNG picture (" + kind + "): Zalo replies carry those only. Send it another way."}
		}
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
	parts := []map[string]any{}
	if strings.TrimSpace(out.Message.Body) != "" || len(out.Message.Files) == 0 {
		for _, text := range split(out.Message.Body, PartRunes) {
			parts = append(parts, map[string]any{"text": text})
		}
	}
	for i := range out.Message.Files {
		parts = append(parts, map[string]any{"file": i})
	}
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
	send := func(message map[string]any) (answer, error) {
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
			case got.Error != 0 && !tokenRefused[got.Error] && message["quote_message_id"] != nil:
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
		message := parts[i]
		if index, ok := message["file"].(int); ok {
			// A picture goes up first, then as a message of its own.
			attachment, err := o.upload(ctx, &token, &refreshed, out, kit, out.Message.Files[index])
			if err != nil {
				return result(channels.Retry(err.Error()), i)
			}
			if attachment.Error != 0 {
				return result(channels.SendResult{Outcome: channels.OutcomeFailed, Code: CodeRefused,
					Reason: fmt.Sprintf("Zalo refused the picture %s (%d: %s).", out.Message.Files[index].Name, attachment.Error, attachment.Message)}, i)
			}
			message = map[string]any{"attachment": map[string]any{"type": "template", "payload": map[string]any{"template_type": "media",
				"elements": []map[string]string{{"media_type": "image", "attachment_id": attachment.Data.AttachmentID}}}}}
		} else if i == 0 && quote != "" {
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

func (o outbound) post(ctx context.Context, token, path string, recipient map[string]string, message map[string]any) (answer, error) {
	body, _ := json.Marshal(map[string]any{"recipient": recipient, "message": message})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, o.a.config.MessageBase+path, bytes.NewReader(body))
	if err != nil {
		return answer{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("access_token", token)
	return o.call(request)
}

// upload puts a picture of the reply at Zalo, which answers the id a message carries it by; a refused token
// is refreshed once for the whole reply.
func (o outbound) upload(ctx context.Context, token *string, refreshed *bool, out channels.Outbound, kit channels.SendKit, file channels.OutFile) (answer, error) {
	for {
		reader, err := kit.OpenFile(ctx, file.StorageKey)
		if err != nil {
			return answer{}, err
		}
		data, err := io.ReadAll(io.LimitReader(reader, MaxPicture+1))
		reader.Close()
		if err != nil {
			return answer{}, err
		}
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		part, err := form.CreateFormFile("file", file.Name)
		if err == nil {
			_, err = part.Write(data)
		}
		if err != nil {
			return answer{}, err
		}
		if err := form.Close(); err != nil {
			return answer{}, err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, o.a.config.MessageBase+"/v2.0/oa/upload/image", &body)
		if err != nil {
			return answer{}, err
		}
		request.Header.Set("Content-Type", form.FormDataContentType())
		request.Header.Set("access_token", *token)
		got, err := o.call(request)
		if err != nil || !tokenRefused[got.Error] || *refreshed {
			return got, err
		}
		*refreshed = true
		if *token, err = o.refresh(ctx, out, kit); err != nil {
			return got, err
		}
	}
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
