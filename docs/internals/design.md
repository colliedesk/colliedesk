# Web app design

Decisions behind the look of `web/`. The brand is **Blue Merle**, dark only. The app and the
colliedesk.com site share one set of tokens.

## Direction

- Dense and keyboard-first: three panes, a list row with status and priority, a property sidebar of
  label/value rows, a composer with Reply / Private note. Only public screenshots of other inboxes
  were studied; see the clean-room rule.
- Dark only. There is no light theme and no theme switch. No cream or light backgrounds, no
  decorative gradients, glass, blur or emoji.
- The mascot is a blue-merle collie head.

## Tokens

All tokens are CSS variables in `web/src/styles/tokens.css`. Components use the token names; never
copy a hex value into a component. (`cloud-sign-in.tsx` reads the tokens at run time to colour
the Clerk form.) Do not create a second palette.

| Group | Tokens | Rule |
|---|---|---|
| Surfaces | `--surface-0` page and rail, `--surface-1` list, panels, cards, `--surface-2` hover, `--surface-3` active rail item and tag, `--surface-inverse` tooltip | Separate layers by surface step first. Add shadow only to floating layers. |
| Lines | `--line` decorative dividers, `--line-strong` borders that show state (inputs, chips, secondary buttons) | Never use `--line` as the only cue for a control. |
| Text | `--text-1` primary, `--text-2` secondary, `--text-3` meta and labels, `--text-inverse` on the tooltip | `--text-3` is the lowest text tone. |
| Brand | `--accent` primary fill, `--accent-hover`, `--on-accent` text on accent, `--accent-text` links, `--accent-soft` selected row, `--accent-strong` graphics only | One primary action per screen: `--accent` with dark `--on-accent` text. |
| Focus and note | `--focus`, `--focus-ring`, `--note-bg`, `--note-line`, `--note-text` | Amber means focus or private note. Never use amber for status. |
| Status | `--danger`, `--warn`, `--ok`, `--open`, `--pending` and `*-soft` backgrounds | A dot or icon plus a word. Never colour alone. |
| Shape | `--radius` 6 px, `--radius-sm` 4 px, `--radius-full` | Pills only for avatars. |
| Space | `--space-1` to `--space-24` on a 4 px grid, `--touch` 44 px | Touch targets are at least `--touch`. |
| Type | `--font-sans`, `--font-mono`, `--fs-ui` 13 px, `--fs-label` 11.5 px | See Type. |
| Elevation | `--shadow-1` cards, `--shadow-2` popovers and the composer, `--shadow-3` modals | |
| Motion | `--ease`, `--dur-fast` 120 ms, `--dur` 180 ms | Both become 0 under `prefers-reduced-motion`. |

Main values: page `#0b0f15`, panel `#111721`, text `#e8edf4`, accent (merle blue) `#7aa5e6`,
focus (amber) `#f0b44c`. Every text pair used in the app is at least 4.5:1; focus rings and state
borders are at least 3:1.

## Type

- IBM Plex Sans (400, 500, 600, 700) for text; IBM Plex Mono (400, 500) for buttons, labels, ticket
  and time stamps. Mono labels are uppercase with `--track-label` spacing.
- Fonts are self-hosted: `web/src/styles/fonts/` (woff2 subsets latin, latin-ext, vietnamese) and
  `fonts.css`, licence SIL OFL 1.1 (`fonts/OFL-*.txt`). Never load a font from Google Fonts or a
  CDN, and never use Be Vietnam Pro.
- Body is `--fs-ui` 13 px: dense on purpose, an agent scans dozens of rows.

## Logo

- The mark (`web/src/ui/logo.tsx`) is the collie head: tipped ears, a white blaze into a long white
  muzzle, a white ruff. It sits on `--surface-0` or `--surface-1` only, in the rail and on the
  sign-in panel, at 20 px or larger.
- The favicon is `web/public/favicon.svg`: the mark on a dark tile. Use the tile for any light or
  unknown background (browser tabs, GitHub, partner pages).
- Do not recolour, outline, rotate, glow or put a gradient on the mark. Do not put the bare mark on
  a photo, a light background or `--accent`.
- Wordmark: "collie" Plex Sans 700 in `--text-1`, "desk" Plex Sans 400 in `--text-2`, one lowercase
  word. In running text write "CollieDesk".

## Rules

- Controls are 32 px with a mouse and 44 px on touch (`--control` switches on `pointer: coarse`).
- Three button shapes only: primary (`--accent`), secondary (default), danger (red text).
- Focus is an amber outline, 2 px wide with a 2 px gap (`:focus-visible`). The gap is required:
  amber on `--accent` is only 1.36:1, so the ring never touches a primary button. In the scrolling
  list the ring is inset (`outline-offset: -2px`) so the next row does not clip it.
- Status is a dot plus its word. Only High and Urgent priorities get colour, with an arrow icon.
- Private notes are amber (`--note-*`), on the timeline and in the composer while writing one.
- Navigation is an icon rail. Each icon has a screen-reader label and a visible tooltip
  (`data-tip`, shown on hover and keyboard focus, dismissed with Escape). Other icon-only buttons follow the same pattern.
- The list groups rows under Today and Earlier by the day in the agent's timezone. The list shows
  only what the API returns: there are no SLA timers or ticket numbers because the backend has none.
- Below 75 rem the details pane becomes a drawer opened by **Show details**; below 44 rem the
  panes stack and the rail becomes a top bar.
- Customer email stays plain text (see `plain-text-body.tsx`); the original HTML opens in a
  sandboxed frame that keeps a white page so the customer's own styling stays readable. Avatars are
  initials, never images, so the timeline loads nothing remote.

## Identity bar

The workspace name and the signed-in address are always visible in a bar above every page
(`identity-bar`, landmark "Signed in as"). It guards against acting in the wrong workspace after a
link switched it. Never move it into a tooltip or hide it at any width. `web/e2e/identity-visible.spec.ts`
checks opacity and position, because Playwright counts text with opacity 0 as visible.

## Contrast

WCAG 2.x ratios of the token pairs the app uses. Text needs 4.5:1; focus rings, state borders and
graphics need 3:1.

| Foreground | Background | Ratio |
|---|---|---|
| `--text-1` | `--surface-0` / `-1` / `-2` / `-3` | 16.33 / 15.28 / 13.93 / 11.96 |
| `--text-2` | `--surface-0` / `-1` / `-2` / `-3` | 10.33 / 9.67 / 8.81 / 7.57 |
| `--text-3` | `--surface-0` / `-1` / `-2` / `-3` | 6.83 / 6.39 / 5.82 / 5.00 |
| `--text-3` | `--accent-soft` | 5.20 |
| `--accent-text` | `--surface-0` / `--accent-soft` | 8.97 / 6.83 |
| `--on-accent` | `--accent` / `--accent-hover` | 7.64 / 9.58 |
| `--danger` on `--danger-soft`, `--warn` on `--warn-soft`, `--ok` on `--ok-soft` | | 6.10, 6.47, 8.10 |
| `--note-text` | `--note-bg` | 10.38 |
| `--focus` | `--surface-0` / `-3` | 10.37 / 7.60 |
| `--line-strong` | `--surface-0` / `-1` / `-2` | 4.42 / 4.13 / 3.77 |
| `--focus` | `--accent` | 1.36 (why the focus ring keeps a 2 px gap) |
