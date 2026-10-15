# Timezones

Colliedesk stores every time in UTC and shows it in your timezone.

- **Workspace timezone** (admins): the default for everyone in the workspace. It starts as `UTC`.
- **Personal timezone override** (everyone): use your own timezone instead of the workspace one.
  **Clear personal timezone** goes back to the workspace default.

Each save button reads "Saving…" and stays disabled until the save ends. Then "Saved" appears next
to that field until you edit it again, or the reason the save failed.

Both are in **Settings → Timezone** and use IANA names such as `America/New_York`,
`Europe/London` or `Australia/Sydney`. Invalid names are rejected.
