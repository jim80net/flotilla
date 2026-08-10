# Proposal — mechanize fleet operations

## Why

Flotilla's operating model already depends on limits and distinctions that are enforced manually: coordinators count standing charges, reorganize overloaded spans, route routine work differently from gates, stage topology edits, curate the operator's decision queue, and compare message content when delivery identity is unclear. Those practices work only while a deployment remembers them perfectly. The maturity step is to make the product represent and enforce the mechanisms itself.

Two operator requirements anchor the change. For decision load: **“Anyhow, the rule of three applies to me, too.”** For message provenance: **“if I send you a message from discord, I want a response in discord. If the product doesn't disambiguate the source of where i'm sending you messages, that's a product gap. Lets get provenance right.”** The latter is the strongest requirement class in this change: operator-origin provenance and reply-to-origin behavior cannot be weakened by convenience routing.

Deployments bridge this gap today with manual discipline: an operator message received through a chat relay gets a reply through that relay, while operator input typed directly in a pane gets an in-pane reply. The product graduates that convention into envelope and routing semantics so correctness no longer depends on a coordinator remembering which surface was used.

This change graduates six manual practices into generic product capabilities without encoding any deployment's topology, paths, identities, channels, or incidents. It is design-only; implementation begins only after independent review and separate build sequencing.

## What changes

1. Derive each seat's standing-charge span from canonical roster relationships and expose it in status output.
2. Detect spans above three, surface the condition, and nudge the responsible coordinator to reorganize.
3. Represent adjutant relationships independently from line ownership and classify traffic as routine or gate/escalation so only routine flow may be compressed.
4. Add a plan/validate/apply topology transaction that validates the complete parent-and-channel graph before an atomic swap and hot reload, including detector-census refresh.
5. Add coordinator-authored decision presentation state so the primary decisions view shows at most three judged priorities while drill-in retains every unresolved decision.
6. Assign stable message identity and source-channel provenance at composition, carry both through relays, make replies return to the origin surface, and expose truthful per-recipient delivery receipts for every recipient class.

## Impact

- **New capability deltas:** `span-computation`, `drowning-detection`, `adjutant-routing`, `topology-apply`, `decision-presentation`, and `message-provenance`.
- **Likely affected product areas:** roster graph and validation, watch detectors and reload state, status and decisions read models, goals compilation, send/inbound/dispatch registries, transport envelopes, reply routing, CLI inspection, and durable audit storage.
- **Compatibility:** existing rosters and goals remain readable through explicit defaults and migration rules. Legacy messages without composition identity or origin provenance remain observable as legacy/unattributable; the product must not invent provenance or claim acknowledgements it cannot prove.
- **Build boundary:** this change includes implementation tasks but executes none of them. Each area can be built and reviewed independently after the design gate, with message provenance and truthful receipts sequenced first because they carry the strongest operator requirement.

## Out of scope

- Any deployment-specific roster, channel, seat, host path, nonce, or incident data.
- Automatic organizational redesign beyond detecting overload and issuing a bounded nudge.
- Replacing coordinator judgment with an automatic decision-ranking algorithm.
- Implementation, deployment, or migration execution as part of this design change.
