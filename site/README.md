# flotilla landing page (`site/`)

The marketing landing page — a single static page, **no framework, no
backend**. Plain HTML + CSS, with a little vanilla JS for copy controls. It is
the first thing a friend clicking a shared link sees, so it is built to look
good on mobile (the product's whole pitch is "run it from your phone").

## Files

| file | what it is |
|------|------------|
| `index.html` | the page (hero, what / how / architecture / 30-second start / footer) |
| `styles.css` | the "fleet command console" theme, **warm light** — a warm-parchment ground, raised ivory panels, a deep-teal signal + ochre bus accent, warm ink; Barlow Condensed display (the dash face) over IBM Plex Mono. Matches the dash so product + marketing read as one system; see `docs/design/README.md` |
| `app.js` | copy-to-clipboard controls |
| `assets/` | responsive landing-page image payloads |

## Preview locally

```sh
cd site
python3 -m http.server 8000
# open http://localhost:8000
```

## Content contract

The seven-section order is fixed: `hero → feel → day → gives → yours → how →
start`. At 390px the complete document must remain at or below 7,200px without
horizontal overflow. Shipped claims stay deterministic HTML. The hero uses a
sanitized capture of the real product dashboard; decorative media is excluded.

## Product-proof assets

The hero uses responsive WebP captures in `assets/`. Its generic fixture data
contains no private hosts, usernames, repositories, tokens, internal paths, or
seat-to-deployment joins. The `yours` section is factual type and comparison
content only; it carries no decorative still life.

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
- **Truthful product proof.** The page names the six registered drivers, the
  exact bounded failover behavior, and five dashboard destinations. It does not
  present shipped capabilities as trial or roadmap work.
