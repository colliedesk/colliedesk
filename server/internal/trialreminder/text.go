package trialreminder

import (
	"fmt"
	"time"
)

// text is the subject and body of a reminder. The end is written in the workspace's timezone, with its name.
func text(kind, workspace string, trialEnd time.Time, timezone, billingURL string) (string, string) {
	loc := location(timezone)
	local := trialEnd.In(loc)
	day, clock, zone := local.Format("Monday, January 2, 2006"), local.Format("3:04 PM"), loc.String()
	var subject, opening string
	if kind == kindEndDay {
		subject = fmt.Sprintf("Your Colliedesk trial for %s ends today", workspace)
		opening = fmt.Sprintf("The free trial of your Colliedesk workspace \"%s\" ends today, %s, at %s (%s).", workspace, day, clock, zone)
	} else {
		subject = fmt.Sprintf("Your Colliedesk trial for %s ends on %s", workspace, local.Format("January 2"))
		opening = fmt.Sprintf("The free trial of your Colliedesk workspace \"%s\" ends on %s at %s (%s).", workspace, day, clock, zone)
	}
	body := fmt.Sprintf(`Hi,

%s

What happens then:
- The workspace locks.
- Nothing is deleted.
- You can still export all your data.

Choose a plan to keep the workspace open:
%s

Questions? Reply to this email.

Chinh, Colliedesk founder

You get this email because you are an admin of the workspace "%s".
`, opening, billingURL, workspace)
	return subject, body
}
