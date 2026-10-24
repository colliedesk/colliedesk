package mcpserver

import (
	"encoding/json"

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

func enumJSON(values []string) string {
	encoded, err := json.Marshal(values)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
