package doctrine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeSkillMember builds a heartbeat-skill (whole-file) member targeting a
// workspace-relative path, so the whole-file install arm can be exercised without
// depending on the real visibility-synthesis registry content.
func fakeSkillMember(name, target, body string) Member {
	return Member{
		Name:       name,
		Mechanism:  MechanismHeartbeatSkill,
		Content:    body,
		TargetFile: target,
	}
}

// A heartbeat-skill member whose TargetFile is absolute or escapes the workspace via `..`
// is REJECTED before any write (cubic P2 — defense-in-depth: a member can never write outside
// the agent's workspace), and the escaping file is NOT created.
func TestHeartbeatSkillRejectsEscapingTargetFile(t *testing.T) {
	dir, identity := writeIdentity(t, "# desk\n")
	for _, bad := range []string{"../escape.md", "skills/../../escape.md", "/etc/escape.md"} {
		member := fakeSkillMember("vs", bad, "BODY\n")
		if _, err := Install(dir, identity, []Member{member}); err == nil {
			t.Errorf("TargetFile %q must be rejected (escapes workspace), got nil error", bad)
		}
	}
	// Nothing was written outside the workspace.
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape.md")); !os.IsNotExist(err) {
		t.Errorf("an escaping write leaked outside the workspace: %v", err)
	}
}

// A heartbeat-skill member installs as a WHOLE FILE: a missing target is CREATED
// (including its parent "skills/" dir), reported created.
func TestHeartbeatSkillCreatesMissingFile(t *testing.T) {
	dir, identity := writeIdentity(t, "# desk\n")
	member := fakeSkillMember("vs", "skills/visibility-synthesis.md", "SKILL BODY\n")

	res, err := Install(dir, identity, []Member{member})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Action != ActionCreated {
		t.Fatalf("install actions = %+v, want one created", res)
	}
	target := filepath.Join(dir, "skills", "visibility-synthesis.md")
	got := readFile(t, target)
	if got != "SKILL BODY\n" {
		t.Errorf("created skill content = %q, want the member body", got)
	}
}

// An EXISTING target is KEPT (operator edits survive), reported kept — the
// idempotency is a STAT of the target file, not a marker fence.
func TestHeartbeatSkillKeepsExistingFileWithOperatorEdits(t *testing.T) {
	dir, identity := writeIdentity(t, "# desk\n")
	member := fakeSkillMember("vs", "skills/visibility-synthesis.md", "PRISTINE BODY\n")

	// Operator has already edited the installed skill.
	skillDir := filepath.Join(dir, "skills")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(skillDir, "visibility-synthesis.md")
	const edited = "OPERATOR EDITED BODY\n"
	if err := os.WriteFile(target, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Install(dir, identity, []Member{member})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Action != ActionKept {
		t.Fatalf("install actions = %+v, want one kept", res)
	}
	if got := readFile(t, target); got != edited {
		t.Errorf("kept skill body = %q, want operator edit %q preserved", got, edited)
	}
}

// A re-install of a heartbeat-skill member is idempotent: first run creates, second
// run keeps — the content is byte-identical across runs.
func TestHeartbeatSkillReinstallKeeps(t *testing.T) {
	dir, identity := writeIdentity(t, "# desk\n")
	member := fakeSkillMember("vs", "skills/visibility-synthesis.md", "BODY\n")

	res1, err := Install(dir, identity, []Member{member})
	if err != nil {
		t.Fatal(err)
	}
	if res1[0].Action != ActionCreated {
		t.Fatalf("first install = %q, want created", res1[0].Action)
	}
	res2, err := Install(dir, identity, []Member{member})
	if err != nil {
		t.Fatal(err)
	}
	if res2[0].Action != ActionKept {
		t.Fatalf("second install = %q, want kept", res2[0].Action)
	}
}

// A whole-file member does NOT route through appendOnce (which hard-errors on an
// empty OpenMarker). It carries no marker and installs cleanly via its own write.
func TestHeartbeatSkillDoesNotRouteThroughAppendOnce(t *testing.T) {
	dir, identity := writeIdentity(t, "# desk\n")
	// No OpenMarker/CloseMarker on a heartbeat-skill member; if it routed through
	// appendOnce this would error on the empty marker fence.
	member := fakeSkillMember("vs", "skills/visibility-synthesis.md", "BODY\n")
	if _, err := Install(dir, identity, []Member{member}); err != nil {
		t.Fatalf("heartbeat-skill install errored (routed through appendOnce?): %v", err)
	}
}

// A heartbeat-skill member with an EMPTY TargetFile is a config error — a clear
// hard-error, not a silent write to the workspace root.
func TestHeartbeatSkillEmptyTargetIsConfigError(t *testing.T) {
	dir, identity := writeIdentity(t, "# desk\n")
	member := fakeSkillMember("vs", "", "BODY\n")
	if _, err := Install(dir, identity, []Member{member}); err == nil {
		t.Fatal("heartbeat-skill member with empty TargetFile = nil error, want config error")
	}
}

// An identity-append member AND a heartbeat-skill member install together via ONE
// Install loop: the identity-append arm appends into the identity file, the
// heartbeat-skill arm writes its own whole file — both via the same call.
func TestInstallMixedMechanismsInOneLoop(t *testing.T) {
	dir, identity := writeIdentity(t, "# desk\n")
	appendM := fakeAppendMember("structural")
	skillM := fakeSkillMember("vs", "skills/visibility-synthesis.md", "SKILL BODY\n")

	res, err := Install(dir, identity, []Member{appendM, skillM})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2 results, got %d: %+v", len(res), res)
	}
	// Identity-append arm appended into the identity file.
	idBody := readFile(t, filepath.Join(dir, identity))
	if !strings.Contains(idBody, appendM.OpenMarker) {
		t.Error("identity-append member was not appended into the identity file")
	}
	// Heartbeat-skill arm wrote its own whole file.
	skillBody := readFile(t, filepath.Join(dir, "skills", "visibility-synthesis.md"))
	if skillBody != "SKILL BODY\n" {
		t.Errorf("heartbeat-skill body = %q, want the member content", skillBody)
	}
}

// The whole-file create is DISJOINT from the identity-content `anyAppended`
// write-back: an install where every identity-append member SKIPS (no identity
// write) but a heartbeat-skill member is missing STILL writes the whole file. The
// identity file must remain untouched (its mtime/content unchanged).
func TestHeartbeatSkillWritesEvenWhenIdentityAllSkip(t *testing.T) {
	dir, identity := writeIdentity(t, "# desk\n")
	appendM := fakeAppendMember("structural")
	skillM := fakeSkillMember("vs", "skills/visibility-synthesis.md", "SKILL BODY\n")

	// First install: append the identity member + create the skill.
	if _, err := Install(dir, identity, []Member{appendM, skillM}); err != nil {
		t.Fatal(err)
	}
	// Capture the identity file's exact bytes, then DELETE the skill so the second
	// install must re-create it while the identity member now SKIPS.
	idPath := filepath.Join(dir, identity)
	idBefore := readFile(t, idPath)
	if err := os.Remove(filepath.Join(dir, "skills", "visibility-synthesis.md")); err != nil {
		t.Fatal(err)
	}

	res, err := Install(dir, identity, []Member{appendM, skillM})
	if err != nil {
		t.Fatal(err)
	}
	// The identity-append member skipped; the heartbeat-skill member created.
	var sawSkip, sawCreate bool
	for _, r := range res {
		if r.Member == appendM.Name && r.Action == ActionSkipped {
			sawSkip = true
		}
		if r.Member == skillM.Name && r.Action == ActionCreated {
			sawCreate = true
		}
	}
	if !sawSkip {
		t.Errorf("identity-append member did not skip on re-install: %+v", res)
	}
	if !sawCreate {
		t.Errorf("heartbeat-skill member did not re-create the deleted file: %+v", res)
	}
	// The whole-file write happened even though the identity write-back did not.
	if got := readFile(t, filepath.Join(dir, "skills", "visibility-synthesis.md")); got != "SKILL BODY\n" {
		t.Errorf("skill not re-created when identity all-skip: %q", got)
	}
	// And the identity file is byte-identical (the all-skip path did not rewrite it).
	if got := readFile(t, idPath); got != idBefore {
		t.Error("identity file was rewritten on an all-skip install (write-back not gated)")
	}
}
