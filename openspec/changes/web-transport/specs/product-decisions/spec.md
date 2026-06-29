# product-decisions Specification

## MODIFIED Requirements

### Requirement: The landing site / dashboard is a separate dedicated desk

The landing-site / dashboard ("flotilla-dash") SHALL be owned by a **separate dedicated
desk**, not the core-flotilla XO; core work stays on the core repo and CLI. This is an
OWNERSHIP decision — which desk develops dashboard work — and SHALL NOT be read as requiring
the dashboard to be a separate web APPLICATION from the coordination bus: refactoring the
dashboard behind the Transport SPI (so the web is a first-class coordination medium backed by
the one existing dashboard surface) honors this decision, because the dashboard remains the
dedicated desk's work. Keeping exactly one web surface is a CONSEQUENCE of that refactor, not a
separate requirement this decision imposes — and this ownership decision does NOT ratify the
architecture choice between refactoring the dashboard and building a second web app (that
choice belongs to the web-transport change, not to this ownership decision). RATIFIED —
operator 2026-06-18 (ownership); the web-transport clarification refines scope without changing
the ownership decision. (Greenlight is settled; the core XO does not need to re-ask whether to
spawn it.)

#### Scenario: Dashboard / landing work arises

- **WHEN** landing-site or dashboard work is needed
- **THEN** it is routed to the dedicated flotilla-dash desk, and the core XO stays on core-flotilla work

#### Scenario: The dashboard is grafted onto the coordination-bus SPI

- **WHEN** the dashboard is refactored to sit behind the Transport SPI as the web coordination medium
- **THEN** this does NOT violate the "separate dedicated desk" decision — the dashboard stays the dedicated desk's work, there is exactly one web surface, and the decision is read as ownership (who develops it), not as a mandate for a separate web application
