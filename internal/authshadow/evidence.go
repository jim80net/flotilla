package authshadow

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// EvidenceEnvelope is the narrow adapter seam for a policy/PDP lane. It carries
// already-resolved identifiers and a simulated decision; it cannot grant or
// materialize authority.
type EvidenceEnvelope struct {
	RequestID         string
	DecisionRequestID string
	DecisionID        string
	OutcomeID         string
	PolicyRevision    PolicyRevision
	DomainContext     DomainContext
	Decision          string
	ReasonCode        string
	RequestedAt       time.Time
	DecidedAt         time.Time
	ObservedAt        time.Time
}

// RecordSimulatedEnvelope durably writes request, decision, and simulated outcome
// in that order. A partial write is returned honestly; it is never treated as an
// authorization or retried by inventing missing IDs.
func RecordSimulatedEnvelope(ctx context.Context, writer *Writer, envelope EvidenceEnvelope) ([]AuditRecord, error) {
	if writer == nil {
		return nil, errors.New("shadow audit writer is required")
	}
	if envelope.RequestID == "" || envelope.DecisionRequestID != envelope.RequestID {
		return nil, errors.New("shadow evidence decision request does not match request")
	}
	steps := []EventInput{
		{Kind: EventRequest, EventID: "request:" + envelope.RequestID, RequestID: envelope.RequestID,
			PolicyRevision: envelope.PolicyRevision, DomainContext: envelope.DomainContext,
			ReasonCode: "shadow_request_observed", ObservedAt: envelope.RequestedAt},
		{Kind: EventDecision, EventID: "decision:" + envelope.DecisionID, RequestID: envelope.RequestID,
			DecisionID: envelope.DecisionID, PolicyRevision: envelope.PolicyRevision,
			DomainContext: envelope.DomainContext, Decision: envelope.Decision,
			ReasonCode: envelope.ReasonCode, ObservedAt: envelope.DecidedAt},
		{Kind: EventSimulatedOutcome, EventID: "outcome:" + envelope.OutcomeID, RequestID: envelope.RequestID,
			DecisionID: envelope.DecisionID, OutcomeID: envelope.OutcomeID,
			PolicyRevision: envelope.PolicyRevision, DomainContext: envelope.DomainContext,
			Decision: envelope.Decision, ReasonCode: "simulated_no_effect", ObservedAt: envelope.ObservedAt},
	}
	if err := prevalidateEvidenceSteps(steps); err != nil {
		return nil, err
	}
	records := make([]AuditRecord, 0, len(steps))
	for _, step := range steps {
		record, err := writer.Append(ctx, step)
		if err != nil {
			return records, err
		}
		records = append(records, record)
	}
	return records, nil
}

func prevalidateEvidenceSteps(steps []EventInput) error {
	predecessor := ""
	for i, step := range steps {
		record, err := newRecord(step, uint64(i+1), predecessor)
		if err != nil {
			return fmt.Errorf("shadow evidence %s preflight failed: %w", step.Kind, err)
		}
		predecessor = record.RecordHash
	}
	return nil
}
