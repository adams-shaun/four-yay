package templates

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// Level-B D6 "mana pool colour" rows: the generated item's step-0 checkpoint
// must show the pool both engines can agree on. Each case replays the item
// gorge's runner sees (Item.Raw) and asserts the pool at the diverging
// checkpoint, plus the precondition the assertion rides on.
//
// generateItem is the same path the set manifest's gen writes: the card's
// level-B requirement for key, through GenerateB.
func generateItem(t *testing.T, reg *cards.Registry, name, key string) oraclegen.Item {
	t.Helper()
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 {
		t.Fatalf("precondition: %s not in the corpus", name)
	}
	for _, r := range levelb.Requirements(c) {
		if r.Key != key {
			continue
		}
		it, skip := GenerateB(reg, name, r)
		if skip != nil {
			t.Fatalf("%s %s does not generate: %s", name, key, skip.Reason)
		}
		return it
	}
	t.Fatalf("precondition: %s carries no level-B requirement %s", name, key)
	return oraclegen.Item{}
}

// poolAtCheckpoint replays the item and returns p0's pool at the checkpoint
// whose label contains want.
func poolAtCheckpoint(t *testing.T, reg *cards.Registry, it oraclegen.Item, want string) string {
	t.Helper()
	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("replay failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	for i := range res.Snapshots {
		if strings.Contains(res.Snapshots[i].Checkpoint, want) {
			return res.Snapshots[i].Players[0].Pool
		}
	}
	t.Fatalf("no checkpoint matching %q\n%s", want, strings.Join(res.Transcript, "\n"))
	return ""
}

// A cast of a Convoke spell must pay the whole cost from the pool when no
// convoke tap is scripted: gorge's forced convoke tap used to leave one
// floating generic while a pool-paid cast spends everything (XMage pays the
// pool), the exact D6 divergence on The Wandering Rescuer and Lofty Dreams.
func TestConvokeCastPoolAgrees(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct{ card string }{
		{"The Wandering Rescuer"},
		{"Lofty Dreams"},
	} {
		it := generateItem(t, reg, tc.card, "static#0.0")
		// Precondition: the cast really is in the scenario's steps.
		if !strings.Contains(string(it.Raw()), `"op":"cast"`) {
			t.Fatalf("%s: scenario has no cast step: %s", tc.card, string(it.Raw()))
		}
		if p := poolAtCheckpoint(t, reg, it, "step 0 (cast)"); p != "" {
			t.Fatalf("%s: step-0 (cast) pool = %q, want \"\": the convoke cast must pay from the pool with no tap scripted", tc.card, p)
		}
	}
}

// Itlimoc's second ability ({T}: Add {G} for each creature you control) with
// zero creatures must produce nothing at the activation checkpoint, and its
// first ability ({T}: Add {G}.) must still produce G: the two pools differ,
// which is what the label-ambiguous wheel used to collapse (the D6 row on
// activate#1.1).
func TestItlimocActivatePoolsDiffer(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	it := generateItem(t, reg, "Growing Rites of Itlimoc", "activate#1.1")
	if p := poolAtCheckpoint(t, reg, it, "step 0 (activate)"); p != "" {
		t.Fatalf("activate#1.1: step-0 pool = %q, want \"\": zero creatures must produce nothing", p)
	}
	first := generateItem(t, reg, "Growing Rites of Itlimoc", "activate#1.0")
	if p := poolAtCheckpoint(t, reg, first, "step 0 (activate)"); p != "G" {
		t.Fatalf("activate#1.0: step-0 pool = %q, want \"G\" (the sibling ability the pool contrast rides on)", p)
	}
}
