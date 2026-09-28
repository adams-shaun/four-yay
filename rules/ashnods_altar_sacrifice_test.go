package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestAshnodsAltarPrioritySacrificeAsksWhichCreature pins the reported
// fb-20260921T193934Z-c815f630 shape on the card itself: activating Ashnod's
// Altar for mana at priority with two creatures poses a one-of-two
// sacrifice ask (fixed by 3946193d) instead of auto-sacrificing the
// first-eligible creature.
func TestAshnodsAltarPrioritySacrificeAsksWhichCreature(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, ids := realCardEngine(t, reg, 208, "Ashnod's Altar", "Gleaming Barrier", "Goblin Guide")
	altar, creature, other := ids[0], ids[1], ids[2]

	// Precondition: both creatures are on the battlefield, so the sacrifice
	// cost really has two legal candidates and the ask must offer both.
	if e.G.Obj(creature).Zone != state.ZBattlefield || e.G.Obj(other).Zone != state.ZBattlefield {
		t.Fatalf("precondition: candidates on battlefield = %s/%s, want both battlefield",
			e.G.Obj(creature).Zone, e.G.Obj(other).Zone)
	}
	if creature == other {
		t.Fatal("precondition: creature ids are identical, want two distinct candidates")
	}

	submitChoices(t, e, activateOption(t, e, altar))
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 1 || d.Max != 1 || len(d.Options) != 2 {
		t.Fatalf("Ashnod's Altar decision = %+v, want one-of-two sacrifice ask", d)
	}
	for _, o := range d.Options {
		if o.Obj != creature && o.Obj != other {
			t.Fatalf("unexpected sacrifice option: %+v", o)
		}
	}
	chooseSacrificeAnswer(t, e, d, creature)
	if e.G.Obj(creature).Zone != state.ZGraveyard {
		t.Fatalf("chosen creature zone = %s, want graveyard", e.G.Obj(creature).Zone)
	}
	if e.G.Obj(altar).Zone != state.ZBattlefield {
		t.Fatalf("altar zone = %s, want still on the battlefield", e.G.Obj(altar).Zone)
	}
	if e.G.Players[0].Pool[state.MC] != 2 {
		t.Fatalf("pool = %+v, want two colorless", e.G.Players[0].Pool)
	}
}

// TestAshnodsAltarSingleCreatureSkipsTheForcedAsk pins the type-spec half of
// the forced-skip contract (7e3c0ca3) on the same real card: with exactly one
// legal <1/Creature> candidate the sacrifice is zero-information, so the
// activation pays it without posing a sacrifice choice and re-poses priority.
func TestAshnodsAltarSingleCreatureSkipsTheForcedAsk(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, ids := realCardEngine(t, reg, 209, "Ashnod's Altar", "Gleaming Barrier")
	altar, creature := ids[0], ids[1]

	// Precondition: the sole creature is on the battlefield, so the cost has
	// exactly one candidate and the forced skip is the behaviour under test.
	if e.G.Obj(creature).Zone != state.ZBattlefield {
		t.Fatalf("precondition: creature zone = %s, want battlefield", e.G.Obj(creature).Zone)
	}

	submitChoices(t, e, activateOption(t, e, altar))
	if d := e.Pending(); d != nil {
		if d.Kind == decision.KChoose {
			t.Fatalf("single-candidate sacrifice posed a choice: %+v", d)
		}
		for _, o := range d.Options {
			if o.Kind == "sacrifice" {
				t.Fatalf("single-candidate sacrifice posed a sacrifice option: %+v", d.Options)
			}
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("after the forced sacrifice = %+v, want priority re-posed", d)
		}
	}
	if z := e.G.Obj(creature).Zone; z != state.ZGraveyard {
		t.Fatalf("sole creature zone = %s, want graveyard", z)
	}
	if e.G.Players[0].Pool[state.MC] != 2 {
		t.Fatalf("pool = %+v, want two colorless", e.G.Players[0].Pool)
	}
}
