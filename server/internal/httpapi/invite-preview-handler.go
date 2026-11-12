package httpapi

import (
	"errors"
	"math"
	"net/http"
	"strconv"

	"github.com/colliedesk/colliedesk/server/internal/auth"
)

// refusedLinksPerAddressPerMinute bounds the tokens no link has that one client address may try, redeem
// and invite preview together. A real link never counts (one that works, or one used, expired, revoked or
// of another kind), so a person who opens their own links is never stopped; someone guessing tokens is.
const refusedLinksPerAddressPerMinute = 30

const invalidLinkMessage = "This link is invalid, expired, revoked, or already used."

// previewInvite describes an invite link without using it: the invite page shows the workspace, the
// inviter and the roles before the person joins, and says why a link stopped working.
func (a *API) previewInvite(response http.ResponseWriter, request *http.Request) {
	var input struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(request, &input); err != nil || input.Token == "" {
		writeError(response, http.StatusBadRequest, "invalid_request", "A one-time token is required.")
		return
	}
	if a.tooManyRefusedLinks(response, request) {
		return
	}
	preview, err := a.auth.PreviewInvite(request.Context(), input.Token)
	if errors.Is(err, auth.ErrNotAnInvite) || errors.Is(err, auth.ErrUnknownLink) {
		a.refuseLink(response, request, err)
		return
	}
	if err != nil {
		writeStoreError(response, err)
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	writeJSON(response, http.StatusOK, preview)
}

// tooManyRefusedLinks answers 429 when the client address already tried refusedLinksPerAddressPerMinute
// tokens no link has in the last minute.
func (a *API) tooManyRefusedLinks(response http.ResponseWriter, request *http.Request) bool {
	full, wait := a.refusedLinks.full(clientAddress(request, a.config.TrustedProxies), a.now())
	if !full {
		return false
	}
	response.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
	writeError(response, http.StatusTooManyRequests, "rate_limited", "Too many links that do not exist were tried from this network. Try again in a minute.")
	return true
}

// refuseLink answers a link that does not work, the same for every reason; a token no link has counts
// toward the address's limit.
func (a *API) refuseLink(response http.ResponseWriter, request *http.Request, err error) {
	if errors.Is(err, auth.ErrUnknownLink) {
		a.refusedLinks.allow(clientAddress(request, a.config.TrustedProxies), a.now())
	}
	writeError(response, http.StatusUnauthorized, "invalid_link", invalidLinkMessage)
}
