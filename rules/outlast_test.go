package rules

// The Outlast keyword (CR 702.107). K:Outlast:<cost> used to have no
// expansion in cards/keywords.go, so an Outlast carrier offered no Outlast
// activation at all and effects.Supported() lacked kw:Outlast. The case now
// synthesizes the ordinary sorcery-speed PutCounter activation (Cost$ T
// <mana>, CounterType$ P1P1); these tests pin it on the real Abzan Battle
// Priest script (read as text from the gitignored corpus, never committed)
// through the ordinary activated-ability path.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestOutlastKeywordIsSupported: the keyword must be registered as an
// implemented non-API primitive, or every Outlast carrier stays
// "unplayable" in make report / the acceptance ratchet regardless of the
// expansion working.
func TestOutlastKeywordIsSupported(t *testing.T) {
	if !effects.Supported()["kw:Outlast"] {
		t.Fatal("effects.Supported() lacks kw:Outlast")
	}
}

// outlastSetup places the real Abzan Battle Priest script on seat 0's
// battlefield and drives to seat 0's NEXT Main1 (turn 3), where the creature
// is untapped and no longer summoning sick (CR 302.6 -- TurnChange clears
// SummonSick for its controller), so the {T} cost is payable for real
// reasons rather than a direct field poke. Nothing is funded; each test
// funds exactly the mana it needs.
func outlastSetup(t *testing.T, seed uint64, src string, extras ...string) (*Engine, Config, state.ObjID) {
	t.Helper()
	e, cfg, _ := newFixtureDeck(t, seed, src, extras...)
	id := putCreature(t, e, 0, src)
	driveToStepAll(t, e, 3, 0, state.StepMain1)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Abzan Battle Priest in zone %v, want Battlefield", o)
	} else if o.SummonSick {
		t.Fatal("precondition: turn 3 must have cleared summoning sickness")
	} else if o.Tapped {
		t.Fatal("precondition: turn 3 must have untapped Abzan Battle Priest")
	}
	return e, cfg, id
}

// TestAbzanBattlePriestOutlastPutsCounterAndTaps drives the real corpus card
// end to end: with one white mana funded, the Outlast {W},{T} activator is
// offered in seat 0's own Main1, paying it taps Abzan Battle Priest and puts
// exactly one +1/+1 counter on it, and the counter switches on the printed
// payoff static (every creature you control with a +1/+1 counter has
// lifelink -- here Abzan Battle Priest itself).
func TestAbzanBattlePriestOutlastPutsCounterAndTaps(t *testing.T) {
	src := corpusCardText(t, "a/abzan_battle_priest.txt")
	e, cfg, id := outlastSetup(t, 1301, src)

	// Precondition: the payoff static is not live before the counter, so
	// the final Lifelink assertion is actually about the counter.
	if got := e.G.Obj(id).Counter("P1P1"); got != 0 {
		t.Fatalf("precondition: Abzan Battle Priest starts with %d +1/+1 counters, want 0", got)
	}
	if e.HasKeyword(id, "Lifelink") {
		t.Fatal("precondition: the counters_GE1_P1P1 lifelink static is already live")
	}

	addMana(t, e, 0, "W")
	opt, ok := findAbilityOption(e, id, 0)
	if !ok {
		t.Fatalf("Outlast not offered in a sorcery window: %+v", e.Pending().Options)
	}
	beforePool := e.G.Players[0].Pool.Total()
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Obj(id).Counter("P1P1"); got != 1 {
		t.Fatalf("+1/+1 counters after one Outlast activation = %d, want 1", got)
	}
	if !e.G.Obj(id).Tapped {
		t.Fatal("Outlast {T} cost did not tap Abzan Battle Priest")
	}
	if got := e.G.Players[0].Pool.Total(); got != beforePool-1 {
		t.Fatalf("mana pool after Outlast = %d, want %d", got, beforePool-1)
	}
	if !e.HasKeyword(id, "Lifelink") {
		t.Fatal("the +1/+1 counter did not switch on the printed lifelink static")
	}
	replayCheck(t, e, cfg)
}

// TestAbzanBattlePriestOutlastIsSorcerySpeedOnly pins both halves of CR
// 702.107a's "only as a sorcery" clause: the activator is withheld while a
// spell sits on the stack and on an opponent's turn, with mana funded in
// each case so the negative is the timing gate and not an empty pool.
func TestAbzanBattlePriestOutlastIsSorcerySpeedOnly(t *testing.T) {
	src := corpusCardText(t, "a/abzan_battle_priest.txt")
	boltSrc := "Name:Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n"

	// (a) A non-empty stack is not a sorcery window, even for the active
	// player who owns the permanent.
	e, _, id := outlastSetup(t, 1302, src, boltSrc)
	addMana(t, e, 0, "W")
	if _, ok := findAbilityOption(e, id, 0); !ok {
		t.Fatalf("positive control: Outlast not offered in an empty-stack Main1: %+v", e.Pending().Options)
	}
	bolt := addToHand(t, e, 0, boltSrc)
	addMana(t, e, 0, "R")
	idx := -1
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == bolt {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Bolt: %+v", e.Pending().Options)
	}
	submitChoices(t, e, idx)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget && len(d.Options) > 0 {
		submitChoices(t, e, d.Options[0].Index)
	}
	if len(e.G.Stack) == 0 {
		t.Fatal("Bolt did not reach the stack")
	}
	if _, ok := findAbilityOption(e, id, 0); ok {
		t.Fatalf("Outlast offered with a spell on the stack: %+v", e.Pending().Options)
	}

	// (b) On an opponent's turn the non-active player's sorcery-speed
	// activator is withheld. Drive to turn 4 (seat 1 active) Main1, then let
	// seat 1 pass so seat 0 actually holds priority -- otherwise this would
	// just be checking seat 1's own option list, where seat 0's permanent
	// never appears. Mana is funded while seat 0 is allowed to spend it.
	e2, _, id2 := outlastSetup(t, 1303, src)
	addMana(t, e2, 0, "W")
	if got := e2.G.Players[0].Pool.Total(); got != 1 {
		t.Fatalf("precondition: seat 0 funded %d mana, want 1", got)
	}
	driveToStepAll(t, e2, 4, 1, state.StepMain1)
	d := e2.Pending()
	if d == nil || d.Player != 1 {
		t.Fatalf("precondition: expected seat 1 to hold priority at turn 4 Main1, got %+v", d)
	}
	pass := -1
	for _, o := range d.Options {
		if o.Kind == "pass" {
			pass = o.Index
		}
	}
	if pass < 0 {
		t.Fatalf("seat 1 has no pass option: %+v", d.Options)
	}
	submitChoices(t, e2, pass)
	d = e2.Pending()
	if d == nil || d.Player != 0 {
		t.Fatalf("precondition: expected seat 0 to hold priority after seat 1 passed, got %+v", d)
	}
	if _, ok := findAbilityOption(e2, id2, 0); ok {
		t.Fatalf("Outlast offered on an opponent's turn: %+v", d.Options)
	}
}

// TestAbzanBattlePriestOutlastIsRepeatable pins that Outlast carries no
// once-per-turn restriction of its own: it is an ordinary {T} activation, so
// it can be activated twice in the same turn when the creature is untapped
// between activations and the mana is there. The creature is untapped
// through a logged events.Untap, so replayCheck reconstructs the same board.
func TestAbzanBattlePriestOutlastIsRepeatable(t *testing.T) {
	src := corpusCardText(t, "a/abzan_battle_priest.txt")
	e, cfg, id := outlastSetup(t, 1304, src)

	// First activation.
	addMana(t, e, 0, "WW")
	opt, ok := findAbilityOption(e, id, 0)
	if !ok {
		t.Fatalf("first Outlast not offered: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(id).Counter("P1P1"); got != 1 {
		t.Fatalf("counters after first activation = %d, want 1", got)
	}
	if !e.G.Obj(id).Tapped {
		t.Fatal("first activation did not tap Abzan Battle Priest")
	}
	if _, ok := findAbilityOption(e, id, 0); ok {
		t.Fatal("precondition: a tapped creature must not be offered a {T} ability")
	}

	// Untap through the ordinary event, rebuild the priority snapshot the
	// tap-time one is stale about, and activate again: a repeatable keyword
	// must offer it a second time the same turn.
	e.emit(events.Event{Kind: events.Untap, Obj: id})
	e.pending = nil
	e.priorityRound()
	if e.G.Obj(id).Tapped {
		t.Fatal("precondition: the Untap event did not clear the tapped state")
	}
	opt, ok = findAbilityOption(e, id, 0)
	if !ok {
		t.Fatalf("second Outlast not offered after untapping: %+v", e.Pending().Options)
	}
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Obj(id).Counter("P1P1"); got != 2 {
		t.Fatalf("+1/+1 counters after two Outlast activations = %d, want 2", got)
	}
	if !e.G.Obj(id).Tapped {
		t.Fatal("second activation did not tap Abzan Battle Priest")
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("mana pool after two activations = %d, want 0", got)
	}
	replayCheck(t, e, cfg)
}
