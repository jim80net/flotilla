# watch Specification (delta)

## MODIFIED Requirements

### Requirement: Stackable material routing scopes primary clock side-effects

When `stackable_wakes` is enabled, material reasons routed only to non-primary coordinator
layers SHALL NOT set the primary `woke` flag, SHALL NOT reset the primary quiet/liveness ping
FSM, and SHALL NOT clear primary `XOSettled` or reset `selfCont`. Primary clock mutations SHALL occur when at least one material reason targets the primary
layer (fleet-wide reasons in the primary slice, or desk-scoped reasons whose OwningXO is
the primary XO). Material routed only to non-primary project layers SHALL NOT advance the
primary clock.

#### Scenario: Primary-owned desk material re-engages the primary clock
- **WHEN** a desk owned by the primary XO finishes and `stackable_wakes` is enabled
- **THEN** the material wake routes through the primary `Wake` path and clears primary settled state

#### Scenario: Subtree-only material preserves primary quiet state
- **WHEN** a tick delivers material only to a project layer via `WakeLayer`
- **THEN** the primary quiet ping clock continues and may fire on schedule

#### Scenario: Subtree-only material preserves primary settled state
- **WHEN** the primary XO is settled and a subtree-only material tick fires
- **THEN** primary `XOSettled` remains true

### Requirement: Non-primary layer clock instructions use per-coordinator paths

When `enqueueLayerMaterialWake` targets an owner other than the primary XO, the wake body
SHALL reference that owner's canonical `flotilla-<owner>-alive` and `flotilla-<owner>-settled`
paths and SHALL NOT fall back to legacy `flotilla-xo-*` basenames.

#### Scenario: Project layer wake does not alias primary clock files
- **WHEN** legacy `flotilla-xo-alive` exists but `flotilla-<owner>-alive` does not
- **THEN** a non-primary layer material wake instructs touch of the per-owner path only