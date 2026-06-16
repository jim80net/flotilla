# Handoff — 2026-06-16: installer optional `FLOTILLA_BACKLOG_FILE` (design RESOLVED → go straight to TDD)

Peer-to-peer handoff for the next flotilla-dev session. Repo: `jim80net/flotilla`
(this checkout: `/home/jim/workspace/github.com/jim80net/flotilla`). The live fleet is the
Spark/General-ML deployment; the live roster + state live under
`/home/jim/workspace/github.com/General-ML/spark/state/`, NOT in this repo.

## What landed this session

1. **Archived `grok-result-reader` openspec change** — PR **#81 merged** (the loose end from the
   prior handoff). Folded the two ADDED surface requirements (the optional `ResultReader`
   capability + the grok session-store impl) into `openspec/specs/surface/spec.md`. Verified
   **zero content loss** per the openspec-archive-overwrites-inline-edits hazard: branched from
   `origin/main`, diffed the surface spec per-spec — one append-only hunk (376→412 lines, no
   existing requirement deleted or modified). `openspec validate --specs/--changes` green. grok
   #58 (A driver + B reader) is now fully DONE, merged, deployed, and archived.

## NEXT TASK — installer optional `FLOTILLA_BACKLOG_FILE` (THE design is fully resolved below)

**Goal:** make `deploy/flotilla-watch-install.sh` support an OPTIONAL 6th env key so a fresh host
enables the goal-driven loop via `deploy/flotilla-watch.env` instead of a hand-made systemd
drop-in. After it lands, the XO replaces the live Spark drop-in
(`~/.config/systemd/user/flotilla-watch.service.d/backlog.conf`, which sets the binary's
`FLOTILLA_BACKLOG_FILE` env) with the `.env` key + re-runs the installer.

**CRITICAL invariant:** OPTIONAL. The 5 current keys stay REQUIRED; this is a 6th OPTIONAL key.
When UNSET the generated unit must be **byte-identical to today** (gate OFF = current default,
zero behavior change for existing installs). The installer currently `exit 1`s on any missing
required key — the new key must NOT be added to that required-missing check.

### Decision (XO, 2026-06-16): **ExecStart-arg form, NOT `Environment=`.**

Reasoning (XO verbatim intent): consistency — every other config in the unit (roster, secrets,
ack-file) is an ExecStart arg, so a reader sees the flag explicitly in `systemctl cat`, and we
keep ONE config mechanism. The `Environment=` form was only used in the Spark hand-made drop-in
because the installer didn't support the flag yet; the proper installer matches the existing
convention. **Do not implement the `Environment=` variant.**

### Key verified fact (don't re-derive)

The binary ALREADY reads the env var: `cmd/flotilla/watch.go:58` defines
`backlogPath := fs.String("backlog-file", os.Getenv("FLOTILLA_BACKLOG_FILE"), …)`. Unset ⇒ nil ⇒
backlog gate OFF (see `cmd/flotilla/watch.go:204`). So the flag exists and works; this task is
purely the installer/template/.env plumbing + its regression test. No Go daemon code changes.

### The resolved design — 6 concrete changes

**Mechanism (the one non-obvious bit):** a static template can't conditionally omit text, so use a
**computed-fragment placeholder** `@FLOTILLA_BACKLOG_ARG@` appended directly to the ExecStart line
(no space before it in the template). The installer substitutes it to either `" --backlog-file
<path>"` (leading space included) or `""`. This keeps the existing pure-bash `${content//@TOK@/…}`
substitution style AND the "fail loud if any `@FLOTILLA_*@` survives" guard intact (the placeholder
is ALWAYS substituted — to the fragment or to empty).

1. **`deploy/flotilla-watch.service.in`** (line 32, the ExecStart line) — append the placeholder:
   ```
   ExecStart=@FLOTILLA_BIN@ watch --roster @FLOTILLA_ROSTER@ --secrets @FLOTILLA_SECRETS@ --ack-file @FLOTILLA_ACK_FILE@@FLOTILLA_BACKLOG_ARG@
   ```
   (`@FLOTILLA_ACK_FILE@@FLOTILLA_BACKLOG_ARG@` — the fragment carries its own leading space when
   set, so unset ⇒ the line ends exactly as today after `--ack-file <ack>`.)

2. **`deploy/flotilla-watch-install.sh`** —
   - **Pre-clear** `FLOTILLA_BACKLOG_FILE=''` alongside the other 5 (the line ~47 that pre-clears
     so an inherited env can't leak). **THIS IS LOAD-BEARING:** the live Spark host *exports*
     `FLOTILLA_BACKLOG_FILE` (the binary reads it). Without pre-clear, an inherited value would
     make `[[ -n "$FLOTILLA_BACKLOG_FILE" ]]` true and inject the arg even when `.env` omits the
     key — silently breaking the byte-identical-when-unset guarantee on the very host we're fixing.
   - Add `FLOTILLA_BACKLOG_FILE` to the `case "$key" in … )` allowlist (~line 58-60) so it's
     LOADED, not warned-as-unknown.
   - **Do NOT** add it to the `missing`/required loop (~lines 66-72) — it's optional.
   - **DO** add it to the template-token-in-value safety check (~lines 77-82) so a value
     containing `@FLOTILLA_*@` is refused like the others. (An empty value is `!= *@FLOTILLA_*@*`,
     so a guarded `if [[ -n … ]]` or unconditional inclusion both work; unconditional is simplest
     and safe.)
   - Compute the fragment + substitute, after the existing substitutions (~line 91):
     ```bash
     if [[ -n "$FLOTILLA_BACKLOG_FILE" ]]; then
       backlog_arg=" --backlog-file $FLOTILLA_BACKLOG_FILE"
     else
       backlog_arg=""
     fi
     content="${content//@FLOTILLA_BACKLOG_ARG@/$backlog_arg}"
     ```
   - **Optional path-check:** if you add an existence check for the backlog file, make it a
     `warning` (non-fatal), NOT an `error` — mirror the `FLOTILLA_BIN` warning at line 117. The
     XO's backlog file may not exist yet at install time on a fresh host. (Recommended but minor;
     the roster/secrets checks are errors because those are hard prerequisites — the backlog is not.)

3. **`deploy/flotilla-watch.env.example`** — add the optional 6th key, **commented out** (so the
   default install leaves it unset), documenting the gate-OFF-when-unset semantics. Suggested:
   ```
   # OPTIONAL (6th key) — the goal-driven loop's fleet backlog (markdown; `- [<status>]` items).
   # When SET, the generated unit's ExecStart gains `--backlog-file <path>` and the XO refuses to
   # settle while the backlog has unblocked items. When UNSET (default — leave this commented out),
   # the backlog gate is OFF and the unit is byte-identical to a no-backlog install (zero behavior
   # change for existing installs). The XO maintains this file (e.g. state/fleet-backlog.md).
   #FLOTILLA_BACKLOG_FILE=%h/.config/flotilla/fleet-backlog.md
   ```
   NOTE: `TestInstallerExampleEnvSubstitutesFully` renders the example env and asserts NO
   placeholder survives. A commented-out key leaves `FLOTILLA_BACKLOG_FILE` unset ⇒ fragment ⇒ ""
   ⇒ `@FLOTILLA_BACKLOG_ARG@` IS substituted (to "") ⇒ no surviving placeholder ⇒ that test still
   passes. (Verify this — it's the subtle interaction between "commented out in example" and "the
   placeholder must always be substituted regardless".)

4. **TEST — extend `cmd/flotilla/watch_install_test.go`** (the functional-identity regression).
   The existing `TestInstallerGeneratesExpectedFunctionalUnit` renders `fixtureEnv`
   (`deploy/testdata/flotilla-watch.fixture.env`, the 5-key fixture) and asserts the exact
   functional-line sequence. Add:
   - **(a) backlog-UNSET = byte-identical (the critical no-regression lock):** the existing
     fixture has no backlog key → its `want` ExecStart line MUST stay exactly
     `ExecStart=%h/go/bin/flotilla watch --roster /srv/fleet/flotilla.json --secrets /srv/fleet/secrets.env --ack-file /srv/fleet/xo-alive` (no `--backlog-file`, no trailing space). The
     existing test already asserts this; confirm it still passes UNCHANGED after the template edit.
     That IS the byte-identical proof for the unset path.
   - **(b) backlog-SET → arg present:** new test — write a temp env with the 5 keys + a 6th
     `FLOTILLA_BACKLOG_FILE=/srv/fleet/backlog.md`, render, assert the ExecStart line ==
     `ExecStart=%h/go/bin/flotilla watch --roster … --ack-file /srv/fleet/xo-alive --backlog-file /srv/fleet/backlog.md` (exact, via `funcLineRe`, with the single space).
   - **(c) inherited-env no-leak (guards the pre-clear):** set `FLOTILLA_BACKLOG_FILE` in the
     subprocess env (`exec.Command(...).Env = append(os.Environ(), "FLOTILLA_BACKLOG_FILE=/leak")`)
     but render the 5-key fixture (which omits the key) → assert the ExecStart line has NO
     `--backlog-file`. This proves the pre-clear defends the byte-identical-when-unset guarantee
     against the live Spark host's exported var. (The current `renderUnit` helper builds the
     command without a custom Env — you'll need a variant that sets Env, or inline the exec.Command.)
   - The fixture env (`flotilla-watch.fixture.env`) should STAY 5-key (it's the unset/baseline
     case). Use temp env files for the set case. Do not add the 6th key to the committed fixture.

5. **OpenSpec change** — propose a change (suggest id `installer-optional-backlog-file`) under the
   `watch` capability (it's the watch unit's deploy surface; `openspec list` shows `spec/watch`
   exists). Document: the optional 6th key, the gate-OFF-when-unset invariant, and the
   byte-identical-when-unset guarantee. Run `openspec validate --strict` (use `openspec validate
   <change-name>` — bare `--strict` in this non-interactive shell prints the "Nothing to validate"
   menu; `--specs` / `--changes` / `<name>` work). Use `opsx:propose` if you want the scaffold.

6. **Standard flow:** design.md → `/systems-review` + `/open-code-review` IN PARALLEL on the design
   → TDD (write the 3 test cases first, watch them fail, implement) → `/systems-review` +
   `/open-code-review` in parallel on the impl diff → PR referencing the change → **XO review +
   merge** (the XO claims the hard code-review gate on substantive PRs; I autonomously merge only
   the doc-only archive chores). After merge, the XO does the Spark drop-in → `.env` cutover.

### Files to read first (fast orientation — all read this session, line refs current as of 4663432)
- `deploy/flotilla-watch-install.sh` — the generator (pre-clear ~47, allowlist ~58, missing-check
  ~66, token-safety ~77, substitution ~86-91, fail-loud guard ~94, path-checks ~107-117).
- `deploy/flotilla-watch.service.in` — the template (ExecStart is line 32).
- `deploy/flotilla-watch.env.example` — the 5 documented keys (add the 6th here).
- `deploy/testdata/flotilla-watch.fixture.env` — the 5-key test fixture (keep it 5-key).
- `cmd/flotilla/watch_install_test.go` — the functional-identity regression (extend it).
- `cmd/flotilla/watch.go:58,204` — proof the `--backlog-file` flag + env default already exist.

## Tracked follow-ups (carried from prior handoff — NOT this task's scope)

- grok official-CLI `AwaitingApproval` gate markers (auth/payment/tool-approval) — needs a live
  capture of that state. Until then a blocked grok reads Idle (documented gap). (#58)
- grok multi-line / bracketed-paste submit validation (single-line is live-confirmed). (#58)
- Cross-process atomicity for confirmed delivery (in-daemon `paneMu` covers the daemon; an operator
  `flotilla send` racing a daemon confirm is the rarer residual). (relay-confirmed-delivery design)
- Voice confirmation inherit (voice already idle-gates; only the confirmation is missing). (#72)
- Other active openspec changes exist (`watch-gateway-doctor` 13/16, `watch-heartbeat-sidechannel`
  9/21, `agent-workspace` 19/22) — NOT this session's work; left as-is.

## KEY LEARNINGS (reflection — reinforcements of existing rules, no new artifacts)

- **The optional-key-in-a-fail-loud-substitution-installer pattern** (computed-fragment placeholder
  that always substitutes to arg-or-empty + pre-clear the inherited env var) is the elegant fit for
  this installer's architecture. It is DESIGNED but UNSHIPPED — deliberately NOT codified as a skill
  yet (per `verify-before-acting`/`never-fabricate`: don't codify an unproven pattern). If it ships
  clean, the implementing session's wrap can decide whether it's skill-worthy.
- **The pre-clear is the subtle correctness hinge,** not boilerplate: the target host exports the
  very env var the installer reads, so omitting the pre-clear would make the "byte-identical when
  unset" guarantee fail on exactly the host we're fixing. Trace the deployment environment, not just
  the code. (This is `verify-before-acting` applied to the runtime env.)
- **`proceed-in-parallel` worked well:** I ran the #2 installer-surface research while #81's CI was
  pending, so the design checkpoint was ready the moment the archive merged. Reinforcement.

## Mechanics / environment notes (unchanged)
- `go` is at `/usr/local/go/bin` (not on PATH): prefix go commands with
  `export PATH=$PATH:/usr/local/go/bin`.
- Reviews: this repo has NO cubic — `/systems-review` + `/open-code-review` (parallel background
  Agents on the diff) are the gates of record.
- Merge policy: merge on clean gates; the XO merges substantive PRs, I autonomously merge doc-only
  archive chores. The XO owns the deploy (it restarts the safety-critical heartbeat clock).
- `openspec validate`: bare `--strict` prints a menu in this non-interactive shell; use
  `--specs` / `--changes` / `<item-name>` instead.
