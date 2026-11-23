package httpapi

import (
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// helpCenterSearchPrefix starts a word of an inbox search that keeps the conversations of one help
// center: help_center:<handle>, or help_center:primary for the primary.
const helpCenterSearchPrefix = "help_center:"

// takeHelpCenterWords takes each help_center:<handle> word out of an inbox search, in any case, and
// returns the other words and the handles in lowercase. A word with no handle after the colon stays
// a word to match; one whose handle cannot be a handle is an error, so a typo never reads as an
// empty inbox. A handle no help center has matches nothing, as an unknown channel id does.
func takeHelpCenterWords(search string) (string, []string, error) {
	if !strings.Contains(strings.ToLower(search), helpCenterSearchPrefix) {
		return search, nil, nil
	}
	var words, handles []string
	for _, word := range strings.Fields(search) {
		if len(word) <= len(helpCenterSearchPrefix) || !strings.EqualFold(word[:len(helpCenterSearchPrefix)], helpCenterSearchPrefix) {
			words = append(words, word)
			continue
		}
		handle := strings.ToLower(word[len(helpCenterSearchPrefix):])
		if !domain.IsHelpCenterHandle(handle) {
			return "", nil, filterError("help_center: takes a help center's handle, such as help_center:hardware, or help_center:primary for the primary help center.")
		}
		handles = append(handles, handle)
	}
	return strings.Join(words, " "), handles, nil
}
