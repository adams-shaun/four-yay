package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestSojournersEnforcermiteAffinityCountsAffinityPermanents drives the real
// Sojourner's Enforcermite script. Its "Affinity for Affinity" quality names
// the Affinity keyword, not a card type (CR 702.41), so each real affinity
// permanent on the controller's battlefield lowers the generic cost by one.
func TestSojournersEnforcermiteAffinityCountsAffinityPermanents(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	enforcermiteCard := lookup(t, reg, "Sojourner's Enforcermite")
	frogmiteCard := lookup(t, reg, "Frogmite")
	myrEnforcerCard := lookup(t, reg, "Myr Enforcer")

	e, cfg := corpusEngineCfg(t, reg,
		[]*cards.Card{enforcermiteCard, frogmiteCard, myrEnforcerCard}, nil)
	enforcermite := moveByName(t, e, 0, "Sojourner's Enforcermite", state.ZHand)
	if e.G.Obj(enforcermite).Zone != state.ZHand {
		t.Fatalf("precondition: Sojourner's Enforcermite is in %s, want hand", e.G.Obj(enforcermite).Zone)
	}
	face := e.G.Obj(enforcermite).Face()
	if face == nil || !face.HasKeyword("Affinity") {
		t.Fatalf("precondition: real Sojourner's Enforcermite lacks Affinity: %+v", face)
	}
	if got := reduceOf(t, e, 0, enforcermite); got != 0 {
		t.Fatalf("precondition: no affinity permanents should reduce the cost, got %d", got)
	}

	frogmite := moveByName(t, e, 0, "Frogmite", state.ZBattlefield)
	if o := e.G.Obj(frogmite); o == nil || o.Zone != state.ZBattlefield || o.Face() == nil || !o.Face().HasKeyword("Affinity") {
		t.Fatalf("precondition: Frogmite is not an affinity permanent on the battlefield: %+v", e.G.Obj(frogmite))
	}
	if got := reduceOf(t, e, 0, enforcermite); got != 1 {
		t.Fatalf("one affinity permanent should reduce by 1, got %d", got)
	}

	myrEnforcer := moveByName(t, e, 0, "Myr Enforcer", state.ZBattlefield)
	if o := e.G.Obj(myrEnforcer); o == nil || o.Zone != state.ZBattlefield || o.Face() == nil || !o.Face().HasKeyword("Affinity") {
		t.Fatalf("precondition: Myr Enforcer is not an affinity permanent on the battlefield: %+v", e.G.Obj(myrEnforcer))
	}
	if got := reduceOf(t, e, 0, enforcermite); got != 2 {
		t.Fatalf("two affinity permanents should reduce by 2, got %d", got)
	}

	replayCheck(t, e, cfg)
}
