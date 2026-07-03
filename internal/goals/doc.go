// Package goals is the YAML authoring half of the fleet goals DAG (dash-next-gen D3).
//
// Coordinators maintain fleet-goals.yaml (human-editable, nested children or flat parent
// refs); this package validates fail-closed (acyclic tree, dangling depends_on rejected),
// normalizes work-item fields to the canonical JSON contract (#277: backlog match, desk
// agent, issue ref, inline text+done), and compiles to fleet-goals.json for the dash
// reader in internal/dash.
//
// Roll-up precedence lives in internal/dash.BuildGoals; tests here verify YAML→JSON→roll-up
// end-to-end so the two halves cannot drift.
package goals
