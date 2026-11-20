package google

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Gmail API paths of the read calls (gmail.readonly).
const (
	profilePath  = "/gmail/v1/users/me/profile"
	historyPath  = "/gmail/v1/users/me/history"
	messagesPath = "/gmail/v1/users/me/messages"
	// maxRawAnswerBytes bounds a messages.get answer: a 25 MiB message, base64url encoded, and its JSON.
	maxRawAnswerBytes = 48 << 20
)

// LabelInbox is the Gmail label of mail in the inbox; only mail that arrived in it is read.
const LabelInbox = "INBOX"

// CanRead reports whether a grant's scopes let Colliedesk read the mailbox.
func CanRead(scopes []string) bool { return slices.Contains(scopes, ScopeGmailRead) }

// ErrHistoryGone means the history id is older than what Gmail keeps (about a week, sometimes less): the
// caller lists the inbox from a time instead (ListInbox).
var ErrHistoryGone = errors.New("gmail no longer keeps this history id")

// Profile is the mailbox's address and its current history id, where reading starts.
type Profile struct {
	Address   string
	HistoryID string
}

// Profile reads the connected mailbox's profile.
func (c Config) Profile(ctx context.Context, accessToken string) (Profile, error) {
	var answer struct {
		EmailAddress string `json:"emailAddress"`
		HistoryID    string `json:"historyId"`
	}
	if err := c.getJSON(ctx, accessToken, profilePath, nil, maxAnswerBytes, &answer); err != nil {
		return Profile{}, err
	}
	if answer.HistoryID == "" {
		return Profile{}, errors.New("gmail answered a profile without a history id")
	}
	return Profile{Address: strings.ToLower(answer.EmailAddress), HistoryID: answer.HistoryID}, nil
}

// HistoryPage is one page of messages added to the inbox since a history id.
type HistoryPage struct {
	// MessageIDs are Gmail's ids of the messages added to the inbox, oldest first, each once.
	MessageIDs []string
	// NextPageToken asks for the next page; empty on the last one.
	NextPageToken string
	// HistoryID is the mailbox's history id at the time of the answer: the next start once every page is read.
	HistoryID string
}

// InboxHistory lists the messages added to the inbox after startHistoryID. ErrHistoryGone means the id
// is too old to read from.
func (c Config) InboxHistory(ctx context.Context, accessToken, startHistoryID, pageToken string) (HistoryPage, error) {
	query := url.Values{"startHistoryId": {startHistoryID}, "historyTypes": {"messageAdded"}, "labelId": {LabelInbox}, "maxResults": {"100"}}
	if pageToken != "" {
		query.Set("pageToken", pageToken)
	}
	var answer struct {
		History []struct {
			MessagesAdded []struct {
				Message struct {
					ID       string   `json:"id"`
					LabelIDs []string `json:"labelIds"`
				} `json:"message"`
			} `json:"messagesAdded"`
		} `json:"history"`
		NextPageToken string `json:"nextPageToken"`
		HistoryID     string `json:"historyId"`
	}
	err := c.getJSON(ctx, accessToken, historyPath, query, maxAnswerBytes, &answer)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
		return HistoryPage{}, ErrHistoryGone
	}
	if err != nil {
		return HistoryPage{}, err
	}
	page := HistoryPage{NextPageToken: answer.NextPageToken, HistoryID: answer.HistoryID}
	seen := map[string]bool{}
	for _, record := range answer.History {
		for _, added := range record.MessagesAdded {
			id := added.Message.ID
			// labelId=INBOX already keeps only records that touch the inbox. The labels are the message's
			// labels now: one archived since it arrived is still read, a draft or spam never is.
			labels := added.Message.LabelIDs
			if id == "" || seen[id] || slices.Contains(labels, "DRAFT") || slices.Contains(labels, "SPAM") {
				continue
			}
			seen[id] = true
			page.MessageIDs = append(page.MessageIDs, id)
		}
	}
	return page, nil
}

// InboxPage is one page of the inbox's messages received after a time.
type InboxPage struct {
	MessageIDs    []string
	NextPageToken string
}

// ListInbox lists the inbox's messages received after a time, newest first as Gmail answers. It is the way
// back when a history id is gone; Gmail's after: search counts whole seconds.
func (c Config) ListInbox(ctx context.Context, accessToken string, after time.Time, pageToken string) (InboxPage, error) {
	query := url.Values{"labelIds": {LabelInbox}, "q": {"after:" + strconv.FormatInt(after.Unix(), 10)}, "maxResults": {"100"}}
	if pageToken != "" {
		query.Set("pageToken", pageToken)
	}
	var answer struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
		NextPageToken string `json:"nextPageToken"`
	}
	if err := c.getJSON(ctx, accessToken, messagesPath, query, maxAnswerBytes, &answer); err != nil {
		return InboxPage{}, err
	}
	page := InboxPage{NextPageToken: answer.NextPageToken}
	for _, message := range answer.Messages {
		if message.ID != "" {
			page.MessageIDs = append(page.MessageIDs, message.ID)
		}
	}
	return page, nil
}

// RawMessage is one message as it arrived in the mailbox.
type RawMessage struct {
	ID       string
	LabelIDs []string
	// Raw is the RFC 5322 message, Gmail's own Received and Authentication-Results headers on top.
	Raw []byte
	// ReceivedAt is when Gmail received it (internalDate).
	ReceivedAt time.Time
}

// ErrMessageGone means the message was deleted between listing and reading it.
var ErrMessageGone = errors.New("gmail no longer has this message")

// Message reads one message whole (format=raw).
func (c Config) Message(ctx context.Context, accessToken, id string) (RawMessage, error) {
	var answer struct {
		ID           string   `json:"id"`
		LabelIDs     []string `json:"labelIds"`
		Raw          string   `json:"raw"`
		InternalDate string   `json:"internalDate"`
	}
	err := c.getJSON(ctx, accessToken, messagesPath+"/"+url.PathEscape(id), url.Values{"format": {"raw"}}, maxRawAnswerBytes, &answer)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
		return RawMessage{}, ErrMessageGone
	}
	if err != nil {
		return RawMessage{}, err
	}
	raw, err := base64.URLEncoding.DecodeString(answer.Raw)
	if err != nil {
		if raw, err = base64.RawURLEncoding.DecodeString(answer.Raw); err != nil {
			return RawMessage{}, fmt.Errorf("gmail message %s: raw is not base64url", id)
		}
	}
	message := RawMessage{ID: answer.ID, LabelIDs: answer.LabelIDs, Raw: raw}
	if millis, err := strconv.ParseInt(answer.InternalDate, 10, 64); err == nil && millis > 0 {
		message.ReceivedAt = time.UnixMilli(millis).UTC()
	}
	return message, nil
}

// getJSON calls a Gmail API GET and decodes its answer. 401 is ErrUnauthorized; other errors are APIError.
func (c Config) getJSON(ctx context.Context, accessToken, path string, query url.Values, limit int64, into any) error {
	target := c.url(path, defaultGmailURL+path)
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := c.client().Do(request)
	if err != nil {
		return fmt.Errorf("gmail read: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return fmt.Errorf("gmail read: %w", err)
	}
	switch {
	case response.StatusCode == http.StatusUnauthorized:
		return ErrUnauthorized
	case response.StatusCode != http.StatusOK:
		return gmailError(response.StatusCode, body)
	case int64(len(body)) > limit:
		return fmt.Errorf("gmail answer larger than %d bytes", limit)
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("gmail answer: %w", err)
	}
	return nil
}
