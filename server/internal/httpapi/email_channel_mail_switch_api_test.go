package httpapi_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

type mailSwitchView struct {
	MailThroughMailbox      bool   `json:"mail_through_mailbox"`
	AnswersBeforeForwarding string `json:"answers_before_forwarding"`
}

func mailSwitch(t *testing.T, api http.Handler, token, path string) mailSwitchView {
	t.Helper()
	response := request(t, api, http.MethodGet, path, nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", path, response.Code, response.Body)
	}
	var view mailSwitchView
	decode(t, response, &view)
	return view
}

// The inbox says that new customer mail still arrives in the old help desk until mail has come through the
// support mailbox. The email channel answers it with the rule the Reply-To uses: a test message does not
// count, forwarded customer mail does, and another workspace's mail never does.
func TestTheEmailChannelSaysWhetherCustomerMailArrivedThroughTheSupportMailbox(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, platformConfig)
	admin := createSession(t, api, service, "Switch Desk", "admin@switch.example.com")
	secret := connectEmail(t, api, admin, "support@switch.example.com")
	other := createSession(t, api, service, "Other Desk", "admin@other-switch.example.com")
	otherSecret := connectEmail(t, api, other, "support@other-switch.example.com")

	want := mailSwitchView{MailThroughMailbox: false, AnswersBeforeForwarding: "colliedesk"}
	if got := mailSwitch(t, api, admin, "/api/v1/channels/email"); got != want {
		t.Fatalf("nothing arrived yet = %+v, want %+v", got, want)
	}
	if got, sent := sendTestMessage(t, api, admin, "/api/v1/channels/email/test-message"); got.Code != http.StatusCreated || sent.ConversationID == "" {
		t.Fatalf("test message: status=%d body=%s", got.Code, got.Body)
	}
	if got := mailSwitch(t, api, admin, "/api/v1/channels/email"); got != want {
		t.Fatalf("only a test message arrived = %+v, want %+v", got, want)
	}
	if result := inboundOK(t, api, otherSecret, mail([]string{"From: Olive <olive@customer.example>", "To: support@other-switch.example.com",
		"Subject: Elsewhere", "Message-ID: <olive-other@customer.example>"}, "For the other desk.")); result.Status != "created" {
		t.Fatalf("other workspace's mail = %+v, want created", result)
	}
	if got := mailSwitch(t, api, admin, "/api/v1/channels/email"); got != want {
		t.Fatalf("after another workspace's forwarded mail = %+v, want %+v", got, want)
	}

	if result := inboundOK(t, api, secret, mail([]string{"From: Quinn <quinn@customer.example>", "To: support@switch.example.com",
		"Subject: First forwarded", "Message-ID: <quinn-forwarded@customer.example>"}, "Hello there.")); result.Status != "created" {
		t.Fatalf("forwarded mail = %+v, want created", result)
	}
	arrived := mailSwitchView{MailThroughMailbox: true, AnswersBeforeForwarding: "colliedesk"}
	if got := mailSwitch(t, api, admin, "/api/v1/channels/email"); got != arrived {
		t.Fatalf("after forwarded customer mail = %+v, want %+v", got, arrived)
	}
	items, _ := listChannels(t, api, admin)
	if got := mailSwitch(t, api, admin, "/api/v1/channels/"+channelOf(t, items, "email").ID+"/email"); got != arrived {
		t.Fatalf("the channel by its id = %+v, want %+v", got, arrived)
	}
}

// A server that does not receive mail on its sending domain cannot bring the customer's answer back
// before the mailbox forwards here: it says the answer goes to the support mailbox.
func TestAServerWithoutPlatformRepliesSaysAnswersGoToTheSupportMailbox(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.Email.MailFromDomain = "mail.colliedesk.test" })
	admin := createSession(t, api, service, "Plain Desk", "admin@plain.example.com")
	connectEmail(t, api, admin, "support@plain.example.com")
	want := mailSwitchView{MailThroughMailbox: false, AnswersBeforeForwarding: "support_mailbox"}
	if got := mailSwitch(t, api, admin, "/api/v1/channels/email"); got != want {
		t.Fatalf("no platform inbound = %+v, want %+v", got, want)
	}
}

type mailSwitchList struct {
	MailSwitch *struct {
		Imported  bool `json:"imported"`
		Mailboxes []struct {
			ChannelID      string `json:"channel_id"`
			Name           string `json:"name"`
			SupportAddress string `json:"support_address"`
			OldHelpDesk    *bool  `json:"old_help_desk"`
		} `json:"mailboxes"`
	} `json:"mail_switch"`
}

// oldHelpDeskOf reads each listed mailbox of the workspace's mail switch as "address=old_help_desk", in order.
func oldHelpDeskOf(t *testing.T, api http.Handler, token string) string {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/channels/email", nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET email settings status=%d body=%s", response.Code, response.Body)
	}
	var body mailSwitchList
	decode(t, response, &body)
	if body.MailSwitch == nil {
		t.Fatalf("GET email settings has no mail_switch: %s", response.Body)
	}
	parts := []string{}
	for _, mailbox := range body.MailSwitch.Mailboxes {
		if mailbox.OldHelpDesk == nil {
			t.Fatalf("mailbox %s has no old_help_desk: %s", mailbox.SupportAddress, response.Body)
		}
		parts = append(parts, fmt.Sprintf("%s=%t", mailbox.SupportAddress, *mailbox.OldHelpDesk))
	}
	return strings.Join(parts, ", ")
}

// mailToMove reads the workspace's mail switch from an email channel's settings as "imported: address, …".
func mailToMove(t *testing.T, api http.Handler, token, path string) string {
	t.Helper()
	response := request(t, api, http.MethodGet, path, nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", path, response.Code, response.Body)
	}
	var body mailSwitchList
	decode(t, response, &body)
	if body.MailSwitch == nil {
		t.Fatalf("GET %s has no mail_switch: %s", path, response.Body)
	}
	addresses := []string{}
	for _, mailbox := range body.MailSwitch.Mailboxes {
		if mailbox.ChannelID == "" || mailbox.Name == "" {
			t.Fatalf("GET %s: mailbox without its channel id or name: %+v", path, mailbox)
		}
		addresses = append(addresses, mailbox.SupportAddress)
	}
	return fmt.Sprintf("imported=%t: %s", body.MailSwitch.Imported, strings.Join(addresses, ", "))
}

func rotateChannelSecret(t *testing.T, api http.Handler, admin, channelID string) string {
	t.Helper()
	response := request(t, api, http.MethodPost, "/api/v1/channels/"+channelID+"/email/secret", nil, admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("rotate the secret of %s: %d %s", channelID, response.Code, response.Body)
	}
	var rotated struct {
		Secret string `json:"inbound_secret"`
	}
	decode(t, response, &rotated)
	return rotated.Secret
}

// The inbox line names every mailbox of the team whose customer mail does not arrive here yet, not only
// the default one; a switched-off channel is not listed, and the list is the same on every channel's settings.
// Whether the workspace finished an import decides whether the line speaks of the old help desk.
func TestTheMailSwitchNamesEveryMailboxWhoseCustomerMailDoesNotArriveYet(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, platformConfig)
	admin := createSession(t, api, service, "Many Switch", "admin@many-switch.example.com")
	support := connectEmail(t, api, admin, "support@many-switch.example.com")
	billing := addEmailChannel(t, api, admin, "billing@many-switch.example.com")
	sales := addEmailChannel(t, api, admin, "sales@many-switch.example.com")
	if response := request(t, api, http.MethodPatch, "/api/v1/channels/"+sales, map[string]any{"enabled": false}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("switch sales off: %d %s", response.Code, response.Body)
	}
	other := createSession(t, api, service, "Many Switch Other", "admin@many-switch-other.example.com")
	otherSecret := connectEmail(t, api, other, "support@many-switch-other.example.com")
	if result := inboundOK(t, api, otherSecret, mail([]string{"From: Olive <olive@customer.example>", "To: support@many-switch-other.example.com",
		"Subject: Elsewhere", "Message-ID: <olive-many@customer.example>"}, "For the other desk.")); result.Status != "created" {
		t.Fatalf("other workspace's mail = %+v, want created", result)
	}

	want := "imported=false: support@many-switch.example.com, billing@many-switch.example.com"
	if got := mailToMove(t, api, admin, "/api/v1/channels/email"); got != want {
		t.Fatalf("nothing forwarded yet = %q, want %q", got, want)
	}
	if got := mailToMove(t, api, admin, "/api/v1/channels/"+billing+"/email"); got != want {
		t.Fatalf("read on the second channel = %q, want %q", got, want)
	}
	if got, want := mailToMove(t, api, other, "/api/v1/channels/email"), "imported=false: "; got != want {
		t.Fatalf("the other workspace, whose mail arrived = %q, want %q", got, want)
	}

	if result := inboundOK(t, api, support, mail([]string{"From: Quinn <quinn@customer.example>", "To: support@many-switch.example.com",
		"Subject: First forwarded", "Message-ID: <quinn-many@customer.example>"}, "Hello there.")); result.Status != "created" {
		t.Fatalf("forwarded mail = %+v, want created", result)
	}
	insertImport(t, pool, workspaceSlug(t, api, admin), "finished")
	if got, want := mailToMove(t, api, admin, "/api/v1/channels/email"), "imported=true: billing@many-switch.example.com"; got != want {
		t.Fatalf("after support@ forwarded and an import finished = %q, want %q", got, want)
	}

	if result := inboundOK(t, api, rotateChannelSecret(t, api, admin, billing), mail([]string{"From: Rae <rae@customer.example>", "To: billing@many-switch.example.com",
		"Subject: Invoice", "Message-ID: <rae-many@customer.example>"}, "Wrong amount.")); result.Status != "created" {
		t.Fatalf("billing's forwarded mail = %+v, want created", result)
	}
	if got, want := mailToMove(t, api, admin, "/api/v1/channels/email"), "imported=true: "; got != want {
		t.Fatalf("after every mailbox forwarded = %q, want %q", got, want)
	}
}

// A channel still on a hosted forwarding address has no mailbox of the team's own to move. The server
// tells it by the hosted mail domain, so it stays out of the list after a new workspace address name made
// that address differ from the hosted address shown now.
func TestAHostedSupportAddressHasNoMailToMoveAfterTheWorkspaceAddressNameChanges(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, hostedConfig)
	admin := createSession(t, api, service, "Hosted Rename", "admin@hosted-rename.example.com")
	connectEmail(t, api, admin, "support@hosted-rename."+hostedDomain)
	addEmailChannel(t, api, admin, "help@hosted-rename.example.com")
	want := "imported=false: help@hosted-rename.example.com"
	if got := mailToMove(t, api, admin, "/api/v1/channels/email"); got != want {
		t.Fatalf("before the rename = %q, want %q", got, want)
	}
	if code := request(t, api, http.MethodPatch, "/api/v1/workspace/slug", map[string]any{"slug": "renamed-desk"}, admin, nil).Code; code != http.StatusOK {
		t.Fatalf("rename status=%d", code)
	}
	if address := hostedAddressOf(t, api, admin); address == nil || *address != "support@renamed-desk."+hostedDomain {
		t.Fatalf("hosted address after the rename = %v", address)
	}
	if got := mailToMove(t, api, admin, "/api/v1/channels/email"); got != want {
		t.Fatalf("after the rename = %q, want %q", got, want)
	}
}

// Only a mailbox the team had before its import finished still gets its new mail in the old help desk; one
// added after the import never was there, and after the import is undone none is.
func TestOnlyAMailboxAddedBeforeTheImportFinishedIsSaidToReachTheOldHelpDesk(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, platformConfig)
	admin := createSession(t, api, service, "Old Desk", "admin@old-desk.example.com")
	connectEmail(t, api, admin, "support@old-desk.example.com")
	if got, want := oldHelpDeskOf(t, api, admin), "support@old-desk.example.com=false"; got != want {
		t.Fatalf("without an import = %q, want %q", got, want)
	}
	insertImport(t, pool, workspaceSlug(t, api, admin), "finished")
	addEmailChannel(t, api, admin, "billing@old-desk.example.com")
	if got, want := oldHelpDeskOf(t, api, admin), "support@old-desk.example.com=true, billing@old-desk.example.com=false"; got != want {
		t.Fatalf("after the import, billing@ added later = %q, want %q", got, want)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO import_jobs(workspace_id,kind,source,domain,undo_of,state,phase,options,started_at,finished_at)
		SELECT workspace_id,'undo','freshdesk',domain,id,'finished','undo','{}'::jsonb,now(),now() FROM import_jobs WHERE workspace_id=(SELECT id FROM workspaces WHERE slug=$1)`,
		workspaceSlug(t, api, admin)); err != nil {
		t.Fatal(err)
	}
	if got, want := oldHelpDeskOf(t, api, admin), "support@old-desk.example.com=false, billing@old-desk.example.com=false"; got != want {
		t.Fatalf("after the import was undone = %q, want %q", got, want)
	}
}
