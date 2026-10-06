package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestThreeTreeScribeFiresOnRealCorpusDestinationList pins the filed defect
// end to end with the REAL corpus card. Three Tree Scribe's trigger carries
// `Destination$ Ante,Command,Exile,Hand,Library`; before Destination$ was read
// as a zone set the whole string degraded to one word, so exile (Swords to
// Plowshares / Path to Exile, the filed repro) and command queued no trigger.
// The trigger grants "put a +1/+1 counter on target creature you control", so
// a target creature must be on p0's battlefield or the mandatory-target gate
// would drop the trigger for want of a target and the assertion would be
// vacuous (I-2).
func TestThreeTreeScribeFiresOnRealCorpusDestinationList(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	scribe, ok := reg.Lookup("Three Tree Scribe")
	if !ok {
		t.Fatal("corpus has no Three Tree Scribe")
	}
	for _, to := range []state.Zone{state.ZExile, state.ZCommand} {
		t.Run(to.String(), func(t *testing.T) {
			t.Parallel()
			e := combatEngine(t)
			src := onBoardCard(t, e, 0, scribe)
			// A legal target for the trigger's +1/+1 counter, so the trigger
			// is not dropped for infeasibility when it is put on the stack.
			target := onBoardCard(t, e, 0, card(t, originListMoverScript))
			if src == target || e.G.Obj(src).Zone != state.ZBattlefield || e.G.Obj(target).Zone != state.ZBattlefield {
				t.Fatal("precondition: scribe and target must be distinct battlefield permanents")
			}
			before := len(e.pendingTriggers)
			movedFrom(t, e, src, state.ZBattlefield, to)
			queuedTriggers(t, e, before, 1, "Three Tree Scribe leaving to "+to.String())
		})
	}
}

// TestThreeTreeScribeExcludesGraveyard is the negative arm: the list omits the
// graveyard, so dying must not queue the trigger. It also proves the fire arms
// above are not vacuous: the same card and setup produce no trigger when the
// destination is the one zone the list leaves out.
func TestThreeTreeScribeExcludesGraveyard(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	scribe, ok := reg.Lookup("Three Tree Scribe")
	if !ok {
		t.Fatal("corpus has no Three Tree Scribe")
	}
	e := combatEngine(t)
	src := onBoardCard(t, e, 0, scribe)
	target := onBoardCard(t, e, 0, card(t, originListMoverScript))
	if src == target || e.G.Obj(src).Zone != state.ZBattlefield || e.G.Obj(target).Zone != state.ZBattlefield {
		t.Fatal("precondition: scribe and target must be distinct battlefield permanents")
	}
	before := len(e.pendingTriggers)
	movedFrom(t, e, src, state.ZBattlefield, state.ZGraveyard)
	queuedTriggers(t, e, before, 0, "Three Tree Scribe excludes dying")
}
