package rules

// Count$ValidStack end-to-end: the stack is ONE shared list (state.Game.Zone
// returns g.Stack for every seat), so the ValidStack count head must scan it
// exactly once, not once per alive seat. The eval-level pin for that lives in
// effects/count_test.go (TestEvalCountValidStackScansTheSharedStackOnce); this
// file pins the REAL corpus carrier end to end: Mindbreak Trap's
// `TargetMax$ MaxTgts` with `SVar:MaxTgts:Count$ValidStack Card`, read through
// the ordinary cast flow on a 2-seat table. Before the fix the announcement
// ask read two spells on a table holding one (a duplicated bound); after it,
// one.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// mindbreakTrapMaxTgtsEngine puts the real corpus Mindbreak Trap and a
// targetless creature spell in seat 0's hand on a 2-seat table, funds the
// pool, and returns the engine plus both hand object ids.
func mindbreakTrapMaxTgtsEngine(t *testing.T) (*Engine, state.ObjID, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	trap, ok := reg.Lookup("Mindbreak Trap")
	if !ok {
		t.Fatal("corpus card Mindbreak Trap not found")
	}
	bearSrc := "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e := handEngine(t, trap, card(t, bearSrc))

	var trapID, bearID state.ObjID
	for _, id := range e.G.Zone(state.ZHand, 0) {
		switch e.G.Obj(id).Face().Name {
		case "Mindbreak Trap":
			trapID = id
		case "Bear":
			bearID = id
		}
	}
	if trapID == 0 || bearID == 0 {
		t.Fatalf("fixture hand missing cards: trap=%d bear=%d", trapID, bearID)
	}
	// {2}{U}{U} for the Trap, plus {1}{G} for the Bear.
	e.G.Players[0].Pool[state.MU] = 4
	e.G.Players[0].Pool[state.MG] = 4
	e.askPriority(0)
	return e, trapID, bearID
}

// TestMindbreakTrapMaxTgtsCountsTheSharedStackOnce drives the real carrier
// through cast/askTarget with exactly one spell on the stack of a 2-seat
// game. The preconditions (two alive seats, one stack card, the carrier bound
// is dynamic) are asserted so a broken setup fails loudly rather than pinning
// a vacuous 1.
func TestMindbreakTrapMaxTgtsCountsTheSharedStackOnce(t *testing.T) {
	t.Parallel()
	e, trapID, bearID := mindbreakTrapMaxTgtsEngine(t)

	// Precondition: two alive seats -- a per-seat stack walk would visit the
	// one stack object twice.
	if alive := e.G.AliveFrom(0); len(alive) != 2 {
		t.Fatalf("fixture precondition: %d alive seats, want 2", len(alive))
	}
	// Precondition: the carrier's bound really is dynamic (an SVar, not a
	// literal) -- otherwise the count head is never reached.
	trapSA := e.G.Obj(trapID).Face().SpellAbility()
	if trapSA.Params["TargetMax"] != "MaxTgts" {
		t.Fatalf("fixture precondition: TargetMax$ = %q, want MaxTgts", trapSA.Params["TargetMax"])
	}
	if got := e.G.Obj(trapID).Face().SVars["MaxTgts"]; got != "Count$ValidStack Card" {
		t.Fatalf("fixture precondition: SVar:MaxTgts = %q, want Count$ValidStack Card", got)
	}

	// Cast the Bear (no targets): it sits on the stack while seat 0 keeps
	// priority. The Trap is still in hand, so a direct bound read counts
	// exactly one card on the shared stack.
	submitChoices(t, e, passToCast(t, e, bearID))
	if n := len(e.G.Stack); n != 1 {
		t.Fatalf("fixture precondition: stack holds %d cards, want 1", n)
	}
	// The double-count discriminator: read the real carrier's dynamic bound
	// while exactly one card is on the shared stack of a 2-seat game. The
	// pre-fix per-seat walk read 2 here. (resolvedTargetBounds is the same
	// resolver askTarget uses for the announcement ask.)
	_, directMax := e.resolvedTargetBounds(0, trapID, trapSA, 0)
	if directMax != 1 {
		t.Fatalf("resolvedTargetBounds Max = %d, want 1 (one card on the shared stack, two seats)", directMax)
	}

	// Respond with Mindbreak Trap: the announcement ask's Max is the bound.
	// The Trap itself is on the stack by the time its target is chosen, so
	// the honest count is 2 (the Bear plus the Trap); the pre-fix
	// double-count read 4 here.
	submitChoices(t, e, passToCast(t, e, trapID))
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want the Trap's target ask, got %+v", d)
	}
	if len(e.G.Stack) != 2 {
		t.Fatalf("fixture precondition: stack holds %d cards after the Trap is cast, want 2", len(e.G.Stack))
	}
	if d.Max != 2 {
		t.Fatalf("Mindbreak Trap target ask Max = %d, want 2 (the Bear plus the Trap on the shared stack)", d.Max)
	}
	// The single legal target is the Bear spell.
	if len(d.Options) != 1 {
		t.Fatalf("target ask offered %d options, want 1 (the one spell)", len(d.Options))
	}
	submitChoices(t, e, d.Options[0].Index)
	attachDrain(t, e, 80)
}
