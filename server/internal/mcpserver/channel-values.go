package mcpserver

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/colliedesk/colliedesk/server/internal/channels/registry"
	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// The values a tool's schema lists for a channel come from the channel registry, so a new type is in
// every tool without editing them: channelTypeEnum for a channel's type, conversationChannelEnum (the
// types and the origins that come through no channel) for how a conversation started.
var (
	channelTypeValues        = registeredTypes()
	conversationChannelCount = len(channelTypeValues) + len(domain.ConversationOrigins)
	channelTypeEnum          = enumJSON(channelTypeValues)
	conversationChannelEnum  = enumJSON(append(append([]string(nil), channelTypeValues...), domain.ConversationOrigins...))
)

func registeredTypes() []string {
	var values []string
	for _, kind := range registry.New(registry.Config{}).Types() {
		values = append(values, string(kind))
	}
	return values
}

// enumJSON writes the values as a JSON array. They are machine names (channel_types and the origins
// allow only [a-z0-9_]), checked here, so each is its own JSON string without escaping.
func enumJSON(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		if !machineName.MatchString(value) {
			panic("mcpserver: channel value " + strconv.Quote(value) + " is not a machine name")
		}
		quoted[i] = `"` + value + `"`
	}
	return "[" + strings.Join(quoted, ",") + "]"
}

var machineName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
