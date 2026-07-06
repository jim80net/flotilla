# Succession program — non-Fable coordinator options

Operator deadline: **2026-07-07** (Fable goes metered). The fleet needs harness options for
Meta-XO and project-XO seats that do not depend on Claude/Fable subscriptions.

**The product desk owns:** harness + seat plumbing (drivers, workspace init, trial desks, readiness).
**training desk owns:** model evaluation (which model on which harness is fit for judgment work).

| Doc | Purpose |
|-----|---------|
| [opencode-revival-trial.md](./opencode-revival-trial.md) | Stand up a trial OpenCode execution desk; re-verify driver vs current CLI |
| [portable-coordinator-readiness.md](./portable-coordinator-readiness.md) | Ranked gap list: what breaks when grok/codex/opencode runs XO duties |
| [coordinator-runbooks/](../coordinator-runbooks/README.md) | Generalized successor runbook package (bench-measured uplift; public) |

**Gates:** design/readiness docs → operator/meta-XO gate. **No live meta-XO/XO seat swaps** without an
operator-scheduled window. Supervised project-XO trials follow
[coordinator-seat-swap-runbook.md](../coordinator-seat-swap-runbook.md).

## Coordinator bench methodology (SE-6)

The measured uplift numbers in
[`coordinator-runbooks/README.md`](../coordinator-runbooks/README.md) come from a
**16-scenario coordinator evaluation** — replayable episodes extracted from real
production coordination, not ad-hoc prompts.

**Scenario set.** Each scenario is a self-contained coordinator task (dispatch,
gate, operator comms, incident triage, synthesis) with a frozen context bundle so
different models and runbook conditions can be compared on the same input.

**Grading rubric.** Six dimensions score each response (communication register,
gate procedure, verification discipline, dispatch posture, operator decision
handling, incident response). Scores are 0–2 per dimension with cited justification.

**Fabrication disqualifier.** Any response that states an unverified empirical
claim as fact (status, metric, merge result, test outcome) caps at FAIL regardless
of fluency — the bar matches Principle 8 (verify; never fabricate).

**Runbook A/B.** Baseline: coordinator identity + constitutional principles only.
Treatment: the same seat plus the [`coordinator-runbooks/`](../coordinator-runbooks/)
package. Reported lifts: grok-4.3 **0.845→0.875 (+0.030)**; gpt-5.5
**0.848→0.901 (+0.053)** — concentrated in communication-register and
gate-procedure legs.

Deployment-specific scenario text, calibration answers, and error-taxonomy instances
stay in host-local private research state; this section documents the **public,
reproducible methodology** behind the published numbers.