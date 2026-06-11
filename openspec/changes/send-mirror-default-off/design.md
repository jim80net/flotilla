# Design: inter-agent mirror default-off

**Status:** design (awaiting XO checkpoint) · **Date:** 2026-06-11 · **Size:** small (one roster field + send gating).

## The mirror decision — precedence

`flotilla send`'s effective "mirror this to Discord?" decision:

| condition | mirror? |
|---|---|
| `--no-mirror` given | **off** (force) |
| `--mirror` given | **on** (force) |
| neither flag; roster `mirror_inter_agent: true` | on |
| neither flag; roster `mirror_inter_agent: false` or absent | **off** (the new default) |

`--no-mirror` and `--mirror` together → a clear "mutually exclusive" error.

Implementation: two `bool` flags both defaulting false (so "given" = `true`); the
gate is `noMirror ? false : (mirror ? true : cfg.MirrorInterAgent)`. This replaces
the current unconditional `if *noMirror { return nil }` at `main.go:222`.

## `notify` is untouched

`cmdNotify` does not go through the mirror path at all — it posts the operator-facing
content directly. This change touches only `cmdSend`'s mirror block. The XO's contract
("only operator-facing `notify` reaches Discord by default") falls out for free: with
the send mirror default-off, `notify` is the sole default Discord poster.

## Backward compatibility

NOT purely additive — the default flips from mirror-on to mirror-off. This is the
intended behavior change (the operator's Discord clutter is the problem being fixed).
A deployment that wants the old audit trail sets `mirror_inter_agent: true` in the
roster. The roster loads with the field absent (→ false → off) with no error
(additive schema field). `--no-mirror` keeps its exact meaning; `--mirror` is new.

## Non-goals

- Distinct inter-agent vs operator Discord channels — the XO flagged this as a future
  step, not this change.
- Any change to `flotilla notify` or to the mirror's best-effort / never-leak-the-webhook
  behavior when it DOES mirror.

## Open question for the checkpoint

- Confirm **default-off when the roster field is absent** (vs. requiring an explicit
  `mirror_inter_agent: false`). Recommend absent → off, so the operator gets relief
  without editing every roster — matches "make off the persistent default."
