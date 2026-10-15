# Web app design

Decisions behind the look of `web/`. The brand is **Blue Merle**. Light is the default theme and
dark is an opt-in. The app and the colliedesk.com site share one set of tokens.

## Direction

- Modelled on Plain (primary) and Front (views), studied from public Mobbin screens; mechanisms
  only, no copied visuals. See the clean-room rule.
- White working panels on a cool grey app background (`--surface-0`), separated by an 8 px gap
  and hairline borders, not by heavy outlines. No cream or beige, no decorative gradients, glass,
  blur or emoji. Desktop views use the full width; no narrow centred column.
- Sentence case everywhere. Plex Sans for every control and label; Plex Mono only for code, IDs,
  URLs and keycaps.
- List header: status tabs (All, Open, Pending, Resolved, Closed), then search beside one
  **Filter** menu for assignee and tag with a count of active filters.
- Rows: bold subject and time, then one meta line (status dot and word, priority, `#tags`).
- Thread: incoming messages on `--surface-2`, replies on `--accent-soft`, private notes amber.
  Composer: a raised card with Reply / Private note tabs (keys **R** and **N**) and
  **Ctrl/Cmd + Enter** to send.
- Sign-in: a dark brand side (mascot, wordmark, one line of promise) beside the form; on narrow
  screens the brand side becomes a header.
- The mascot is a blue-merle collie head.

## Themes

- Light is the default for everyone, whatever the OS preference. Dark is a per-browser choice in
  **Settings → Appearance**, saved in this origin's `localStorage` (`colliedesk.theme`) and applied
  as `data-theme="dark"` on `<html>`. A one-line inline script in the `<head>` of `web/index.html` sets it
  while the head is parsed, before first paint, so a dark choice never paints light first; `web/src/lib/theme.ts` holds the same
  rule for the app (`src/lib/theme-boot.test.ts` keeps the two in step).
- The choice belongs to one origin. A self-hosted or Cloud app and colliedesk.com each keep their
  own setting; choosing dark on one does not change the other.
- `tokens.css` holds one light block (`:root, :root[data-theme="light"]`) and one dark block
  (`:root[data-theme="dark"]`) with the same token names. Shape, space, type and motion tokens are
  shared.

## Tokens

All tokens are CSS variables in `web/src/styles/tokens.css`. Components use the token names; never
copy a hex value into a component. (`cloud-sign-in.tsx` reads the tokens at run time to colour
the Clerk form.) Do not create a second palette.

| Group | Tokens | Rule |
|---|---|---|
| Surfaces | `--surface-0` page and rail, `--surface-1` list, panels, cards, `--surface-2` hover, `--surface-3` active rail item and tag, `--surface-inverse` tooltip | Separate layers by surface step first. Add shadow only to floating layers. |
| Lines | `--line` hairline dividers and panel edges, `--line-control` secondary button outline, `--line-strong` input borders | Inputs keep `--line-strong` (3:1). Buttons are identified by their label; their outline is decoration. |
| Text | `--text-1` primary, `--text-2` secondary, `--text-3` meta and labels, `--text-inverse` on the tooltip | `--text-3` is the lowest text tone. |
| Brand | `--accent` primary fill, `--accent-hover`, `--on-accent` text on accent, `--accent-text` links, `--accent-soft` selected row and reply bubble, `--accent-strong` graphics only, `--logo-tile`, `--brand-panel`, `--on-brand-panel(-2)` | One primary action per screen. Light: white on `#2d62b3`. Dark: near-black on `#7aa5e6`. |
| Focus and note | `--focus`, `--focus-ring`, `--note-bg`, `--note-line`, `--note-text` | Amber means focus or private note. Never use amber for status. |
| Status | `--danger`, `--warn`, `--ok`, `--open`, `--pending` and `*-soft` backgrounds | A dot or icon plus a word. Never colour alone. |
| Shape | `--radius` 6 px controls, `--radius-lg` 10 px panels, cards, composer, `--radius-sm` 4 px chips and keycaps, `--radius-full` avatars and tag chips | |
| Space | `--space-1` to `--space-24` on a 4 px grid, `--touch` 44 px | Touch targets are at least `--touch`. |
| Type | `--font-sans`, `--font-mono`, `--fs-ui` 14 px, `--fs-small` 13 px, `--fs-label` 12 px | See Type. |
| Elevation | `--shadow-1` cards, `--shadow-2` popovers and the composer, `--shadow-3` modals | |
| Motion | `--ease`, `--dur-fast` 120 ms, `--dur` 180 ms | Both become 0 under `prefers-reduced-motion`. |

Main values, light: app background `#f5f7fa`, panel `#ffffff`, text `#151c27`, accent `#2d62b3`,
focus (amber) `#b86e00`. Dark: page `#0b0f15`, panel `#111721`, text `#e8edf4`, accent `#7aa5e6`,
focus `#f0b44c`. Every text pair is at least 4.5:1; focus rings and input borders at least 3:1.
`web/scripts/check-contrast.mjs` checks every pair in both themes and runs in `pnpm test`.

## Type

- IBM Plex Sans (400, 500, 600, 700) for text, buttons, tabs, labels and time stamps, always in
  sentence case. IBM Plex Mono (400, 500) only for code, IDs, URLs and keycaps. No uppercase labels.
- Fonts are self-hosted: `web/src/styles/fonts/` (woff2 subsets latin, latin-ext, vietnamese) and
  `fonts.css`, licence SIL OFL 1.1 (`fonts/OFL-*.txt`). Never load a font from Google Fonts or a
  CDN, and never use Be Vietnam Pro.
- Body is `--fs-ui` 14 px; meta lines are 12 to 13 px.

## Logo

- The mark (`web/src/ui/logo.tsx`) is the collie head: tipped ears, a white blaze into a long white
  muzzle, a white ruff. The white parts vanish on a light surface, so the app always draws the mark
  on its rounded dark tile (`--logo-tile`), the same shape as the favicon. 20 px or larger.
- The favicon is `web/public/favicon.svg`: the mark on a dark tile. Use the tile for any light or
  unknown background (browser tabs, GitHub, partner pages).
- Do not recolour, outline, rotate, glow or put a gradient on the mark. Do not put the bare mark
  (without its tile) on a photo, a light background or `--accent`.
- Wordmark: "collie" Plex Sans 700 in `--text-1`, "desk" Plex Sans 400 in `--text-2`, one lowercase
  word. In running text write "Colliedesk".

## Rules

- Controls are 32 px with a mouse and 44 px on touch (`--control` switches on `pointer: coarse`;
  rail links, status tabs and list rows also grow to 44 px below 44 rem).
- Three button shapes only: primary (`--accent`), secondary (white with a light outline and a small
  shadow), danger (red text).
- Focus is an amber outline, 2 px wide with a 2 px gap (`:focus-visible`). The gap is required:
  amber on `--accent` is low contrast in both themes, so the ring never touches a primary button. In the scrolling
  list the ring is inset (`outline-offset: -2px`) so the next row does not clip it.
- Settings saves show feedback inline, never a toast: the button is disabled and reads "Saving…" while the request runs (a second submit is refused), then a "Saved" line (`role="status"`) or the error (`role="alert"`) appears beside that form. "Saved" clears when the form is edited. `features/settings/save-feedback.tsx` holds the pattern.
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

WCAG 2.x ratios of the token pairs the app uses, generated by `node web/scripts/check-contrast.mjs`
(the same check runs in `pnpm test`). Text needs 4.5:1; focus rings, input borders and status dots
need 3:1. The focus ring keeps a 2 px gap from `--accent` in both themes.

### light

| Foreground | Background | Ratio | Min | Use |
|---|---|---|---|---|
| `--text-1` #151c27 | `--surface-0` #f5f7fa | 15.94 | 4.5 | text |
| `--text-1` #151c27 | `--surface-1` #ffffff | 17.11 | 4.5 | text |
| `--text-1` #151c27 | `--surface-2` #eef1f5 | 15.10 | 4.5 | text |
| `--text-1` #151c27 | `--surface-3` #e4e9f0 | 14.02 | 4.5 | text |
| `--text-2` #465267 | `--surface-0` #f5f7fa | 7.35 | 4.5 | text |
| `--text-2` #465267 | `--surface-1` #ffffff | 7.89 | 4.5 | text |
| `--text-2` #465267 | `--surface-2` #eef1f5 | 6.96 | 4.5 | text |
| `--text-2` #465267 | `--surface-3` #e4e9f0 | 6.46 | 4.5 | text |
| `--text-3` #5b687c | `--surface-0` #f5f7fa | 5.27 | 4.5 | text |
| `--text-3` #5b687c | `--surface-1` #ffffff | 5.65 | 4.5 | text |
| `--text-3` #5b687c | `--surface-2` #eef1f5 | 4.99 | 4.5 | text |
| `--text-3` #5b687c | `--surface-3` #e4e9f0 | 4.63 | 4.5 | text |
| `--accent-text` #2a5ca9 | `--surface-0` #f5f7fa | 6.11 | 4.5 | text |
| `--accent-text` #2a5ca9 | `--surface-1` #ffffff | 6.56 | 4.5 | text |
| `--accent-text` #2a5ca9 | `--surface-2` #eef1f5 | 5.79 | 4.5 | text |
| `--accent-text` #2a5ca9 | `--surface-3` #e4e9f0 | 5.38 | 4.5 | text |
| `--danger` #c0261b | `--surface-0` #f5f7fa | 5.54 | 4.5 | text |
| `--danger` #c0261b | `--surface-1` #ffffff | 5.94 | 4.5 | text |
| `--danger` #c0261b | `--surface-2` #eef1f5 | 5.24 | 4.5 | text |
| `--danger` #c0261b | `--surface-3` #e4e9f0 | 4.87 | 4.5 | text |
| `--warn` #a3460a | `--surface-0` #f5f7fa | 5.69 | 4.5 | text |
| `--warn` #a3460a | `--surface-1` #ffffff | 6.11 | 4.5 | text |
| `--warn` #a3460a | `--surface-2` #eef1f5 | 5.39 | 4.5 | text |
| `--warn` #a3460a | `--surface-3` #e4e9f0 | 5.01 | 4.5 | text |
| `--ok` #11703d | `--surface-0` #f5f7fa | 5.74 | 4.5 | text |
| `--ok` #11703d | `--surface-1` #ffffff | 6.16 | 4.5 | text |
| `--ok` #11703d | `--surface-2` #eef1f5 | 5.44 | 4.5 | text |
| `--ok` #11703d | `--surface-3` #e4e9f0 | 5.05 | 4.5 | text |
| `--accent-text` #2a5ca9 | `--accent-soft` #e8effa | 5.67 | 4.5 | link on selected row |
| `--text-1` #151c27 | `--accent-soft` #e8effa | 14.80 | 4.5 | selected row |
| `--text-3` #5b687c | `--accent-soft` #e8effa | 4.89 | 4.5 | meta on selected row |
| `--on-accent` #ffffff | `--accent` #2d62b3 | 5.98 | 4.5 | primary button |
| `--on-accent` #ffffff | `--accent-hover` #234f94 | 8.02 | 4.5 | primary button hover |
| `--text-inverse` #ffffff | `--surface-inverse` #1b2330 | 15.79 | 4.5 | tooltip |
| `--note-text` #7f5200 | `--note-bg` #fff6e0 | 6.27 | 4.5 | private note label |
| `--text-1` #151c27 | `--note-bg` #fff6e0 | 15.90 | 4.5 | private note body |
| `--danger` #c0261b | `--danger-soft` #fdeceb | 5.20 | 4.5 | urgent chip |
| `--text-1` #151c27 | `--danger-soft` #fdeceb | 14.97 | 4.5 | error card body |
| `--warn` #a3460a | `--warn-soft` #fdf0e5 | 5.46 | 4.5 | high chip |
| `--ok` #11703d | `--ok-soft` #e6f5ec | 5.47 | 4.5 | sent badge, reconcile footer |
| `--text-2` #465267 | `--ok-soft` #e6f5ec | 7.00 | 4.5 | reconcile footer body |
| `--accent-text` #2a5ca9 | `--ok-soft` #e6f5ec | 5.82 | 4.5 | reconcile footer link |
| `--on-brand-panel` #ffffff | `--brand-panel` #141b26 | 17.29 | 4.5 | sign-in brand side |
| `--on-brand-panel-2` #b4bfcf | `--brand-panel` #141b26 | 9.30 | 4.5 | sign-in brand side, secondary |
| `--pending` #a3730f | `--surface-1` #ffffff | 4.19 | 3 | pending dot |
| `--open` #2d62b3 | `--surface-1` #ffffff | 5.98 | 3 | open dot |
| `--focus` #b86e00 | `--surface-0` #f5f7fa | 3.71 | 3 | focus ring |
| `--focus` #b86e00 | `--surface-1` #ffffff | 3.99 | 3 | focus ring |
| `--focus` #b86e00 | `--surface-2` #eef1f5 | 3.52 | 3 | focus ring |
| `--focus` #b86e00 | `--surface-3` #e4e9f0 | 3.27 | 3 | focus ring |
| `--line-strong` #77839a | `--surface-0` #f5f7fa | 3.56 | 3 | input border |
| `--line-strong` #77839a | `--surface-1` #ffffff | 3.82 | 3 | input border |
| `--line-strong` #77839a | `--surface-2` #eef1f5 | 3.37 | 3 | input border |
| `--accent` #2d62b3 | `--surface-1` #ffffff | 5.98 | 3 | selected bar, active tab |

### dark

| Foreground | Background | Ratio | Min | Use |
|---|---|---|---|---|
| `--text-1` #e8edf4 | `--surface-0` #0b0f15 | 16.33 | 4.5 | text |
| `--text-1` #e8edf4 | `--surface-1` #111721 | 15.28 | 4.5 | text |
| `--text-1` #e8edf4 | `--surface-2` #18202c | 13.93 | 4.5 | text |
| `--text-1` #e8edf4 | `--surface-3` #212c3d | 11.96 | 4.5 | text |
| `--text-2` #b4bfcf | `--surface-0` #0b0f15 | 10.33 | 4.5 | text |
| `--text-2` #b4bfcf | `--surface-1` #111721 | 9.67 | 4.5 | text |
| `--text-2` #b4bfcf | `--surface-2` #18202c | 8.81 | 4.5 | text |
| `--text-2` #b4bfcf | `--surface-3` #212c3d | 7.57 | 4.5 | text |
| `--text-3` #8e9bb0 | `--surface-0` #0b0f15 | 6.83 | 4.5 | text |
| `--text-3` #8e9bb0 | `--surface-1` #111721 | 6.39 | 4.5 | text |
| `--text-3` #8e9bb0 | `--surface-2` #18202c | 5.82 | 4.5 | text |
| `--text-3` #8e9bb0 | `--surface-3` #212c3d | 5.00 | 4.5 | text |
| `--accent-text` #8fb3ea | `--surface-0` #0b0f15 | 8.97 | 4.5 | text |
| `--accent-text` #8fb3ea | `--surface-1` #111721 | 8.39 | 4.5 | text |
| `--accent-text` #8fb3ea | `--surface-2` #18202c | 7.65 | 4.5 | text |
| `--accent-text` #8fb3ea | `--surface-3` #212c3d | 6.57 | 4.5 | text |
| `--danger` #f2837a | `--surface-0` #0b0f15 | 7.59 | 4.5 | text |
| `--danger` #f2837a | `--surface-1` #111721 | 7.11 | 4.5 | text |
| `--danger` #f2837a | `--surface-2` #18202c | 6.48 | 4.5 | text |
| `--danger` #f2837a | `--surface-3` #212c3d | 5.56 | 4.5 | text |
| `--warn` #f19a55 | `--surface-0` #0b0f15 | 8.68 | 4.5 | text |
| `--warn` #f19a55 | `--surface-1` #111721 | 8.13 | 4.5 | text |
| `--warn` #f19a55 | `--surface-2` #18202c | 7.41 | 4.5 | text |
| `--warn` #f19a55 | `--surface-3` #212c3d | 6.36 | 4.5 | text |
| `--ok` #6fcf97 | `--surface-0` #0b0f15 | 10.10 | 4.5 | text |
| `--ok` #6fcf97 | `--surface-1` #111721 | 9.46 | 4.5 | text |
| `--ok` #6fcf97 | `--surface-2` #18202c | 8.62 | 4.5 | text |
| `--ok` #6fcf97 | `--surface-3` #212c3d | 7.40 | 4.5 | text |
| `--accent-text` #8fb3ea | `--accent-soft` #1a2940 | 6.83 | 4.5 | link on selected row |
| `--text-1` #e8edf4 | `--accent-soft` #1a2940 | 12.44 | 4.5 | selected row |
| `--text-3` #8e9bb0 | `--accent-soft` #1a2940 | 5.20 | 4.5 | meta on selected row |
| `--on-accent` #0b0f15 | `--accent` #7aa5e6 | 7.64 | 4.5 | primary button |
| `--on-accent` #0b0f15 | `--accent-hover` #96b9ee | 9.58 | 4.5 | primary button hover |
| `--text-inverse` #0b0f15 | `--surface-inverse` #e8edf4 | 16.33 | 4.5 | tooltip |
| `--note-text` #f2cd8a | `--note-bg` #2a2213 | 10.38 | 4.5 | private note label |
| `--text-1` #e8edf4 | `--note-bg` #2a2213 | 13.36 | 4.5 | private note body |
| `--danger` #f2837a | `--danger-soft` #3a1c1b | 6.10 | 4.5 | urgent chip |
| `--text-1` #e8edf4 | `--danger-soft` #3a1c1b | 13.11 | 4.5 | error card body |
| `--warn` #f19a55 | `--warn-soft` #3a2615 | 6.47 | 4.5 | high chip |
| `--ok` #6fcf97 | `--ok-soft` #13291e | 8.10 | 4.5 | sent badge, reconcile footer |
| `--text-2` #b4bfcf | `--ok-soft` #13291e | 8.28 | 4.5 | reconcile footer body |
| `--accent-text` #8fb3ea | `--ok-soft` #13291e | 7.19 | 4.5 | reconcile footer link |
| `--on-brand-panel` #e8edf4 | `--brand-panel` #111721 | 15.28 | 4.5 | sign-in brand side |
| `--on-brand-panel-2` #b4bfcf | `--brand-panel` #111721 | 9.67 | 4.5 | sign-in brand side, secondary |
| `--pending` #c9a35a | `--surface-1` #111721 | 7.59 | 3 | pending dot |
| `--open` #7aa5e6 | `--surface-1` #111721 | 7.15 | 3 | open dot |
| `--focus` #f0b44c | `--surface-0` #0b0f15 | 10.37 | 3 | focus ring |
| `--focus` #f0b44c | `--surface-1` #111721 | 9.70 | 3 | focus ring |
| `--focus` #f0b44c | `--surface-2` #18202c | 8.85 | 3 | focus ring |
| `--focus` #f0b44c | `--surface-3` #212c3d | 7.60 | 3 | focus ring |
| `--line-strong` #6b7a93 | `--surface-0` #0b0f15 | 4.42 | 3 | input border |
| `--line-strong` #6b7a93 | `--surface-1` #111721 | 4.13 | 3 | input border |
| `--line-strong` #6b7a93 | `--surface-2` #18202c | 3.77 | 3 | input border |
| `--accent` #7aa5e6 | `--surface-1` #111721 | 7.15 | 3 | selected bar, active tab |
