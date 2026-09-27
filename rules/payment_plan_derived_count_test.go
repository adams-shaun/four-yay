package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// A mana ability whose Amount$ counts a TYPE must be priced by the planner
// against the same layer-4 derived types its resolution reads. effects.Resolve
// binds the engine's derived-type table on the resolving Ctx, so Cloudpost's
// Count$Valid Locus counts an opponent's Planar Nexus ("is every nonbasic land
// type", a characteristic-defining type change) and adds {C}{C}; the
// planner's castWindowAmount built a bare Ctx that read printed faces only,
// promised {C}, and run A's witness read wrong_production (cardfuzz mirror
// seed 1526572289851508430: "Cloudpost planned C got CC").

const planLocusPost = "Name:Plan Locus Post\nTypes:Land Locus\n" +
	"A:AB$ Mana | Cost$ T | Produced$ C | Amount$ X | SpellDescription$ Add {C} for each Locus on the battlefield.\n" +
	"SVar:X:Count$Valid Locus\nOracle:x\n"

const planEveryTypeLand = "Name:Plan Every Type Land\nTypes:Land\n" +
	"S:Mode$ Continuous | Affected$ Card.Self | CharacteristicDefining$ True | AddType$ AllNonBasicLandType | Description$ CARDNAME is every nonbasic land type.\n" +
	"A:AB$ Mana | Cost$ T | Produced$ C | SpellDescription$ Add {C}.\nOracle:x\n"

const planTwoSpell = "Name:Plan Two\nManaCost:2\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n"

func TestPaymentPlanPricesCountAmountWithDerivedTypes(t *testing.T) {
	spellCard, post, nexus := card(t, planTwoSpell), card(t, planLocusPost), card(t, planEveryTypeLand)
	// NameUniverse carries the land-subtype vocabulary AllNonBasicLandType
	// expands against (corpusLandTypeWords), as a hosted match's corpus does.
	cfg := seatZeroStart(Config{Seed: 9413, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{spellCard, post}, mountainDeck(t, 38)...),
			append([]*cards.Card{nexus}, mountainDeck(t, 39)...),
		},
		Tokens:       map[string]*cards.Card{},
		NameUniverse: []*cards.Card{spellCard, post, nexus},
	})
	e := New(cfg)
	e.Advance()
	spell := findAndMoveTo(t, e, 0, "Plan Two", state.ZHand)
	postID := findAndMoveToBattlefield(t, e, 0, "Plan Locus Post")
	findAndMoveToBattlefield(t, e, 1, "Plan Every Type Land")
	driveToStep(t, e, 1, 0, state.StepMain1)

	got := e.PlanCastPayment(0, paymentCast(spell))
	if got.Plan == nil {
		t.Fatalf("no plan: %+v", got)
	}
	if len(got.Plan.Activations) != 1 || got.Plan.Activations[0].Source != postID {
		t.Fatalf("plan = %#v, want the Locus Post alone (it pays the {2} by itself)", got.Plan)
	}
	if n := got.Plan.Activations[0].Produces[state.MC]; n != 2 {
		t.Fatalf("Locus Post witness Produces colourless = %d, want 2: the opponent's land is a Locus through its layer-4 type change", n)
	}

	d := paymentPlanReask(t, e)
	a := paymentPlanActionFor(t, d, spell)
	submitPaymentPlan(t, e, d, a)
	if z := e.G.Obj(spell).Zone; z != state.ZStack {
		t.Fatalf("spell zone after the planned cast = %s, want stack (pending %s)", z, paymentPlanPendingSummary(e.Pending()))
	}
	if p := e.G.Players[0].Pool.Total(); p != 0 {
		t.Fatalf("pool after the planned cast = %d, want 0", p)
	}
	replayCheck(t, e, cfg)
}

// findAndMoveTo moves seat p's named card from hand or library to zone to (a
// no-op when it is already there), through the event the replay folds.
func findAndMoveTo(t *testing.T, e *Engine, p state.PlayerID, name string, to state.Zone) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(to, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	return moveByName(t, e, p, name, to)
}
