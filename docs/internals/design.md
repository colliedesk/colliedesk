# Web app design

Decisions behind the look of `web/`. The brand is **ink and paper** (owner decision 19/10/2026, replacing
Blue Merle): neutral grey surfaces, ink for text and the primary action, and colour only where it carries
meaning (status, focus, private notes; on colliedesk.com also the price). Light is the default theme and
dark is an opt-in. The app and the colliedesk.com site share one set of tokens.

## Brand guideline

Chosen from 20 researched directions after measuring nine helpdesk home pages (research and evidence:
https://design.hclabs.co, owner only). The site direction is "price ledger"; the app follows the same rules.

1. **Paper and ink.** Surfaces, lines and text are neutral grey with no hue; the primary action is ink
   (`#171717` light, `#fafafa` dark). `web/scripts/check-contrast.test.mjs` fails when a grey, a text
   token or the primary action carries a hue.
2. **Colour means something.** Status (open blue, pending amber, resolved green, urgent red, high orange),
   focus (amber) and private notes (amber) keep their colours; nothing else is coloured. On the site the
   price has its own colour (`--price`, orange) and nothing else does.
3. **Serif for titles only.** Instrument Serif (`--font-display`) sets page titles (Inbox, Settings,
   Requests, Billing, the sign-in heading and promise) and the site's h1/h2. Every control, label and
   body line stays IBM Plex Sans.
4. **Rings, not borders.** Cards and our replies sit on a 1px shadow ring (`--shadow-1`), so the eye reads
   layers by surface and ring, not by outlines.
5. **Links are ink and underlined** inside running text (`a:not([class])`); colour alone never marks a link.
6. **One product, one look.** Product pictures on colliedesk.com are screenshots of this app; when the app
   changes, the site's screenshots are retaken.

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
- Bulk actions: a checkbox at the left of each row and a **Select all on this page** row. Once a row
  is ticked, that row becomes the action bar (`role="group"`, named "Bulk actions"), as in Gmail and
  Gorgias: count on the left, then icon controls with a tooltip and an accessible name: **Resolve**
  (the most common bulk change, one press), **Set status**, **Assign to**, **Tags**, **Clear selection**.
  The title and filters stay visible. **Tags** is one menu, as in Linear: a check mark when every selected
  row has the tag, a dash when some do (`aria-pressed` true/mixed); a checked tag comes off, any other goes
  on. Each choice is a button in a `<details>` menu, never a `<select>`: a select fires on every arrow key
  in some browsers and one stray key would rewrite fifty conversations. Escape closes an open menu, a
  second Escape clears the selection. Success clears the selection and writes `role="status"`; failure
  writes `role="alert"` and keeps the selection. Focus follows the change: after success or Clear it goes
  to **Select all on this page** (the list pane when no rows remain); after a failure it stays on the menu used.
- Rows: bold subject and time, then one meta line (status dot and word, priority, `#tags`).
- Thread: incoming messages on `--surface-2`, our replies on white with a 1px ring (`--surface-1`,
  `--shadow-1`), private notes amber.
  Composer: a raised card with Reply / Private note tabs (keys **R** and **N**) and
  **Ctrl/Cmd + Enter** to send.
- Sign-in: a dark brand side (mascot, wordmark, one line of promise) beside the form; on narrow
  screens the brand side becomes a header.
- The mascot is the headset collie on an ink tile (see Logo).

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
| Lines | `--line` hairline dividers and panel edges, `--line-control` secondary button outline, `--line-strong` input borders | Inputs keep `--line-strong` (3:1). Buttons are identified by their label, but their outline is about 2.3:1 so they do not look faded (owner feedback 16/10/2026: buttons looked washed out at 1.5:1). |
| Text | `--text-1` primary, `--text-2` secondary, `--text-3` meta and labels, `--text-inverse` on the tooltip | `--text-3` is the lowest text tone. |
| Brand | `--accent` primary fill, `--accent-hover`, `--on-accent` text on accent, `--accent-text` links, `--accent-soft` selected row and reply bubble, `--accent-strong` graphics only, `--logo-tile`, `--brand-panel`, `--on-brand-panel(-2)` | One primary action per screen. Light: white on ink `#171717`. Dark: near-black on `#fafafa`. `--accent-text` is ink; inline links are underlined. |
| Focus and note | `--focus`, `--focus-ring`, `--note-bg`, `--note-line`, `--note-text` | Amber means focus or private note. Never use amber for status. |
| Status | `--danger`, `--warn`, `--ok`, `--open`, `--pending` and `*-soft` backgrounds | A dot or icon plus a word. Never colour alone. |
| Shape | `--radius` 6 px controls, `--radius-lg` 10 px panels, cards, composer, `--radius-sm` 4 px chips and keycaps, `--radius-full` avatars and tag chips | |
| Space | `--space-1` to `--space-24` on a 4 px grid, `--touch` 44 px | Touch targets are at least `--touch`. |
| Type | `--font-sans`, `--font-mono`, `--fs-ui` 14 px, `--fs-small` 13 px, `--fs-label` 12 px | See Type. |
| Elevation | `--shadow-1` cards, `--shadow-2` popovers and the composer, `--shadow-3` modals | |
| Motion | `--ease`, `--dur-fast` 120 ms, `--dur` 180 ms | Both become 0 under `prefers-reduced-motion`. |

Main values, light: app background `#fafafa`, panel `#ffffff`, text `#171717`, primary action `#171717`,
focus (amber) `#b86e00`. Dark: page `#0a0a0a`, panel `#141414`, text `#fafafa`, primary action `#fafafa`,
focus `#f0b44c`. Every text pair is at least 4.5:1; focus rings and input borders at least 3:1.
`web/scripts/check-contrast.mjs` checks every pair in both themes and runs in `pnpm test`.

## Type

- IBM Plex Sans (400, 500, 600, 700) for text, buttons, tabs, labels and time stamps, always in
  sentence case. Instrument Serif 400 (`--font-display`) only for page titles and the sign-in promise. IBM Plex Mono (400, 500) only for code, IDs, URLs and keycaps. No uppercase labels.
- Fonts are self-hosted: `web/src/styles/fonts/` (woff2 subsets latin, latin-ext, vietnamese) and
  `fonts.css`, licence SIL OFL 1.1 (`fonts/OFL-*.txt`). Never load a font from Google Fonts or a
  CDN, and never use Be Vietnam Pro.
- Body is `--fs-ui` 14 px; meta lines are 12 to 13 px.

## Logo

Owner decision 22/10/2026, chosen from concepts that pair a collie trait with a help desk object.
Source of every asset: `plans/261015-0932-colliedesk-brand-directions/logo-options/build-assets.py`
in the HC Labs workspace (it writes the favicon, `.ico`, apple-touch icon and both wordmark files).

- **Mark** (`web/src/ui/logo.tsx`, `LogoMark`): a collie in profile wearing a support headset. Wedge
  muzzle, tipped ear, white ruff, the mic running along the muzzle. Greys only (ink and paper). The white
  parts need a dark ground, so the mark always sits on its rounded tile (`--logo-tile`). 20 px or larger.
- **Favicon** (`web/public/favicon.svg`, `.ico`): a shepherd's crook drawn as a question mark, with a
  sheep for the dot. The headset collie turns to noise at 16 px; the crook stays legible. Same tile.
  `apple-touch-icon.png` (180 px) uses the mark, full bleed.
- **Wordmark** (`LogoWordmark` reads `web/public/wordmark-on-dark.svg`, kept out of the JS bundle; the
  site uses `public/logo-wordmark.svg`): outlined IBM Plex Sans,
  "collie" 700 and "desk" 400. The second "l" is tipped forward like a collie ear. Use the outlined
  version, never retype it in a font. In running text write "Colliedesk".
- Do not recolour, outline, rotate, glow or put a gradient on the mark. Do not put the bare mark
  (without its tile) on a photo, a light background or `--accent`.
- Trademark: not yet searched for this mark; search before any registration or print run.

## Rules

- Controls are 32 px with a mouse and 44 px on touch (`--control` switches on `pointer: coarse`;
  rail links, status tabs and list rows also grow to 44 px below 44 rem).
- Three button shapes only: primary (`--accent`, ink), secondary (white with a light outline and a small
  shadow), danger (red text).
- Focus is an amber outline, 2 px wide with a 2 px gap (`:focus-visible`). The gap is required:
  amber on `--accent` is low contrast in both themes, so the ring never touches a primary button. In the scrolling
  list the ring is inset (`outline-offset: -2px`) so the next row does not clip it.
- Form controls share the neutral surface and ink tokens. Every select draws the same chevron from
  `--select-chevron`, a per-theme token whose stroke equals `--text-3` (checked by
  `web/scripts/form-controls.test.mjs`); it stays visible when disabled. Checkboxes and radios share
  one rule in `base.css`: native controls with `accent-color: var(--accent)`, so checked and
  indeterminate states are ink in both themes; their labels keep readable text when disabled.
  Forced-colour mode keeps native checkbox and radio rendering and restores the native select arrow.
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
- Every empty list says which kind of empty it is and gives the next step: a new workspace gets
  **Get started** links, a filtered list with no match gets **Clear filters**, a failed load gets
  **Try again**.

## Review checklists

When a screen changes, check it against the matching lists on [checklist.design](https://www.checklist.design).
They are a gap finder, not a spec: where a list conflicts with a rule above, the rule wins.

| Surface | Checklists |
|---|---|
| Conversation list and filters | Empty State, Filtering items, Search Results, Data Table |
| Timeline and composer | Comments, Submitting a form, Showing input error |
| Settings and email channel | Settings, Integrations, Saving changes |
| Sign-in, invites, sessions | Login, User Management |
| Every screen | Accessibility, Loading, Toast, Drawer |

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
| `--text-1` #171717 | `--surface-0` #fafafa | 17.18 | 4.5 | text |
| `--text-1` #171717 | `--surface-1` #ffffff | 17.93 | 4.5 | text |
| `--text-1` #171717 | `--surface-2` #f4f4f4 | 16.30 | 4.5 | text |
| `--text-1` #171717 | `--surface-3` #ebebeb | 15.04 | 4.5 | text |
| `--text-2` #4d4d4d | `--surface-0` #fafafa | 8.10 | 4.5 | text |
| `--text-2` #4d4d4d | `--surface-1` #ffffff | 8.45 | 4.5 | text |
| `--text-2` #4d4d4d | `--surface-2` #f4f4f4 | 7.69 | 4.5 | text |
| `--text-2` #4d4d4d | `--surface-3` #ebebeb | 7.09 | 4.5 | text |
| `--text-3` #636363 | `--surface-0` #fafafa | 5.76 | 4.5 | text |
| `--text-3` #636363 | `--surface-1` #ffffff | 6.01 | 4.5 | text |
| `--text-3` #636363 | `--surface-2` #f4f4f4 | 5.46 | 4.5 | text |
| `--text-3` #636363 | `--surface-3` #ebebeb | 5.04 | 4.5 | text |
| `--accent-text` #171717 | `--surface-0` #fafafa | 17.18 | 4.5 | text |
| `--accent-text` #171717 | `--surface-1` #ffffff | 17.93 | 4.5 | text |
| `--accent-text` #171717 | `--surface-2` #f4f4f4 | 16.30 | 4.5 | text |
| `--accent-text` #171717 | `--surface-3` #ebebeb | 15.04 | 4.5 | text |
| `--danger` #c0261b | `--surface-0` #fafafa | 5.69 | 4.5 | text |
| `--danger` #c0261b | `--surface-1` #ffffff | 5.94 | 4.5 | text |
| `--danger` #c0261b | `--surface-2` #f4f4f4 | 5.40 | 4.5 | text |
| `--danger` #c0261b | `--surface-3` #ebebeb | 4.98 | 4.5 | text |
| `--warn` #a3460a | `--surface-0` #fafafa | 5.85 | 4.5 | text |
| `--warn` #a3460a | `--surface-1` #ffffff | 6.11 | 4.5 | text |
| `--warn` #a3460a | `--surface-2` #f4f4f4 | 5.55 | 4.5 | text |
| `--warn` #a3460a | `--surface-3` #ebebeb | 5.12 | 4.5 | text |
| `--ok` #11703d | `--surface-0` #fafafa | 5.90 | 4.5 | text |
| `--ok` #11703d | `--surface-1` #ffffff | 6.16 | 4.5 | text |
| `--ok` #11703d | `--surface-2` #f4f4f4 | 5.60 | 4.5 | text |
| `--ok` #11703d | `--surface-3` #ebebeb | 5.17 | 4.5 | text |
| `--accent-text` #171717 | `--accent-soft` #efefef | 15.59 | 4.5 | link on selected row |
| `--text-1` #171717 | `--accent-soft` #efefef | 15.59 | 4.5 | selected row |
| `--text-3` #636363 | `--accent-soft` #efefef | 5.22 | 4.5 | meta on selected row |
| `--on-accent` #ffffff | `--accent` #171717 | 17.93 | 4.5 | primary button |
| `--on-accent` #ffffff | `--accent-hover` #3d3d3d | 10.86 | 4.5 | primary button hover |
| `--text-inverse` #ffffff | `--surface-inverse` #171717 | 17.93 | 4.5 | tooltip |
| `--note-text` #7f5200 | `--note-bg` #fff6e0 | 6.27 | 4.5 | private note label |
| `--text-1` #171717 | `--note-bg` #fff6e0 | 16.66 | 4.5 | private note body |
| `--danger` #c0261b | `--danger-soft` #fdeceb | 5.20 | 4.5 | urgent chip |
| `--text-1` #171717 | `--danger-soft` #fdeceb | 15.69 | 4.5 | error card body |
| `--warn` #a3460a | `--warn-soft` #fdf0e5 | 5.46 | 4.5 | high chip |
| `--ok` #11703d | `--ok-soft` #e6f5ec | 5.47 | 4.5 | sent badge, reconcile footer |
| `--text-2` #4d4d4d | `--ok-soft` #e6f5ec | 7.50 | 4.5 | reconcile footer body |
| `--accent-text` #171717 | `--ok-soft` #e6f5ec | 15.91 | 4.5 | reconcile footer link |
| `--on-brand-panel` #fafafa | `--brand-panel` #0a0a0a | 18.97 | 4.5 | sign-in brand side |
| `--on-brand-panel-2` #a3a3a3 | `--brand-panel` #0a0a0a | 7.85 | 4.5 | sign-in brand side, secondary |
| `--pending` #a3730f | `--surface-1` #ffffff | 4.19 | 3 | pending dot |
| `--open` #2d62b3 | `--surface-1` #ffffff | 5.98 | 3 | open dot |
| `--focus` #b86e00 | `--surface-0` #fafafa | 3.82 | 3 | focus ring |
| `--focus` #b86e00 | `--surface-1` #ffffff | 3.99 | 3 | focus ring |
| `--focus` #b86e00 | `--surface-2` #f4f4f4 | 3.62 | 3 | focus ring |
| `--focus` #b86e00 | `--surface-3` #ebebeb | 3.34 | 3 | focus ring |
| `--line-strong` #858585 | `--surface-0` #fafafa | 3.54 | 3 | input border |
| `--line-strong` #858585 | `--surface-1` #ffffff | 3.69 | 3 | input border |
| `--line-strong` #858585 | `--surface-2` #f4f4f4 | 3.35 | 3 | input border |
| `--accent` #171717 | `--surface-1` #ffffff | 17.93 | 3 | selected bar, active tab |

### dark

| Foreground | Background | Ratio | Min | Use |
|---|---|---|---|---|
| `--text-1` #fafafa | `--surface-0` #0a0a0a | 18.97 | 4.5 | text |
| `--text-1` #fafafa | `--surface-1` #141414 | 17.65 | 4.5 | text |
| `--text-1` #fafafa | `--surface-2` #1c1c1c | 16.33 | 4.5 | text |
| `--text-1` #fafafa | `--surface-3` #262626 | 14.50 | 4.5 | text |
| `--text-2` #d4d4d4 | `--surface-0` #0a0a0a | 13.36 | 4.5 | text |
| `--text-2` #d4d4d4 | `--surface-1` #141414 | 12.43 | 4.5 | text |
| `--text-2` #d4d4d4 | `--surface-2` #1c1c1c | 11.50 | 4.5 | text |
| `--text-2` #d4d4d4 | `--surface-3` #262626 | 10.21 | 4.5 | text |
| `--text-3` #a3a3a3 | `--surface-0` #0a0a0a | 7.85 | 4.5 | text |
| `--text-3` #a3a3a3 | `--surface-1` #141414 | 7.30 | 4.5 | text |
| `--text-3` #a3a3a3 | `--surface-2` #1c1c1c | 6.76 | 4.5 | text |
| `--text-3` #a3a3a3 | `--surface-3` #262626 | 6.00 | 4.5 | text |
| `--accent-text` #fafafa | `--surface-0` #0a0a0a | 18.97 | 4.5 | text |
| `--accent-text` #fafafa | `--surface-1` #141414 | 17.65 | 4.5 | text |
| `--accent-text` #fafafa | `--surface-2` #1c1c1c | 16.33 | 4.5 | text |
| `--accent-text` #fafafa | `--surface-3` #262626 | 14.50 | 4.5 | text |
| `--danger` #f2837a | `--surface-0` #0a0a0a | 7.83 | 4.5 | text |
| `--danger` #f2837a | `--surface-1` #141414 | 7.28 | 4.5 | text |
| `--danger` #f2837a | `--surface-2` #1c1c1c | 6.74 | 4.5 | text |
| `--danger` #f2837a | `--surface-3` #262626 | 5.98 | 4.5 | text |
| `--warn` #f19a55 | `--surface-0` #0a0a0a | 8.95 | 4.5 | text |
| `--warn` #f19a55 | `--surface-1` #141414 | 8.33 | 4.5 | text |
| `--warn` #f19a55 | `--surface-2` #1c1c1c | 7.70 | 4.5 | text |
| `--warn` #f19a55 | `--surface-3` #262626 | 6.84 | 4.5 | text |
| `--ok` #6fcf97 | `--surface-0` #0a0a0a | 10.42 | 4.5 | text |
| `--ok` #6fcf97 | `--surface-1` #141414 | 9.69 | 4.5 | text |
| `--ok` #6fcf97 | `--surface-2` #1c1c1c | 8.97 | 4.5 | text |
| `--ok` #6fcf97 | `--surface-3` #262626 | 7.96 | 4.5 | text |
| `--accent-text` #fafafa | `--accent-soft` #222222 | 15.24 | 4.5 | link on selected row |
| `--text-1` #fafafa | `--accent-soft` #222222 | 15.24 | 4.5 | selected row |
| `--text-3` #a3a3a3 | `--accent-soft` #222222 | 6.31 | 4.5 | meta on selected row |
| `--on-accent` #0a0a0a | `--accent` #fafafa | 18.97 | 4.5 | primary button |
| `--on-accent` #0a0a0a | `--accent-hover` #e0e0e0 | 15.00 | 4.5 | primary button hover |
| `--text-inverse` #0a0a0a | `--surface-inverse` #fafafa | 18.97 | 4.5 | tooltip |
| `--note-text` #f2cd8a | `--note-bg` #2a2213 | 10.38 | 4.5 | private note label |
| `--text-1` #fafafa | `--note-bg` #2a2213 | 15.05 | 4.5 | private note body |
| `--danger` #f2837a | `--danger-soft` #3a1c1b | 6.10 | 4.5 | urgent chip |
| `--text-1` #fafafa | `--danger-soft` #3a1c1b | 14.77 | 4.5 | error card body |
| `--warn` #f19a55 | `--warn-soft` #3a2615 | 6.47 | 4.5 | high chip |
| `--ok` #6fcf97 | `--ok-soft` #13291e | 8.10 | 4.5 | sent badge, reconcile footer |
| `--text-2` #d4d4d4 | `--ok-soft` #13291e | 10.39 | 4.5 | reconcile footer body |
| `--accent-text` #fafafa | `--ok-soft` #13291e | 14.75 | 4.5 | reconcile footer link |
| `--on-brand-panel` #fafafa | `--brand-panel` #141414 | 17.65 | 4.5 | sign-in brand side |
| `--on-brand-panel-2` #a3a3a3 | `--brand-panel` #141414 | 7.30 | 4.5 | sign-in brand side, secondary |
| `--pending` #c9a35a | `--surface-1` #141414 | 7.78 | 3 | pending dot |
| `--open` #79a8ff | `--surface-1` #141414 | 7.77 | 3 | open dot |
| `--focus` #f0b44c | `--surface-0` #0a0a0a | 10.69 | 3 | focus ring |
| `--focus` #f0b44c | `--surface-1` #141414 | 9.95 | 3 | focus ring |
| `--focus` #f0b44c | `--surface-2` #1c1c1c | 9.20 | 3 | focus ring |
| `--focus` #f0b44c | `--surface-3` #262626 | 8.17 | 3 | focus ring |
| `--line-strong` #787878 | `--surface-0` #0a0a0a | 4.48 | 3 | input border |
| `--line-strong` #787878 | `--surface-1` #141414 | 4.17 | 3 | input border |
| `--line-strong` #787878 | `--surface-2` #1c1c1c | 3.86 | 3 | input border |
| `--accent` #fafafa | `--surface-1` #141414 | 17.65 | 3 | selected bar, active tab |
