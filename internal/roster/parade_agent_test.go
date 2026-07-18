package roster

import "testing"

func TestParadeAgentValidation(t *testing.T) {
	cfg, err := Load(writeRoster(t, `{
	  "cos_agent":"cos", "parade_agent":"parade-desk",
	  "agents":[{"name":"cos"},{"name":"parade-desk"}]}`))
	if err != nil || cfg.ParadeAgent != "parade-desk" {
		t.Fatalf("load parade agent: cfg=%+v err=%v", cfg, err)
	}
	if _, err := Load(writeRoster(t, `{
	  "cos_agent":"cos", "parade_agent":"missing",
	  "agents":[{"name":"cos"}]}`)); err == nil {
		t.Fatal("unknown parade_agent must fail closed")
	}
	if _, err := Load(writeRoster(t, `{
	  "parade_agent":"parade-desk", "agents":[{"name":"parade-desk"}]}`)); err == nil {
		t.Fatal("parade_agent without cos_agent fallback must fail closed")
	}
}
