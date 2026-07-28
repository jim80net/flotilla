package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnforceCapacityHold(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name         string
		body         string
		operation    string
		slot         string
		surface      string
		scheduledE2E bool
		wantErr      bool
	}{
		{name: "missing allows primary", slot: SlotPrimary, surface: "codex"},
		{name: "active blocks primary", body: `{"schema":"flotilla.capacity_hold/v1","status":"ACTIVE"}`, slot: SlotPrimary, surface: "codex", wantErr: true},
		{name: "active allows safe fallback", body: `{"schema":"flotilla.capacity_hold/v1","status":"ACTIVE","forbid_surfaces":["codex"]}`, slot: "fallback-1", surface: "grok"},
		{name: "active blocks forbidden fallback surface", body: `{"schema":"flotilla.capacity_hold/v1","status":"ACTIVE","forbid_surfaces":["Codex"]}`, slot: "fallback-1", surface: "codex", wantErr: true},
		{name: "future hard limit blocks primary", body: `{"schema":"flotilla.capacity_hold/v1","status":"INACTIVE","hard_limit_until":"2026-07-18T13:00:00Z"}`, slot: SlotPrimary, surface: "codex", wantErr: true},
		{name: "future restore blocks primary", body: `{"schema":"flotilla.capacity_hold/v1","restore_after":"2026-07-18T13:00:00Z"}`, slot: SlotPrimary, surface: "codex", wantErr: true},
		{name: "active remains sticky after deadline", body: `{"schema":"flotilla.capacity_hold/v1","status":"ACTIVE","restore_after":"2026-07-18T11:00:00Z"}`, slot: SlotPrimary, surface: "codex", wantErr: true},
		{name: "inactive past deadline allows primary", body: `{"schema":"flotilla.capacity_hold/v1","status":"INACTIVE","hard_limit_until":"2026-07-18T11:00:00Z"}`, slot: SlotPrimary, surface: "codex"},
		{name: "explicit primary forbid survives past deadline", body: `{"schema":"flotilla.capacity_hold/v1","status":"INACTIVE","forbid_primary":true,"hard_limit_until":"2026-07-18T11:00:00Z"}`, slot: SlotPrimary, surface: "codex", wantErr: true},
		{name: "explicit surface forbid survives past deadline", body: `{"schema":"flotilla.capacity_hold/v1","status":"INACTIVE","forbid_surfaces":["codex"],"hard_limit_until":"2026-07-18T11:00:00Z"}`, slot: "fallback-1", surface: "codex", wantErr: true},
		{name: "malformed blocks primary", body: `{`, slot: SlotPrimary, surface: "codex", wantErr: true},
		{name: "malformed allows fallback recovery", body: `{`, slot: "fallback-1", surface: "grok"},
		{name: "unknown schema blocks primary", body: `{"schema":"flotilla.capacity_hold/v2"}`, slot: SlotPrimary, surface: "codex", wantErr: true},
		{name: "invalid deadline blocks primary", body: `{"schema":"flotilla.capacity_hold/v1","hard_limit_until":"tomorrow"}`, slot: SlotPrimary, surface: "codex", wantErr: true},
		{name: "cleared e2e policy blocks resume", body: `{"schema":"flotilla.capacity_hold/v1","status":"CLEARED","class":"codex-e2e-only-dev"}`, operation: "resume", slot: SlotPrimary, surface: "codex", wantErr: true},
		{name: "cleared e2e policy blocks forced resume", body: `{"schema":"flotilla.capacity_hold/v1","status":"CLEARED","policy_id":"codex-e2e-only"}`, operation: "resume --force", slot: SlotPrimary, surface: "codex", wantErr: true},
		{name: "nested conservation blocks primary switch", body: `{"schema":"flotilla.capacity_hold/v1","status":"CLEARED","conservation":{"policy_id":"codex-e2e-only"}}`, operation: "switch --to primary", slot: SlotPrimary, surface: "codex", wantErr: true},
		{name: "legacy restore contract blocks operator manual selection", body: `{"schema":"flotilla.capacity_hold/v1","status":"CLEARED","restore_requires":"window clear AND scheduled e2e/canary"}`, operation: "operator-manual harness selection", slot: SlotPrimary, surface: "codex", wantErr: true},
		{name: "e2e policy retains fallback path", body: `{"schema":"flotilla.capacity_hold/v1","status":"CLEARED","class":"codex-e2e-only-dev"}`, operation: "resume", slot: "fallback-1", surface: "grok"},
		{name: "scheduled e2e authorization allows conserved primary", body: `{"schema":"flotilla.capacity_hold/v1","status":"CLEARED","policy_id":"codex-e2e-only"}`, operation: "switch --to primary", slot: SlotPrimary, surface: "codex", scheduledE2E: true},
		{name: "scheduled e2e does not waive active capacity", body: `{"schema":"flotilla.capacity_hold/v1","status":"ACTIVE","policy_id":"codex-e2e-only"}`, operation: "switch --to primary", slot: SlotPrimary, surface: "codex", scheduledE2E: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("FLOTILLA_WORKSPACE_ROOT", root)
			if tt.body != "" {
				dir := filepath.Join(root, "seat")
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, CapacityHoldFileName), []byte(tt.body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			operation := tt.operation
			if operation == "" {
				operation = "resume"
			}
			err := EnforceHarnessTarget("seat", operation, tt.slot, tt.surface, now, TargetAuthorization{ScheduledE2E: tt.scheduledE2E})
			if (err != nil) != tt.wantErr {
				t.Fatalf("EnforceCapacityHold() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && (!strings.Contains(err.Error(), "desk untouched") || !strings.Contains(err.Error(), CapacityHoldFileName)) {
				t.Fatalf("error is not operator-actionable: %v", err)
			}
			if err != nil && strings.Contains(tt.name, "e2e policy blocks") {
				for _, want := range []string{"codex-e2e-only", "current fallback retained"} {
					if !strings.Contains(err.Error(), want) {
						t.Fatalf("conservation refusal %q does not name %q", err, want)
					}
				}
			}
		})
	}
}
