package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
	"github.com/colliedesk/colliedesk/server/internal/email"
)

// maxCC is the most addresses one reply can copy.
const maxCC = 10

func (a *API) invalidCC(message string) *uploadError {
	return &uploadError{http.StatusBadRequest, "invalid_request", message}
}

// cleanCC validates the Cc list of a new message and returns it lowercased and without duplicates.
// Only a reply is copied, at most maxCC plain addresses, none of them ours: copying our own support or
// forwarding address would loop the reply back into the workspace. The contact is dropped later, in the
// store, where the conversation is read.
func (a *API) cleanCC(ctx context.Context, workspaceID, kind string, raw []string) ([]string, *uploadError) {
	if len(raw) == 0 {
		return nil, nil
	}
	switch {
	case kind != "outgoing":
		return nil, a.invalidCC("Only a reply can be copied to other addresses.")
	case len(raw) > maxCC:
		return nil, a.invalidCC("A reply can be copied to at most 10 addresses.")
	}
	channel, err := a.store.GetEmailChannel(ctx, workspaceID)
	if err != nil {
		return nil, &uploadError{http.StatusInternalServerError, "internal_error", "The reply could not be checked. Nothing was sent."}
	}
	mailbox, err := a.store.GetMailboxConnection(ctx, workspaceID)
	if err != nil {
		return nil, &uploadError{http.StatusInternalServerError, "internal_error", "The reply could not be checked. Nothing was sent."}
	}
	seen := map[string]bool{}
	clean := make([]string, 0, len(raw))
	for _, value := range raw {
		if !plainAddress(value) {
			return nil, a.invalidCC("Every copied address must be a plain email address such as name@example.com.")
		}
		address := strings.ToLower(value)
		if a.isOwnAddress(address, channel) {
			return nil, a.invalidCC("A reply cannot be copied to this workspace's own support or forwarding address.")
		}
		if mailbox != nil && email.SameMailbox(address, mailbox.Address) {
			return nil, a.invalidCC("A reply cannot be copied to the Gmail address it is sent from.")
		}
		if !seen[address] {
			seen[address] = true
			clean = append(clean, address)
		}
	}
	return clean, nil
}

// isOwnAddress reports whether mail to the address would arrive back in a workspace: the support
// address or a plus form of it, or anything on the hosted forwarding or platform sending domains.
func (a *API) isOwnAddress(address string, channel *domain.EmailChannel) bool {
	if channel != nil && email.SameMailbox(address, channel.SupportAddress) {
		return true
	}
	config := a.config.Email
	return email.UnderDomain(address, config.HostedMailDomain) || email.UnderDomain(address, config.MailFromDomain)
}
