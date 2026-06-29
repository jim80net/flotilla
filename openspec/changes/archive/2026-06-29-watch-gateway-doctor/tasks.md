# Tasks — watch-gateway-doctor

## 1. Escalator script

- [x] 1.1 `deploy/flotilla-doctor.sh` — pure-bash, `set -euo pipefail`, flag-driven
      (`--self`/`--secrets`/`--workdir`/`--bin`/`--claude`/`--skill`/`--state-dir`
      + optional `--threshold`/`--cooldown`/`--recheck`).
- [x] 1.2 flock single-flight; cheap `gateway_healthy()` (active + MainPID + `:443`
      ESTABLISHED socket from PID); `ss`-error is indeterminate, NOT an escalation.
- [x] 1.3 Confirm-once recheck after a short sleep; strike counter; threshold gate.
- [x] 1.4 Escalation: cooldown guard; status payload (gateway state, MainPID, `:443`
      socket dump, journal tail, ack-file age, per-resolver DNS probe); best-effort
      notify; time-bounded headless `claude --print "/recover-flotilla …"` with the
      gatekeeper allowlist (no `--dangerously-skip-permissions`).
- [x] 1.5 Loud SAFETY INVARIANT comment block: NEVER restarts flotilla-watch.

## 2. systemd units

- [x] 2.1 `deploy/flotilla-doctor.service.in` (Type=oneshot, generous
      `TimeoutStartSec`, `@PLACEHOLDER@` ExecStart flags).
- [x] 2.2 `deploy/flotilla-doctor.timer` (static, OnBootSec/OnUnitActiveSec 3min,
      Persistent, cadence×threshold comment).

## 3. Installer + config

- [x] 3.1 `deploy/flotilla-doctor-install.sh` modeled on `flotilla-watch-install.sh`
      (pure-bash substitution, key allowlist, fail-loud on leftover placeholder,
      `--dry-run`/`--print`, daemon-reload error wrapper, copies script + timer,
      prints the enable command — does NOT auto-enable).
- [x] 3.2 `deploy/flotilla-doctor.env.example` (every key, commented).
- [x] 3.3 `deploy/testdata/flotilla-doctor.fixture.env` (host-neutral) + `.gitignore`
      re-include.

## 4. Tests + gates

- [x] 4.1 `cmd/flotilla/doctor_install_test.go` (placeholder substitution, ExecStart
      flags, example-env drift, missing-var failure, unknown-key warning,
      placeholder-in-value rejection, determinism).
- [x] 4.2 `gofmt -l .` clean, `go vet ./...` clean, `go test ./...` green.
- [x] 4.3 `openspec validate --strict watch-gateway-doctor` clean.

## 5. Deploy (operator-gated — NOT in this change)

- [ ] 5.1 Operator reviews; copies `deploy/flotilla-doctor.env.example` →
      `deploy/flotilla-doctor.env` with this host's paths.
- [ ] 5.2 `bash deploy/flotilla-doctor-install.sh` then
      `systemctl --user enable --now flotilla-doctor.timer`.
- [ ] 5.3 Verify with `systemctl --user list-timers flotilla-doctor.timer` and a
      forced down test (stop the gateway / block DNS) → confirm escalation fires.
