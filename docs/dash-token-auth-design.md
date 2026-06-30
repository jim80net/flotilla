# Design — token-gated non-loopback dash bind (#208)

**Status:** design v2 (design-trio folded — systems-review + OCR + STORM — then an adversarial re-verify: all 7 prior P1s confirmed closed + 4 new findings folded, incl. the load-bearing mux-pattern gate §3). PASS-ready → openspec next.
**Operator goal:** serve the fleet dash on `0.0.0.0:8787` so it is **phone-reachable**, WITHOUT exposing the unauthenticated trade-control surface to the network. The operator flips the live bind to `0.0.0.0:8787` on merge.

## The problem (why `validateBind` fail-closes today)

`internal/dash/server.go:validateBind` refuses any non-loopback bind. The reason is precise: the dash's state-changing surface — `POST /api/control/{route,notify,resume}` (deliver an instruction into a pane / post an operator note / restart a desk) — is **trade-control and fleet-control**, protected today only by `requireWrite`: a **browser-CSRF** defense (`X-Flotilla-Dash: 1` custom header + an Origin allowlist; `tracker_handlers.go:172-195`) plus the loopback bind.

**CSRF defense is NOT authentication.** `requireWrite` stops a *browser the operator is logged into* from being driven cross-site; it does nothing against a **direct, non-browser client** (curl on the LAN) that sets `X-Flotilla-Dash: 1` and a matching `Origin`. On loopback that's fine — the only clients are trusted local processes. On `0.0.0.0` it is catastrophic. The data reads (`/api/status|topology|history|issues`) and the SSE stream are also deployment recon/PII (every desk name, the fleet shape). A safe non-loopback bind needs a **real authentication gate** layered with the anti-CSRF + anti-DNS-rebinding defenses.

## Threat model on a non-loopback bind

| Threat | Defense |
|---|---|
| Unauthenticated network client hits `/api/control/*` or a data read | **Bearer token** required on every sensitive request, enforced **deny-by-default** (§3) |
| Logged-in browser driven cross-site (CSRF) to POST control | retained `requireWrite` (custom header + Origin) for **cookie**-auth writes + **SameSite=Strict** cookie |
| DNS-rebinding | **Host-allowlist** (existing), extended to configured external hosts; the unspecified bind host is NEVER self-allowed (§5) |
| Token/cookie sniffed on the wire (plaintext http) | **authentication ≠ confidentiality** — documented; a captured cookie is replayable for the TTL. Public-address bind warns/refuses; TLS via proxy for internet (§7) |
| Token brute-force / login-flood | **constant-time compare on fixed-length MACs** + ≥256-bit token enforced at config + **mandatory login rate-limit** (§6) |
| XSS steals the session | **HttpOnly** cookie; the page server-renders NO fleet data (`server.go:256-258`) |
| Auth-failure oracle | **single generic 401** for every failure mode (§4) |

## 1. The token — source, loader, precedence (folded: OCR/STORM "no loader")

The `Server` does **not** load secrets today (`SecretsPath` is consumed lazily only inside `control.NewLibrary`; `roster.Secrets` exposes only `BotToken()`/`Webhook()`). So this change adds the load path explicitly:

- **New accessor** `roster.Secrets.DashToken() string` → `s.vals["FLOTILLA_DASH_TOKEN"]` (mirrors `BotToken()`).
- **Resolution at the wiring boundary** (`cmd/flotilla/dash.go`, the one place permitted to resolve credentials, consistent with how `Transport`/`WebTransport` are injected): a `--dash-token` flag defaulting to `os.Getenv("FLOTILLA_DASH_TOKEN")`, else the secrets-file `DashToken()`. **Precedence: flag → env → secrets-file, first non-empty wins.** The resolved token is injected as a new `dash.Config.DashToken string`.
- `validateBind` (§6) is called with `cfg.DashToken != ""` as `tokenConfigured`.
- **Token is read once at process start.** Rotation requires a **dash restart** (and forces all sessions to re-login — the key changes). Documented as the rotation procedure; no live reload.
- **Minimum entropy enforced:** if `DashToken` is set and shorter than 32 bytes, `NewServer` errors (refuse to stand up the gate with a weak token — closes the realistic weak-operator-token case). Token never logged, echoed, server-rendered, or placed in any 401 body.

Backward-compat matrix:
- **Loopback + no token →** current posture unchanged (open reads, `requireWrite` CSRF on writes). Existing users set nothing.
- **Non-loopback + no token →** `validateBind` FAILS CLOSED, message names `FLOTILLA_DASH_TOKEN`.
- **Token configured (any bind) →** the deny-by-default auth gate (§3-4) is enforced, defense-in-depth even on loopback.

## 2. Two credential forms (authentication)

A request authenticates by EITHER:

- **(A) Bearer token** — `Authorization: Bearer <token>`. For API clients (curl) and the page's `fetch()`. **CSRF-immune** (a cross-site page can neither read the token nor set `Authorization` cross-origin).
- **(B) Session cookie** — `flotilla_dash_session`, **HttpOnly, SameSite=Strict** (+ `Secure` per §3), established at login. For the **browser**, and specifically **SSE** (`EventSource` cannot set `Authorization` — the sole reason the cookie form exists).

**Stateless HMAC cookie — fixed-width framing (folded: canonicalization-forgery P1-D):**
```
payload (56 bytes, fixed):  [ expiry: 8B big-endian uint64 ][ nonce: 16B random ][ mac: 32B ]
mac = HMAC-SHA256(key, expiry8 || nonce16)        // signs the EXACT fixed-width preimage — one parse only
key = HMAC-SHA256(FLOTILLA_DASH_TOKEN, "flotilla-dash-session-v1")   // domain-separated; raw token is never the cookie
cookie value = base64.RawURLEncoding(payload)
verify: base64-decode → REQUIRE len==56 (else generic 401 — a short input must never reach the slice; folded: re-verify P3-len-guard) → slice at fixed offsets [0:8],[8:24],[24:56] → recompute mac → hmac.Equal → check expiry>now
```
No length-extension (HMAC). No canonicalization ambiguity (fixed widths). Rotating the token changes `key` ⇒ every outstanding cookie fails `hmac.Equal`. TTL default 12h (`FLOTILLA_DASH_SESSION_TTL`, `time.ParseDuration`, 12h on empty/malformed). **The TTL is the replay window on a sniffable LAN** (§7) — pick it knowing that.

All secret comparisons use `crypto/subtle`/`hmac.Equal` on **fixed-length 32-byte MACs**, never raw variable-length tokens (a raw compare leaks token length via the length-mismatch early-exit). Bearer verify = `hmac.Equal(mac(key,"bearer"||token), mac(key,"bearer"||presented))` (or compare the configured token's HMAC to the presented token's HMAC — fixed 32B either way).

## 3. Deny-by-default gate (folded: the #1 structural fix — all three reviewers)

`requireAuth` is a **top-level middleware in `handler()`**, composed with `hostAllow` so it wraps **every** route — NOT a per-route opt-in (which is fail-open-by-omission: a future route added without the wrapper would ship open on `0.0.0.0`). It enforces an explicit **open-allowlist keyed on the mux-matched PATTERN**; everything else is authenticated:

```
handler() = hostAllow( requireAuth( mux ) )
openPatterns (exact deny-by-default allowlist of mux PATTERNS) = {
   "/",  "GET /static/",  "POST /api/auth/login",  "POST /api/auth/logout"
}
```
- **The gate keys on the MUX-MATCHED PATTERN, never a hand-rolled path match (folded: re-verify NEW-1).** `requireAuth` resolves the pattern the mux WILL dispatch via `_, pat := s.mux.Handler(r)` and checks `openPatterns[pat]`. Keying on `r.URL.Path` directly would be a SECOND router that can desync from the mux's path-cleaning/dispatch (`//`, `/static/../api/status`) — exactly the fail-open class deny-by-default exists to remove. Using the mux's own matched pattern makes the open-set decision identical to the dispatch decision, so it is truly gated-by-construction. (`mux.Handler(r)` does not execute the handler — it only returns the handler + matched pattern.) The bare `/` pattern is Go's catch-all: an unmatched path resolves to `/` → `handleIndex`, which 404s anything but exactly `/` — open but harmless (a 404, no data).
- A new route is **gated by construction** unless someone deliberately adds its pattern to `openPatterns`.
- `/static/*` is compile-time **embedded** assets only (`staticAssets()`) — never fleet-derived; stated as an invariant so no future dynamic file is served under the open prefix.
- `/` is static chrome (no server-rendered fleet data) + the **login UI** (§ below).
- `logout` is open-to-auth-state but **CSRF-gated** (a holder of an expired/None session must be able to clear the cookie; it must not require a valid session).

When the token is **not** configured (loopback back-compat), `requireAuth` is a **no-op** (every request passes) — the gate only engages once a token exists, preserving the current loopback posture exactly.

`requireAuth` records the **verified credential form** in `r.Context()` (`authBearer` | `authCookie` | `authNone`) for the CSRF composition (§4), **bearer-first (folded: re-verify P3-precision):** verify the `Authorization: Bearer` token first → on success set `authBearer`; else verify the cookie → `authCookie`; else `authNone`. Bearer-first means a valid-cookie + garbage-bearer cannot create a fall-through ambiguity (the garbage bearer fails, the cookie wins → `authCookie` → CSRF enforced).

## 4. Per-surface enforcement

| Surface (server.go route) | Auth (requireAuth, top-level) | CSRF |
|---|---|---|
| `GET /`, `GET /static/*` | **open** | — |
| `POST /api/auth/login` | **open** (it *establishes* auth) | **requireWrite** (custom header + Origin) — cannot be driven cross-site |
| `POST /api/auth/logout` | **open** (clears cookie) | **requireWrite** |
| `GET /api/status\|topology\|history` | **gated** (bearer or cookie) | — (reads) |
| `GET /api/issues`, `GET /api/issues/{n}` | **gated** (was un-named in v1; gated by deny-by-default) | — |
| `GET /events` (SSE) | **gated** — checked **before `hub.add`** (no pre-auth slot consumption) | — |
| `POST /api/issues/*`, `POST /api/control/*` | **gated** | **requireWrite IFF the verified form is `authCookie`**; a verified **bearer** skips the custom-header+Origin sub-check (CSRF-immune). The skip keys on the **context form set by `requireAuth`**, never on raw `Authorization`-header presence (folded: P2-A correctness hinge). |

**Generic 401 (folded: no-oracle S2):** every auth failure — absent/garbage/expired cookie, wrong token, no-token-configured — returns one identical `401 {"error":"authentication required"}`. No body distinguishes the mode; no token material ever appears.

**Login UI is NET-NEW work (folded: OCR High #3 — `index.html` is today static `{Bind,XO}`):** a token-entry view in `index.html`, the `fetch('/api/auth/login', {headers:{Authorization:'Bearer '+t, 'X-Flotilla-Dash':'1'}})` handshake + cookie establishment in `dash.js`, and the **"data fetch 401 → show login"** gate. The entered token lives only in the request; after the cookie is set the JS drops it (the HttpOnly cookie is the durable credential).

**SSE re-auth on expiry (folded: O1 — the sharp safety hazard):** `EventSource` cannot read a 401 and silently retries forever — so a lapsed session would leave the phone showing a **stale-but-connected-looking trade dashboard**. Required client behavior: on SSE `onerror`/reconnect, the page probes `GET /api/status`; a 401 there surfaces a **visible "session expired — re-enter token"** prompt and stops trusting the stale board (a clear staleness banner), rather than degrading silently.

## 5. Host + Origin allowlists for a non-loopback bind (folded: Origin gap + unspecified self-grant)

- **`FLOTILLA_DASH_HOSTS`** (flag `--dash-hosts` + env, comma-separated `host` or `host:port`; bare host → `host:bindport`) lists the phone-facing host(s) (LAN IP, tailscale name, a proxy hostname).
- **`buildHostAllowlist(bind, extraHosts)`** = loopback names ∪ `extraHosts` ∪ the bind host:port — **EXCEPT** when the bind IP `IsUnspecified()` (`0.0.0.0`/`::`), in which case the bind host is **NOT** added (an attacker `Host: 0.0.0.0:8787` must never pass anti-rebinding; only loopback + explicit `extraHosts` are allowable).
- **`buildOriginAllowlist(bind, extraHosts)`** is extended IDENTICALLY (the v1 design extended only Host — the gap that would 403 the phone's own cookie login/writes via `originAllowed`). Same unspecified-host exclusion. **Scheme (folded: re-verify NEW-2):** the allowlist is `http://<host>` for each form by default; when `--behind-tls-proxy` is set (§6), ALSO admit `https://<host>` — else the TLS-proxy case (§7), whose browser Origin is `https://<proxy-host>`, would 403 every cookie login/write in exactly the internet-exposure deployment the design blesses.
- Anti-DNS-rebinding preserved: an unknown `Host` is still `403`, token or not.

## 6. `validateBind` relaxation + guards (folded: unspecified + public-bind + rate-limit)

`validateBind(bind, tokenConfigured bool)`:
- Loopback → allowed (token optional).
- `IsUnspecified()` or any other non-loopback → allowed **IFF `tokenConfigured`**, else the fail-closed refusal naming `FLOTILLA_DASH_TOKEN`. (Unspecified is explicitly a non-loopback bind that requires the token.)
- **Public-address guard:** when the bind host is non-loopback AND **non-private** (not RFC-1918 `10/8`·`172.16/12`·`192.168/16`, CGNAT `100.64/10`, link-local, or `fc00::/7`/`fe80::/10`), serving plaintext trade-control to a routable address is almost never intended → **refuse** unless an explicit `--insecure-public-bind` override is set, and even then emit a loud stderr warning pointing at §7 (TLS). The realistic phone case (`192.168.x`/tailscale) is private → no friction.

**Mandatory login rate-limit (folded: S3/P2-C — promoted from optional):** a global token-bucket on `POST /api/auth/login` (a few lines, no deps) blunts a login-flood that would otherwise spin the HMAC compute, independent of brute-force. In-scope, not deferred.

**`Secure` cookie flag behind a TLS proxy (folded: E3/P3-C):** the dash is always plaintext http in-process (TLS terminates at a proxy, §7), so it cannot detect TLS from its listener. A `--behind-tls-proxy` flag (or trusting `X-Forwarded-Proto: https` **only** from an allowlisted proxy Host) makes `requireAuth`/login set the cookie `Secure`. Default off (plain-LAN http case sets no `Secure`, correct).

## 7. Documented limitation — plaintext http (operator must ACTIVELY accept)

The dash serves **plaintext http**; the token gate is **authentication, not confidentiality**. On the wire the bearer token (at login) and the session cookie (every request) are **sniffable**, and a captured cookie is **replayable for the TTL** (default 12h). Concrete home-LAN exposure: any other device on the WiFi — a guest phone, a compromised IoT, a malicious app — can passively capture the cookie and place trades until it expires.

- **Trusted private LAN (`192.168.x`/tailscale):** the operator's **active** risk acceptance — the realistic phone case. The §6 public-bind guard ensures this is the only no-TLS path that serves without an explicit override.
- **Internet exposure:** **TLS required** — terminate at a reverse proxy (caddy/nginx) or tunnel (tailscale/cloudflared) in front; the dash stays http behind it; `FLOTILLA_DASH_HOSTS` lists the proxy hostname; set `--behind-tls-proxy` so the cookie is `Secure`.

Pick the session TTL with the replay window in mind (shorter TTL = smaller window, more re-logins). This limitation is surfaced to the operator explicitly, not buried.

## 8. Also-closed in this phase (folded: P3-A)

`tracker_handlers.go` has a standing `TODO(dash, Phase 3)` to genericize verbatim `gh` stderr in error bodies (it can carry host paths / repo internals = recon). This IS Phase 3 → resolve it: a generic client message for 5xx, the detail to stderr only.

## Non-goals (this change)
- In-process TLS (use a proxy/tunnel — §7). Multi-user / per-operator tokens / roles (one shared operator token). Server-side session store / per-session revoke (token rotation revokes all — sufficient, restart-survivable). Changing the loopback posture for token-less users (back-compat preserved). Live token reload (rotation = restart).

## Verification plan
1. **Auth:** valid bearer → pass; valid cookie → pass; absent/garbage/expired/tampered cookie → generic 401; wrong token (constant-time) → generic 401; no-token+loopback → reads open (back-compat); token configured → reads/SSE/control all gated.
2. **Deny-by-default:** a synthetic new route added without an `openPaths` entry is **gated** (a test that registers a route and asserts 401 without auth) — proves the default is closed.
3. **validateBind:** loopback ± token ok; `0.0.0.0`/`::` + token ok; `0.0.0.0` + **no** token → error naming `FLOTILLA_DASH_TOKEN`; a public IP + token + no override → refused; + override → served with the warning.
4. **Cookie:** fixed-width HMAC round-trip; tamper expiry/nonce/mac → reject; expired → reject; token rotation (key change) → all old cookies reject; boundary-shift forgery attempt → reject (fixed-width framing).
5. **CSRF composition:** cookie-auth POST without `X-Flotilla-Dash` → 403; **bearer**-auth POST without it → allowed (verified-form-in-context, not header presence).
6. **Host/Origin:** configured external host passes both; unknown Host → 403 even with a valid token; `0.0.0.0` is NOT a self-allowed Host; the phone's `Origin: http://<lan-ip>:8787` passes `originAllowed` (the v1 gap).
7. **SSE:** auth checked before `hub.add` (unauth never takes a slot); `EventSource` from the loaded page carries the SameSite=Strict cookie (same-site) and authenticates; the on-expiry client probe → re-login prompt.
8. **Boundary/guard:** no token/secret in any fixture, log, rendered page, or 401 body; private-boundary guard PASS.
