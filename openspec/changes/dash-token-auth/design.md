# Design — token-gated non-loopback dash bind (`flotilla dash --bind 0.0.0.0:8787`)

**Status:** Design-trio gate PASSED (systems-review + open-code-review + STORM,
code-grounded) followed by an adversarial re-verify (all 7 prior P1s confirmed closed + 4
new findings folded). The canonical source is `docs/dash-token-auth-design.md`; this is a
TIGHT restatement that elevates the security invariants to first-class. Where this and the
source doc conflict, the source doc's re-verified text wins.

**Goal:** Serve the fleet dash on `0.0.0.0:8787` so it is phone-reachable, WITHOUT exposing
the unauthenticated trade-control surface to the network. The operator flips the live bind to
`0.0.0.0:8787` on merge.

This design EXTENDS the dash's existing `handler()` composition, `validateBind`,
`buildHostAllowlist`/`buildOriginAllowlist`, `requireWrite`, and the secrets loader. It does
NOT replace any of them, and the load-bearing invariant is that the **loopback + no-token
posture is byte-unchanged**.

---

## The security invariants (NON-NEGOTIABLE — each maps to a `dash-auth` requirement)

1. **DENY-BY-DEFAULT.** `requireAuth` is TOP-LEVEL in `handler()` (`hostAllow(requireAuth(mux))`),
   keyed on the **mux-matched PATTERN** (`mux.Handler(r)`), with an exact open-allowlist; every
   other route — including any route added later — is gated by construction when a token is
   configured. Keying on `r.URL.Path` would be a second router that can desync from the mux's
   path-cleaning/dispatch — the fail-open class deny-by-default exists to remove.

2. **FIXED-WIDTH STATELESS COOKIE.** The session cookie is a fixed 56-byte HMAC token; the
   HMAC signs the EXACT fixed-width preimage; verify requires `len==56` BEFORE any slice;
   compares are `hmac.Equal` on the 32-byte MAC; the key is domain-separated from the raw
   token; rotating the token revokes every outstanding cookie.

3. **BEARER-SKIPS-CSRF KEYED ON CONTEXT FORM.** A verified bearer skips `requireWrite`'s
   custom-header+Origin sub-check; a cookie write keeps it. The decision keys on the
   `authBearer`/`authCookie` form `requireAuth` recorded in `r.Context()` (bearer-first),
   NEVER on raw `Authorization`-header presence.

4. **TOKEN LOADER WITH PRECEDENCE + ENTROPY FLOOR.** `roster.Secrets.DashToken()` + the one
   wiring boundary resolves flag→env→secrets-file (first non-empty); `NewServer` errors on a
   token shorter than 32 bytes; the token is read once (rotation = restart) and never logged,
   echoed, rendered, or placed in a 401 body.

5. **`validateBind` RELAXATION, FAIL-CLOSED.** Non-loopback (incl. `IsUnspecified()`) is
   allowed IFF a token is configured, else fail-closed naming `FLOTILLA_DASH_TOKEN`; a
   public/non-private address is refused without `--insecure-public-bind` (+ a loud warning).

6. **HOST + ORIGIN ALLOWLISTS EXTENDED.** `FLOTILLA_DASH_HOSTS` is admitted into BOTH
   allowlists; the unspecified bind host is NEVER self-allowed; `https://<host>` is admitted
   only under `--behind-tls-proxy`.

7. **GENERIC 401, NO ORACLE.** Every auth failure returns one identical
   `401 {"error":"authentication required"}`; no body distinguishes the failure mode; no token
   material ever appears.

---

## 1. The token — source, loader, precedence

The `Server` does NOT load secrets today (`SecretsPath` is consumed lazily only inside
`control.NewLibrary`; `roster.Secrets` exposes only `BotToken()`/`Webhook()`). This change
adds the load path explicitly:

- **New accessor** `roster.Secrets.DashToken() string` → `s.vals["FLOTILLA_DASH_TOKEN"]`
  (mirrors `BotToken()`).
- **Resolution at the wiring boundary** (`cmd/flotilla/dash.go`, the one place permitted to
  resolve credentials, consistent with `Transport`/`WebTransport`): a `--dash-token` flag
  defaulting to `os.Getenv("FLOTILLA_DASH_TOKEN")`, else the secrets-file `DashToken()`.
  **Precedence: flag → env → secrets-file, first non-empty wins.** The resolved token is
  injected as a new `dash.Config.DashToken string`.
- `validateBind` (§6) is called with `cfg.DashToken != ""` as `tokenConfigured`.
- **Read once at process start.** Rotation requires a dash restart (the key changes ⇒ all
  sessions re-login). Documented; no live reload.
- **≥32-byte entropy floor:** a set-but-short `DashToken` makes `NewServer` error (refuse to
  stand up the gate with a weak token). The token is never logged/echoed/rendered or in a 401.

**Backward-compat matrix:**
- **Loopback + no token →** current posture unchanged (open reads, `requireWrite` CSRF on
  writes). Existing users set nothing.
- **Non-loopback + no token →** `validateBind` FAILS CLOSED, naming `FLOTILLA_DASH_TOKEN`.
- **Token configured (any bind) →** the deny-by-default gate engages (defense-in-depth even on
  loopback).

## 2. Two credential forms

A request authenticates by EITHER:
- **(A) Bearer** — `Authorization: Bearer <token>`. For curl + the page's `fetch()`.
  CSRF-immune (a cross-site page can neither read the token nor set `Authorization`
  cross-origin).
- **(B) Session cookie** — `flotilla_dash_session`, HttpOnly + SameSite=Strict (+ `Secure`
  under §6's TLS-proxy flag), established at login. For the browser and specifically SSE
  (`EventSource` cannot set `Authorization` — the sole reason the cookie form exists).

**Stateless HMAC cookie — fixed-width framing:**
```
payload (56 bytes, fixed):  [ expiry: 8B big-endian uint64 ][ nonce: 16B random ][ mac: 32B ]
mac = HMAC-SHA256(key, expiry8 || nonce16)        // signs the EXACT fixed-width preimage
key = HMAC-SHA256(FLOTILLA_DASH_TOKEN, "flotilla-dash-session-v1")   // domain-separated
cookie value = base64.RawURLEncoding(payload)
verify: base64-decode → REQUIRE len==56 (else generic 401) → slice [0:8],[8:24],[24:56]
        → recompute mac → hmac.Equal → check expiry>now
```
No length-extension (HMAC). No canonicalization ambiguity (fixed widths). Rotating the token
changes `key` ⇒ every outstanding cookie fails `hmac.Equal`. TTL default 12h
(`FLOTILLA_DASH_SESSION_TTL`, `time.ParseDuration`, 12h on empty/malformed). **The TTL is the
replay window on a sniffable LAN** (§7). All secret compares are `hmac.Equal` on the
fixed-length 32-byte MACs, never raw variable-length tokens.

## 3. Deny-by-default gate

`requireAuth` is a TOP-LEVEL middleware composed in `handler()`:
```
handler() = hostAllow( requireAuth( mux ) )
openPatterns (exact deny-by-default allowlist of mux PATTERNS) = {
   "/",  "GET /static/",  "POST /api/auth/login",  "POST /api/auth/logout"
}
```
- The gate resolves the pattern the mux WILL dispatch via `_, pat := s.mux.Handler(r)` and
  checks `openPatterns[pat]` — `mux.Handler(r)` returns the handler + matched pattern WITHOUT
  executing the handler. The bare `/` pattern is Go's catch-all: an unmatched path resolves to
  `/` → `handleIndex`, which 404s anything but exactly `/` (open but harmless — a 404, no data).
- A new route is gated by construction unless someone deliberately adds its pattern.
- `/static/*` is compile-time embedded assets only (`staticAssets()`) — never fleet-derived.
- `/` is static chrome (no server-rendered fleet data) + the login UI.
- `logout` is open-to-auth-state but `requireWrite`-gated (a holder of an expired/None session
  must clear the cookie without requiring a valid session).
- When the token is NOT configured, `requireAuth` is a NO-OP (every request passes), preserving
  the loopback posture exactly.
- `requireAuth` records the verified form in `r.Context()` (`authBearer`/`authCookie`/`authNone`)
  for the §4 CSRF composition, **bearer-first:** verify the bearer first → on success set
  `authBearer`; else verify the cookie → `authCookie`; else `authNone`. Bearer-first means a
  valid-cookie + garbage-bearer cannot create a fall-through ambiguity.

## 4. Per-surface enforcement

| Surface (server.go route) | Auth (requireAuth, top-level) | CSRF |
|---|---|---|
| `GET /`, `GET /static/*` | open | — |
| `POST /api/auth/login` | open (it establishes auth) | `requireWrite` (cannot be driven cross-site) |
| `POST /api/auth/logout` | open (clears cookie) | `requireWrite` |
| `GET /api/status\|topology\|history\|issues\|issues/{n}` | gated | — (reads) |
| `GET /events` (SSE) | gated — checked BEFORE `hub.add` (no pre-auth slot) | — |
| `POST /api/issues/*`, `POST /api/control/*` | gated | `requireWrite` IFF the verified form is `authCookie`; a verified bearer skips it (keyed on the context form, never raw header presence) |

**Generic 401:** every auth failure — absent/garbage/expired cookie, wrong token,
no-token-configured — returns one identical `401 {"error":"authentication required"}`.

**Login UI is NET-NEW** (`index.html` is today static `{Bind,XO}`): a token-entry view in
`index.html`, the `fetch('/api/auth/login', {headers:{Authorization:'Bearer '+t,
'X-Flotilla-Dash':'1'}})` handshake + cookie establishment in `dash.js`, and the
"data fetch 401 → show login" gate. The entered token lives only in the request; after the
cookie is set the JS drops it (the HttpOnly cookie is the durable credential).

**SSE re-auth on expiry:** `EventSource` cannot read a 401 and silently retries forever — a
lapsed session would leave the phone showing a stale-but-connected-looking trade dashboard.
On SSE `onerror`/reconnect the page probes `GET /api/status`; a 401 there surfaces a visible
"session expired — re-enter token" prompt and stops trusting the stale board.

## 5. Host + Origin allowlists for a non-loopback bind

- **`FLOTILLA_DASH_HOSTS`** (flag `--dash-hosts` + env, comma-separated `host` or `host:port`;
  bare host → `host:bindport`) lists the phone-facing host(s) (LAN IP, tailscale name, proxy
  hostname).
- **`buildHostAllowlist(bind, extraHosts)`** = loopback names ∪ `extraHosts` ∪ the bind
  host:port — EXCEPT when the bind IP `IsUnspecified()` (`0.0.0.0`/`::`), in which case the
  bind host is NOT added (an attacker `Host: 0.0.0.0:8787` must never pass anti-rebinding).
- **`buildOriginAllowlist(bind, extraHosts)`** is extended IDENTICALLY (the v1 design extended
  only Host — the gap that would 403 the phone's own cookie login/write via `originAllowed`).
  Same unspecified-host exclusion. Scheme is `http://<host>` per form by default; under
  `--behind-tls-proxy` ALSO `https://<host>` (else the TLS-proxy case, whose browser Origin is
  `https://<proxy-host>`, would 403 every cookie login/write).
- Anti-DNS-rebinding preserved: an unknown `Host` is still 403, token or not.

## 6. `validateBind` relaxation + guards

`validateBind(bind, tokenConfigured bool)`:
- Loopback → allowed (token optional).
- `IsUnspecified()` or any other non-loopback → allowed IFF `tokenConfigured`, else the
  fail-closed refusal naming `FLOTILLA_DASH_TOKEN`.
- **Public-address guard:** when the bind host is non-loopback AND non-private (not RFC-1918
  `10/8`·`172.16/12`·`192.168/16`, CGNAT `100.64/10`, link-local, `fc00::/7`/`fe80::/10`),
  serving plaintext trade-control to a routable address is almost never intended → REFUSE
  unless `--insecure-public-bind`, and even then emit a loud stderr warning pointing at §7.
  The realistic phone case (`192.168.x`/tailscale) is private → no friction.

**Mandatory login rate-limit:** a global token-bucket on `POST /api/auth/login` (a few lines,
no deps) blunts a login-flood that would spin the HMAC compute. In-scope, not deferred.

**`Secure` cookie behind a TLS proxy:** the dash is always plaintext http in-process (TLS
terminates at a proxy, §7). A `--behind-tls-proxy` flag makes `requireAuth`/login set the
cookie `Secure`. Default off (plain-LAN http sets no `Secure`, correct).

## 7. Documented limitation — plaintext http (operator must ACTIVELY accept)

The dash serves plaintext http; the token gate is **authentication, not confidentiality**. On
the wire the bearer token (at login) and the session cookie (every request) are sniffable, and
a captured cookie is replayable for the TTL (default 12h). Any other device on the WiFi can
passively capture the cookie and place trades until it expires.

- **Trusted private LAN (`192.168.x`/tailscale):** the operator's active risk acceptance — the
  realistic phone case. The §6 public-bind guard ensures this is the only no-TLS path that
  serves without an explicit override.
- **Internet exposure:** TLS required — terminate at a reverse proxy / tunnel in front; the
  dash stays http behind it; `FLOTILLA_DASH_HOSTS` lists the proxy hostname; set
  `--behind-tls-proxy` so the cookie is `Secure`.

Pick the session TTL with the replay window in mind. This limitation is surfaced to the
operator explicitly, not buried.

## 8. Also-closed in this phase

`tracker_handlers.go:304-307` has a standing `TODO(dash, Phase 3)` to genericize verbatim `gh`
stderr in 5xx error bodies (it can carry host paths / repo internals = recon). This IS Phase 3
→ resolve it: a generic client message for 5xx, the detail to stderr only.

## Non-goals (this change)

In-process TLS (use a proxy/tunnel — §7). Multi-user / per-operator tokens / roles (one shared
operator token). A server-side session store / per-session revoke (token rotation revokes all —
sufficient, restart-survivable). Changing the loopback posture for token-less users (back-compat
preserved). Live token reload (rotation = restart).
