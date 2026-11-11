# Timezones

Colliedesk stores every time in UTC and shows it in your timezone.

- **Your timezone** (everyone, **Settings → Profile**): choose your own timezone, or
  **Same as workspace** to follow the workspace default.
- **Workspace timezone** (admins, **Settings → General**): the default for everyone who has not
  chosen their own, and the zone business hours count in ([response targets](response-targets.md)).
  It starts as `UTC` until the person who created the workspace signs in.
- **The first sign-in of the person who created the workspace** sets the workspace timezone to their
  browser's, so the team's times and business hours match their clock. They follow the workspace
  (**Same as workspace**). This happens only while nobody has chosen the workspace timezone: a timezone an
  admin chose, `UTC` included, is kept, and so is the timezone of every workspace created before this
  change. A browser set to `UTC` or another name of it (`GMT`, `Etc/UTC`, `Zulu`) changes nothing.
  Change it at any time in **Settings → General**.
- **Your first sign-in** to a workspace still on `UTC` (as a teammate, or when the admin chose `UTC`)
  sets only your own timezone to your browser's, so times match your clock. It never changes the
  workspace timezone. It happens only at your first sign-in: choose **Same as workspace** to go back, and
  no later sign-in, from any browser, changes it again.
- **A time shown in another zone than your computer's says so**: a message time in the timeline adds the
  zone (`UTC`, `GMT+7`) after it, the conversation list adds it to the time you point at, and a message
  time always names its zone when you point at it.

Both are lists of IANA timezones such as `America/New_York`, `Europe/London` or
`Australia/Sydney`. There is no Save button: a choice is saved when you leave the list,
press Enter, or stop on a zone for a moment, so browsing with the arrow keys does not save every
zone you pass. "Saving…" and then "Saved", or the reason the save failed, appears next to it.
