# Email channel

Customers write to your support address. Each new email thread becomes a conversation, and the
sender becomes a contact. Customer replies join the same conversation. Your replies go back to
the customer by email. Private notes are never emailed.

On Colliedesk Cloud every workspace has a ready-made address; you only forward your mailbox to it.
On your own server, the person who runs it first connects a mail forwarder and an SMTP relay: see
[configuring email](../self-host/email.md).

## Colliedesk Cloud: forward to your address

On Colliedesk Cloud every workspace has a ready-made address, `support@<workspace>.colliedesk.com`.
You do not run a forwarder or a Worker. A workspace started from the sign-up page receives mail at
that address at once: its Support address is set to it. To keep your own address for customers,
change where your mailbox forwards:

1. Open **Settings → Email channel** and enter your **Support address**, for example
   `support@example.com`. Select **Save support address**. A workspace created another way accepts
   mail only after this step. The button reads "Saving…" until the save ends, then "Saved" appears
   next to it.
2. Copy the **Forwarding address** (select **Copy forwarding address**).
3. In your mailbox, forward all mail for the support address to the forwarding address:
   - **Gmail:** Settings → See all settings → Forwarding and POP/IMAP → Add a forwarding address.
     Gmail sends a confirmation message to the forwarding address. It appears as a new conversation
     in Colliedesk; open it and select the confirmation link. Then choose **Forward a copy of
     incoming mail** and save.
   - **Outlook:** Settings → Mail → Forwarding, enter the address and select **Start forwarding**.
4. Send a test message to your support address. It opens a conversation.

Forwarded mail keeps its original sender, so the conversation is with your customer, not with your
mailbox. Which address your replies come from is described in
[send from your own domain](#send-from-your-own-domain).

An admin can change the part before `.colliedesk.com` under **Workspace address name**. The old
address stops receiving mail at once, so change the forwarding in your mailbox at the same time.
A name is 1 to 63 lowercase letters, digits or hyphens; names such as `app`, `www` and `mail` are
reserved. Do not set the Support address to a `colliedesk.com` address of another workspace.

## Sender name

The **Sender name** is the name customers see in the From line of every reply, for example
**"Acme Support" <support@example.com>**. It belongs to the email channel, not to the workspace:
renaming the workspace does not change it.

A new channel starts with the workspace name. To change it:

1. Open **Settings → Email channel**. The field appears once a support address is saved.
2. Enter the name in **Sender name** and select **Save sender name**.

The next reply uses the new name, on every way a reply leaves: your own domain, the
`via Colliedesk` address and Gmail. A name is 1 to 100 characters. Spaces at the ends are removed.
Line breaks and other control characters are refused. Only admins can change it; other teammates
see it. Through the API, send `sender_name` with `PUT /api/v1/channels/email`; an AI agent uses
`colliedesk_email_channel_update`.

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
2. Open **Settings → Email channel → Send from Gmail** and select **Connect Gmail**.
3. Google asks you to choose the account and allow **Send email on your behalf**. Leave that box
   ticked. Google then brings you back to Settings, which says whether Gmail is connected and, if not,
   why.
4. **Send from Gmail** now shows the account. Replies, including copies (Cc), attached files and
   **Retry delivery**, go out through it with the same threading headers, so customer answers join
   the same conversation.

Connect the account of your support address. If you connect another one (for example
`acme.help@gmail.com` while customers write to `support@example.com`), customers see that account as
the sender, and their answers still go to the support address. Settings says so.

**When Gmail stops working.** If the access is removed in the Google account, the password changes,
or Google refuses to send, **Send from Gmail** turns into an alert with the reason. Replies are then
marked **Delivery failed** with the same reason. They are never sent another way without you knowing.
Select **Reconnect Gmail**, then **Retry delivery** on the failed replies.

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

## Send from your own domain

Until your domain is verified, replies are sent as **"<sender name> via Colliedesk"** from a
`reply+…@` address on the server's sending domain, with `Reply-To` set to your support address.
Customer replies still reach you and thread. **Settings → Email channel** shows **Replies are sent
from …** with the current address. Verify the domain to send as your support address.

**Colliedesk Cloud** verifies in two steps:

1. Publish the TXT record shown in **Settings → Email channel** (**TXT record name**, for example
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

1. In **Settings → Email channel**, copy the **TXT record name** (for example
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

- **Settings → Email channel** shows **Email sending is paused** with the counts. Admins cannot lift it.
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
- One support address per workspace.
