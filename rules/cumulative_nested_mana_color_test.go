// Regression tests for a mana ability whose colour choice lives on a
// SUB-ability (the Gemstone Caverns shape: "Add {C}. If this has a luck
// counter, instead add one mana of any color" -- the top-level Produced$ is C
// and gated off, the any-colour Mana is its SubAbility$) activated inside a
// resolution-time payment window. The sub's colour ask came through
// effects.Ask, which parked a stack-oriented resume point while the window's
// flow marker stayed chooseNone, so the window re-asked on top of it:
// "rules: ask overwrote a suspended resolution's pending decision" (the
// most frequent botbench crash: Mystic Remora + Gemstone Caverns).
package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// luckGrotto is a test-local land with the Gemstone Caverns mana-ability
// shape: the colour choice is on the conditional sub-ability.
const luckGrotto = "Name:Luck Grotto\nManaCost:no cost\nTypes:Land\n" +
	"A:AB$ Mana | Cost$ T | Produced$ C | ConditionCheckSVar$ CheckCounter | ConditionSVarCompare$ EQ0 | SubAbility$ DBMana | SpellDescription$ Add {C}, or any colour with a luck counter.\n" +
	"SVar:DBMana:DB$ Mana | Produced$ Any | ConditionCheckSVar$ CheckCounter | ConditionSVarCompare$ GE1\n" +
	"SVar:CheckCounter:Count$CardCounters.LUCK\nOracle:x\n"

// lifeSpring adds {U} and then gains its controller 1 life on a sub-ability.
const lifeSpring = "Name:Life Spring\nManaCost:no cost\nTypes:Land\n" +
	"A:AB$ Mana | Cost$ T | Produced$ U | SubAbility$ DBGain | SpellDescription$ Add {U}. You gain 1 life.\n" +
	"SVar:DBGain:DB$ GainLife | LifeAmount$ 1\nOracle:x\n"

const upkeepEnchantment = "Name:Aging Charm\nManaCost:0\nTypes:Enchantment\nK:Cumulative upkeep:1\nOracle:x\n"

func optionIndex(t *testing.T, d *decision.Decision, pred func(decision.Option) bool) int {
	t.Helper()
	for _, o := range d.Options {
		if pred(o) {
			return o.Index
		}
	}
	t.Fatalf("no matching option in %q: %+v", d.Prompt, d.Options)
	return -1
}

func TestCumulativeWindowSubAbilityColourChoiceResumesWindow(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	charm := onBoard(t, e, 0, upkeepEnchantment)
	grotto := onBoard(t, e, 0, luckGrotto)
	e.emit(events.Event{Kind: events.CounterChange, Obj: grotto, Counter: "LUCK", Amount: 1})
	resolveUpkeepCumulative(t, e)
	d := e.Pending()
	if d == nil || !strings.Contains(d.Prompt, "cumulative upkeep") {
		t.Fatalf("precondition: want the cumulative mana window, got %+v", d)
	}
	// Tap the grotto for mana inside the window. Before the fix this panicked.
	submitChoices(t, e, optionIndex(t, d, func(o decision.Option) bool { return o.Kind == "activate" && o.Obj == grotto }))
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || !strings.Contains(strings.ToLower(d.Prompt), "colo") {
		t.Fatalf("want the sub-ability's colour ask, got %+v", d)
	}
	if e.resume != nil {
		t.Fatalf("the off-stack mana colour ask parked a stack resume point: %+v", e.resume)
	}
	submitChoices(t, e, optionIndex(t, d, func(o decision.Option) bool { return o.Label == "Add U" }))
	if got := e.G.Players[0].Pool.Total(); got != 1 {
		t.Fatalf("pool after the colour answer = %d, want 1", got)
	}
	// The payment window resumed: the cost is now payable, so the pay/sac ask.
	d = e.Pending()
	if d == nil {
		t.Fatal("the payment window was dropped after the colour answer")
	}
	pay := optionIndex(t, d, func(o decision.Option) bool { return o.Kind == "cumulative_pay" })
	submitChoices(t, e, pay)
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("paying the upkeep left %d mana in the pool", got)
	}
	if o := e.G.Obj(charm); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the upkeep was paid but the permanent left the battlefield")
	}
	if e.cumulative != nil {
		t.Fatal("the cumulative window never closed")
	}
}

// A mana ability's SubAbility$ chain must still run in full when the ability
// is activated inside the cumulative window: rules' Suspended() is widened by
// the window itself, which must not read as "this resolution suspended".
func TestCumulativeWindowManaAbilitySubChainRuns(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	onBoard(t, e, 0, upkeepEnchantment)
	spring := onBoard(t, e, 0, lifeSpring)
	life := e.G.Players[0].Life
	resolveUpkeepCumulative(t, e)
	d := e.Pending()
	if d == nil || !strings.Contains(d.Prompt, "cumulative upkeep") {
		t.Fatalf("precondition: want the cumulative mana window, got %+v", d)
	}
	submitChoices(t, e, optionIndex(t, d, func(o decision.Option) bool { return o.Kind == "activate" && o.Obj == spring }))
	if got := e.G.Players[0].Life; got != life+1 {
		t.Fatalf("life after the in-window activation = %d, want %d (the SubAbility$ was dropped)", got, life+1)
	}
}

// Outside any window, the same sub-ability colour ask with a spell on the
// stack must not re-enter (and finish) that unrelated stack object.
func TestSubAbilityColourChoiceWithStackDoesNotHijackStackTop(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	grotto := onBoard(t, e, 0, luckGrotto)
	e.emit(events.Event{Kind: events.CounterChange, Obj: grotto, Counter: "LUCK", Amount: 1})
	onStack := e.G.AddObject(card(t, "Name:Waiting Bolt\nManaCost:R\nTypes:Instant\nA:SP$ GainLife | LifeAmount$ 3\nOracle:x\n"), 1)
	onStack.Zone = state.ZStack
	e.G.Stack = append(e.G.Stack, onStack.ID)
	e.activateManaFor(0, grotto, false, false, true)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("want the colour ask, got %+v", d)
	}
	submitChoices(t, e, optionIndex(t, d, func(o decision.Option) bool { return o.Label == "Add G" }))
	if got := e.G.Players[0].Pool.Total(); got != 1 {
		t.Fatalf("pool = %d, want 1", got)
	}
	if o := e.G.Obj(onStack.ID); o.Zone != state.ZStack {
		t.Fatalf("the unrelated stack object was moved to %s by the mana colour answer", o.Zone)
	}
}
