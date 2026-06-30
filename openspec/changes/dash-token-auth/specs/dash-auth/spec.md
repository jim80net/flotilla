# dash-auth Specification (delta)

## ADDED Requirements

### Requirement: A top-level deny-by-default auth gate keyed on the mux-matched pattern

The system SHALL enforce dash authentication through a single top-level `requireAuth`
middleware composed in `handler()` as `hostAllow(requireAuth(mux))`, so it wraps EVERY route —
reads, SSE, tracker writes, and control — rather than a per-route opt-in (which is
fail-open-by-omission). `requireAuth` SHALL resolve the route the mux WILL dispatch via the
mux's own matched pattern (`mux.Handler(r)` — which returns the handler and the matched
pattern WITHOUT executing the handler) and SHALL gate on that PATTERN, never on a hand-rolled
`r.URL.Path` comparison (a second router that can desync from the mux's path-cleaning and
dispatch). The open-allowlist SHALL be exactly the mux patterns
`{"/", "GET /static/", "POST /api/auth/login", "POST /api/auth/logout"}`; every other matched
pattern SHALL require a verified credential when a token is configured. A route added later
without its pattern in the open-allowlist SHALL therefore be gated by construction.

#### Scenario: A configured-token request to a gated route without a credential is refused

- **WHEN** a token is configured and a request arrives at any pattern outside the open-allowlist
  (e.g. `GET /api/status`, `GET /events`, `POST /api/control/route`) with neither a valid bearer
  nor a valid session cookie
- **THEN** the request is refused before reaching the route handler

#### Scenario: A synthetic new route is gated by construction

- **WHEN** a new route pattern is registered on the mux and a token is configured, but its
  pattern is NOT added to the open-allowlist, and a request hits it without a credential
- **THEN** the gate refuses the request (the default is closed) without the new route author
  having to opt in to authentication

#### Scenario: The gate keys on the dispatched pattern, not the raw path

- **WHEN** a request path that path-cleans/dispatches to a gated handler is crafted to look like
  an open path (e.g. `/static/../api/status`)
- **THEN** the gate resolves the mux-matched pattern (the gated `GET /api/status`) and refuses,
  because the open-set decision is the same decision the mux uses to dispatch

#### Scenario: The open chrome and login routes are reachable without a credential

- **WHEN** a token is configured and a request hits `/`, `GET /static/<asset>`,
  `POST /api/auth/login`, or `POST /api/auth/logout` with no credential
- **THEN** the gate allows it (these establish or clear auth, or serve static chrome), while
  every other route stays gated

### Requirement: The auth gate is a no-op when no token is configured (loopback back-compat)

The system SHALL treat a dash with NO configured token exactly as it behaves today: `requireAuth`
SHALL pass every request through unchanged, the reads SHALL stay open, and `requireWrite` SHALL
remain the only write gate. The token-gated authentication path SHALL engage ONLY once a token is
configured, so an existing loopback user who sets nothing observes byte-unchanged behavior.

#### Scenario: No token means the current loopback posture, unchanged

- **WHEN** the dash runs on loopback with no `FLOTILLA_DASH_TOKEN` configured
- **THEN** reads are open, writes are gated by `requireWrite` only, and no request is ever
  refused for missing a bearer or cookie — identical to the pre-change behavior

#### Scenario: A configured token engages the gate even on loopback

- **WHEN** a token IS configured and the dash runs on loopback
- **THEN** the deny-by-default gate engages (defense-in-depth), so gated routes require a verified
  credential even though the bind is loopback

### Requirement: Two credential forms — bearer and a stateless fixed-width HMAC session cookie

The system SHALL accept EITHER of two credential forms on a gated request: (A) a bearer token in
`Authorization: Bearer <token>` (for API clients and the page's `fetch()`), which is CSRF-immune;
or (B) a `flotilla_dash_session` cookie set `HttpOnly` and `SameSite=Strict` (for the browser and
specifically SSE, because `EventSource` cannot set `Authorization`). The cookie SHALL be a
stateless fixed-width 56-byte HMAC token framed as `[expiry: 8B big-endian uint64][nonce: 16B
random][mac: 32B]`, where `mac = HMAC-SHA256(key, expiry8 || nonce16)` signs the EXACT fixed-width
preimage and `key = HMAC-SHA256(FLOTILLA_DASH_TOKEN, "flotilla-dash-session-v1")` is
domain-separated so the raw token is never the cookie. Cookie verification SHALL base64-decode the
value, REQUIRE the decoded length to be exactly 56 (a short input never reaches the slice), slice
at the fixed offsets `[0:8]`/`[8:24]`/`[24:56]`, recompute the MAC, compare with `hmac.Equal` on
the fixed-length 32-byte MAC, and require `expiry > now`. All secret comparisons SHALL use
`hmac.Equal`/`crypto/subtle` on fixed-length MACs, never on raw variable-length tokens.

#### Scenario: A valid bearer authenticates a gated request

- **WHEN** a request to a gated route carries `Authorization: Bearer <the-configured-token>`
- **THEN** it is authenticated and reaches the route handler

#### Scenario: A valid session cookie authenticates a gated request

- **WHEN** a request to a gated route carries a `flotilla_dash_session` cookie minted at login
  whose MAC verifies and whose expiry is in the future
- **THEN** it is authenticated and reaches the route handler

#### Scenario: A tampered or wrong-length cookie is rejected before slicing

- **WHEN** a cookie's expiry, nonce, or MAC bytes are altered, or its decoded length is not
  exactly 56 bytes
- **THEN** verification fails (the length guard rejects a short input before any slice; a tampered
  MAC fails `hmac.Equal`) and the request is treated as unauthenticated

#### Scenario: An expired cookie is rejected

- **WHEN** a structurally valid cookie's embedded expiry is at or before now
- **THEN** verification fails and the request is treated as unauthenticated

#### Scenario: Rotating the token revokes every outstanding cookie

- **WHEN** `FLOTILLA_DASH_TOKEN` is rotated and the dash restarts, and a client presents a cookie
  minted under the previous token
- **THEN** the cookie fails `hmac.Equal` (the derived key changed) and is rejected — all prior
  sessions are revoked by the rotation

### Requirement: The verified credential form is recorded in the request context, bearer-first

The system SHALL record the verified credential form in `r.Context()` as one of `authBearer`,
`authCookie`, or `authNone`, resolved bearer-first: `requireAuth` SHALL verify the bearer token
first and on success record `authBearer`; otherwise verify the cookie and on success record
`authCookie`; otherwise record `authNone`. Downstream CSRF composition SHALL key on this recorded
form, never on the raw presence of an `Authorization` header, so a valid cookie accompanied by a
garbage bearer resolves unambiguously to `authCookie` (the garbage bearer fails first, the cookie
wins).

#### Scenario: Bearer-first resolves a valid cookie plus a garbage bearer to authCookie

- **WHEN** a request carries a malformed `Authorization: Bearer <garbage>` AND a valid session
  cookie
- **THEN** the bearer verification fails, the cookie verification succeeds, and the context form is
  `authCookie` (so the write path keeps `requireWrite`)

#### Scenario: A verified bearer records authBearer

- **WHEN** a request carries a valid bearer token
- **THEN** the context form is `authBearer` regardless of any cookie present

### Requirement: A verified bearer skips the CSRF sub-check; a cookie write keeps requireWrite

The system SHALL keep `requireWrite`'s browser-CSRF sub-check (the `X-Flotilla-Dash: 1` custom
header + the Origin allowlist) for a state-changing request whose verified form is `authCookie`,
AND SHALL skip that sub-check for a state-changing request whose verified form is `authBearer`
(a bearer is CSRF-immune — a cross-site page can neither read the token nor set `Authorization`
cross-origin). The skip SHALL key on the `authBearer` form recorded in `r.Context()` by
`requireAuth`, NEVER on the raw presence of an `Authorization` header.

#### Scenario: A cookie-auth control POST without the custom header is refused

- **WHEN** a `POST /api/control/route` is authenticated by a session cookie but omits
  `X-Flotilla-Dash: 1` (or carries a disallowed Origin)
- **THEN** `requireWrite` refuses it (the CSRF sub-check still applies to cookie writes)

#### Scenario: A bearer-auth control POST without the custom header is allowed

- **WHEN** a `POST /api/control/route` is authenticated by a valid bearer and omits
  `X-Flotilla-Dash: 1`
- **THEN** the CSRF sub-check is skipped (keyed on the context `authBearer` form) and the request
  reaches the handler

### Requirement: validateBind permits a non-loopback bind only when a token is configured

The system SHALL relax `validateBind` to take `tokenConfigured bool`: a loopback bind SHALL be
allowed with or without a token; a non-loopback bind — INCLUDING an unspecified address
(`IsUnspecified()`: `0.0.0.0`/`::`) — SHALL be allowed IFF a token is configured, and SHALL
otherwise fail closed with an error that names `FLOTILLA_DASH_TOKEN`. A bind whose host is
non-loopback AND non-private (not RFC-1918, CGNAT `100.64/10`, link-local, or `fc00::/7`/
`fe80::/10`) SHALL be REFUSED unless an explicit `--insecure-public-bind` override is set, and even
with the override the system SHALL emit a loud stderr warning pointing at the plaintext-TLS
limitation.

#### Scenario: A non-loopback bind with no token fails closed naming the env var

- **WHEN** `flotilla dash --bind 0.0.0.0:8787` runs with no token configured
- **THEN** `validateBind` fails closed with an error that names `FLOTILLA_DASH_TOKEN`

#### Scenario: An unspecified bind with a token is allowed

- **WHEN** `flotilla dash --bind 0.0.0.0:8787` (or `[::]:8787`) runs with a configured token
- **THEN** `validateBind` allows it (the unspecified address is a non-loopback bind that the token
  gate makes safe)

#### Scenario: A private LAN bind with a token serves without friction

- **WHEN** the bind host is a private address (`192.168.x` / tailscale CGNAT) and a token is
  configured
- **THEN** `validateBind` allows it with no public-bind override required

#### Scenario: A public-routable bind is refused without the explicit override

- **WHEN** the bind host is a public, routable (non-private, non-loopback) address with a token
  configured but no `--insecure-public-bind`
- **THEN** `validateBind` refuses it; WITH `--insecure-public-bind` it is served and a loud stderr
  warning about plaintext exposure is emitted

### Requirement: The Host and Origin allowlists are extended for the external host and never self-allow the unspecified bind

The system SHALL admit configured external hosts from `FLOTILLA_DASH_HOSTS` (a flag + env,
comma-separated `host` or `host:port`, a bare host taking the bind port) into BOTH
`buildHostAllowlist` and `buildOriginAllowlist`, so the phone's own cookie login/write is not
403'd by `originAllowed`. The Origin allowlist SHALL admit `http://<host>` for each form by default
and SHALL ALSO admit `https://<host>` when `--behind-tls-proxy` is set. When the bind IP
`IsUnspecified()` (`0.0.0.0`/`::`), the bind host SHALL NOT be self-added to either allowlist (only
loopback names + the explicit external hosts are allowable). An unknown `Host` SHALL still be
refused with the anti-DNS-rebinding 403 whether or not a token is configured.

#### Scenario: A configured external host passes both allowlists

- **WHEN** `FLOTILLA_DASH_HOSTS` names the phone-facing host and a browser on that host issues a
  cookie login/write carrying that Host and a matching Origin
- **THEN** the Host check passes and the Origin check passes (both allowlists were extended)

#### Scenario: The unspecified bind host is never self-allowed

- **WHEN** the bind is `0.0.0.0:8787` and a request carries `Host: 0.0.0.0:8787`
- **THEN** the Host is NOT in the allowlist (the unspecified bind host is excluded) and the request
  is refused with the anti-rebinding 403, even with a valid token

#### Scenario: The TLS-proxy Origin scheme is admitted only behind the proxy flag

- **WHEN** `--behind-tls-proxy` is set and a browser behind the proxy issues a cookie write with
  `Origin: https://<proxy-host>` for a configured host
- **THEN** the `https://<host>` Origin is in the allowlist and the write is not 403'd; without
  `--behind-tls-proxy` only `http://<host>` is admitted

#### Scenario: An unknown Host is refused regardless of the token

- **WHEN** a request carries a `Host` outside the allowlist
- **THEN** it is refused with the anti-DNS-rebinding 403 whether or not a token is configured

### Requirement: The token is loaded with flag-env-secrets precedence and an entropy floor, read once

The system SHALL add a `roster.Secrets.DashToken()` accessor returning `FLOTILLA_DASH_TOKEN` from
the secrets file (mirroring `BotToken()`), and SHALL resolve the dash token at the wiring boundary
(`cmd/flotilla/dash.go`) with the precedence `--dash-token` flag → `FLOTILLA_DASH_TOKEN` env →
secrets-file `DashToken()`, first non-empty winning, injecting the result as a new
`dash.Config.DashToken`. A configured token shorter than 32 bytes SHALL make `NewServer` error
(refuse to stand up the gate with a weak token). The token SHALL be read ONCE at process start —
rotation requires a dash restart, with no live reload — and SHALL NEVER be logged, echoed,
server-rendered, or placed in any 401 body.

#### Scenario: Precedence is flag over env over secrets-file

- **WHEN** more than one of `--dash-token`, `FLOTILLA_DASH_TOKEN`, and the secrets-file token is set
- **THEN** the first non-empty in the order flag → env → secrets-file is the resolved token

#### Scenario: A too-short token refuses to stand up the gate

- **WHEN** a token is configured but is shorter than 32 bytes
- **THEN** `NewServer` returns an error rather than serving with a weak token

#### Scenario: The token is never emitted anywhere

- **WHEN** the dash logs, server-renders the page, or writes any 401 body
- **THEN** the token material never appears in any of them

### Requirement: Every authentication failure returns one generic 401 with no oracle

The system SHALL return a single identical response — `401` with body
`{"error":"authentication required"}` — for EVERY authentication failure mode (absent credential,
garbage bearer, wrong token, absent/garbage/expired/tampered cookie, or no-token-configured), so no
response body distinguishes which check failed and no token material is ever revealed.

#### Scenario: Distinct failure modes are indistinguishable

- **WHEN** one request presents a wrong bearer token and another presents an expired cookie, both
  to a gated route
- **THEN** both receive the identical `401 {"error":"authentication required"}` response — the body
  reveals neither the failure mode nor any token material

### Requirement: A mandatory login rate-limit blunts a login flood

The system SHALL apply a mandatory token-bucket rate-limit to `POST /api/auth/login` so a flood of
login attempts cannot spin the HMAC compute unbounded, independent of brute-force resistance. The
rate-limit SHALL be in-scope and always-on (not an optional or deferred feature) and SHALL require
no external dependency.

#### Scenario: A login flood is throttled

- **WHEN** `POST /api/auth/login` is hit faster than the token-bucket refill allows
- **THEN** excess attempts are throttled rather than each driving a full HMAC verification

### Requirement: SSE authentication is enforced before a hub slot is consumed

The system SHALL enforce the SSE stream's authentication via the top-level `requireAuth` gate so a
`GET /events` request is authenticated BEFORE `handleEvents` calls `hub.add` — an unauthenticated
client SHALL NOT consume an SSE connection slot. The same-site `flotilla_dash_session` cookie
(SameSite=Strict) SHALL accompany the `EventSource` connection from the loaded page and authenticate
it, since `EventSource` cannot set an `Authorization` header.

#### Scenario: An unauthenticated SSE connection never takes a slot

- **WHEN** a token is configured and an unauthenticated client connects to `GET /events`
- **THEN** the top-level gate refuses it before `hub.add` is called, so it consumes no SSE slot

#### Scenario: The page's EventSource authenticates via the same-site cookie

- **WHEN** the loaded page opens an `EventSource` on `/events` and the browser attaches the
  same-site `flotilla_dash_session` cookie
- **THEN** the connection is authenticated by the cookie (the form `EventSource` can carry)

### Requirement: The client re-authenticates on SSE expiry and stops trusting a stale board

The system's page SHALL, on an SSE `onerror`/reconnect, probe `GET /api/status`; when that probe
returns `401`, the page SHALL surface a visible "session expired — re-enter token" prompt and stop
trusting the displayed board (a clear staleness banner) rather than letting `EventSource` silently
retry behind a stale-but-connected-looking trade dashboard.

#### Scenario: A lapsed session surfaces a staleness banner instead of a silent stale board

- **WHEN** the session cookie expires and the SSE connection errors/reconnects
- **THEN** the page's `/api/status` probe returns 401, a visible "session expired" prompt is shown,
  and the stale board is no longer presented as live

### Requirement: A login UI and login/logout endpoints establish and clear the session cookie

The system SHALL provide a token-entry login view in `index.html` plus a `dash.js` handshake that
posts `fetch('/api/auth/login', {headers:{Authorization:'Bearer '+token, 'X-Flotilla-Dash':'1'}})`
and, on success, relies on the server-set `HttpOnly` cookie (dropping the entered token from the
page — the HttpOnly cookie is the durable credential), gating the rest of the UI on the
"data fetch 401 → show login" path. The server SHALL provide `POST /api/auth/login` (open-to-auth so
it can establish a session; `requireWrite`-gated so it cannot be driven cross-site) that verifies
the presented bearer and on success mints the `flotilla_dash_session` cookie, and
`POST /api/auth/logout` (open and `requireWrite`-gated) that clears the cookie without requiring a
valid session. The login/logout responses SHALL set the cookie `Secure` when `--behind-tls-proxy`
is set and SHALL omit `Secure` otherwise (the plain-LAN http case).

#### Scenario: A correct token at login mints the session cookie

- **WHEN** the login view posts a correct bearer to `POST /api/auth/login` with the custom header
- **THEN** the server mints and sets the `HttpOnly`, `SameSite=Strict` session cookie and the page
  drops the entered token

#### Scenario: Logout clears the cookie without a valid session

- **WHEN** a client with an expired or absent session posts to `POST /api/auth/logout` with the
  custom header
- **THEN** the cookie is cleared (logout does not require a valid session)

#### Scenario: The Secure flag follows the TLS-proxy flag

- **WHEN** `--behind-tls-proxy` is set
- **THEN** the login/logout responses set the session cookie `Secure`; without the flag the cookie
  is set without `Secure`

### Requirement: The tracker error body is genericized on gateway failures

The system SHALL return a generic client message for tracker 5xx (gateway-class) failures rather
than the verbatim `gh` stderr (which can carry host paths or repo internals = recon), keeping the
detailed `gh` error on the dash's own stderr only. This closes the standing
`TODO(dash, Phase 3)` in `tracker_handlers.go`.

#### Scenario: A gateway-class tracker failure returns a generic body

- **WHEN** a tracker operation fails with a 5xx (gateway-class) error carrying verbatim `gh` stderr
- **THEN** the client receives a generic error message while the verbatim `gh` detail is logged to
  the dash's stderr only
