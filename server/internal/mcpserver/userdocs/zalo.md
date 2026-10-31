# Zalo OA

A Zalo Official Account (OA) is a channel: customers write to it one to one, and in the Zalo groups the OA
is a member of. Each message lands in your inbox as the [chat rules](channels.md#chat-rules) of the
channel decide, and your replies go back where the conversation was said: into its group, or to the
customer alone, quoting the message they answer.

## Connect an OA

An admin adds the channel with the API (the settings page comes later):

1. `POST /api/v1/channels` with `{"type": "zalo", "config": {"oa_id": "<the OA's id>", "app_id": "<the Zalo app's id>"}}`.
   One OA is one channel on the whole server.
2. Its four secrets, one at a time, with `PUT /api/v1/channels/{id}/credentials` (`name`, `value`):
   `oa_secret_key` (the OA secret key of the app's webhook settings), `app_secret` (the app's secret
   key), `access_token` and `refresh_token` (the OA's tokens). A secret is never shown again: the
   channel's `config.credentials` says for each whether it is set, its last four characters and when it
   was last written (a token refresh too).
3. In the Zalo app's webhook settings, the URL is the server's address followed by the channel's
   `webhook_path` (`/api/v1/channels/zalo/webhook`). One app may serve several OAs: each delivery is taken
   by the channel of its OA, and one of an OA no channel is answers 200 and is dropped.

Set `oa_secret_key` and `app_id` before deliveries arrive: a delivery whose signature does not match is
dropped (answered as one of an OA no channel is, so nobody learns which OAs are here), and Zalo does not
send it again. The channel then says **Needs attention** (`zalo_signature_failed`) for a day, or until a
delivery is signed right again. Without the key it says `zalo_secret_missing`; without the tokens
or the app's secret, `zalo_token_missing` (deliveries are taken, replies cannot be sent).

`config` also takes `staff_user_ids` (your team's Zalo user ids: what they write in a group is the team's,
never a customer's request) and `groups` (a group's id to the company its members are customers of, so
a conversation of that group names it). `PATCH /api/v1/channels/{id}` changes them; a field left out is
kept. The channel's line in **Channels** reads "OA 1234…5678 · 2 groups linked".

## Messages

1. One to one (scope `direct`): every message is the customer's, and opens or joins their conversation
   as the channel's rules say.
2. In a group (scope `group`): by default a conversation opens on a command only (`settings.commands`);
   what the members say in between is collected and decided by the rules, or by
   [AI intake](ai.md) where it is on.
3. Pictures, files and videos are fetched and kept with the message. A sticker, a location or another
   kind Zalo sends is kept as a message saying what it was, never dropped.
4. A customer is called by their Zalo id until their name is read from Zalo, right after their first
   message is taken: their profile as the OA sees it, or for a group member who does not follow the OA,
   the group's member list (read once for every member, at most once an hour a group). The contact and
   the chat then show it. Zalo is asked at most once an hour for someone it has no name for, and again
   with the next message when it could not answer (a token it refused, the network); your team's Zalo
   users are never looked up.
5. What the OA sends (your replies, and what someone writes in the OA's own chat on Zalo) is the team's.

## Replies

A reply goes to the group of the conversation, or to the customer alone, quoting their last message (sent
again without the quote when Zalo cannot make it). A reply longer than 2000 characters goes in parts. When
Zalo refuses the access token, Colliedesk refreshes it once with the refresh token and keeps both new
tokens; only one sender refreshes at a time, because a refresh token is used once.

A reply carries pictures (JPEG or PNG, read from the file itself, 1 MB at most each; not GIF yet): each goes up to Zalo and is sent as a
message of its own after the text. Another file, or a larger picture, fails the reply before anything is
sent and says why (`zalo_no_files`, `zalo_file_too_large`): send the text again without it.

Not yet: other files in a reply, connecting an OA from Zalo's own consent page.
