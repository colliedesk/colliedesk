package trialreminder

import (
	"fmt"
	"time"
)

// moreTime is said to a team that signed up to leave its helpdesk and has not answered a real ticket yet:
// the switch offer lets it pay after that first ticket, so the trial is extended on request.
const moreTime = "Still moving from Freshdesk, and not answering real tickets here yet? Reply to this email and I extend your trial. You pay after your first real ticket, not before.\n\n"

// text is the subject and body of a reminder. The end is written in the reader's timezone, with its name.
func text(kind, workspace string, trialEnd time.Time, timezone, billingURL string, stillMoving bool) (string, string) {
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
	offer := ""
	if stillMoving {
		offer = moreTime
	}
	body := fmt.Sprintf(`Hi,

%s

What happens then:
- The workspace locks.
- Nothing is deleted.
- You can still export all your data.

%sChoose a plan to keep the workspace open:
%s

Questions? Reply to this email.

Chinh, Colliedesk founder

You get this email because you are an admin of the workspace "%s".
`, opening, offer, billingURL, workspace)
	return subject, body
}
