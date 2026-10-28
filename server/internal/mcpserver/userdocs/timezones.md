# Timezones

Colliedesk stores every time in UTC and shows it in your timezone.

- **Your timezone** (everyone, **Settings → Profile**): choose your own timezone, or
  **Same as workspace** to follow the workspace default.
- **Workspace timezone** (admins, **Settings → General**): the default for everyone who has not
  chosen their own. It starts as `UTC`.
- **The first time you sign in** to a workspace still on `UTC`, if you have not chosen a timezone,
  Colliedesk sets yours to your browser's timezone, so times match your clock. This happens once per
  browser; choose **Same as workspace** to go back, and it stays that way.
- **A time shown in another zone than your computer's says so**: a message time in the timeline adds the
  zone (`UTC`, `GMT+7`) after it, the conversation list adds it to the time you point at, and a message
  time always names its zone when you point at it.

Both are lists of IANA timezones such as `America/New_York`, `Europe/London` or
`Australia/Sydney`. There is no Save button: a choice is saved when you leave the list,
press Enter, or stop on a zone for a moment, so browsing with the arrow keys does not save every
zone you pass. "Saving…" and then "Saved", or the reason the save failed, appears next to it.
