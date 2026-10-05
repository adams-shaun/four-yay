package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestOracleScenarioDecodesTargetGroups: a generated multi-slot cast carries
// per-slot target groups for the XMage driver. decodeOracleScenario rejects
// unknown fields, so the runner's step type must accept the field even
// though it selects targets from Targets -- without it every grouped
// scenario is a harness error. The scenario below is Lightning Bolt with the
// same target expressed as one group; the play must be identical.
func TestOracleScenarioDecodesTargetGroups(t *testing.T) {
	const grouped = `{
  "name": "bolt-the-bears-grouped", "cr": ["608.2"], "why": "target group decode",
  "setup": {
    "p0": {"hand": ["Lightning Bolt"]},
    "p1": {"battlefield": ["Grizzly Bears", "Grizzly Bears"], "library_top": ["Shock", "Lightning Bolt"]}
  },
  "steps": [
    {"op": "cast", "seat": 0, "card": "p0:Lightning Bolt", "mana": "R", "targets": ["p1:Grizzly Bears#2"],
     "target_groups": [{"picks": ["p1:Grizzly Bears#2"], "max": 0}]},
    {"op": "resolve"}
  ],
  "expect": [{"card": "p1:Grizzly Bears#2", "zone": "graveyard"}]
}`
	reg := testutil.CorpusRegistry(t)
	res, err := RunOracleScenarioJSON(reg, []byte(grouped))
	if err != nil {
		t.Fatalf("grouped scenario did not decode: %v", err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	// Precondition: the run really posed and resolved the cast (three
	// checkpoints), so a decoded-but-ignored field is still exercised.
	if len(res.Snapshots) != 3 {
		t.Fatalf("%d snapshots, want 3", len(res.Snapshots))
	}
}

// TestOracleScenarioRejectsUnknownStepField pins the strict decoder this
// ticket had to satisfy: an unknown field is still an error, so the fix is
// the explicit TargetGroups field, not relaxing the decoder.
func TestOracleScenarioRejectsUnknownStepField(t *testing.T) {
	_, err := RunOracleScenarioJSON(nil, []byte(`{"name":"x","steps":[{"op":"cast","bogus":1}]}`))
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Fatalf("want an unknown-field error naming bogus, got %v", err)
	}
}
