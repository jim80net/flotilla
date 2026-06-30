# Proposal — token-gated non-loopback dash bind (#208)

## Why

The fleet dash (`flotilla dash`) is a phone-shaped operator surface, but
`internal/dash/server.go:validateBind` refuses any non-loopback bind, so the operator can
reach it only through an SSH tunnel. The operator wants to serve it on `0.0.0.0:8787` so it
is **phone-reachable** on the LAN — WITHOUT exposing its unauthenticated, state-changing
trade-control surface to the network.

The fail-closed bind is correct TODAY because the dash's only network defense is
`requireWrite` (`internal/dash/tracker_handlers.go:188-200`): a **browser-CSRF** defense
(the `X-Flotilla-Dash: 1` custom header + an Origin allowlist) plus the loopback bind.
**CSRF defense is not authentication.** `requireWrite` stops a logged-in browser from being
driven cross-site; it does nothing against a direct non-browser client (curl on the LAN)
that sets `X-Flotilla-Dash: 1` and a matching `Origin`. On loopback the only clients are
trusted local processes, so that is fine; on `0.0.0.0` it is catastrophic — any LAN host
could `POST /api/control/{route,notify,resume}` (deliver an instruction into a pane / post
an operator note / restart a desk) and read `/api/{status,topology,history,issues}` + the
SSE stream (every desk name, the whole fleet shape = recon/PII).

A safe non-loopback bind needs a **real authentication gate** layered with the existing
anti-CSRF + anti-DNS-rebinding defenses. This change adds that gate — a shared-operator
bearer token + an HMAC session cookie for the browser/SSE — enforced **deny-by-default** so
the trade-control surface is gated by construction, and it relaxes `validateBind` to permit
a non-loopback bind IFF a token is configured. The design cleared a full design-trio
(systems-review + open-code-review + STORM) plus an adversarial re-verify; the canonical
source is `docs/dash-token-auth-design.md` and every mechanism here restates it.

This is an UPGRADE that EXTENDS the dash's existing posture — it does NOT change the
loopback experience: **loopback + no token = the current behavior, byte-unchanged** (open
reads, `requireWrite` CSRF on writes). The gate engages only once a token exists.

## What changes

1. **A deny-by-default auth gate (`requireAuth`), TOP-LEVEL in `handler()`.** `requireAuth`
   wraps EVERY route (composed `hostAllow(requireAuth(mux))`), NOT a per-route opt-in. It
   keys an open-allowlist on the **mux-matched PATTERN** (`mux.Handler(r)`), not a
   hand-rolled `r.URL.Path` match — so the open-set decision is identical to the dispatch
   decision and a future route is **gated by construction**. The exact open set is
   `{"/", "GET /static/", "POST /api/auth/login", "POST /api/auth/logout"}`; everything else
   is authenticated. When NO token is configured, `requireAuth` is a **no-op** (loopback
   back-compat preserved exactly).

2. **Two credential forms.** (A) **Bearer** — `Authorization: Bearer <token>` for API
   clients and the page's `fetch()`, CSRF-immune. (B) **Session cookie** —
   `flotilla_dash_session`, HttpOnly + SameSite=Strict (+ Secure behind a TLS proxy), for
   the browser and specifically SSE (`EventSource` cannot set `Authorization`). The cookie is
   a **stateless fixed-width HMAC token**: `[8B BE expiry][16B nonce][32B HMAC]`, signed with
   a domain-separated key `HMAC-SHA256(token, "flotilla-dash-session-v1")`; verify requires
   `len==56`, slices at fixed offsets, `hmac.Equal` on the 32-byte MAC, and `expiry>now`.
   Rotating the token changes the key ⇒ every outstanding cookie fails — rotation revokes all.

3. **CSRF composition keyed on the VERIFIED FORM in `r.Context()`** (`authBearer` |
   `authCookie` | `authNone`), set bearer-first. A control POST keeps `requireWrite` IFF the
   verified form is `authCookie`; a verified **bearer** skips the custom-header+Origin
   sub-check (it is CSRF-immune). The skip keys on the context form, NEVER on raw
   `Authorization`-header presence.

4. **`validateBind(bind, tokenConfigured)` relaxation.** Loopback → allowed (token optional).
   `IsUnspecified()` (`0.0.0.0`/`::`) or any other non-loopback → allowed IFF
   `tokenConfigured`, else fail-closed naming `FLOTILLA_DASH_TOKEN`. A public/non-private
   (non-RFC-1918/CGNAT/link-local/ULA) address is REFUSED unless `--insecure-public-bind`,
   and even then a loud stderr warning points at the TLS limitation.

5. **Host + Origin allowlists extended for the external host.** `FLOTILLA_DASH_HOSTS`
   (flag + env, comma-separated) is admitted into BOTH `buildHostAllowlist` and
   `buildOriginAllowlist` (the v1 design extended only Host — the gap that would 403 the
   phone's own cookie login/write via `originAllowed`). Origin scheme is `http://<host>` by
   default, `https://<host>` ALSO when `--behind-tls-proxy`. The unspecified bind host
   (`0.0.0.0`/`::`) is NEVER self-allowed (anti-rebinding).

6. **The token loader.** A new `roster.Secrets.DashToken()` accessor + resolution at the
   wiring boundary (`cmd/flotilla/dash.go`): `--dash-token` flag → `FLOTILLA_DASH_TOKEN` env
   → secrets-file, first non-empty wins, injected as a new `dash.Config.DashToken`. The token
   must be ≥32 bytes (else `NewServer` errors), is read ONCE at start (rotation = restart),
   and is never logged, echoed, server-rendered, or placed in any 401 body.

7. **Generic 401 (no oracle), mandatory login rate-limit, pre-auth SSE gating, SSE re-auth
   client behavior.** Every auth failure returns one identical
   `401 {"error":"authentication required"}`. `POST /api/auth/login` carries a mandatory
   token-bucket rate-limit. SSE auth is checked BEFORE `hub.add` (no pre-auth slot
   consumption — satisfied by the top-level gate). On SSE reconnect the page probes
   `/api/status`; a 401 there surfaces a visible "session expired" banner and stops trusting
   the stale board (rather than `EventSource` silently retrying behind a stale dashboard).

8. **Login UI + login/logout endpoints (NET-NEW).** A token-entry view in `index.html`, the
   `fetch('/api/auth/login', …)` handshake + cookie establishment in `dash.js`, the data-fetch
   `401 → show login` gate, and `POST /api/auth/{login,logout}` (login mints the cookie,
   logout clears it; both `requireWrite`-gated, login open-to-auth so it can establish auth).

9. **Close the `tracker_handlers.go` Phase-3 TODO.** Genericize verbatim `gh` stderr in
   5xx error bodies (it can carry host paths / repo internals = recon); the detail goes to
   stderr only.

## Impact

- **Affected specs:**
  - `dash-auth` (**NEW capability**) — the deny-by-default gate, the two credential forms +
    the fixed-width cookie, the bearer-skips-CSRF context composition, the `validateBind`
    relaxation + bind guards, the Host+Origin allowlist extension, the token loader, the
    generic 401 + login rate-limit + SSE gating/re-auth, the login UI, the back-compat
    invariant, and the genericized tracker error body.
- **Affected code:** `internal/roster/secrets.go` (+`DashToken()`); `cmd/flotilla/dash.go`
  (token/hosts/ttl/tls-proxy/insecure-public-bind flags+env; `Config` injection);
  `internal/dash/server.go` (`Config` fields; `requireAuth` top-level gate; the cookie
  mint/verify; `validateBind(bind, tokenConfigured)`; `buildHostAllowlist`/
  `buildOriginAllowlist` extension; login/logout handlers; the ≥32-byte + rate-limit guards);
  `internal/dash/tracker_handlers.go` (`requireWrite` bearer-skip via context; genericized
  5xx body); `internal/dash/sse.go` (gated by the top-level `requireAuth`, no change to the
  hub); `internal/dash/assets/{index.html,dash.js}` (the login view + handshake + SSE
  re-auth probe).
- **No behavior change on the loopback path:** with no token configured, `requireAuth` is a
  no-op, `validateBind` keeps refusing non-loopback, and the reads stay open with
  `requireWrite` CSRF on writes — the current posture, byte-unchanged.

## Trio findings folded (systems-review + open-code-review + STORM — design gate)

The design (`docs/dash-token-auth-design.md`) cleared the trio AND an adversarial re-verify;
the load-bearing refinements are encoded as testable requirements here (where a finding
conflicts with the source framing, the finding wins):

- **Deny-by-default keyed on the mux-matched pattern (NEW-1):** keying on `r.URL.Path`
  directly is a SECOND router that can desync from the mux's path-cleaning (`//`,
  `/static/../api/status`); keying on `mux.Handler(r)` makes the open-set decision identical
  to dispatch — truly gated-by-construction.
- **Fixed-width cookie framing (P1-D):** the HMAC signs the exact 56-byte preimage, verify
  requires `len==56` before any slice, eliminating canonicalization-forgery / boundary-shift.
- **Bearer-skips-CSRF keyed on context form, bearer-first (P2-A / P3-precision):** never on
  raw header presence; bearer-first so a valid-cookie + garbage-bearer cannot create a
  fall-through ambiguity.
- **Origin allowlist extension + unspecified-not-self-allowed (Origin gap / self-grant):**
  extend BOTH allowlists; never admit the `0.0.0.0`/`::` bind host as a self-Host/Origin.
- **No loader today (OCR/STORM):** the `Server` does not load secrets; the change adds the
  `DashToken()` accessor + the explicit flag→env→secrets-file resolution at the one wiring
  boundary, plus the ≥32-byte minimum.
- **TLS-proxy Origin scheme (NEW-2) + Secure cookie (E3):** admit `https://<host>` and set
  the cookie `Secure` only under `--behind-tls-proxy`, so the internet-via-proxy case the
  design blesses does not 403 every cookie login/write.
- **SSE re-auth hazard (O1):** `EventSource` cannot read a 401 and retries forever; the page
  must probe `/api/status` on reconnect and surface a staleness banner.
- **Mandatory login rate-limit (S3/P2-C):** promoted from optional — in-scope, not deferred.

## Not in

- In-process TLS (use a reverse proxy / tunnel — the documented plaintext limitation).
- Multi-user / per-operator tokens / roles (one shared operator token).
- A server-side session store / per-session revoke (token rotation revokes all — restart-
  survivable, sufficient).
- Live token reload (rotation = restart).
- Changing the loopback posture for token-less users (back-compat preserved).
