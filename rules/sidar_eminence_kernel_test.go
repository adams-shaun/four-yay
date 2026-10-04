package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestSidarJabariEminenceTriggersFromCommandZoneKernel: with Sidar in seat
// 0's command zone and a controlled Knight declared attacking, the Eminence
// ability queues exactly once and, on resolution, draws a card then poses
// its discard; the answer discards one card.
func TestSidarJabariEminenceTriggersFromCommandZoneKernel(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	sidar := mustCorpusCard(t, reg, "Sidar Jabari of Zhalfir")

	e, sidarID, knight := eminenceBoard(t, sidar, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	handBefore := len(e.G.Zone(state.ZHand, 0))

	declareAttackersReal(t, e, knight)
	if !e.G.Obj(knight).IsAttacking {
		t.Fatal("precondition: the declared Knight is not marked attacking")
	}
	if e.G.Obj(sidarID).IsAttacking {
		t.Fatal("precondition: Sidar must not be an attacker")
	}

	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("Eminence did not queue exactly once: stack = %v, want one trigger", e.G.Stack)
	}
	trig := e.G.Obj(e.G.Stack[0])
	if trig == nil || trig.Ability == nil {
		t.Fatalf("stack top is not a trigger ability: %+v", trig)
	}
	if trig.Source != sidarID {
		t.Fatalf("stack top source = %d, want Sidar %d", trig.Source, sidarID)
	}

	// Resolve under the kernel (no stale priority snapshot pending, so the
	// probe owns the ask): draw a card, then the discard ask.
	kr9ResolveTop(e)
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore+1 {
		t.Fatalf("Eminence draw: hand = %d, want %d (one drawn)", got, handBefore+1)
	}
	d := e.Pending()
	if d == nil || d.ResumeKind != "discard" || len(d.Options) == 0 {
		t.Fatalf("Eminence did not pose its discard after the draw: %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	if got := len(e.G.Zone(state.ZHand, 0)); got != handBefore {
		t.Fatalf("Eminence discard: hand = %d, want %d (drew one, discarded one)", got, handBefore)
	}
	if len(e.G.Zone(state.ZGraveyard, 0)) == 0 {
		t.Fatal("no card in the graveyard after the Eminence discard")
	}
}
