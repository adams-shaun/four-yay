package templates

// Task agent-20261009T091513Z-3c5153cc: the compliance pool probe for the
// ChooseColor -> DB$ Mana chain. ECL Foraging Wickermaw's generated
// activate#0.0 scenario must show the produced mana in the pool at the
// ACTIVATION checkpoint (step 0), matching XMage's frozen expectation -- the
// D6 triage row "step 0 (activate) p0.pool: gorge \"\", xmage \"W\"".

import (
	"path/filepath"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/rules"
)

func TestForagingWickermawChainPoolAtTheActivateCheckpoint(t *testing.T) {
	reg, err := cards.SharedCorpus(filepath.Join("..", "..", "..", ".cards"))
	if err != nil {
		t.Skipf("corpus unavailable: %v", err)
	}
	c, ok := reg.Lookup("Foraging Wickermaw")
	if !ok {
		t.Fatalf("precondition: Foraging Wickermaw missing from the corpus")
	}
	var req *levelb.Requirement
	for _, r := range levelb.Requirements(c) {
		if r.Key == "activate#0.0" {
			r := r
			req = &r
		}
	}
	if req == nil {
		t.Fatalf("precondition: Foraging Wickermaw has no activate#0.0 requirement")
	}
	it, skip := GenerateB(reg, "Foraging Wickermaw", *req)
	if skip != nil {
		t.Fatalf("activate#0.0 did not generate: %s", skip.Reason)
	}
	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	if err != nil {
		t.Fatalf("run generated scenario: %v", err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("generated scenario failed: %v", res.Fails)
	}
	act := -1
	for i, st := range it.Steps {
		if st.Op == "activate" {
			act = i
		}
	}
	if act < 0 {
		t.Fatalf("precondition: generated scenario has no activate step: %+v", it.Steps)
	}
	if act+1 >= len(res.Snapshots) {
		t.Fatalf("precondition: no snapshot for the activate step: %d snapshots", len(res.Snapshots))
	}
	snap := res.Snapshots[act+1]
	if snap.Players[0].Pool != "W" {
		t.Fatalf("pool at the activate checkpoint = %q, want W (the D6 XMage expectation)", snap.Players[0].Pool)
	}
}
