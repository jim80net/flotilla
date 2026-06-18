# flotilla — genuine open product questions

> **What this is.** The audited successor to the overnight "public-release-strategy
> DRAFT." That draft re-opened decisions the operator had already made (the positioning
> one-liner; it even re-introduced the disavowed "no new daemon" framing). This doc
> **strips everything already settled** — pointing each settled item to the canonical
> [`product-decisions` register](../openspec/specs/product-decisions/spec.md) — and
> surfaces **only the questions that are genuinely un-decided.**
>
> Source of truth for settled calls: the `product-decisions` register. Decisions there are
> **not re-asked** here.

---

## A. Already DECIDED — do not re-ask (audit of the old draft)

Every "decision/question" the old draft raised, with its real status. The register
(`product-decisions`) is the citation of record.

| Old-draft item | Status | Where it's decided |
|---|---|---|
| §1 / §8.1 — the one-liner ("Option A/B/C, his call") | **DECIDED** | The current README **is** the answer: "drop-in chief of staff … pluggable coordination layer" (README.md:3-14). Operator 2026-06-18: *"Q1 was definitely answered and the current README is the result."* Register: *Positioning*. |
| §5 README proposal line "drop-in, **no new daemon**" | **DISAVOWED — that line was the taint** | "No daemon / no lock-in" is not a differentiator (operator 2026-06-18; #96/`5ef0f38`). The current README correctly omits it. Register: *No-daemon … not differentiators*. |
| §2 — "biggest miss: no visual / not 30-sec-shiny" | **STALE / DONE** | The README is already chat-first and carries an **illustrative demo mockup** (README.md:21-38, `docs/assets/flotilla-demo.gif`, added in #89 and explicitly labeled "Mockup — illustrative" in #94). A real screen-recording could still be a nice-to-have, but the "no visual" gap is closed. |
| §5 — "rewrite the README" | **STALE** | The chat-first rewrite already landed (#96, commit `3450996`). The README is the decided positioning, not a draft to redo. |
| §3.2 — `flotilla status` (whole-fleet snapshot) | **DONE / SHIPPED** | `flotilla status` (one line per desk) shipped in #97 (`cmd/flotilla/status.go`), with `--json`; #99 wired the landing widget to real `flotilla status --json`. Not future work. |
| §6 / §8.5 — landing site greenlight | **DECIDED** | Owned by a **separate dedicated desk** the operator is standing up (operator 2026-06-18). Register: *landing/dashboard = separate desk*. Core-flotilla XO stays on core. |
| §7 — "public surface must use generic examples" | **DECIDED (already practiced)** | Generic `infra`/`research`/`data` only, never the private deployment. Register: *public surface uses generic examples only*. |
| §7 — herdr framing | **DECIDED** | Complementary, no tie-in (`docs/competitive/herdr-vs-flotilla.md`). Register: *herdr = complementary*. |
| §8 closing — "the rest flows on clean gates, no per-step permission" | **DECIDED (posture)** | Trio-gated autonomy; clean-gated non-major work merges without a nod (operator 2026-06-18 autonomous-workflow directive). Register: *workflow posture*. |

---

## B. Genuine OPEN questions (the real forks)

After the audit, the honestly-open questions are few — and one isn't even a real question
yet because its premise is unverified. Each is flagged with its provenance status.

### B1. Is **first-class "Modes"** a direction we're pursuing at all? *(premise UNVERIFIED — needs operator provenance first)*

The old draft called Modes "the operator's headline differentiator." **That provenance does
not check out.** A forensic trawl of the code, openspec history, and commit bodies found no
operator ratification of a "Modes" product posture — "mode" in the code is only
`liveness_ping_mode` and the legacy-vs-v2 heartbeat switch. So before any taxonomy fork,
the real question is upstream:

- **Do you actually want first-class, named, switchable fleet Modes** (bundling today's
  scattered knobs — change-detector / backlog-drive / liveness / self-continuation — into
  named presets like standby / supervised / autonomous), as a product direction?
- **Only if yes:** is the taxonomy + semantics a fork worth designing? (It would be a Mode
  registry/SPI — architectural, so it's a genuine operator call, not autonomous work.)

If "not a priority," this drops off entirely. (Flagging the bad provenance per the
don't-elevate-inferred-requirements discipline — I won't build toward "Modes" on an
unverified premise.)

### B2. Do we build a **pluggable interface (Channel) SPI** beyond Discord? *(architectural; abstraction-laden; adjacent to parked #103)*

The README already (correctly, decided) positions flotilla as a "pluggable coordination
layer." But a *Channel SPI in the code* (so "Discord is one interface" is true in code, not
just pitch) is **not ratified scope**. This is a real fork because:

- It's an architectural abstraction — and it sits in the **same premature-abstraction
  caution** as the parked #103 issue-tracker fork (the operator's own #104 criterion: a
  second real implementation must justify the interface). Discord is the only channel today;
  the cheapest "second" (a $0 terminal channel) would be the justifying second impl.
- If yes: which second channel first (terminal / Slack / Telegram)?

Recommend deciding this **together with the #103 abstraction fork** — same question class.

### B3. Is there a deliberate **"public launch" push**, and if so what's its scope? *(strategy; the old draft's unstated premise)*

The whole old draft presumed a launch campaign with tiers. That presumption itself was
never ratified. flotilla is **already a public repo** with a chat-first README + demo GIF.
So the genuine strategy question:

- Do you want a **deliberate launch moment** (a scoped "v1 public cut" you point people at),
  or does flotilla just **keep evolving continuously** as a public repo (no launch event)?
- If a launch: the README + illustrative demo + `flotilla status` already exist, so the
  minimum bar may already be met — what (if anything) is the gate before you'd point people at it?

---

## C. Buildable now — no decision needed

Under the trio-gated autonomous-workflow posture, these are aligned, non-major, low-risk and
flow on clean gates without an operator call (listed so they're visible, not to ask):

- A **scheduled reporting digest** — push the existing `flotilla status` snapshot to Discord
  on a cadence (morning brief / end-of-run), a small follow-on to the shipped `status` command.
- The **release-sign-off workflow** (the one unchecked README roadmap item) — buildable;
  note it pairs with B1 *if* Modes happen, but it stands alone too.

(`flotilla status` itself already shipped — see §A.)

---

## D. Bottom line

There are **no blocking open product questions** for flotilla to keep evolving — the
positioning, framing, competitive stance, generic-examples discipline, merge posture, and
landing ownership are all decided (register). The only genuine operator forks are the three
in §B, and **B1's premise (Modes) needs your confirmation before it's even a real
question.** If your answer to all three of §B is "not now," I proceed on §C and the existing
backlog under the autonomous-workflow posture, and there is nothing to re-ask.
