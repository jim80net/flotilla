# Authorized-state reconciliation

Shared host identity makes actor attribution soft: a changed binary proves that
an act occurred, but not which desk performed it. `flotilla reconcile-state`
answers the narrower operational question that protects the fleet: **does the
observed state match an instruction that was actually authorized?**

The command is read-only. It reports drift and never repairs state or identifies
an actor. Keep its manifest host-private because it contains local paths and
service names.

## Scope: divergence only

Reconciliation detects **divergence**: the authorization record says one thing
and the observed host says another. It cannot detect **faithful-wrong** behavior,
where the authorized policy, built artifact, installed configuration, and runtime
all agree while implementing the wrong behavior. Every comparison here is
internal to artifacts the system produced; agreement is not proof of correctness.

This boundary imposes two construction rules:

1. Populate desired values from the authorization record. Never generate a
   manifest by snapshotting the current host: doing so would bless unauthorized
   state as the baseline the checker then defends.
2. Test behavioral correctness with expectations authored independently from
   intent. Never capture current decisions as golden expectations: that creates
   a change detector which preserves stable defects rather than a correctness
   check.

The complete safety toolchain therefore has three non-substitutable instruments:

- reconciliation detects divergence from an instruction-linked desired state;
- intent-authored fixtures prevent regression against known requirements;
- adversarial probing discovers missing requirements, which then become new
  intent-authored fixtures.

`reconcile-state` supplies only the first instrument. It makes no coverage claim
about the other two.

```json
{
  "schema": "flotilla.authorized_state/v1",
  "authorized_at": "2026-08-07T01:00:00Z",
  "checks": [
    {
      "id": "policy-binary-version",
      "kind": "executable-version",
      "instruction_ref": "dispatch:example-version-approval",
      "path": "/opt/example/bin/policy-engine",
      "expected_version": "policy-engine 1.5.1"
    },
    {
      "id": "policy-config",
      "kind": "file-sha256",
      "instruction_ref": "change:example-config-approval",
      "path": "/opt/example/etc/policy.toml",
      "expected_sha256": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    },
    {
      "id": "fleet-watch",
      "kind": "systemd-user-service",
      "instruction_ref": "deploy:example-watch-approval",
      "unit": "example-watch.service",
      "expected_active": "active",
      "expected_executable": "/opt/example/bin/fleet-watch",
      "expected_executable_sha256": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
    }
  ]
}
```

Run it manually or from a user timer/cron job:

```sh
flotilla reconcile-state --manifest /opt/example/state/authorized-state.json
flotilla reconcile-state --manifest /opt/example/state/authorized-state.json --json
```

Exit status is `0` when every check is clean, `1` when observed state differs,
and `2` when any state cannot be observed or the manifest is invalid. A
configuration error never produces a clean report.

The executable-version check always invokes the absolute path with only
`--version`; manifests cannot inject shell commands. File checks compare SHA-256.
Systemd checks read `ActiveState` and `MainPID` from the user manager, then resolve
and hash the running `/proc/<pid>/exe` when a process exists.
