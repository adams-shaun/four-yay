package templates

import (
	"testing"

	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// TestHedgeShredderResidueIsServed: Hedge Shredder's library-to-graveyard row
// (its body moves ChangeType$ Card.TriggeredCards) was a named gap until main
// implemented the TriggeredCards predicate (merge ec0a97a7d). Now the mill
// cause serves it: the row must generate, play through gorge, cast the mill
// probe, and show the row's own trigger on the stack. It fails in both
// directions -- a regression to the named skip fails the generation assert, a
// cause that never fires fails the stack assert.
func TestHedgeShredderResidueIsServed(t *testing.T) {
	reg := loadGenRegistry(t)
	it := triggerRequirement(t, reg, "Hedge Shredder", "trigger#0.1", "trigger.zone-change-residue")
	milled := false
	for _, st := range it.Scenario.Steps {
		if st.Op == "cast" && st.Card == "p0:Tome Scour" {
			milled = true
		}
	}
	if !milled {
		t.Fatalf("precondition: scenario never mills with Tome Scour: %+v", it.Scenario.Steps)
	}
	res, ok := oraclegen.PlaysThrough(reg, it.Scenario)
	if !ok || len(res.Fails) != 0 {
		t.Fatalf("does not play through gorge: ok=%v fails=%v", ok, res.Fails)
	}
	// The cause replays with the trailing resolves dropped (a resolve empties
	// the stack, spending the trigger it was meant to expose) and with one
	// pass round, then with the checkpoint pass_to: the mill trigger queues
	// behind the spell's resolution and only the checkpoint holds it on the
	// stack for the snapshot.
	cause := it.Scenario.Steps
	for len(cause) > 0 && cause[len(cause)-1].Op == "resolve" {
		cause = cause[:len(cause)-1]
	}
	passes := []oraclegen.Step{{Op: "pass", Seat: 0}, {Op: "pass", Seat: 1}}
	checkpoint := append(append([]oraclegen.Step(nil), passes...), oraclegen.Step{Op: "pass_to", Decision: "priority"})
	sc := it.Scenario
	found := false
	for _, try := range [][]oraclegen.Step{
		cause,
		append(append([]oraclegen.Step(nil), cause...), passes...),
		append(append([]oraclegen.Step(nil), cause...), checkpoint...),
	} {
		sc.Steps = try
		if _, res, ok := oraclegen.Settle(reg, sc); ok && abilityOnStack(res.Snapshots, []string{"hedge shredder"}, "1") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Hedge Shredder trigger slot 1 never reaches the stack: %+v", it.Scenario.Steps)
	}
	// The row is served, not a named gap: the residue's named-skip register
	// must not take it back.
	c, ok := reg.Lookup("Hedge Shredder")
	if !ok {
		t.Fatalf("Hedge Shredder not in the corpus")
	}
	for _, r := range levelb.Requirements(c) {
		if r.Key == "trigger#0.1" && r.Gap != "" {
			t.Fatalf("Hedge Shredder trigger#0.1 is a named gap again: %+v", r)
		}
	}
}
