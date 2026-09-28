package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// PotentialPaymentPlans (rules/potential_plan.go): the planner's verdict on
// every potential play, with an exact witness for a printed activated
// ability's mana part.

const (
	ppFiller = "Name:Potential Filler\nManaCost:7\nTypes:Sorcery\nA:SP$ Draw | NumCards$ 1\nOracle:x\n"
	ppRelic  = "Name:Potential Relic\nTypes:Artifact\n" +
		"A:AB$ Draw | Cost$ 1 U | NumCards$ 1 | SpellDescription$ Draw a card.\nOracle:x\n"
	// ppTapLand taps itself for C, or taps itself plus {1} to draw: its own
	// tap can never pay the draw's {1}.
	ppTapLand = "Name:Potential Tap Land\nTypes:Land\n" +
		"A:AB$ Mana | Cost$ T | Produced$ C | SpellDescription$ Add {C}.\n" +
		"A:AB$ Draw | Cost$ 1 T | NumCards$ 1 | SpellDescription$ Draw a card.\nOracle:x\n"
	ppDual = "Name:Potential Dual\nTypes:Land Plains Island\nOracle:x\n"
)

func ppFind(t *testing.T, pps []PotentialPlan, kind string, obj state.ObjID) PotentialPlan {
	t.Helper()
	for _, pp := range pps {
		if pp.Action.Kind == kind && pp.Action.Obj == obj {
			return pp
		}
	}
	t.Fatalf("no potential %s for %d in %+v", kind, obj, pps)
	return PotentialPlan{}
}

// ppPriority re-poses seat 0's priority decision (the fixture's pending one
// may predate the placed permanents) and returns it.
func ppPriority(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	e.pending = nil
	e.askPriority(0)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("pending = %#v, want seat-0 priority", d)
	}
	return d
}

func ppChoose(t *testing.T, e *Engine, pred func(decision.Option) bool) {
	t.Helper()
	d := e.Pending()
	for _, o := range d.Options {
		if pred(o) {
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("no matching option in %+v", d.Options)
}

// TestPotentialPaymentPlansAbilityWitness: a {1}{U} ability over a Plains
// and an Island is planned exactly (both sources, U from the Island), the
// query is a pure read, and tapping the witness's sources on the manual
// surface makes the ability's own option offered.
func TestPotentialPaymentPlansAbilityWitness(t *testing.T) {
	t.Parallel()
	e, _, _ := newFixtureDeck(t, 9401, ppFiller)
	relic := onBoard(t, e, 0, ppRelic)
	plains := onBoard(t, e, 0, lrPlains)
	island := onBoard(t, e, 0, lrIsland)
	d := ppPriority(t, e)
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == relic {
			t.Fatal("the ability is offered on an empty pool")
		}
	}
	mark, seq := len(e.L.Events), d.Seq
	pps := e.PotentialPaymentPlans(0)
	if len(e.L.Events) != mark || e.Pending() != d || d.Seq != seq {
		t.Fatal("PotentialPaymentPlans is not a pure read")
	}
	pp := ppFind(t, pps, "ability", relic)
	if pp.Plan == nil || pp.Reason != "" {
		t.Fatalf("no witness: reason %q detail %q", pp.Reason, pp.Detail)
	}
	got := map[state.ObjID]decision.ManaAmount{}
	for _, a := range pp.Plan.Activations {
		got[a.Source] = a.Produces
	}
	if len(got) != 2 || got[plains] != lrW || got[island] != lrU {
		t.Fatalf("witness %s, want Plains W + Island U", lrPlanString(pp.Plan.Activations))
	}
	for _, a := range pp.Plan.Activations {
		src := a.Source
		ppChoose(t, e, func(o decision.Option) bool { return o.Kind == "activate" && o.Obj == src })
	}
	ppChoose(t, e, func(o decision.Option) bool {
		return o.Kind == "ability" && o.Obj == relic && o.Ability == pp.Action.Ability
	})
	if pool := e.G.Players[0].Pool; pool.Total() != 0 {
		t.Fatalf("pool after the activation = %v, want empty", pool)
	}
}

// TestPotentialPaymentPlansExcludesTheTappedSource: an ability whose cost
// taps its own source never plans that source for mana: alone it is
// proven unpayable, and with a Plains the Plains pays.
func TestPotentialPaymentPlansExcludesTheTappedSource(t *testing.T) {
	t.Parallel()
	e, _, _ := newFixtureDeck(t, 9402, ppFiller)
	land := onBoard(t, e, 0, ppTapLand)
	ppPriority(t, e)
	for _, pp := range e.PotentialPaymentPlans(0) {
		if pp.Action.Kind == "ability" && pp.Action.Obj == land && pp.Plan != nil {
			t.Fatalf("the land's own tap was planned to pay its {1}: %s", lrPlanString(pp.Plan.Activations))
		}
	}
	plains := onBoard(t, e, 0, lrPlains)
	ppPriority(t, e)
	pp := ppFind(t, e.PotentialPaymentPlans(0), "ability", land)
	if pp.Plan == nil || len(pp.Plan.Activations) != 1 || pp.Plan.Activations[0].Source != plains {
		t.Fatalf("want the Plains alone to pay: %+v", pp)
	}
}

// TestPotentialPaymentPlansProvesAnOverBound: the potential walk prices a
// Plains-Island dual as both colours at once, so a {W}{U} spell and a
// {1}{U} ability are potential plays; the planner proves the spell
// unpayable (one source, two pips) and pays the ability once a second
// source exists.
func TestPotentialPaymentPlansProvesAnOverBound(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9403, "Name:Potential Azorius\nManaCost:W U\nTypes:Instant\nA:SP$ Draw | NumCards$ 1\nOracle:x\n")
	onBoard(t, e, 0, ppDual)
	ppPriority(t, e)
	pp := ppFind(t, e.PotentialPaymentPlans(0), "cast", spell)
	if pp.Reason != "insufficient" || pp.Plan != nil {
		t.Fatalf("spell verdict %+v, want insufficient", pp)
	}
	onBoard(t, e, 0, lrPlains)
	ppPriority(t, e)
	if pp = ppFind(t, e.PotentialPaymentPlans(0), "cast", spell); pp.Reason != "" {
		t.Fatalf("with a Plains the spell is payable, got %+v", pp)
	}
}

// TestPotentialPaymentPlansUnsupportedShapes: an {X} ability has no witness
// and is never reported proven unpayable.
func TestPotentialPaymentPlansUnsupportedShapes(t *testing.T) {
	t.Parallel()
	e, _, _ := newFixtureDeck(t, 9404, ppFiller)
	x := onBoard(t, e, 0, "Name:Potential X\nTypes:Artifact\n"+
		"A:AB$ Draw | Cost$ X | NumCards$ X | SpellDescription$ Draw X cards.\nOracle:x\n")
	onBoard(t, e, 0, lrPlains)
	ppPriority(t, e)
	for _, pp := range e.PotentialPaymentPlans(0) {
		if pp.Action.Obj == x && (pp.Plan != nil || pp.Reason == "insufficient") {
			t.Fatalf("X ability verdict %+v, want unsupported", pp)
		}
	}
}
