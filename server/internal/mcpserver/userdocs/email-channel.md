# Email channel

Customers write to your support address. Each new email thread becomes a conversation, and the
sender becomes a contact. Customer replies join the same conversation. Your replies go back to
the customer by email. Private notes are never emailed.

On Colliedesk Cloud every workspace has a ready-made address; you only forward your mailbox to it.
On your own server, the person who runs it first connects a mail forwarder and an SMTP relay: see
[configuring email](../self-host/email.md).

## Email in Settings

Open **Settings** and select **Email** under **Channels**. The **Connect your support email** link of
the empty inbox opens the same page.

- **No support address yet:** the page is the setup, four steps on one page (see
  [Connect your support email](#connect-your-support-email)). A new Colliedesk Cloud workspace starts
  here too: its only address is still its forwarding address.
- **One address or more:** the page is a table of your support addresses. Each row shows the
  **Support address** (and **Default** when there are several), what it **Receives by** (its
  forwarding address, or **Inbound URL** on your own server), what it **Replies through** (**Gmail**,
  **Microsoft 365**, **Your domain**, **Colliedesk** or **—**) and its **Status**:
  - **Working**: mail arrived on it, and it sends.
  - **Waiting for first email**: it is set up, but nothing has arrived yet. Its row offers
    **Continue setup**, which opens its setup at the first step still to do.
  - **Off** or **Needs attention**: see [channels](channels.md#the-list).

  **Manage** opens the address's own page: its name, **Make default**, the on/off switch, messages
  held while it was off, its **Connection**, **Replies** and **History** tabs, and **Disconnect
  email**. **Add another address** starts the setup of a second mailbox
  ([several mailboxes](#several-mailboxes)).

Below the table, folded, are the settings of the default address that people rarely change:
**Sender name**, **Your domain** (verified or not), **Send a test message**, and on Colliedesk Cloud
**Change forwarding address** (the **Workspace address name**). To disconnect an address, select
**Manage** on its row, then **Disconnect email**.

Old links keep working: `/settings/channels/<id>` opens the address's page, and a link with a tab
(`/settings/channels/type/email?tab=replies`) opens that tab of the default address.

## Connect your support email

The setup is one page with four steps. The step you are on is open and says **Step N of 4**; the steps
after it wait their turn. A step that is done shows what it holds (the address, the time the first
email arrived, how replies leave) and opens again with **Change**; a skipped step opens again with
**Set up**. Leaving the page loses nothing: every step reads its state from the server, and the
provider you picked, the steps you skipped and your test message are remembered in this browser. Only
admins change the steps; teammates see them. Beside the steps, **Customers see** shows the **To:**
address customers write to and the **Replies from:** line of your replies, from the current settings.

### 1. Your support address

Enter the address customers write to, for example `support@example.com`, and select **Save support
address**. The button reads "Saving…" until the save ends. The setup then goes on with step 2 of that
address (its address is `/settings/channels/<id>/setup`).

### 2. Forward its mail to Colliedesk

On Colliedesk Cloud the step starts with the **Forward to this address** field, your forwarding
address `support@<workspace>.colliedesk.com`; select **Copy**. Then pick your mail provider:

- **Gmail** and **Google Workspace:** Settings → See all settings → Forwarding and POP/IMAP → **Add a
  forwarding address**, paste the address, **Next** → **Proceed**. Google sends a confirmation email to
  Colliedesk. The step shows it under **Gmail's confirmation email** within a few seconds: its
  **Confirmation code**, **Open confirm link** (opens Google in a new tab) and **Open its conversation**
  (the email also opens a conversation in your inbox, as any mail does). Back in Gmail, choose **Forward a copy of incoming mail** and **Save Changes**. **Open Gmail** opens the
  forwarding settings in a new tab. A Google Workspace admin may have turned automatic forwarding off;
  the tab says where they allow it.
- **Microsoft 365:** Outlook on the web → Settings → Mail → Forwarding, turn on forwarding, paste the
  address, keep a copy and **Save**. Forwarding outside your organization is often blocked: the sender
  gets a bounce with `5.7.520 Access denied, Your organization does not allow external forwarding`.
  Your Microsoft 365 admin allows it for the support mailbox only:
  1. Open the [Microsoft Defender portal anti-spam policies](https://security.microsoft.com/antispam)
     (role Security Administrator or Organization Management).
  2. Select **Create policy** → **Outbound** and name it, for example `Colliedesk forwarding`.
  3. Under **Users, groups, and domains**, add only the support mailbox.
  4. Under **Protection settings**, set **Automatic forwarding rules** to **On - Forwarding is
     enabled**, then select **Create**.
  5. If mail still bounces, a remote domain setting or a mail flow rule of the organization may also
     block forwarding, and a block wins; your admin checks those too.
- **Other / own server:** forward every message for the support address to the forwarding address,
  keeping a copy. On your own server this tab is where mail comes in: see
  [On your own server: the inbound URL](#on-your-own-server-the-inbound-url).

The confirm link is offered only when the email passed the sender check of the receiving mail server
and the link is on Google's own address (`mail.google.com` or `mail-settings.google.com`): anyone can
send mail that claims to come from Google. Otherwise only the code shows; type it in Gmail instead. On
your own server the sender check needs `EMAIL_TRUSTED_AUTHSERV_ID` ([configuring email](../self-host/email.md)).
Agents read the same with `GET /api/v1/channels/{id}/email/forwarding-confirmation` or the MCP tool
`colliedesk_email_forwarding_confirmation_get`; it answers for the last 24 hours.

The step then reads **Waiting for first email**. Send a message to your support address from another
address. When it arrives the step reads **First email received** with its time, without reloading the
page (the inbox checks for new mail every 20 seconds). **Continue** unlocks only then. **Skip for now**
is always there: the address stays **Waiting for first email** in the table until mail arrives.
Mail from the support address itself is never turned into a conversation, so test from a different
address.

Forwarded mail keeps its original sender, so the conversation is with your customer, not with your
mailbox.

**Change the forwarding address** (folded under the step, Colliedesk Cloud only) holds the
**Workspace address name**: the part before `.colliedesk.com`. The old address stops receiving mail
at once, so change the forwarding in your mailbox at the same time. A name is 1 to 63 lowercase
letters, digits or hyphens; names such as `app`, `www` and `mail` are reserved. Do not set the Support
address to a `colliedesk.com` address of another workspace.

### 3. Send replies from your address

Pick how replies leave. The step first says how they leave now ("Replies are sent from …"). The choice
that fits the provider tab you picked in step 2 is marked **Recommended**: Gmail and Google Workspace
recommend **Gmail**, Microsoft 365 recommends **Microsoft 365**, Other recommends **Colliedesk sends**.
All three stay visible.

- **Gmail** and **Microsoft 365:** one sign-in at the provider (**Connect Gmail**, **Connect Microsoft
  365**). Customers see your own address, and the mailbox keeps the replies in its Sent folder. See
  [send replies from Gmail](#send-replies-from-gmail) and
  [Microsoft 365](#send-replies-from-microsoft-365). The provider brings you back to the setup.
- **Colliedesk sends:** publish the DNS records shown, then **Verify domain**; each record shows its
  status. See [send from your own domain](#send-from-your-own-domain).

The step is done when a mailbox is connected or replies already leave from your support address.
**Skip for now** names the address replies go from meanwhile (the `via Colliedesk` address on
Colliedesk Cloud).

### 4. Try it with a test message

Select **Send yourself a test message** (see [below](#send-yourself-a-test-message)). The test
conversation opens; answer it. The step is done when your reply is delivered: back in the setup it
reads **Reply delivered**, and the page says **Email is set up**.

## Send yourself a test message

The quickest way to see the whole loop needs no forwarder and no terminal. Select **Send yourself a
test message**: on the empty inbox, in step 4 of the setup, or under **Send a test message** on
**Settings → Email**. Colliedesk delivers one email from your own address to the channel, the way a
customer's email arrives. It opens a conversation, **Test message from Colliedesk**, and opens it, from
any of those places. The test works before forwarding is set up, so it shows that your inbox and
replies work; customer email still needs your support mailbox to forward here. Before a support
address is saved, the button says to save one first. Pressing it several times quickly sends one test
message.

Answer it like any customer. The reply is emailed to your own address, so you see what customers
receive; on Colliedesk Cloud it arrives in your real mailbox. During the trial this reply is allowed,
because it reaches only you: its only recipient is you, an agent of the workspace who signed in with a
verified address (a Colliedesk Cloud sign-in, or a sign-in link from whoever runs the server). A Cc to
anyone else is still refused, and the reply counts toward the trial's daily limit. An agent who joined
only with an invite link has no verified address yet, so a reply to their test message waits until
they sign in that way.

A workspace can send itself five test messages an hour. Agent tokens do the same with the MCP tool
`colliedesk_email_test_message_send`.

## On your own server: the inbound URL

A server without a hosted mail domain has no forwarding address. Mail enters at the **Inbound URL**, a
web address, not an email address; step 2 opens on **Other / own server** with it.

1. In step 2, select **Create inbound secret**. Copy the secret now: it is shown once. The secret is a
   password for this only, not your email password.
2. Give the inbound URL and the secret to the person who runs the server. Your mailbox cannot send to
   the URL by itself: they connect a forwarder (a Cloudflare Email Worker, Postmark, Amazon SES or a
   pipe on your mail server) as described in
   [forward mail to Colliedesk](../self-host/email.md#3-forward-mail-to-colliedesk).
3. To see a conversation arrive before the forwarder is ready, select **Send yourself a test
   message** (see [above](#send-yourself-a-test-message)). Whoever runs the server can also check the
   URL itself: open **Test from a terminal instead**, copy the **Test request** (**Copy test request**)
   and run it in a terminal. It posts one message from `customer@example.com`, which opens a
   conversation in the inbox, and step 2 reads **First email received**.

The secret is replaced later on the address's **Connection** tab (**Manage**).

On Colliedesk Cloud, mail posted to the inbound URL does not count as a customer who wrote first
during the trial, because whoever holds the secret can write any sender; see
[Plan and billing](billing.md#the-free-trial).

## Sender name

The **Sender name** is the name customers see in the From line of every reply, for example
**"Acme Support" <support@example.com>**. It belongs to the email channel, not to the workspace:
renaming the workspace does not change it.

A new channel starts with the workspace name. To change it:

1. Open **Settings → Email** and unfold **Sender name** (or the address's **Replies** tab under
   **Manage**). The field appears once a support address is saved.
2. Enter the name in **Sender name** and select **Save sender name**.

The next reply uses the new name, on every way a reply leaves: your own domain, the
`via Colliedesk` address and Gmail. A name is 1 to 100 characters. Spaces at the ends are removed.
Line breaks and other control characters are refused. On Colliedesk Cloud a name with a well-known
brand in it (for example "PayPal Support") is refused, as at sign-up. If it is your own brand, contact
us: the operator sets the name for you with `collie email sender-name`
([self-hosting guide](../self-host/email.md#set-a-sender-name-with-a-brand-in-it)). Only admins can
change it; other teammates see it.

Saving the sender name changes only the name. The support address, its domain verification and the
sending domain stay as they are.

Through the API, send `PUT /api/v1/channels/email` with `{"sender_name": "Acme Support"}` alone. The
address stays. Send `support_address` too only to change the address. An AI agent uses
`colliedesk_email_channel_update` the same way.

## Send replies from Gmail

If your support address is a Gmail or Google Workspace mailbox, an admin can connect it. Replies are
then sent through Gmail from that address, and a copy of each reply stays in the mailbox's **Sent**
folder. Incoming mail still arrives by forwarding, as set up above. Colliedesk asks Google only for
permission to send email and to see the account's address; it cannot read the mailbox.

On Colliedesk Cloud, next to **Connect Gmail** an admin also sees that Colliedesk's use of data from
Google APIs adheres to the Google API Services User Data Policy, including the Limited Use
requirements, with a link to the [privacy policy](https://colliedesk.com/privacy).
A self-hosted server shows no such link: it sends through your own Google OAuth client (see below).

1. Connect the email channel first (a saved Support address).
2. In step 3 of the setup select **Gmail**, then **Connect Gmail** (or open the address's **Replies**
   tab under **Manage** → **Send from your mailbox**).
3. Google asks you to choose the account and allow **Send email on your behalf**. Leave that box
   ticked. Google then brings you back to the setup (or the Replies tab you started from), which says
   whether Gmail is connected and, if not, why.
4. **Send from your mailbox** now shows the account. Replies, including copies (Cc), attached files and
   **Retry delivery**, go out through it with the same threading headers, so customer answers join
   the same conversation.

Connect the account of your support address: a channel sends as its own address. Google bringing back
another account (for example `acme.help@gmail.com` while customers write to `support@example.com`)
connects nothing, and Settings says to sign in with the support address. An account another email
channel of the workspace already sends through is refused too, with a link to that channel. A
connection made before this rule with another account keeps sending; **Send from your mailbox** warns that
it does not match and suggests connecting the right one.

**When Gmail stops working.** If the access is removed in the Google account, the password changes,
or Google refuses to send, **Send from your mailbox** turns into an alert with the reason. Replies are then
marked **Delivery failed** with the same reason. They are never sent another way without you knowing.
Select **Reconnect Gmail**, then **Retry delivery** on the failed replies.

**When the server turns Gmail off.** The operator of the server can switch Gmail (or Microsoft 365) off
for every workspace. The connection stays; **Send from your mailbox** says sending through it is turned
off, and replies wait in the outbox with that reason instead of failing. They are sent once it is on
again. You can still disconnect. Without a connection, the provider is not offered (on your own server an
admin reads that it is not turned on).

**Disconnect Gmail** asks first, then removes Colliedesk's access at Google. Replies go out the way
they did before Gmail was connected (the server's SMTP relay). A reply that was still waiting for Gmail
fails with a note and can be retried. **Use another Gmail account** replaces the connected account.
Disconnecting the whole email channel also disconnects Gmail.

Agents (AI) cannot connect or disconnect Gmail themselves: `colliedesk_gmail_connect_request` and
`colliedesk_gmail_disconnect_request` file a request, and the admin who approves it gets Google's
page. On Colliedesk Cloud the request card shows the same note on Google data first. See
[Agent requests](agent-requests.md).

### Self-hosted

On a server you run, Gmail sending needs your own Google OAuth client. See
[send replies through Gmail](../self-host/email.md#send-replies-through-gmail).

## Send replies from Microsoft 365

If your support address is a Microsoft 365 mailbox (a work or school account) or an Outlook.com mailbox
(a personal Microsoft account), an admin can connect it. Replies are then sent through Microsoft from
that address, and a copy of each reply stays in the mailbox's **Sent Items**. Incoming mail still
arrives by forwarding, as set up above. Colliedesk asks Microsoft only for permission to send email and
to read the account's address (`Mail.Send`, `User.Read`, `offline_access`, `openid`); it cannot read
the mailbox.

1. Connect the email channel first (a saved Support address).
2. In step 3 of the setup select **Microsoft 365**, then **Connect Microsoft 365** (or open the
   address's **Replies** tab under **Manage** → **Send from your mailbox**).
3. Microsoft asks you to sign in. Choose the mailbox of your support address and select **Accept**.
   Microsoft then brings you back to the setup (or the Replies tab you started from), which says
   whether Microsoft 365 is connected and, if not, why.
4. **Send from your mailbox** now shows the mailbox. Replies, including copies (Cc), attached files and
   **Retry delivery**, go out through it with the same threading headers, so customer answers join the
   same conversation.

**Admin approval.** Many organizations only let an admin approve apps that send email. If Microsoft says
you need admin approval, open **Work account? Your IT admin may need to approve Colliedesk first** under
**Connect Microsoft 365**, select **Copy link** and send the link to your Microsoft 365 admin. After they
approve, select **Connect Microsoft 365** again. Microsoft may keep you on its "Need admin approval"
page instead of bringing you back to Colliedesk; come back to Settings yourself once your admin
approved.

Connect the mailbox of your support address: a channel sends as its own address. Microsoft signing in
another mailbox connects nothing, and Settings says which mailbox to choose. If your support address is
an alias or a shared mailbox in Microsoft 365, it cannot be connected yet. A Microsoft account created
with another provider's address (for example a Gmail address) has no Outlook mailbox to send from and is
refused too. An account another email channel of the workspace already sends through is refused, with a
link to that channel.

**When Microsoft 365 stops working.** If access is removed in the Microsoft account, the password is
reset, a sign-in policy of your organization now blocks Colliedesk, or the mailbox loses its license,
**Send from your mailbox** turns into an alert with the reason. Replies are then marked **Delivery
failed** with the same reason; they are never sent another way without you knowing. Select **Reconnect
Microsoft 365**, then **Retry delivery** on the failed replies. If the server's own Microsoft settings
stop working (its client secret expired), the connection stays, replies wait with a note for the
administrator, and reconnecting does not help.

**Disconnect Microsoft 365** asks first, then deletes Colliedesk's access to the mailbox. Microsoft has no
way for Colliedesk to remove itself from your account, so Colliedesk stays listed among the account's
apps until you remove it there: at [My Apps](https://myapps.microsoft.com) for a work or school account,
or at [Microsoft account permissions](https://account.live.com/consent/Manage) for a personal account.
Replies then go out the way they did before Microsoft 365 was connected; a reply that was still waiting
for Microsoft 365 fails with a note and can be retried.

A channel sends through one mailbox. Selecting **Use Gmail instead** or **Use Microsoft 365 instead**
asks first, then replaces the connection; replies still waiting for the old one fail and can be
retried.

**Large attachments.** Microsoft may refuse a reply with large files. When the files of a reply are
larger than 3 MB and it goes through Microsoft 365, the reply box warns you. If Microsoft refuses the
reply, it is marked **Delivery failed** with the reason and keeps its files: remove a file and retry.

Agents (AI) cannot connect or disconnect Microsoft 365 themselves: `colliedesk_microsoft_connect_request`
and `colliedesk_microsoft_disconnect_request` file a request, and the admin who approves it gets
Microsoft's sign-in page. See [Agent requests](agent-requests.md).

On a server you run, Microsoft 365 sending needs your own app registration in Microsoft Entra. See
[send replies through Microsoft 365](../self-host/email.md#send-replies-through-microsoft-365).

## Several mailboxes

A workspace can have several email channels, one per support address: `support@example.com` and
`billing@example.com`, or a second domain. Add one with **Add another address** on **Settings → Email** (or **Settings → Channels → Add channel →
Connect your support email**): step 1 asks a **Name** (left empty, it is called by its address) and its
**Support address**, and the setup goes on for that address. Two channels never share a mailbox: `support+billing@example.com` is the same mailbox as
`support@example.com`, so it is refused, with a link to the channel that has it.

Each mailbox has its own settings, on its own page (**Manage**):

- **Forwarding.** On Colliedesk Cloud each mailbox has its own forwarding address: the first one is
  `support@<workspace>.colliedesk.com`, a later one is named after its support address
  (`billing@<workspace>.colliedesk.com`, with `-2` added when that is taken). Forward each mailbox to
  its own forwarding address. On your own server each mailbox has its own inbound secret: give each
  forwarder the secret of its mailbox. Mail that reaches a mailbox opens its conversation in that
  channel.
- **Sender name, Gmail and sending pause** belong to the mailbox.
- **Domain verification belongs to the domain.** Two mailboxes on `example.com` share it: verify
  once, and a later mailbox on the domain is verified at once. **Replies** says so.

The **Workspace address name** sits on **Settings → Email** under **Change forwarding address or
disconnect**: it is part of every mailbox's forwarding address, so changing it changes them all.

Replies leave from the mailbox the conversation came through; see
[channels](channels.md#several-mailboxes-in-the-inbox) for the default, starting a conversation from
a chosen mailbox and filtering by one. A conversation imported from Freshdesk or Help Scout keeps the
mailbox it was sent to (`origin.mailboxes`), and its replies leave from the channel with that support
address when there is one.

Through the API each mailbox's settings and actions are under `/api/v1/channels/{id}/email`
(`/secret`, `/domain-verification`, `/gmail`), and `PATCH /api/v1/channels/{id}` with `config`
changes its address or sender name. `/api/v1/channels/email` and the MCP tools without `channel_id`
act on the default mailbox, as before.

## Send from your own domain

Until your domain is verified, replies are sent as **"<sender name> via Colliedesk"** from a
`reply+…@` address on the server's sending domain, with `Reply-To` set to your support address.
Customer replies still reach you and thread. **Settings → Email** (step 3 of the setup, or the address's **Replies** tab) shows **Replies are sent
from …** with the current address. Verify the domain to send as your support address.

**Colliedesk Cloud** verifies in two steps:

1. Publish the TXT record shown in **Settings → Email** (step 3 of the setup, or the address's **Replies** tab) (**TXT record name**, for example
   `_colliedesk.example.com`, and **TXT record value**). It proves you control the domain. Select
   **Verify domain**; until the TXT record is found, Colliedesk asks for it and does nothing else.
2. Colliedesk then lists a table **DNS records to publish** (DKIM, SPF and an MX record for bounces),
   each with its status (Not started, Pending, Verified, Failed). Add every record at your DNS provider and
   select **Verify domain** again. The domain is verified only when Resend reports it verified; DNS changes
   can take from a few minutes to a day. The button works once every 30 seconds per workspace and is
   disabled while a check runs.

The records stay valid for 7 days. A domain that was never verified and is still not verified after that is released at Resend
and Settings says the records expired; select **Verify domain** to get new ones.

If Resend refuses the domain name, Colliedesk shows Resend's reason; any other Resend problem shows as
"could not be reached" and the operator is told. If Resend cannot be reached, nothing changes. Changing the
support address to another domain replaces the list; the old domain is deleted at Resend in the background.
**Only one workspace can verify a given domain name this way.** If another workspace already has the
domain verified, Colliedesk says so and you keep sending from the platform address; if you own the domain,
contact support and the operator can release it. If Resend later
stops verifying your domain (for example the records were removed), Colliedesk stops sending as your
address until you verify it again.

**Your own server** verifies with one TXT record, unless its operator set it up like Cloud:

1. In **Settings → Email** (step 3 of the setup, or the address's **Replies** tab), copy the **TXT record name** (for example
   `_colliedesk.example.com`) and the **TXT record value**.
2. Add that TXT record at your DNS provider.
3. Select **Verify domain**. DNS changes can take a few minutes; try again if it fails.

A server whose operator owns the relay may skip verification and send every reply as the support
address (see [configuring email](../self-host/email.md#1-configure-the-server)).

Changing the support address resets verification. With a TXT record it also creates a new value; with Resend the same domain keeps its records and a new domain gets its own list.

## Sending pauses after spam complaints

Colliedesk counts spam complaints per workspace. A workspace is
paused when it has **3 or more complaints in the last 7 days and they are more than 0.3% of the
replies it sent in those 7 days** (3 on 1,000 replies is exactly 0.3% and does not pause; 4 does).

- **Settings → Email** (step 3 of the setup, or the address's **Replies** tab) shows **Email sending is paused** with the counts. Admins cannot lift it.
- Replies written during the pause stay **Queued** with the note "Email sending is paused after spam
  complaints; contact support." Nothing is failed and no attempt is used up. They are sent after
  the pause ends.
- The operator is told and lifts the pause after checking the complaints. Counting then starts
  again, so earlier complaints do not pause the workspace a second time.
- Complaints count for replies sent from the platform address and for replies sent as your own verified
  support address.

## What you see in the inbox

- Incoming messages show the sender address. A message with an HTML version is shown formatted,
  inline in the conversation, as tall as the mail (no scrolling inside the message), with tables, bold
  text and images. Mail wider than the column is scaled down to fit. **Show plain text** switches to the
  plain version; **Show formatted email** switches back. A message with no HTML is shown as plain text,
  where only web addresses written in the text become links.
- The formatted view is locked: no scripts run, forms do nothing, and the mail cannot send you away
  from Colliedesk. Links open in a new tab. Quoted earlier mail under a reply is folded behind a
  **•••** button; select it to read the quote.
- Remote images (logos, charts, banners) are shown, but your browser never contacts the sender:
  Colliedesk fetches each image on its server, keeps a copy for up to 30 days, and serves it to you. The
  sender sees the server's address, not yours. The first time anyone opens the mail the server does fetch
  the images, so a tracking pixel in a mail still tells its sender that the mail was opened by someone on
  your team, as it does in Freshdesk. Only PNG, JPEG, GIF and WebP images up to 5 MiB are shown, at most 50
  distinct images per message; an SVG, a private or internal address, or a port other than 80 or 443 is not.
  The copies are a cache: they do not count against your storage, are not part of the export, and the
  oldest are removed when the cache is full.
- Attachments are listed under the message, including the files you sent with a reply; select a
  file name to download it. An image that Colliedesk embedded in the formatted message is not
  listed again; it returns to the list in the plain-text view. **Download original email** gives the
  complete message as received (`.eml.gz`), so nothing is lost even if a part could not be shown.
- **Failed sender verification**: the trusted receiving server reported an SPF, DKIM, or DMARC
  failure. Treat the message as possibly forged.
- **Sender not verified**: no trusted verification result was present.
- Your replies show **Queued**, **Sent**, or **Delivery failed**. Hover **Delivery failed** to
  read the reason, for example the relay's error or a bounce. Select **Retry delivery** to send it
  again.

## How threading works

A customer reply joins the conversation when any of these match, in this order:

1. `In-Reply-To` names a message in the conversation.
2. `References` names a message in the conversation.
3. The reply went to the reply address in our `Reply-To` (`support+r-<token>@example.com`).

Otherwise the message starts a new conversation. A reply to a **Resolved** or **Closed**
conversation reopens it.

A message from someone other than the conversation contact (for example a colleague in Cc, who
answers with reply-all) also joins the thread, with their address shown on the message. That person
does not become the contact of the conversation.

## Copy other people on a reply (Cc)

When a customer writes with colleagues in Cc, copy them on your reply so the thread does not split
on their side.

- Under the message box, select **Cc** to add addresses. Type or paste an address and press Enter,
  comma, semicolon or space. Select the **x** on an address to remove it, or press Backspace in the
  empty box to remove the last one. While the box holds text that is not a valid address, **Send**
  stays off: fix the address or clear it.
- The box opens with the people the customer copied on their latest message (and a copied colleague
  who answered with reply-all). Remove any of them before you send. Mail received before Colliedesk
  kept Cc lines offers nothing, and so does mail whose sender did not pass verification: anyone can
  write a Cc line, so you add those people yourself if you mean to.
- The reply goes to the conversation contact in **To** and to every Cc address. There is no Bcc.
  The timeline shows **Cc: …** under the reply, and under customer mail that copied someone.
- At most 10 addresses per reply. Plain addresses only (`name@example.com`, no display names). You
  cannot copy your own support address (also in its `support+…@` form), the forwarding address or
  the platform sending domain.
- Private notes cannot be copied: they never leave your team.
- **Retry delivery** sends to the same Cc list again.
- A copied address that does not exist never stops the reply to the customer. The reply shows
  **Sent**, with a line naming each copied address it did not reach, whether the mail system
  refused it at once or bounced it later.
- During the **trial**, as long as the sending domain is not verified, you can copy only addresses
  the customer already wrote from or copied. Any other address is refused, the message names each
  refused address, your text stays in the composer, and nothing is stored. Verify the domain or choose a plan to lift
  it. The daily trial limit counts people, not replies: a reply to the customer with two people in Cc
  counts 3 of the 200.
- A copied person who answers with reply-all joins the same conversation.

Through the API, send `cc` (a list of addresses) with `kind: "outgoing"`; in a multipart request
send one `cc` field per address before the files. See `createMessage` in `contracts/openapi.yaml`.

## Limits

- A reply carries at most 10 attachments and 10 MiB of files in total (the raw size, before email
  encoding). The limit is also in `GET /api/v1/config` as `limits.outgoing_attachments_bytes` and
  `limits.outgoing_attachments_files`. More answers `413` with the reason, and nothing is stored
  or sent. Make sure your mail relay accepts a message of about 14 MiB; email encoding adds a third.
  Some recipient servers cap incoming mail at about 10 MB, so a large reply can bounce; it then shows
  **Delivery failed** with **Retry delivery**.
- If a stored file is gone when the reply is sent, the reply shows **Delivery failed** and names the
  file. Colliedesk never sends the reply without it.
- A message may be up to 25 MiB with at most 100 attachments. Larger mail gets `413` and nothing
  is stored; the forwarder should keep a copy for a person.
- If attachment storage fails, the forwarder gets `500` and nothing is stored; it retries.
- Mail marked as automatic is dropped: auto-replies (`Auto-Submitted`, out-of-office), bulk mail
  (`Precedence: bulk` or `junk`, or list headers together with a bulk-sender marker such as
  one-click unsubscribe), and delivery reports (`MAILER-DAEMON`, `postmaster`). A delivery report
  about one of your replies marks that reply **Delivery failed**. Mail sent through a Google Group
  or list alias in front of your support address is not dropped.
- Mail from your own support address and copies of your own replies are dropped.
- One sender can send 10 messages per 10 minutes, and a workspace can open 200 new conversations
  per 10 minutes. Above that the forwarder gets `429` and should retry.
- Each workspace sends at most 300 emailed replies per hour; more wait and send later.
- Mail Colliedesk writes by itself (operator alerts, notices, reports) carries
  `Auto-Submitted: auto-generated` (RFC 3834). Colliedesk drops inbound mail with that header, so an
  automatic answer to it never opens a conversation. Replies you write carry no such header.
- Text longer than 5 MiB is cut and marked. Subjects longer than 250 characters are shortened in
  replies.
- Replies are plain text. Markdown is sent as written.
- One support address per email channel. For more addresses, add more channels (see [Several mailboxes](#several-mailboxes)).
