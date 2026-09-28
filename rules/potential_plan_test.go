package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
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

// TestPotentialPaymentPlansReservesTapCostCandidates: Heap Gate's
// "{1}, {T}, tap an untapped Gate you control" beside a second Gate and a
// Plains. Tapping the other Gate for the {1} would leave no Gate to tap for
// the cost; the witness must pay the {1} from the Plains.
func TestPotentialPaymentPlansReservesTapCostCandidates(t *testing.T) {
	t.Parallel()
	e, _, _ := newFixtureDeck(t, 9405, ppFiller)
	heap := onBoardCard(t, e, 0, corpusCard(t, "Heap Gate"))
	other := onBoardCard(t, e, 0, corpusCard(t, "Heap Gate"))
	plains := onBoard(t, e, 0, lrPlains)
	ppPriority(t, e)
	for _, pp := range e.PotentialPaymentPlans(0) {
		if pp.Action.Kind != "ability" || (pp.Action.Obj != heap && pp.Action.Obj != other) || pp.Plan == nil {
			continue
		}
		for _, a := range pp.Plan.Activations {
			if a.Source != plains {
				t.Fatalf("%q planned from %d, want the Plains only: %s", pp.Action.Label, a.Source, lrPlanString(pp.Plan.Activations))
			}
		}
	}
	pp := PotentialPlan{}
	for _, c := range e.PotentialPaymentPlans(0) {
		if c.Action.Kind == "ability" && c.Action.Obj == heap && c.Plan != nil {
			pp = c
		}
	}
	if pp.Plan == nil {
		t.Fatal("no witness for Heap Gate's Treasure ability")
	}
	ppChoose(t, e, func(o decision.Option) bool { return o.Kind == "activate" && o.Obj == plains })
	ppChoose(t, e, func(o decision.Option) bool {
		return o.Kind == "ability" && o.Obj == heap && o.Ability == pp.Action.Ability
	})
}

// ppFindMode is ppFind for a play with a Mode.
func ppFindMode(t *testing.T, pps []PotentialPlan, kind string, obj state.ObjID, mode string) PotentialPlan {
	t.Helper()
	for _, pp := range pps {
		if pp.Action.Kind == kind && pp.Action.Obj == obj && pp.Action.Mode == mode {
			return pp
		}
	}
	t.Fatalf("no potential %s/%s for %d in %+v", kind, mode, obj, pps)
	return PotentialPlan{}
}

// TestPotentialPaymentPlansTappedLandKeepsTheProof: a TAPPED land's {T}
// ability cannot be activated, so it never makes the census incomplete:
// {W}{U}{U} over two untapped duals (the potential bound counts each as
// both colours) beside a tapped Plains is still a proof.
func TestPotentialPaymentPlansTappedLandKeepsTheProof(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9406, "Name:Potential Triple\nManaCost:W U U\nTypes:Instant\nA:SP$ Draw | NumCards$ 1\nOracle:x\n")
	onBoard(t, e, 0, ppDual)
	onBoard(t, e, 0, ppDual)
	plains := onBoard(t, e, 0, lrPlains)
	e.G.Obj(plains).Tapped = true
	ppPriority(t, e)
	if pp := ppFind(t, e.PotentialPaymentPlans(0), "cast", spell); pp.Reason != "insufficient" {
		t.Fatalf("verdict %+v, want insufficient", pp)
	}
}

// ppBear is a vanilla creature, the companion Saruli Caretaker taps.
const ppBear = "Name:Potential Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// TestPotentialPaymentPlansRelaxedCensusProof: Saruli Caretaker's
// "{T}, tap an untapped creature you control: add any colour" is outside the
// planner census. A {5} spell over Saruli, a Bear and a Forest is still
// proven unpayable (relaxed: even a free Saruli makes only two mana), while
// a {1}{G} spell, which Saruli and the Forest really pay, is priced with a
// scripted prefix: activate Saruli (tapping the Bear), after which the
// planner pays the rest from the Forest.
func TestPotentialPaymentPlansRelaxedCensusProof(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		cost, reason string
	}{{"5", "insufficient"}, {"1 G", ""}} {
		e, _, spell := newFixtureDeck(t, 9407, "Name:Potential Relaxed\nManaCost:"+tc.cost+"\nTypes:Instant\nA:SP$ Draw | NumCards$ 1\nOracle:x\n")
		onBoardReadyCard(t, e, 0, corpusCard(t, "Saruli Caretaker"))
		bear := onBoard(t, e, 0, ppBear)
		e.G.Obj(bear).SummonSick = false
		onBoard(t, e, 0, lrForest)
		ppPriority(t, e)
		pp := ppFind(t, e.PotentialPaymentPlans(0), "cast", spell)
		if pp.Reason != tc.reason || (tc.reason == "" && len(pp.Script) == 0) {
			t.Fatalf("{%s}: verdict %+v, want %q", tc.cost, pp, tc.reason)
		}
	}
}

// TestPotentialPaymentPlansSacrificesATappedSource: Makeshift Munitions'
// "{1}, Sacrifice an artifact or creature" beside a lone Great Furnace (an
// artifact land). The Furnace taps for the {1} and is then sacrificed
// (CR 602.2b: tapping it for mana does not stop it paying the sacrifice).
func TestPotentialPaymentPlansSacrificesATappedSource(t *testing.T) {
	t.Parallel()
	e, _, _ := newFixtureDeck(t, 9408, ppFiller)
	munitions := onBoardCard(t, e, 0, corpusCard(t, "Makeshift Munitions"))
	furnace := onBoardCard(t, e, 0, corpusCard(t, "Great Furnace"))
	ppPriority(t, e)
	pp := ppFind(t, e.PotentialPaymentPlans(0), "ability", munitions)
	if pp.Plan == nil || len(pp.Plan.Activations) != 1 || pp.Plan.Activations[0].Source != furnace {
		t.Fatalf("want the Furnace to pay: %+v", pp)
	}
	ppChoose(t, e, func(o decision.Option) bool { return o.Kind == "activate" && o.Obj == furnace })
	ppChoose(t, e, func(o decision.Option) bool {
		return o.Kind == "ability" && o.Obj == munitions && o.Ability == pp.Action.Ability
	})
}

// TestPotentialPaymentPlansProvesReservedSources: two Heap Gates alone. The
// Treasure ability "{1}, {T}, tap an untapped Gate" taps one Gate for its
// cost and the other for the tapXType part, leaving nothing for the {1}:
// every candidate assignment was tried, so it is proven unpayable (Heap
// Gate's paid "{1}, {T}: any colour" is outside the census and relaxed).
func TestPotentialPaymentPlansProvesReservedSources(t *testing.T) {
	t.Parallel()
	e, _, _ := newFixtureDeck(t, 9409, ppFiller)
	heap := onBoardCard(t, e, 0, corpusCard(t, "Heap Gate"))
	onBoardCard(t, e, 0, corpusCard(t, "Heap Gate"))
	ppPriority(t, e)
	if pp := ppFind(t, e.PotentialPaymentPlans(0), "ability", heap); pp.Reason != "insufficient" {
		t.Fatalf("%q verdict %+v, want a proof", pp.Action.Label, pp)
	}
}

// TestPotentialPaymentPlansFlashbackWitness: a flashback cast from the
// graveyard gets a witness for its flashback cost, and tapping it makes the
// flashback option offered.
func TestPotentialPaymentPlansFlashbackWitness(t *testing.T) {
	t.Parallel()
	e, _, _ := newFixtureDeck(t, 9410, ppFiller)
	fb := putGraveyard(t, e, 0, "Name:Potential Flashback\nManaCost:U\nTypes:Instant\nK:Flashback:1 U\nA:SP$ Draw | NumCards$ 1\nOracle:x\n")
	onBoard(t, e, 0, lrPlains)
	onBoard(t, e, 0, lrIsland)
	ppPriority(t, e)
	pp := ppFindMode(t, e.PotentialPaymentPlans(0), "cast", fb, "flashback")
	if pp.Plan == nil || pp.Reason != "" || len(pp.Plan.Activations) != 2 {
		t.Fatalf("flashback verdict %+v, want a two-source witness", pp)
	}
	for _, a := range pp.Plan.Activations {
		src := a.Source
		ppChoose(t, e, func(o decision.Option) bool { return o.Kind == "activate" && o.Obj == src })
	}
	ppChoose(t, e, func(o decision.Option) bool { return o.Kind == "cast" && o.Obj == fb && o.Mode == "flashback" })
}

// TestPotentialPaymentPlansXWitnessesTheLargestX: an {X}{G} spell over three
// Forests is witnessed at X = 2 (every source), so the X announcement then
// offers every value up to 2.
func TestPotentialPaymentPlansXWitnessesTheLargestX(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9411, "Name:Potential X Spell\nManaCost:X G\nTypes:Instant\nA:SP$ Draw | NumCards$ X\nSVar:X:Count$xPaid\nOracle:x\n")
	for i := 0; i < 3; i++ {
		onBoard(t, e, 0, lrForest)
	}
	ppPriority(t, e)
	pp := ppFind(t, e.PotentialPaymentPlans(0), "cast", spell)
	if pp.Plan == nil || pp.Reason != "" || len(pp.Plan.Activations) != 3 {
		t.Fatalf("X verdict %+v, want a three-Forest witness", pp)
	}
}

// TestFlashbackPaysTheSpellsAdditionalCost: CR 702.34a/601.2f -- flashback
// replaces only the mana cost, so Eviscerator's Insight's "sacrifice an
// artifact or creature" is still owed: with five lands and nothing to
// sacrifice the flashback is not offered; with a Bear it is, and the cast
// sacrifices the Bear.
func TestFlashbackPaysTheSpellsAdditionalCost(t *testing.T) {
	t.Parallel()
	e, _, _ := newFixtureDeck(t, 9412, ppFiller)
	insight := e.G.AddObject(corpusCard(t, "Eviscerator's Insight"), 0).ID
	e.emit(events.Event{Kind: events.MoveZone, Obj: insight, From: state.ZLibrary, To: state.ZGraveyard})
	var swamps []state.ObjID
	for i := 0; i < 5; i++ {
		swamps = append(swamps, onBoard(t, e, 0, "Name:Swamp\nTypes:Basic Land Swamp\nOracle:x\n"))
	}
	ppPriority(t, e)
	for _, id := range swamps {
		src := id
		ppChoose(t, e, func(o decision.Option) bool { return o.Kind == "activate" && o.Obj == src })
	}
	d := ppPriority(t, e)
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == insight {
			t.Fatalf("flashback offered with nothing to sacrifice: %+v", o)
		}
	}
	bear := onBoard(t, e, 0, ppBear)
	ppPriority(t, e)
	ppChoose(t, e, func(o decision.Option) bool { return o.Kind == "cast" && o.Obj == insight && o.Mode == "flashback" })
	for n := 0; n < 4 && e.G.Obj(bear).Zone == state.ZBattlefield; n++ {
		d := e.Pending()
		if d == nil || d.Kind == decision.KPriority {
			break
		}
		ppChoose(t, e, func(o decision.Option) bool { return o.Obj == bear })
	}
	if z := e.G.Obj(bear).Zone; z == state.ZBattlefield {
		t.Fatalf("the Bear was not sacrificed (zone %v)", z)
	}
}

// ppSpawn is an Eldrazi Spawn: "Sacrifice this creature: Add {C}."
const ppSpawn = "Name:Potential Spawn\nTypes:Creature Eldrazi Spawn\nPT:0/1\n" +
	"A:AB$ Mana | Cost$ Sac<1/CARDNAME> | Produced$ C | Amount$ 1 | SpellDescription$ Add {C}.\nOracle:x\n"

// TestPotentialPaymentPlansReservesSacrificeCandidates: Makeshift
// Munitions' "{1}, Sacrifice an artifact or creature" over Spawns. With one
// Spawn the {1} can only come from sacrificing it, leaving nothing to
// sacrifice for the cost: every assignment was tried, a proof. With two,
// one pays the {1} and the other is kept for the sacrifice.
func TestPotentialPaymentPlansReservesSacrificeCandidates(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		spawns int
		reason string
	}{{1, "insufficient"}, {2, ""}} {
		e, _, _ := newFixtureDeck(t, 9413, ppFiller)
		munitions := onBoardCard(t, e, 0, corpusCard(t, "Makeshift Munitions"))
		for i := 0; i < tc.spawns; i++ {
			onBoard(t, e, 0, ppSpawn)
		}
		ppPriority(t, e)
		pp := ppFind(t, e.PotentialPaymentPlans(0), "ability", munitions)
		if pp.Reason != tc.reason || (tc.reason == "" && (pp.Plan == nil || len(pp.Plan.Activations) != 1)) {
			t.Fatalf("%d spawns: verdict %+v, want %q", tc.spawns, pp, tc.reason)
		}
	}
}

// TestPotentialPaymentPlansScriptsAFilter: a {W} spell over Heap Gate and
// a colourless land. Only Heap Gate's filter ("{1}, {T}: Add one mana of
// any color", outside the census) makes {W}, funded by the land's {C}: the
// verdict is a scripted prefix (tap the land, filter for W), and playing it
// makes the spell offered.
func TestPotentialPaymentPlansScriptsAFilter(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9414, "Name:Potential White\nManaCost:W\nTypes:Instant\nA:SP$ Draw | NumCards$ 1\nOracle:x\n")
	onBoardCard(t, e, 0, corpusCard(t, "Heap Gate"))
	onBoard(t, e, 0, "Name:Potential Wastes\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ C | SpellDescription$ Add {C}.\nOracle:x\n")
	ppPriority(t, e)
	pp := ppFind(t, e.PotentialPaymentPlans(0), "cast", spell)
	if pp.Reason != "" || len(pp.Script) == 0 {
		t.Fatalf("verdict %+v, want a scripted prefix", pp)
	}
	for _, st := range pp.Script {
		d := e.Pending()
		var choices []int
		for _, pk := range st.Picks {
			for _, o := range d.Options {
				if pk.Matches(&o) {
					choices = append(choices, o.Index)
				}
			}
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
			t.Fatalf("script step %+v: %v", st, err)
		}
	}
	ppChoose(t, e, func(o decision.Option) bool { return o.Kind == "cast" && o.Obj == spell })
}
