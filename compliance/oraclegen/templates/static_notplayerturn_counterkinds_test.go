package templates_test

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestStaticNotPlayerTurn: Midnight Mangler's "during turns other than yours,
// this Vehicle is an artifact creature" (Condition$ NotPlayerTurn) was
// unobservable because the bare scenario is p0's own turn, where the gate is
// false. The fixture places the card and advances the scenario to p1's first
// main phase, so the static is live at the final checkpoint. The precondition
// asserts the card is printed as a non-creature and the scenario really moves
// to p1's turn, so the test cannot pass on a card that was already a creature
// or on a scenario still on p0's turn.
func TestStaticNotPlayerTurn(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name = "Midnight Mangler"
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("precondition: %s not in the corpus", name)
	}
	if c.Faces[0].IsCreature() {
		t.Fatalf("precondition: %s is printed as a creature, so the type change is not observable", name)
	}
	it, final := selfFilterItem(t, reg, name, "static#0.0")
	moved := false
	for _, st := range it.Steps {
		if st.Op == "pass_to" && st.Active == "p1" {
			moved = true
		}
	}
	if !moved {
		t.Fatalf("%s: scenario does not advance to p1's turn: %+v", name, it.Steps)
	}
	p, ok := permNamedSnap(final, name)
	if !ok {
		t.Fatalf("%s not on the final battlefield", name)
	}
	if !slices.Contains(p.Types, "Creature") {
		t.Fatalf("%s types = %v, want Creature added on p1's turn", name, p.Types)
	}
}

// TestStaticDifferentCounterKinds: Hundred-Battle Veteran's "as long as there
// are three or more different kinds of counters among creatures you control"
// pump (CheckSVar$ X with SVar:X:Count$DifferentCounterKinds_Creature.YouCtrl,
// SVarCompare$ GE3) was unobservable because the bare scenario leaves the count
// at zero. The fixture places a creature holding three distinct counter kinds,
// so the count reaches the gate and the card itself is pumped. The
// precondition asserts the counter-bearing creature is not the compared probe
// and holds the three distinct kinds the gate reads.
func TestStaticDifferentCounterKinds(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const name = "Hundred-Battle Veteran"
	c, ok := reg.Lookup(name)
	if !ok {
		t.Fatalf("precondition: %s not in the corpus", name)
	}
	printed := c.Faces[0].PT
	if printed != "4/2" {
		t.Fatalf("precondition: %s printed P/T = %q, want 4/2", name, printed)
	}
	it, final := selfFilterItem(t, reg, name, "static#0.0")
	holder, kinds := "", 0
	for card, ks := range it.Setup["p0"].Counters {
		if len(ks) > kinds {
			holder, kinds = card, len(ks)
		}
	}
	if holder == "" || kinds < 3 {
		t.Fatalf("%s: no fixture creature holds three distinct counter kinds: %v", name, it.Setup["p0"].Counters)
	}
	if holder == "Grizzly Bears" {
		t.Fatalf("%s: the counters are on the probe, which would shift its compared P/T", name)
	}
	p, ok := permNamedSnap(final, name)
	if !ok {
		t.Fatalf("%s not on the final battlefield", name)
	}
	if p.PT == printed {
		t.Fatalf("%s P/T %s equals printed %s; the different-counter-kinds gate did not pump it", name, p.PT, printed)
	}
}
