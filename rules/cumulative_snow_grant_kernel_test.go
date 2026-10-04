package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

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
	e.pending = nil // the upkeep resolution starts from a quiet engine
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
	e.pending = nil // the upkeep resolution starts from a quiet engine
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
	e.pending = nil // the upkeep resolution starts from a quiet engine
	resolveUpkeepCumulative(t, e)
	if got := e.G.Obj(idol).Counter("AGE"); got != 1 {
		t.Fatalf("pump-granted cumulative upkeep placed %d age counters, want 1", got)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("no cumulative pay/sacrifice ask for the pump-granted keyword: %+v", d)
	}
}
