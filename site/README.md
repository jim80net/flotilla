# flotilla landing page (`site/`)

The marketing landing page — a single static page, **no framework, no
backend**. Plain HTML + CSS, with a little vanilla JS only for the live
fleet-status widget. It is the first thing a friend clicking a shared link
sees, so it is built to look good on mobile (the product's whole pitch is "run
it from your phone").

## Files

| file | what it is |
|------|------------|
| `index.html` | the page (hero, what / how / architecture / live status / 30-second start / footer) |
| `styles.css` | the "fleet command console" theme, **warm light** — a warm-parchment ground, raised ivory panels, a deep-teal signal + ochre bus accent, warm ink; Barlow Condensed display (the dash face) over IBM Plex Mono. Matches the dash so product + marketing read as one system; see `docs/design/README.md` |
| `app.js` | copy-to-clipboard on the install one-liner + the live fleet-status widget |
| `status.json` | **SAMPLE** fleet status (generic desks) so the widget renders live today |
| `assets/` | drop the demo gif / asciinema cast here (see below) |

## Preview locally

```sh
cd site
python3 -m http.server 8000
# open http://localhost:8000
```

(The widget `fetch`es `./status.json`, so it must be served over HTTP —
opening `index.html` via `file://` will block the fetch.)

## Where the final copy slots in

The hero one-liner / hook is **not locked** yet. Every spot whose wording is
still a placeholder is tagged with an HTML comment so it is trivially findable:

```sh
grep -n "COPY:" index.html
```

The most important one is the `<h1 class="hero-headline">` — the draft
placeholder there is:

> *flotilla is a drop-in chief of staff for the AI coding agents you already
> run.*

Replace **only** the `<h1>` text (and the adjacent `<!-- COPY: HERO SUBHEAD -->`
sentence) with the operator's final hook; the surrounding structure is
message-independent. Other `COPY:` markers cover the meta description, section
ledes, CTA labels, the status pill, and the footer tagline.

## Where the demo asset goes

The hero reserves a framed demo panel. The static chat mock inside it renders
today as a stand-in; the real asset replaces it:

```sh
grep -n "ASSET:" index.html
```

Drop `demo.gif` (or an asciinema embed) into `assets/` and swap the
`.demo-canvas` contents per `assets/.gitkeep`.

## Wiring the widget to real `flotilla status --json`

`status.json` is a committed **sample** (the `flotilla status --json` command
isn't built yet). When it ships, the widget contract is an `agents` array of
`{ name, role?, surface?, state, task? }` where `state ∈
working | idle | awaiting | errored | offline` (synonyms like
`awaiting-approval`, `crashed`, `down` are normalized in `app.js`). Either
generate `status.json` from real output, or point the widget elsewhere by
changing the `data-src` on `#fleet-status` in `index.html`. The `TODO:`
comments in `index.html` and `app.js` mark these spots.

## Publishing (GitHub Pages) — operator's one step

The deploy workflow `.github/workflows/pages.yml` publishes this directory, but
it is **manual-only** (`workflow_dispatch`) and does **not** auto-go-live. To
make the site public the operator takes ONE step:

> **Settings → Pages → Build and deployment → Source: GitHub Actions**

Then run the **Deploy landing page (GitHub Pages)** workflow from the Actions
tab (or it runs on the next manual dispatch). Nothing publishes until that
source is enabled and the workflow is dispatched.

## Constraints honored

- **Generic only.** No private deployment details, hostnames, real IDs, or
  domain-specific terms appear anywhere. Example agents (`xo`, `backend`,
  `frontend`, `data`, `infra`) are generic, matching the public quickstart.
- **Honesty flags.** Shipped capabilities (surface drivers, inter-harness
  fleets, the clock, the relay) are separated from roadmap items (first-class
  modes, pluggable interfaces, richer reporting, the Cursor driver), per the
  README's roadmap.
