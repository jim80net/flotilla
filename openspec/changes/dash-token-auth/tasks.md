# Tasks — token-gated non-loopback dash bind (TDD, ordered)

Load-bearing security invariants (assert across paths — each maps to a `dash-auth` requirement):
- **(INV-1) DENY-BY-DEFAULT** — `requireAuth` top-level in `handler()` keyed on the mux-matched
  pattern (`mux.Handler(r)`), exact open-allowlist, no-op when no token.
- **(INV-2) FIXED-WIDTH COOKIE** — `[8B expiry][16B nonce][32B mac]`, `len==56` guard before slice,
  `hmac.Equal` on 32B, domain-separated key, rotation revokes all.
- **(INV-3) BEARER-CSRF-CONTEXT** — bearer skips `requireWrite` keyed on the `authBearer` context
  form (bearer-first), never raw header presence; cookie writes keep `requireWrite`.
- **(INV-4) TOKEN-LOADER** — `Secrets.DashToken()` + flag→env→secrets precedence + ≥32B floor +
  read-once.
- **(INV-5) validateBind RELAXATION** — non-loopback/unspecified IFF token, else fail-closed naming
  `FLOTILLA_DASH_TOKEN`; public-bind guard + `--insecure-public-bind`.
- **(INV-6) HOST+ORIGIN ALLOWLIST** — `FLOTILLA_DASH_HOSTS` into BOTH; unspecified never
  self-allowed; `https://` only behind `--behind-tls-proxy`.
- **(INV-7) GENERIC-401** — one `401 {"error":"authentication required"}` for every failure mode.

---

## 1. Token loader + config plumbing (INV-4)

- [ ] 1.1 TEST FIRST (`internal/roster/secrets_test.go`): `DashToken()` returns
  `FLOTILLA_DASH_TOKEN` from a parsed secrets file and `""` when absent (mirror the `BotToken()`
  test).
- [ ] 1.2 Implement `roster.Secrets.DashToken()` (`internal/roster/secrets.go:51-54`, sibling of
  `BotToken`).
- [ ] 1.3 TEST FIRST (`cmd/flotilla/dash_test.go`): the dash token resolves flag → env →
  secrets-file (first non-empty); `--dash-hosts`/`FLOTILLA_DASH_HOSTS`, `FLOTILLA_DASH_SESSION_TTL`
  (12h default on empty/malformed `time.ParseDuration`), `--behind-tls-proxy`, and
  `--insecure-public-bind` parse and inject into `dash.Config`.
- [ ] 1.4 Implement the `--dash-token`/`--dash-hosts`/`--session-ttl`/`--behind-tls-proxy`/
  `--insecure-public-bind` flags + env defaults in `cmdDash` (`cmd/flotilla/dash.go:31-45`) and the
  new `dash.Config` fields (`DashToken`, `Hosts`, `SessionTTL`, `BehindTLSProxy`, `InsecurePublicBind`)
  in `internal/dash/server.go:24-52`.

## 2. The stateless fixed-width session cookie (INV-2)

- [ ] 2.1 TEST FIRST (`internal/dash/session_test.go`): mint→verify round-trips; a tampered
  expiry/nonce/mac byte rejects; an expired cookie rejects; a decoded length != 56 rejects BEFORE
  any slice (feed a 55B and a 57B input); a cookie minted under token A rejects under a rotated
  token B (key change ⇒ all-old-cookies-fail); the key is `HMAC-SHA256(token,
  "flotilla-dash-session-v1")` (domain-separated, never the raw token).
- [ ] 2.2 Implement `mintSession(key, ttl, now) (string, error)` and `verifySession(key, value,
  now) bool` in a new `internal/dash/session.go`: `[8B BE expiry][16B nonce][32B
  HMAC-SHA256(key, expiry||nonce)]`, `base64.RawURLEncoding`, `len==56` guard, fixed-offset slice,
  `hmac.Equal` on the 32B MAC. Derive `key` once in `NewServer` from `cfg.DashToken`.

## 3. requireAuth top-level gate + open-allowlist + context form + generic 401 + rate-limit (INV-1/3/7)

- [ ] 3.1 TEST FIRST (`internal/dash/auth_test.go`): with a token configured, a request to a gated
  pattern with no credential → generic `401 {"error":"authentication required"}`; a valid bearer →
  pass (context `authBearer`); a valid cookie → pass (context `authCookie`); the open patterns
  `{"/", "GET /static/", "POST /api/auth/login", "POST /api/auth/logout"}` pass without a
  credential; with NO token configured every request passes (no-op).
- [ ] 3.2 TEST (INV-1 deny-by-default): register a SYNTHETIC new route pattern NOT in the
  open-allowlist and assert it is gated (401 without auth) — proves the default is closed. A
  crafted `/static/../api/status` resolves (after mux path-cleaning) to the gated bare `/api/status`
  pattern via `mux.Handler(r)` and is refused (mux-matched-pattern, not raw-path keying). NB: the
  read routes are registered BARE (`mux.Handler` returns `/api/status`, not `GET /api/status`) — fine,
  they're gated by ABSENCE from the open-set, so their exact pattern string is irrelevant to the gate.
- [ ] 3.2a IMPLEMENT (step-5 P1 fix): re-register the static route as `mux.Handle("GET /static/", …)`
  (`server.go:192`, bare `/static/` today) so `mux.Handler(r)` returns `GET /static/` to MATCH the
  `"GET /static/"` open-set member — else static assets gate and the login UI 401s. Only the OPEN-set
  patterns must equal their registrations; this also method-gates static to GET.
- [ ] 3.3 TEST (INV-3 bearer-first): a valid cookie + a garbage bearer resolves to `authCookie`
  (the garbage bearer fails first, the cookie wins).
- [ ] 3.4 TEST (INV-7 rate-limit): `POST /api/auth/login` faster than the token-bucket refill is
  throttled.
- [ ] 3.5 Implement `requireAuth(next http.Handler) http.Handler` in a new `internal/dash/auth.go`:
  resolve `_, pat := s.mux.Handler(r)`; if `s.dashToken == ""` pass through (no-op); if
  `openPatterns[pat]` pass through; else verify bearer-first → set the context form (`authBearer`/
  `authCookie`/`authNone`) → on `authNone` write the generic 401. Recompose `handler()`
  (`internal/dash/server.go:182-184`) as `hostAllow(requireAuth(mux))`. Add the login token-bucket.
  Define the context key + the `openPatterns` set as package constants.

## 4. validateBind relaxation + Host/Origin allowlist extension (INV-5/6)

- [ ] 4.1 TEST FIRST (`internal/dash/server_test.go`): `validateBind("127.0.0.1:8787", false)` ok;
  `validateBind("0.0.0.0:8787", false)` errors naming `FLOTILLA_DASH_TOKEN`; `validateBind(
  "0.0.0.0:8787", true)` ok; `validateBind("[::]:8787", true)` ok; a private `192.168.x` + token ok;
  a public-routable IP + token + no override → error; + `InsecurePublicBind` → ok (warning emitted).
- [ ] 4.2 Implement `validateBind(bind string, tokenConfigured bool)` (`server.go:369-387`):
  loopback ok; `IsUnspecified()`/other non-loopback ok IFF `tokenConfigured` else fail-closed;
  the public/non-private guard (RFC-1918/CGNAT/link-local/ULA classification) refusing without
  `InsecurePublicBind` + a loud stderr warning. Thread `cfg.DashToken != ""` from `NewServer`
  (`server.go:81-83`).
- [ ] 4.3 TEST FIRST (`internal/dash/server_test.go`): `buildHostAllowlist("0.0.0.0:8787",
  ["phone.lan"])` includes `phone.lan:8787` + loopback but NOT `0.0.0.0:8787`;
  `buildOriginAllowlist(...)` includes `http://phone.lan:8787` and, with the TLS-proxy flag,
  `https://phone.lan:8787`, but never the unspecified host.
- [ ] 4.4 Implement `buildHostAllowlist(bind string, extraHosts []string)` +
  `buildOriginAllowlist(bind string, extraHosts []string, httpsToo bool)` (`server.go:333-360`):
  loopback ∪ extraHosts ∪ (bind host:port UNLESS `IsUnspecified()`); Origin `http://` per form +
  `https://` when `httpsToo`. Parse `FLOTILLA_DASH_HOSTS` (bare host → `host:bindport`). Wire from
  `NewServer` (`server.go:110-111`).

## 5. requireWrite bearer-skip via context (INV-3)

- [ ] 5.1 TEST FIRST (`internal/dash/tracker_handlers_test.go`): a cookie-auth `POST /api/control/
  route` without `X-Flotilla-Dash` → 403; a bearer-auth one without it → reaches the handler; raw
  `Authorization` presence WITHOUT a verified `authBearer` context form does NOT skip the sub-check.
- [ ] 5.2 Implement the bearer-skip in `requireWrite` (`tracker_handlers.go:188-200`): read the
  verified form from `r.Context()`; if `authBearer`, skip the custom-header+Origin sub-check; else
  apply it as today.

## 6. Login / logout endpoints + Secure-behind-proxy

- [ ] 6.1 TEST FIRST (`internal/dash/auth_test.go`): `POST /api/auth/login` with a correct bearer +
  custom header → 200 + a `Set-Cookie: flotilla_dash_session=…; HttpOnly; SameSite=Strict`;
  with `BehindTLSProxy` the cookie also carries `Secure`; a wrong token → generic 401;
  `POST /api/auth/logout` clears the cookie WITHOUT a valid session (open-to-auth-state).
- [ ] 6.2 Implement `handleAuthLogin`/`handleAuthLogout` and register them under `requireWrite`
  (`server.go:routes()`, after the `/api/control/*` block); login verifies the bearer and mints the
  cookie via `mintSession`; logout sets an expired clearing cookie. `Secure` follows
  `cfg.BehindTLSProxy`. Add the two patterns to `openPatterns`.

## 7. Login UI + dash.js handshake + SSE re-auth probe + genericized tracker errors

- [ ] 7.1 Implement the token-entry login view in `internal/dash/assets/index.html` (a view the JS
  shows when a data fetch returns 401).
- [ ] 7.2 Implement the `dash.js` handshake (`internal/dash/assets/dash.js:39-42` is the existing
  `postJSON` custom-header pattern): `fetch('/api/auth/login', {headers:{Authorization:'Bearer '+t,
  'X-Flotilla-Dash':'1'}})`, drop the entered token on success, and the "data fetch 401 → show
  login" gate around `getJSON` (`dash.js:29`).
- [ ] 7.3 Implement the SSE re-auth probe in `dash.js` `es.onerror` (`dash.js:213`): probe
  `/api/status`; on 401 show the "session expired — re-enter token" prompt + a staleness banner,
  stop trusting the board.
- [ ] 7.4 Genericize the tracker 5xx body (`tracker_handlers.go:304-311`): return a generic client
  message for `status >= 500`, keep the verbatim `gh` stderr server-side only (closes the standing
  `TODO(dash, Phase 3)`).

## 8. Integration + deny-by-default + boundary

- [ ] 8.1 TEST (`internal/dash/integration_test.go`): a server on a non-loopback bind WITH a token
  serves the open `/` + `/static/*` unauthenticated; gates `/api/status`, `/events`, and
  `/api/control/route`; an unauth control POST → 401; a bearer-auth control POST → reaches the
  handler; an SSE connect without auth never reaches `hub.add`.
- [ ] 8.2 TEST (deny-by-default synthetic-new-route): the §3.2 test re-asserted at the
  `handler()` composition level (a new mux pattern not in `openPatterns` is gated end-to-end).
- [ ] 8.3 TEST (back-compat): loopback + no token serves the reads open and gates writes by
  `requireWrite` only — byte-unchanged from before this change.
- [ ] 8.4 Boundary guard: no token/secret/host in any fixture, log, rendered page, or 401 body;
  `scripts/check-private-boundary.sh` PASS (CI `private-boundary` job).

## Review + ship

- [ ] 9.1 Implementation-trio (systems-review + open-code-review + STORM, parallel) on the diff;
  iterate clean. Verify INV-1..7 each have a test.
- [ ] 9.2 `openspec validate dash-token-auth --strict` green; `go build ./...` +
  `go test ./internal/dash/... ./internal/roster/... ./cmd/flotilla/...` green; PR referencing this
  change (#208).
