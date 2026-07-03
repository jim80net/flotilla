# Flotilla design book (seed)

The visual language flotilla uses across its surfaces — the local **dash**
(`internal/dash/assets/`) and the public **landing site** (`site/`). One system,
so the product and its marketing read as the same thing.

This is a *seed*: the tokens and patterns extracted from the shipped dash theme,
documented so the next surface inherits them instead of reinventing a look. It is
**generic** — no deployment carries identifiers here; every example uses the
public example roles (`xo`, `backend`, `frontend`, `data`, `infra`).

The source of truth for the tokens is `internal/dash/assets/dash.css` (the
`:root` block). When a token changes there, update this book.

---

## 1. Voice of the surface

A **fleet command console**: a deep abyssal-navy ground, a phosphor-cyan signal,
an amber accent for the audit/bus, restrained condensed display type over a
monospace body. Calm, instrument-like, legible at a glance — not loud, not
"techno". The dash is a working instrument; the landing page borrows its calm so
the product you install matches the page that sold it.

---

## 2. Palette tokens

Dark-first. One ground family (abyss/hull), one primary signal (cyan), one accent
(amber), one secondary accent (violet), a three-step ink ramp, and status colors.

| Token | Hex | Role |
|---|---|---|
| `--abyss` | `#060a12` | page ground |
| `--abyss-2` | `#0a1019` | panels |
| `--abyss-3` | `#0e1622` | raised surfaces (chrome, headers) |
| `--hull` | `#131d2b` | cards / hover fill |
| `--line` | `#1d2b3e` | hairlines / borders |
| `--line-soft` | `#16212f` | faint dividers, grid |
| `--cyan` | `#4fe3d0` | primary signal — links, active, in-flight |
| `--cyan-dim` | `#2a9f93` | secondary cyan — kickers, muted accents |
| `--amber` | `#ffb454` | accent — the chat/audit bus, awaiting-you |
| `--violet` | `#b088f0` | secondary accent — session output, speakers |
| `--ink` | `#e8f0f7` | high-emphasis text, headings |
| `--ink-2` | `#a7b8c9` | body text |
| `--ink-3` | `#6b8096` | muted / meta / labels |
| `--ok` | `#6ee787` | success / idle / realized |
| `--warn` / `--amber` | `#ffb454` | warning / awaiting |
| `--err` | `#ff6b6b` | error / blocked / crashed |

**Status color mapping** (used by both the dash node states and the fleet-status
widget): in-flight → cyan, idle/realized → ok-green, awaiting-you → amber,
blocked/crashed → err-red, aspirational → ghosted `--ink-3`.

Accents are used *sparingly* — a surface is mostly ground + ink, with cyan/amber
carrying meaning, never decoration.

---

## 3. Typography

Two families, both loaded from Google Fonts:

- **Display — `Barlow Condensed`** (`--display`). Weights 500/600/700. A calm
  condensed grotesque. Used for headings, the brand, kickers, node titles, tab
  labels. Condensed = compact headlines that don't shout. Kickers/labels are
  **uppercase with positive letter-spacing** (`.12em`–`.22em`); large headlines
  sit near neutral tracking.
- **Body — `IBM Plex Mono`** (`--mono`). Weights 400/500/600. The instrument
  body: prose, code, install commands, the cyan inline accents, status text.

Scale (fluid, `clamp()` on the landing; fixed rem in the dash):

| Use | Size |
|---|---|
| Hero headline | `clamp(2rem, 5vw, 3.2rem)` display 700 |
| Section heading (`h2`) | `clamp(1.6rem, 4vw, 2.4rem)` display |
| Card / node title (`h3`) | `1.0–1.2rem` display 600 |
| Body | `1rem` mono, line-height `1.6` |
| Kicker / eyebrow / label | `.6–.74rem` mono/display, uppercase, tracked |
| Meta / caption | `.62–.72rem` mono, `--ink-3` |

**Do:** condensed display for headings; mono for everything you'd read or type.
**Don't:** heavy geometric display weights or tight negative tracking — that
reads "techno startup", the opposite of the instrument voice.

---

## 4. Component patterns

- **Panel** — `--abyss-2` fill, `1px solid --line` border, `--r`/`--r-sm` radius.
  Raised chrome (headers, tab bars) uses `--abyss-3`.
- **Card** — a panel with a `--hull` hover fill and a `--cyan-dim` hover border;
  a small uppercase `--ink-3` eyebrow, a display title, mono body.
- **Status pill** — a small rounded chip; text/border colored by the status map
  (§2). One pill per node/desk.
- **Harness badge** — a subdued, right-aligned uppercase micro-chip naming a
  surface (`grok`, `claude-code`, …). `--ink-3` on a `--line-soft` border.
- **Segmented toggle** — two/three flush buttons in a bordered group; the active
  one gets a `color-mix(in srgb, var(--cyan) 18%, transparent)` fill. Used for the
  dash `tree|org` layout and `info|debug` verbosity toggles.
- **Goals canvas (command-chart)** — the hero pattern: an org node graph. Nodes
  are cards (`.gnode`) sized by scope (flotilla > desk > task); in **org** layout
  the coordinator sits at the visual center and org units orbit on rings with
  straight **spoke** edges (`--cyan` at low opacity); in **tree** layout they
  stack in tiered altitude columns. Node color is live status (§2).
- **Atmosphere** — a fixed faint `--line-soft` signal-grid (masked to fade at the
  edges) + a low-opacity grain overlay + a soft cyan aurora behind the hero.
  Subtle; it sets the console mood without competing with content.
- **Buttons** — primary is a solid `--cyan` on dark ink; ghost is a `--line`
  border that warms to `--cyan-dim` on hover. Mono label, small radius.

---

## 5. Motion & accessibility

- Motion is minimal: a staggered hero rise, a 2.4s status pulse, hover lifts.
- Everything meaningful is a real DOM element with a text label — the map nodes,
  the status widget, the pills — so screen readers and keyboard users reach them.
- All motion is disabled under `prefers-reduced-motion`.
- Contrast: `--ink`/`--ink-2` on the abyss grounds clear AA for body and headings.

---

## 6. Where this is used

- `internal/dash/assets/dash.css` — the source of the tokens; the live instrument.
- `site/styles.css` — the landing site, styled to match (this book's first
  consumer beyond the dash itself).
- Future surfaces (docs themes, additional dash views) inherit from here.

_This seed grows as surfaces are added. Keep it generic; keep it honest to the
shipped `dash.css`._
