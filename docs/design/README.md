# Flotilla design book

The visual language flotilla uses across its surfaces — the local **dash**
(`internal/dash/assets/`) and the public **landing site** (`site/`). One system,
so the product and its marketing read as the same thing.

Flotilla is **warm-light-first.** The dash ships a warm-paper theme as its
default: a calm parchment ground, near-white panels lifted by soft warm shadows
and crisp warm borders, a deep-teal signal, an ochre bus accent, and warm-ink
telemetry. The goal is an instrument you can read at a glance where **every
section is clearly its own card** — separation comes from surface contrast +
shadow + border, not from cramming color.

The source of truth for the tokens is the `:root` block in
`internal/dash/assets/dash.css`. When a token changes there, update this book;
every CSS snippet below is copied from that file verbatim.

---

## 1. Voice of the surface

A **fleet command console**, rendered on warm paper: a parchment ground, ivory
instrument panels, a phosphor-deep teal signal, an ochre accent for the
chat/audit bus, restrained condensed display type over a monospace body. Calm,
instrument-like, legible at a glance — not loud, not "techno", and deliberately
**not dark**. The dash is a working instrument; the landing page borrows its calm
so the product you install matches the page that sold it.

---

## 2. Token architecture — semantic layer + legacy aliases

The palette is expressed in two layers:

1. A **semantic layer** — `--ground / --surface / --raised / --card`, the two
   `--line*` borders, the `--ink*` scale, the accents, and the state colors. This
   is the canonical vocabulary. **To retheme, change values here.**
2. A **legacy-alias layer** — `--abyss* / --hull`, mapping the historical
   dark-theme names onto the semantic tokens so every existing `var(--abyss-2)`
   reference in the CSS keeps resolving. No JavaScript reads token names, so
   aliasing is safe; new rules should reach for the semantic names.

Every dark-toned wash, border, or glow that used to be a hard-coded `rgba()` of a
hue is now written as `color-mix(in srgb, var(--token) N%, transparent)`, so it
**re-derives from the token** instead of pinning a dead color. That is why a
single `:root` edit re-themes the whole instrument.

The `:root` block, verbatim:

```css
:root {
  /* ── surfaces (warm light) — page is the deepest warm tone so panels pop ── */
  --ground:     #efe7d8;   /* the page — warm parchment */
  --surface:    #faf5ec;   /* panels, threads, log panes — raised warm ivory */
  --raised:     #f5eee1;   /* chrome: headers, tab strips, inputs, detail bodies */
  --card:       #fffdf9;   /* top surface — hover/selected/active/node fills */

  /* ── lines — warm taupe, deliberately visible for section separation ── */
  --line:       #cdbb98;   /* primary border / hairline */
  --line-soft:  #e0d5c0;   /* faint dividers, grid, sub-panel edges */

  /* ── ink — warm near-black scale ── */
  --ink-1:      #1f1810;   /* strongest — active toggle labels */
  --ink:        #2b2318;   /* headings / high-emphasis */
  --ink-2:      #544632;   /* body text */
  --ink-3:      #6d5f42;   /* muted / meta / labels (AA on the page) */

  /* ── accents + state (deep, AA-as-text on the warm surfaces) ── */
  --cyan:       #017468;   /* primary signal — links, active, in-flight */
  --cyan-dim:   #2b7269;   /* secondary teal — kickers, owners, hover borders */
  --cyan-strong:#015a51;   /* primary-button hover (darker, not brighter) */
  --amber:      #8c5a0c;   /* ochre bus accent — chat/audit, awaiting-you */
  --violet:     #6f4ec9;   /* secondary accent — session output, speakers */
  --ok:         #1c7439;   /* success / idle / realized (green) */
  --ok-dim:     #1c7439;   /* realized swatch border */
  --warn:       var(--amber);
  --err:        #bd2f38;   /* error / blocked / crashed (red) */

  /* ── derived surface treatments (light-native elevation, not dark glow) ── */
  --chrome:     rgba(250, 246, 238, .82);        /* translucent header over blur */
  --scrim:      rgba(43, 35, 24, .34);           /* modal overlay — warm dark */
  --grid-line:  rgba(120, 100, 60, .10);         /* atmosphere + goals-canvas grid */
  --shadow-sm:  0 1px 2px rgba(74, 58, 30, .06);
  --shadow:     0 1px 2px rgba(74, 58, 30, .05), 0 4px 12px -2px rgba(74, 58, 30, .09);
  --shadow-lg:  0 18px 44px -16px rgba(58, 44, 20, .30);

  --display: "Barlow Condensed", ui-sans-serif, system-ui, sans-serif;
  --mono: "IBM Plex Mono", ui-monospace, "SF Mono", Menlo, monospace;
  --r: 10px;
  --r-sm: 6px;
  --r-xs: 3px;
  --ease: cubic-bezier(.22, 1, .36, 1);

  /* ── legacy aliases (dark-theme names → semantic light tokens) ── */
  --abyss:      var(--ground);
  --abyss-2:    var(--surface);
  --abyss-3:    var(--raised);
  --hull:       var(--card);
}
```

### Surfaces (the elevation ladder)

| Token | Hex | Role |
|---|---|---|
| `--ground` (`--abyss`) | `#efe7d8` | the page — the **deepest** warm tone, so panels sit above it |
| `--surface` (`--abyss-2`) | `#faf5ec` | panels, conversation threads, log panes — raised warm ivory |
| `--raised` (`--abyss-3`) | `#f5eee1` | chrome: headers, tab strips, inputs, detail bodies |
| `--card` (`--hull`) | `#fffdf9` | the top surface — hover / selected / active / node fills |

The ladder runs page → surface → raised → card, from the deepest warm paper up to
near-white. Contrast between rungs — plus a soft `--shadow` and a visible `--line`
— is what makes each section read as its own card. **This is the fix for "the
sections are hard to see."**

### Lines, ink, and accents

| Token | Hex | Role |
|---|---|---|
| `--line` | `#cdbb98` | primary border / hairline (deliberately visible) |
| `--line-soft` | `#e0d5c0` | faint dividers, grid, sub-panel edges |
| `--ink-1` | `#1f1810` | strongest ink — active toggle labels |
| `--ink` | `#2b2318` | headings / high-emphasis text |
| `--ink-2` | `#544632` | body text |
| `--ink-3` | `#6d5f42` | muted / meta / labels |
| `--cyan` | `#017468` | primary signal — links, active, in-flight |
| `--cyan-dim` | `#2b7269` | secondary teal — kickers, owners, hover borders |
| `--amber` | `#8c5a0c` | ochre bus accent — the chat/audit bus, awaiting-you |
| `--violet` | `#6f4ec9` | secondary accent — session output, per-speaker |
| `--ok` | `#1c7439` | success / idle / realized |
| `--err` | `#bd2f38` | error / blocked / crashed |

Accents are used **sparingly** — a surface is mostly paper + ink, with teal and
ochre carrying meaning, never decoration.

---

## 3. State colors — the semantics, and why these exact hues

The dash speaks a fixed status vocabulary. Each state maps to one hue, used on the
node/desk **rails**, the state **pills**, the goal **tiles**, and the graph
**edges**:

| State | Token | Meaning |
|---|---|---|
| realized / idle | `--ok` `#1c7439` | done & solidified; a desk resting at an idle composer |
| in-flight / working | `--cyan` `#017468` | a desk mid-turn; a goal being actively driven |
| awaiting-you | `--amber` `#8c5a0c` | an operator decision or authorization is gating progress |
| blocked / crashed / errored | `--err` `#bd2f38` | a genuine block or a dead process |
| aspirational | ghost `--ink-3`, dashed | planned, not yet started (receded, italic, dashed border) |

**Why they are deep.** On a light surface a bright status hue fails contrast as
text. These five are tuned so that **every one clears WCAG AA (≥ 4.5:1) as small
text on the warm surfaces** — verified against the page (`#efe7d8`), the panel
(`#faf5ec`), and the card (`#fffdf9`):

| Color | vs page | vs panel | vs card |
|---|---|---|---|
| `--ok` `#1c7439` | 4.74 | 5.28 | 5.66 |
| `--cyan` `#017468` | 4.62 | 5.14 | 5.51 |
| `--amber` `#8c5a0c` | 4.77 | 5.40 | 5.77 |
| `--err` `#bd2f38` | 4.71 | 5.33 | 5.70 |
| `--violet` `#6f4ec9` | 4.74 | 5.36 | 5.73 |

They also pass a colour-vision-deficiency separation check as a categorical set,
and — per data-viz practice — **never travel on color alone**: every state ships
with a text label, an icon, or a shaped rail, so a red/green-confusable viewer
still reads it. (The teal sits a hair under the categorical chroma floor, which is
acceptable precisely because it is a reserved status color that always carries a
label.)

**Fills and borders re-derive, they are not separate hexes.** A state wash is
`color-mix(in srgb, var(--state) N%, transparent)` (≈ 10–16% for a fill, ≈ 40–55%
for a border). Two consequences: a state tint is always a faithful tint of its
own token, and re-theming a state means editing **one** hex.

```css
.gpill-in-flight { color: var(--cyan); border-color: color-mix(in srgb, var(--cyan) 45%, transparent); background: color-mix(in srgb, var(--cyan) 10%, transparent); }
```

**GitHub label chips** are a special case: their hue comes from external data, not
our palette, and GitHub's defaults are light. So the chip keeps its text in
`--ink-2` and uses the incoming hue only as a faint tint + accent border (the hue
arrives as a `--label` custom property), guaranteeing legibility on paper for
*any* label color:

```css
.issue-label {
  font-size: .66rem;
  color: var(--ink-2);
  background: color-mix(in srgb, var(--label, var(--ink-3)) 16%, transparent);
  border: 1px solid color-mix(in srgb, var(--label, var(--ink-3)) 55%, var(--line));
  border-radius: 2px;
  padding: .05em .45em;
}
```

---

## 4. Typography

Two families, both from Google Fonts (unchanged across the light move):

- **Display — `Barlow Condensed`** (`--display`), weights 500/600/700. A calm
  condensed grotesque for headings, the brand, kickers, node titles, tab labels.
  Kickers/labels are **uppercase with positive letter-spacing** (`.12em`–`.22em`);
  large headlines sit near neutral tracking.
- **Body — `IBM Plex Mono`** (`--mono`), weights 400/500/600. The instrument body:
  prose, install commands, the teal inline accents, status text.

| Use | Size |
|---|---|
| Section heading (`h2`) | `.88rem` display, uppercase |
| Card / node title (`h3`) | `1.0–1.2rem` display 600 |
| Body | `13px` mono, line-height `1.55` |
| Kicker / eyebrow / label | `.55–.72rem` display/mono, uppercase, tracked |
| Meta / caption | `.62–.72rem` mono, `--ink-3` |

**Do:** condensed display for headings; mono for anything you'd read or type.
**Don't:** heavy geometric display weights or tight negative tracking — that reads
"techno startup", the opposite of the instrument voice.

---

## 5. Component patterns

- **Panel** — `--surface` fill, `1px solid --line` border, a soft `--shadow`, and
  `--r`/`--r-sm` radius. Raised chrome (headers, tab bars) uses `--raised`. The
  shadow + border is what lifts a panel off the parchment page.
- **Card** — a panel with a `--card` hover/selected fill and a `--cyan-dim` hover
  border; a small uppercase `--ink-3` eyebrow, a display title, mono body.
- **Status pill** — a small rounded chip; text in the state color, border + fill
  as `color-mix` derivations (§3). One pill per node/desk.
- **Harness badge** — a subdued, right-aligned uppercase micro-chip naming a
  surface (`grok`, `claude-code`, …). `--ink-3` on a `--line-soft` border.
- **Segmented toggle** — two/three flush buttons in a bordered group; the active
  one gets a `color-mix(in srgb, var(--cyan) 18%, transparent)` fill and `--ink-1`
  label. Used for the goals `tree|org` layout and `info|debug` verbosity toggles.
- **Buttons** — primary is a solid `--cyan` with a `--card` (near-white) label,
  darkening to `--cyan-strong` on hover; danger is `--err`; ghost is a `--line`
  border warming to `--cyan-dim` on hover. Mono label, small radius.
- **Goals canvas (command-chart)** — the hero pattern: an org node graph on a
  faint `--grid-line` canvas. Nodes are cards (`.gnode`, `--card`→`--raised`
  gradient) with a soft `--shadow-sm`, sized by scope (flotilla > desk > task);
  node border + pill color is live status (§3); realized nodes recede to a muted
  paper tone. In **org** layout the coordinator sits at center with straight spoke
  edges; in **tree** layout nodes stack in tiered altitude columns.
- **Atmosphere** — a fixed faint `--grid-line` graph grid (masked to fade at the
  edges, low opacity) plus a barely-there `multiply` grain. Subtle warmth; it sets
  the paper mood without competing with content.

Elevation on a light theme is a **shadow + border** problem, not a glow problem:
use `--shadow-sm` / `--shadow` / `--shadow-lg` (warm-brown, low-alpha) for lift;
the modal scrim is `--scrim` (warm dark), not black.

---

## 6. Motion & accessibility

- Motion is minimal: a 2.4s status pulse on the live-link dot, hover lifts, a slow
  in-flight node scan. All motion is disabled under `prefers-reduced-motion`.
- Everything meaningful is a real DOM element with a text label — map nodes, the
  status pills, the rails — so screen readers and keyboard users reach them, and
  state is never color-alone.
- **Contrast is AA by construction.** `--ink` / `--ink-2` clear AA for body and
  headings on every surface; `--ink-3` clears AA on the page; and every state
  color clears AA as text (§3). The focus ring is `2px solid var(--cyan)` — a deep
  teal that stays visible on paper.

---

## 7. Where this is used

- `internal/dash/assets/dash.css` — the source of the tokens; the live instrument.
- `internal/dash/assets/tracker.js` — passes GitHub label hues as `--label` so the
  chip styling stays in CSS (§3).
- `site/styles.css` — the landing site, styled to match (this book's first
  consumer beyond the dash itself).
- Future surfaces (docs themes, additional dash views) inherit from here.

_Keep this book generic — no deployment carries identifiers here; every example
uses the public example roles (`alpha-xo`, `research-desk`, `backend-desk`, …).
Keep it honest to the shipped `dash.css`: the `:root` block above is the contract._

---

## 8. Gallery — the theme in place

The move from the previous dark theme to the warm-light default. Sections that
blended into the dark ground now read as distinct cards on parchment.

**Before** — dark theme (sections hard to distinguish):

![Dash Conversations, previous dark theme](assets/before-dark-conversations-1440.png)

**After** — warm-light default:

| View | |
|---|---|
| Conversations | ![Conversations, warm light](assets/after-light-conversations-1440.png) |
| Goals — tree | ![Goals tree, warm light](assets/after-light-goals-tree-1440.png) |
| Goals — org | ![Goals org, warm light](assets/after-light-goals-org-1440.png) |
| Issues | ![Issues, warm light](assets/after-light-issues-1440.png) |
| Conversations (mobile, 390px) | ![Conversations mobile, warm light](assets/after-light-conversations-390.png) |

All example data above is generic (public example roles); no deployment
identifiers appear.
