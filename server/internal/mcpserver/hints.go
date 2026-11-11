package mcpserver

import "github.com/colliedesk/colliedesk/server/internal/httpapi"

// ReadOnly says the tool changes nothing: every route it calls only reads (a tool with no route, such
// as the docs search, reads the server's own text).
func (s Spec) ReadOnly() bool { return s.effect() == httpapi.Reads }

// Destructive says the tool's effect cannot be undone now: a route it calls reaches a customer,
// deletes for good, or revokes. A delete that waits in a trash is not destructive; its description
// says when it becomes permanent.
func (s Spec) Destructive() bool { return s.effect() == httpapi.Final }

// effect is the strongest effect of the tool's routes. A route an agent token can only ask for (an
// operator request someone approves) changes nothing but the list of requests, whatever it does once
// approved.
func (s Spec) effect() httpapi.Effect {
	strongest := httpapi.Reads
	for _, key := range s.REST {
		effect := httpapi.RouteEffect(key)
		if effect == httpapi.ByBody {
			effect = s.BodyEffect
		}
		if routeAccess[key].Approval && effect == httpapi.Final {
			effect = httpapi.Changes
		}
		if effect == httpapi.EffectUnknown {
			// Unclassified: the gate tests name the route; until then never claim it is harmless.
			effect = httpapi.Final
		}
		strongest = max(strongest, effect)
	}
	return strongest
}
