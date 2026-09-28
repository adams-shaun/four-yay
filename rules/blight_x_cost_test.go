package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// findBlightedNightmareAbility returns the index of Blighted Nightmare's
// "Blight X, Return this enchantment to its owner's hand" activated ability
// (the corpus's Blight<X> activator).
func findBlightedNightmareAbility(t *testing.T, reg *cards.Registry) int {
	t.Helper()
	f := searchCorpusCard(t, reg, "Blighted Nightmare").Faces[0]
	for i, sa := range f.Abilities {
		if sa.Kind == "AB" && sa.API == "ChangeZone" {
			return i
		}
	}
	t.Fatalf("no ChangeZone ability on Blighted Nightmare: %+v", f.Abilities)
	return -1
}

// TestBlightXCostAnnouncesAndPaysX is the Blight<X> cost end-to-end carrier:
// Blighted Nightmare's `Cost$ Blight<X> Return<1/CARDNAME> | Announce$ X`
// ability is activated, X is announced as the greatest toughness among the
// activator's creatures (CR 601.2b / the card's `XMax$ GrTo`), the payment
// places exactly X -1/-1 counters on the chosen creature, and the X-dependent
// effect (return a creature card with mana value X or less) returns the
// cmc-2 bear -- impossible at X < 2.
func TestBlightXCostAnnouncesAndPaysX(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := blightEngine(t, reg, 2, "Blighted Nightmare")
	nightmare := blightMove(t, e, 0, "Blighted Nightmare", state.ZBattlefield)
	bearA := blightMove(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	bearB := blightMove(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	grave := blightMove(t, e, 0, "Grizzly Bears", state.ZGraveyard)
	// Preconditions: both bears are 2/2 on the battlefield (so the announced
	// cap is 2 and the blight pick is a real ask), and the target card is a
	// cmc-2 creature card in the graveyard.
	if o := e.G.Obj(bearA); o == nil || o.Zone != state.ZBattlefield || e.Toughness(bearA) != 2 {
		t.Fatalf("precondition: bearA zone=%v toughness=%d, want battlefield/2", o, e.Toughness(bearA))
	}
	if o := e.G.Obj(bearB); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: bearB zone=%v, want battlefield", o)
	}
	if o := e.G.Obj(grave); o == nil || o.Zone != state.ZGraveyard || o.Face() == nil ||
		o.Face().Name != "Grizzly Bears" || !slices.Contains(o.Face().Types, "Creature") {
		t.Fatalf("precondition: target card zone=%v face=%v, want a Grizzly Bears creature card in the graveyard", o, o.Face())
	}
	// Blighted Nightmare's enters-the-battlefield trigger is on the stack
	// after the moves; let it resolve so the sorcery-speed ability is offered
	// (a non-empty stack withholds every SorcerySpeed$ True activation).
	passUntilStackEmpty(t, e, 20)
	passToSeat(t, e, 0)

	ab := findBlightedNightmareAbility(t, reg)
	submitChoices(t, e, abilityOption(t, e, nightmare, ab).Index)

	// CR 601.2b: the announced X. The cap is the greatest toughness among the
	// activator's creatures (2), so X=2 is offered and X=3 is not.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "x" {
		t.Fatalf("want an announced-X choice, got %+v", d)
	}
	xIdx, xMax := -1, int32(-1)
	for _, o := range d.Options {
		if int32(o.Amount) > xMax {
			xMax = int32(o.Amount)
		}
		if o.Amount == 2 {
			xIdx = o.Index
		}
	}
	if xMax != 2 {
		t.Fatalf("announced-X cap = %d, want 2 (greatest toughness among two 2/2 bears): %+v", xMax, d.Options)
	}
	if xIdx < 0 {
		t.Fatalf("X=2 not offered: %+v", d.Options)
	}
	submitChoices(t, e, xIdx)

	// The blight pick, the Return<1/CARDNAME> cost and the X-bound target ask.
	picked := state.ObjID(0)
	targeted := false
	for i := 0; i < 12; i++ {
		d = e.Pending()
		if d == nil || d.Kind == decision.KPriority {
			break
		}
		switch {
		case d.Kind == decision.KChoose && len(d.Options) > 0 && d.Options[0].Kind == "blightcost":
			picked = d.Options[0].Obj
			submitChoices(t, e, d.Options[0].Index)
		case d.Kind == decision.KTarget:
			tidx := -1
			for _, o := range d.Options {
				if o.Obj == grave {
					tidx = o.Index
				}
			}
			if tidx < 0 {
				t.Fatalf("cmc-2 graveyard card %d not offered at X=2: %+v", grave, d.Options)
			}
			submitChoices(t, e, tidx)
			targeted = true
		default:
			t.Fatalf("unexpected decision: %+v", d)
		}
	}
	passUntilStackEmpty(t, e, 30)

	if !targeted {
		t.Fatal("the ability never posed its X-bound target ask")
	}
	if picked != bearA && picked != bearB {
		t.Fatalf("blight pick %d was not one of the controlled bears (%d/%d)", picked, bearA, bearB)
	}
	if got := blightCounters(t, e)[picked]; got != 2 {
		t.Fatalf("paid Blight<X=2> placed %d counters on %d, want 2", got, picked)
	}
	// The X-dependent effect: a mana value X or less card returned from the
	// graveyard. The cmc-2 bear could not have returned at X < 2.
	if o := e.G.Obj(grave); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("X-dependent reanimation missing: graveyard card zone=%v", o)
	}
	if o := e.G.Obj(nightmare); o == nil || o.Zone != state.ZHand {
		t.Fatalf("Return<1/CARDNAME> cost did not return the source to hand: zone=%v", o)
	}
}

// TestBlightXNoManaCeiling guards the announcement bound when the only X
// cost is blighting: the mana/graveyard ceiling must not cap a mana-free X.
func TestBlightXNoManaCeiling(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := blightEngine(t, reg, 2)
	bear := blightMove(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield || e.Toughness(bear) != 2 {
		t.Fatalf("precondition: bear %d must be a 2-toughness battlefield creature", bear)
	}
	if pool, gy := e.G.Players[0].Pool.Total(), len(e.G.Zone(state.ZGraveyard, 0)); pool != 0 || gy != 0 {
		t.Fatalf("precondition: need zero mana and graveyard cards, got pool=%d grave=%d", pool, gy)
	}
	cost := ParseCost("Blight<X>")
	if len(cost.Blight) != 1 || !cost.Blight[0].Announced || len(cost.Unknown) != 0 {
		t.Fatalf("precondition: Blight<X> not parsed as announced cost: %+v", cost)
	}
	e.cast = &pendingCast{player: 0, card: bear, cost: cost}
	if !e.xAsk() {
		t.Fatal("Blight<X> did not pose an X announcement")
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("want X choice, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "x" && o.Amount == 2 {
			return
		}
	}
	t.Fatalf("X=2 withheld by mana ceiling despite 2-toughness blight candidate: %+v", d.Options)
}
