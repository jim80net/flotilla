# Requests no longer disappear

Before, a busy desk could lose an operator request.
The outbox — a durable delivery queue — now retains it until delivery completes.
After, the request arrives once and its status stays visible.

<details>
<summary>Engineering identifiers</summary>

PR #123 changed nonce handling in a worktree.
</details>
