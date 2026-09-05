---
name: systems-review
description: Review a Flotilla diff or exact revision with systems-level scrutiny across code, boundaries, and the real runtime. Use for PR reviews, native or Linux process behavior, dependency changes, and fixes whose static implementation may diverge from production behavior.
---

# Systems review

Ask whether the exact implementation solves the production problem on the path that actually
runs. Verify claims from source, boundary behavior, and runtime evidence; do not substitute the
author's intent for any of them.

## Isolate the object

- Review a clean, isolated checkout of the exact commit named by the request. Record the full
  commit and parent before reading the diff.
- Diff the explicit parent and commit. Do not review a moving branch, an uncommitted worktree,
  or a larger range and attribute that evidence to the named object.
- Bind every test or probe result to the commit, command, environment, and output that produced
  it. A green result proves only the object and path it exercised.

## Trace the system

1. State the claimed failure and the invariant the change must restore.
2. Follow the actual call path across packages, processes, files, sockets, and native or kernel
   boundaries. Check every caller of a changed shared function.
3. Verify dependency and artifact identity: the module, binary, inode, lockfile resolution, or
   generated file inspected must be the one used at runtime.
4. Look for irreversible edges and crash-before-recovery paths. Preconditions and compatibility
   probes belong before the first destructive step.
5. Require tests at the failure boundary, including ordering, cleanup, and adverse-path behavior;
   use a real integration probe where mocks cannot reproduce the contract.

## Linux process-lifecycle checklist

For changes that start, signal, replace, or reap Linux processes, require these proofs:

- **Pipe and fd direction:** prove which endpoint each process owns and whether it reads or writes.
  A matching pipe inode alone does not prove the communication direction or ownership.
- **Pidfd identity:** bind signals and waits to a pidfd (or an equally identity-safe primitive).
  A numeric PID is recyclable and is not process identity.
- **Probe before the irreversible edge:** test kernel support before kill, respawn, pane replacement,
  or handoff. In particular, `ENOSYS` must abort with the original pane untouched.
- **Joined reap errors:** if respawn or takeover fails, still run reap and preserve both errors;
  the primary failure must not skip cleanup or erase the cleanup failure.
- **Real-child race:** exercise exit/signal/wait ordering with a real child process. A fake PID
  cannot prove PID reuse resistance, wait semantics, or the race being fixed.

## Report separate verdicts

Do not collapse different evidence classes into one "clean" result.

| Verdict | Required evidence |
| --- | --- |
| **Code** | The exact diff, call graph, state transitions, cleanup, and error composition are sound. |
| **Boundary** | Inputs, outputs, fd ownership, filesystem/process/kernel contracts, and failure ordering are proved. |
| **Runtime** | The exact built artifact was exercised in the relevant environment; otherwise mark this verdict unverified. |

List prioritized findings with severity, evidence (`file:line`, trace, or test), impact, and a
concrete fix. Finish with `APPROVE`, `REQUEST CHANGES`, or `DO NOT MERGE`, while retaining the three
verdicts above so a static pass cannot masquerade as boundary or runtime proof.
