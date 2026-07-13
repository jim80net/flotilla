# Tasks — preemptive provider-usage monitoring (#653)

## 1. Surface capability

- [ ] 1.1 Add `UsageReport`, optional `UsageProbe`, and support helper
- [ ] 1.2 Implement Grok live-chrome weekly-percentage parsing with generic fixtures
- [ ] 1.3 Prove unsupported/unparseable surfaces return no report

## 2. Detector collection and state

- [ ] 2.1 Add configurable probe period and low-water threshold wiring
- [ ] 2.2 Collect usage off-mutex on the slow wall-clock cadence
- [ ] 2.3 Persist optional observations in `watch.Snapshot` with observed time
- [ ] 2.4 Add provider/window durable latch and recovery hysteresis to the existing cooldown store

## 3. Reuse auto-switch

- [ ] 3.1 Add typed trigger/evidence to `RateLimitAutoSwitchCandidate`
- [ ] 3.2 Feed proactive candidates into `runAutoSwitch` / `AutoSwitchFlight`
- [ ] 3.3 Branch the existing dispatcher final guard by trigger; retain one cap/recipe/cooldown/exec path
- [ ] 3.4 Add proactive under-lock recheck to `switch --auto`
- [ ] 3.5 Prove reactive/proactive concurrency admits one switch per seat

## 4. Operator visibility

- [ ] 4.1 Add optional usage fields to text/JSON `flotilla status`
- [ ] 4.2 Add optional fresh/stale usage display to dash agent items
- [ ] 4.3 Prove absent probes omit usage rather than rendering zero/healthy data

## 5. Verification and docs

- [ ] 5.1 Unit tests for threshold edge, provider/window latch, reset re-arm, persistence failure, and no-fallback refusal
- [ ] 5.2 Race test the shared flight/candidate path
- [ ] 5.3 Update `docs/usage-limit-resilience.md` with configuration and visibility semantics
- [ ] 5.4 Run full tests, systems review, OCR, and boundary audit
