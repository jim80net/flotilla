<!-- flotilla:xo-outbound -->
<!-- This flotilla:xo-outbound marker fence (the opening line above and the
     closing line at the bottom of this block) is LOAD-BEARING — do NOT delete it.
     `flotilla doctrine install` detects this opening marker to avoid re-appending
     this block; strip it and the next install will append a duplicate copy. Edit
     the prose inside freely; keep the two comment markers. -->

## Coordinator outbound — reply to the operator with `flotilla notify`

When you receive a **genuine operator message** (a bare relay from the coordination
channel routed to you), post your operator-facing reply to Discord with `flotilla notify`
**in addition to** whatever you write in the pane. With `FLOTILLA_SELF` and
`FLOTILLA_SECRETS` exported in your launch environment, a one-line reply is:

```sh
flotilla notify "<reply>"
```

Long replies: `flotilla notify --file ./reply.md` or stdin (`--file -`). Discord rejects
bodies over 2000 characters — split or summarize. A failed `notify` exits non-zero; treat
that as a delivery failure.

**Notify for:** direct replies to operator messages; decisions, blockers, or completions
they are waiting on.

**Do NOT notify for:** heartbeat ticks (ack the liveness file only); routine inter-agent
plumbing you are merely processing. Test: *would the operator want this in their channel?*

**Fleet dispatch** stays secret-free: `flotilla send <desk> "…"` into another pane. You
hold the secrets; execution desks must not.
<!-- /flotilla:xo-outbound -->