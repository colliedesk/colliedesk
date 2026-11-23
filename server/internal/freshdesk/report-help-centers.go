package freshdesk

import (
	"fmt"

	"github.com/colliedesk/colliedesk/server/internal/domain"
)

// helpCenterLines says, for an account with several portals, which help center each portal became.
// An account with one portal keeps its report as it was: everything is on the primary help center.
func helpCenterLines(portals []KBPortalHelpCenter) []string {
	if len(portals) < 2 {
		return nil
	}
	lines := make([]string, 0, len(portals))
	for _, portal := range portals {
		lines = append(lines, fmt.Sprintf("%s (Freshdesk portal %d, %d categories): %s", portal.PortalName, portal.PortalID, portal.Categories, portalOutcome(portal)))
	}
	return lines
}

func portalOutcome(portal KBPortalHelpCenter) string {
	switch portal.Status {
	case portalPrimary:
		return "on the primary help center, " + portal.HelpCenterName
	case portalCreated:
		return fmt.Sprintf("made the help center %s at /%s", portal.HelpCenterName, portal.Handle)
	case portalExisting:
		return fmt.Sprintf("on the help center %s at /%s, made by an earlier run", portal.HelpCenterName, portal.Handle)
	case portalPlanned:
		return "gets a help center of its own"
	case portalOverLimit:
		return fmt.Sprintf("on the primary help center: a workspace has at most %d help centers", domain.MaxHelpCenters)
	case portalTrashed:
		return "on the primary help center: the help center an earlier import made for it was deleted in Colliedesk"
	default:
		return "on the primary help center: its help center could not be made (see Failures)"
	}
}
