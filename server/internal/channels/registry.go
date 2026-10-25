package channels

import (
	"errors"
	"fmt"
)

// Errors of the frame, the same for every type.
var (
	ErrUnknownType     = errors.New("unknown channel type")
	ErrNotFound        = errors.New("channel not found")
	ErrChannelOff      = errors.New("the channel is off")
	// ErrChannelCannotSend means a conversation cannot start on the channel: its type sends no replies of its own.
	ErrChannelCannotSend = errors.New("a conversation cannot start on this channel")
	ErrExternalAccount = errors.New("that account is connected to another channel")
	// ErrConflict means another change of the same channels won a race; trying again works.
	ErrConflict = errors.New("another change of these channels was made at the same moment")
	// ErrNoRegistry means a store was asked to route a reply without the channel registry.
	ErrNoRegistry = errors.New("the channel registry is not configured")
)

// NameTakenError refuses a name a live channel of the same type has, naming that channel.
type NameTakenError struct{ ChannelID, ChannelName string }

func (e *NameTakenError) Error() string {
	return "the channel " + e.ChannelName + " already has this name"
}

// HeldError refuses to remove a channel that still holds messages that arrived while it was off:
// removed, they would be out of reach in the app and still count toward storage.
type HeldError struct{ Count int }

func (e *HeldError) Error() string {
	noun := "messages"
	if e.Count == 1 {
		noun = "message"
	}
	return fmt.Sprintf("the channel holds %d %s that arrived while it was off", e.Count, noun)
}

// Registry is the set of channel types this server runs, in the order the web app lists them.
type Registry struct {
	order    []Type
	adapters map[Type]Adapter
}

// NewRegistry registers adapters in order. A type registered twice, or a definition without a
// type, is a programming error.
func NewRegistry(adapters ...Adapter) (*Registry, error) {
	r := &Registry{adapters: map[Type]Adapter{}}
	for _, adapter := range adapters {
		definition := adapter.Definition()
		if definition.Type == "" {
			return nil, errors.New("channel adapter without a type")
		}
		if _, ok := r.adapters[definition.Type]; ok {
			return nil, fmt.Errorf("channel type %s registered twice", definition.Type)
		}
		if definition.WhenOff != OffHold && definition.WhenOff != OffRefuse {
			return nil, fmt.Errorf("channel type %s: WhenOff must be %q or %q", definition.Type, OffHold, OffRefuse)
		}
		if adapter.Outbound() == nil && definition.RepliesVia == "" {
			return nil, fmt.Errorf("channel type %s sends nothing and names no type that sends its replies", definition.Type)
		}
		r.order = append(r.order, definition.Type)
		r.adapters[definition.Type] = adapter
	}
	for _, t := range r.order {
		if via := r.adapters[t].Definition().RepliesVia; via != "" {
			if carrier, ok := r.adapters[via]; !ok || carrier.Outbound() == nil {
				return nil, fmt.Errorf("channel type %s replies via %s, which is not registered or sends nothing", t, via)
			}
		}
	}
	return r, nil
}

// Types lists the registered types in order.
func (r *Registry) Types() []Type { return append([]Type(nil), r.order...) }

// Adapter returns the adapter of a type.
func (r *Registry) Adapter(t Type) (Adapter, bool) {
	if r == nil {
		return nil, false
	}
	adapter, ok := r.adapters[t]
	return adapter, ok
}

// Definitions lists every type's definition in order.
func (r *Registry) Definitions() []Definition {
	definitions := make([]Definition, 0, len(r.order))
	for _, t := range r.order {
		definitions = append(definitions, r.adapters[t].Definition())
	}
	return definitions
}

// ReplyType is the type whose channel carries a reply on a conversation that came through origin:
// origin itself when it sends, the type it names in RepliesVia, or, for an origin that is no
// registered type (an agent's conversation, an import), the type that carries other replies.
func (r *Registry) ReplyType(origin Type) (Type, bool) {
	if r == nil {
		return "", false
	}
	if adapter, ok := r.adapters[origin]; ok {
		definition := adapter.Definition()
		if adapter.Outbound() != nil {
			return origin, true
		}
		return definition.RepliesVia, true
	}
	for _, t := range r.order {
		adapter := r.adapters[t]
		if adapter.Definition().CarriesOtherReplies && adapter.Outbound() != nil {
			return t, true
		}
	}
	return "", false
}
