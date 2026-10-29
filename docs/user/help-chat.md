# Help chat

The help chat puts an AI on your help center that answers your customers from your **published help
articles**, and lets them hand the chat to your team. It needs two things turned on:

1. A **Help chat** channel in **Settings → Channels** (admins): what the chat says first, the questions
   it offers, what you add to the AI's instructions, and the sites that may embed it.
2. The **AI chat** feature in **Settings → Built-in AI** (admins): which provider and model answer. See
   [AI chat](ai-chat.md) for what the AI does, its limits and the transcripts.

While either is off, the chat answers `404`, the same as a help center that is off, and no page shows
a chat button.

## Where customers chat

- **The chat page** of your help center: `<your help center>/chat` (for example
  `https://acme.colliedesk.com/chat`, or `https://desk.example.com/help/acme/chat` on a self-hosted
  server without a help center domain).
- **The chat box of an article**: an **Ask AI** button on every article page opens the chat beside
  the article, asking about it.
- **Your own site**, with the code from the channel's **Your site** tab:

  ```html
  <script src="https://acme.colliedesk.com/chat/widget.js" data-channel="<channel id>" async></script>
  ```

  The button shows only on the sites you list under **Sites that may embed the chat** (exact origins
  such as `https://shop.example.com`, at most 20). It adds no cookie to your site and sends nothing
  before a visitor presses it.

## Set it up

1. **Settings → Channels → Add channel → Add help chat.**
2. **Setup**: a greeting (at most 300 characters), up to 4 suggested questions (each at most 120), and
   extra instructions for the AI (at most 2,000; the AI still answers only from published articles,
   instructions cannot add facts).
3. **Your site**: the sites that may embed the chat, one per line, and the code to copy.

A workspace may have several help chats, for example one per site; the default one is the help
center's chat page. Turn a help chat off with its switch; its settings stay.

## Talk to a person

Every chat offers **Talk to a person**. The customer types an email address (and a name and a note if
they like); a conversation opens in your inbox on the **Help chat** channel, with the tag `help-chat`,
the note, and the whole chat as text with the articles it cited. The address was typed, never
checked: the message is marked **Unverified sender (Help chat)**, and the conversation links to its
chat transcript. A second hand-off of the same chat joins the same conversation.

You reply by email. On a Colliedesk Cloud **trial**, a reply to a help chat conversation is refused
(error `trial_outbound_restricted`), as for the web form; the customer is told that you have their
message, not that you will reply by email. One address may hand off 5 chats in 10 minutes, and the
channel opens at most 50 conversations in 10 minutes.

## Limits you should know

- Changing the help center's custom domain moves its address: a chat in progress does not continue
  at the new address.
