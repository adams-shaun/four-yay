// Tests for the two cumulative-upkeep remainders the AGENTS.md closing row
// named: a snow {S} upkeep cost must be a real, per-age-counter snow
// requirement (CR 702.24a / CR 107.4h), and a dynamically granted cumulative
// upkeep (a layer-6 AddKeyword$ or an A:AB$ Pump's KW$ grant) must run through
// the same rules/cumulative.go machinery a printed K:Cumulative upkeep line
// does. Kept in its own file so the ticket cannot conflict on a shared test
// file.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// coverOfWinterEngine seeds seat 0's library with the REAL corpus Cover of
// Winter (K:Cumulative upkeep:S) and returns the engine and its id once it is
// on the battlefield. The brief names no specific card; this is the corpus's
// snow cumulative-upkeep carrier.
func coverOfWinterEngine(t *testing.T) (*Engine, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	cover, ok := reg.Lookup("Cover of Winter")
	if !ok {
		t.Fatal("corpus fixture: Cover of Winter missing")
	}
	e := New(Config{Seed: 7, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{append([]*cards.Card{cover}, mountainDeck(t, 39)...), mountainDeck(t, 40)}})
	e.Advance()
	toMain1(t, e)
	id := moveByName(t, e, 0, "Cover of Winter", state.ZBattlefield)
	return e, id
}

// resolveUpkeepCumulative drives one beginning-of-upkeep StepChange, places
// the queued cumulative-upkeep trigger on the stack and resolves it, leaving
// the engine at the resolution-time payment ask. It fails if no
// CumulativeUpkeep ability reached the stack, so a missing trigger can never
// masquerade as a resolved one.
func resolveUpkeepCumulative(t *testing.T, e *Engine) {
	t.Helper()
	e.G.Active, e.G.Priority = 0, 0
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.putTriggersOnStack()
	idx := -1
	for i, sid := range e.G.Stack {
		if o := e.G.Obj(sid); o != nil && o.Ability != nil && o.Ability.API == "CumulativeUpkeep" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("cumulative upkeep was not placed as a triggered ability: stack=%v", e.G.Stack)
	}
	e.resolveTop()
}

// TestCumulativeUpkeepSnowCostRequiresSnowAtEveryAgeCounter is the snow half
// of the row: a {S} upkeep cost must be a real snow requirement at EVERY age
// counter, not only the first. Before the fix rules/cumulative.go's scaleCost
// dropped Cost.Snow when scaling by the age-counter count, so at two age
// counters the snow pip vanished, the cost was empty, and the permanent could
// be kept for free with no mana at all.
func TestCumulativeUpkeepSnowCostRequiresSnowAtEveryAgeCounter(t *testing.T) {
	t.Parallel()
	e, cover := coverOfWinterEngine(t)
	// Precondition: the card is where the rule reads it and the printed
	// keyword parsed to a real snow pip (not a generic substitute).
	if o := e.G.Obj(cover); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Cover of Winter not on the battlefield: %+v", e.G.Obj(cover))
	}
	if !e.G.Obj(cover).Face().HasKeyword("Cumulative upkeep") {
		t.Fatal("precondition: Cover of Winter does not print Cumulative upkeep")
	}
	if c := ParseCost("S"); c.Snow != 1 || c.Generic != 0 {
		t.Fatalf("precondition: ParseCost(S) = %+v, want one snow pip", c)
	}
	// Precondition: the two age counts under comparison produce DIFFERENT
	// amounts -- otherwise the assertion below could pass vacuously.
	if scaleCost(ParseCost("S"), 1).Snow == scaleCost(ParseCost("S"), 2).Snow {
		t.Fatalf("precondition: scaleCost does not distinguish age 1 from age 2")
	}
	// Seed one age counter so the resolution below makes it two. The event is
	// the ordinary CounterChange, so replay sees it too.
	e.emit(events.Event{Kind: events.CounterChange, Obj: cover, Counter: "AGE", Amount: 1})
	if got := e.G.Obj(cover).Counter("AGE"); got != 1 {
		t.Fatalf("precondition: seeded %d age counters, want 1", got)
	}
	resolveUpkeepCumulative(t, e)
	if got := e.G.Obj(cover).Counter("AGE"); got != 2 {
		t.Fatalf("resolution left %d age counters, want 2 (precondition)", got)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("no cumulative pay/sacrifice ask after resolution: %+v", d)
	}
	kinds := optionKinds(d)
	if kinds["cumulative_sac"] != 1 {
		t.Fatalf("sacrifice option missing from the ask: %+v", d.Options)
	}
	if kinds["cumulative_pay"] != 0 {
		t.Fatalf("a {S} upkeep cost was payable with no snow mana at 2 age counters: %+v", d.Options)
	}
}

// TestCumulativeUpkeepSnowCostIsPayableWithSnowMana is the positive partner:
// at the SAME two age counters, floating two snow mana units makes the cost
// payable (the pay option appears) and paying consumes exactly that snow.
func TestCumulativeUpkeepSnowCostIsPayableWithSnowMana(t *testing.T) {
	t.Parallel()
	e, cover := coverOfWinterEngine(t)
	e.emit(events.Event{Kind: events.CounterChange, Obj: cover, Counter: "AGE", Amount: 1})
	// Precondition: two snow-white mana units are in the pool and the
	// parallel snow tally before the ask.
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "SW", Amount: 1})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "SW", Amount: 1})
	if got := e.G.Players[0].Snow.Total(); got != 2 {
		t.Fatalf("precondition: snow tally = %d, want 2", got)
	}
	resolveUpkeepCumulative(t, e)
	if got := e.G.Obj(cover).Counter("AGE"); got != 2 {
		t.Fatalf("resolution left %d age counters, want 2 (precondition)", got)
	}
	d := e.Pending()
	if d == nil {
		t.Fatal("no cumulative pay/sacrifice ask after resolution")
	}
	pay := -1
	for _, o := range d.Options {
		if o.Kind == "cumulative_pay" {
			pay = o.Index
		}
	}
	if pay < 0 {
		t.Fatalf("a {S} upkeep cost was not payable with two snow mana: %+v", d.Options)
	}
	submitChoices(t, e, pay)
	if got := e.G.Players[0].Snow.Total(); got != 0 {
		t.Fatalf("paying the {S} upkeep left %d snow mana, want 0 spent", got)
	}
	if e.G.Obj(cover).Zone != state.ZBattlefield {
		t.Fatalf("Cover of Winter left the battlefield after paying: %s", e.G.Obj(cover).Zone)
	}
}

// cumulativeGrantStatic is a layer-6 AddKeyword$ Cumulative upkeep:2 grant --
// the Breath of Dreams / Mana Chains shape. The granting static is an
// Enchantment on the battlefield; the affected creature is a separate object.
const cumulativeGrantStatic = "Name:Cumulus Cover\nManaCost:0\nTypes:Enchantment\n" +
	"S:Mode$ Continuous | Affected$ Creature.YouCtrl | AddKeyword$ Cumulative upkeep:2 | Description$ Creatures you control have cumulative upkeep {2}.\nOracle:x\n"

// cumulativeGrantPump is an activated A:AB$ Pump with a KW$ Cumulative
// upkeep:1 grant -- the Balduvian Shaman / Dreams of the Dead shape (the brief
// names KW$ Cumulative upkeep:...). It grants to its own source so a test can
// activate it without a target decision.
const cumulativeGrantPump = "Name:Cumulus Idol\nManaCost:0\nTypes:Artifact\n" +
	"A:AB$ Pump | Cost$ 0 | Defined$ Self | KW$ Cumulative upkeep:1 | Duration$ Permanent | SpellDescription$ CARDNAME gains cumulative upkeep {1}.\nOracle:x\n"

// TestGrantedCumulativeUpkeepStaticGrantTriggers is the grant half of the row:
// a creature granted Cumulative upkeep by a layer-6 AddKeyword$ must accrue an
// age counter and pose the pay/sacrifice ask at its controller's upkeep,
// exactly as a printed K:Cumulative upkeep line does. Before the fix the
// keyword sat in the derived list with no Phase trigger to carry it, so the
// upkeep simply passed.
func TestGrantedCumulativeUpkeepStaticGrantTriggers(t *testing.T) {
	t.Parallel()
	const bear = "Name:Testbear\nManaCost:0\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e := handEngine(t)
	_ = onBoard(t, e, 0, cumulativeGrantStatic)
	bearID := onBoard(t, e, 0, bear)
	// Precondition: the grant is live in the DERIVED keyword list while the
	// creature's printed face does NOT carry it -- so the trigger below can
	// only come from the synthesis, never the printed expansion.
	if !e.HasKeyword(bearID, "Cumulative upkeep") {
		t.Fatal("precondition: granted cumulative upkeep is not in the derived keyword list")
	}
	if e.G.Obj(bearID).Face().HasKeyword("Cumulative upkeep") {
		t.Fatal("precondition: the test creature prints cumulative upkeep; it must only be granted")
	}
	resolveUpkeepCumulative(t, e)
	if got := e.G.Obj(bearID).Counter("AGE"); got != 1 {
		t.Fatalf("granted cumulative upkeep placed %d age counters, want 1", got)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("no cumulative pay/sacrifice ask for the granted keyword: %+v", d)
	}
	if optionKinds(d)["cumulative_sac"] != 1 {
		t.Fatalf("sacrifice option missing from the ask: %+v", d.Options)
	}
}

// TestGrantedCumulativeUpkeepPumpGrantTriggers is the KW$ route the brief
// names: activating an A:AB$ Pump | KW$ Cumulative upkeep:1 grant must give
// the permanent the keyword, and the next upkeep must run the ordinary
// age-counter + pay/sacrifice window.
func TestGrantedCumulativeUpkeepPumpGrantTriggers(t *testing.T) {
	t.Parallel()
	e, _, _ := newFixtureDeck(t, 11, cumulativeGrantPump)
	idol := moveByName(t, e, 0, "Cumulus Idol", state.ZBattlefield)
	e.G.Obj(idol).SummonSick = false
	// Precondition: the keyword is NOT there before the activation.
	if e.HasKeyword(idol, "Cumulative upkeep") {
		t.Fatal("precondition: cumulative upkeep already present before the pump resolves")
	}
	e.priorityRound() // a fresh priority decision offering the pump ability
	submitChoices(t, e, abilityOption(t, e, idol, 0).Index)
	passUntilStackEmpty(t, e, 20)
	// Precondition: the pump's KW$ grant is now live in the derived list.
	if !e.HasKeyword(idol, "Cumulative upkeep") {
		t.Fatal("precondition: the KW$ pump did not grant cumulative upkeep")
	}
	resolveUpkeepCumulative(t, e)
	if got := e.G.Obj(idol).Counter("AGE"); got != 1 {
		t.Fatalf("pump-granted cumulative upkeep placed %d age counters, want 1", got)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("no cumulative pay/sacrifice ask for the pump-granted keyword: %+v", d)
	}
}
