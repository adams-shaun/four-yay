package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// This file pins the CombatDamage$ trigger gate (rules/trigger_match.go's
// trigmatch.DamageMatches) on real combat: a Damage event emitted by dealCombatDamage's
// assignment loop (combat.go) satisfies CombatDamage$ True, every non-combat
// DealDamage site does not, and a prevented (protection) combat hit -- which
// emit substitutes with a Note -- never fires one. Mode$ DamageDoneOnce gets
// the same once-per-damage-batch gate DamageDealtOnce has. The Jitte and Keeper tests
// run the REAL compiled corpus scripts; the bearer and the blocker fixtures
// are inline because no corpus shape needs their exact body.

// combatTriggerBoard deals a seat-0 deck holding the given corpus cards
// (placed on the battlefield, attacker-ready on demand) plus inline fixtures
// (placed on the battlefield by name), with the clock parked at seat 0's
// declare-attackers step. Every placement is a logged MoveZone, so the whole
// board replays.
func combatTriggerBoard(t *testing.T, reg *cards.Registry, corpus0, inline0, corpus1, inline1 []string) (*Engine, Config) {
	t.Helper()
	var deck0, deck1 []*cards.Card
	for _, name := range corpus0 {
		deck0 = append(deck0, mustCorpusCard(t, reg, name))
	}
	for _, src := range inline0 {
		deck0 = append(deck0, card(t, src))
	}
	for _, name := range corpus1 {
		deck1 = append(deck1, mustCorpusCard(t, reg, name))
	}
	for _, src := range inline1 {
		deck1 = append(deck1, card(t, src))
	}
	cfg := Config{Seed: 73, Names: []string{"a", "b"},
		Tokens: reg.Tokens,
		Decks: [][]*cards.Card{
			append(deck0, mountainDeck(t, 40-len(deck0))...),
			append(deck1, mountainDeck(t, 40-len(deck1))...),
		}}
	e := New(cfg)
	place := func(seat state.PlayerID, named []*cards.Card) {
		for i := range e.G.Objs {
			o := &e.G.Objs[i]
			if o.Owner != seat {
				continue
			}
			for _, c := range named {
				if o.Card == c {
					e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: state.ZBattlefield})
					break
				}
			}
		}
	}
	place(0, deck0)
	place(1, deck1)
	for i := range e.G.Objs {
		if o := &e.G.Objs[i]; o.Zone == state.ZBattlefield && o.Owner == 0 &&
			o.Face() != nil && o.Face().IsCreature() {
			o.SummonSick = false
		}
	}
	return e, cfg
}

// findBattlefield returns the battlefield object of seat p whose face name is
// name. nth selects among same-named copies (0 = first in battlefield order).
func findBattlefield(t *testing.T, e *Engine, p state.PlayerID, name string, nth int) state.ObjID {
	t.Helper()
	seen := 0
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Zone == state.ZBattlefield && o.Owner == p && o.Face() != nil && o.Face().Name == name {
			if seen == nth {
				return o.ID
			}
			seen++
		}
	}
	t.Fatalf("seat %d has no battlefield %q #%d", p, name, nth)
	return 0
}

// countChargeChanges counts CHARGE CounterChange events on obj with the given
// per-emission amount.
func countChargeChanges(e *Engine, obj state.ObjID, amount int32) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.CounterChange && ev.Obj == obj && ev.Counter == "CHARGE" && ev.Amount == amount {
			n++
		}
	}
	return n
}

// countDraws counts Draw events for player p.
func countDraws(e *Engine, p state.PlayerID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Draw && ev.Player == p {
			n++
		}
	}
	return n
}

// The brief's headline defect, on the real corpus script: Umezawa's Jitte's
// T:Mode$ DamageDealtOnce | CombatDamage$ True | ValidSource$
// Creature.EquippedBy fires when its equipped creature deals combat damage,
// putting two charge counters on the Jitte per damage STEP. The bearer here
// carries Double Strike, so it deals combat damage TWICE in the one combat
// damage step (first strike, then regular) -- two damage batches, two
// triggers, four counters total. The old once-per-TURN latch suppressed the
// second firing (this test used to pin exactly that -- it asserted n != 1 and
// was flipped by the per-batch fix).
func TestUmezawasJitteGainsChargeCountersPerDamageStep(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := combatTriggerBoard(t, reg, []string{"Umezawa's Jitte"},
		[]string{"Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nK:Double Strike\nOracle:x\n"}, nil, nil)
	// A logged TurnChange both clears the bearer's summoning sickness (CR
	// 302.6a's "since the beginning of your most recent turn") and is the
	// replay-consistent way to arm seat 0 for combat.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	jitte := findBattlefield(t, e, 0, "Umezawa's Jitte", 0)
	bearer := findBattlefield(t, e, 0, "Bear", 0)

	// Equip: the Equipment is attached with a logged Attach event (the same
	// kind resolveTop's equip arm emits), so replay rebuilds AttachedTo.
	e.emit(events.Event{Kind: events.Attach, Obj: jitte, IDs: []state.ObjID{bearer}})
	if e.G.Obj(jitte).AttachedTo != bearer {
		t.Fatalf("jitte attached to %d, want bearer %d", e.G.Obj(jitte).AttachedTo, bearer)
	}

	e.askAttackers()
	submitAttackers(t, e, bearer)
	drainCombatDamagePriority(t, e)
	// The first-strike pass queued the trigger and the between-passes drain
	// put it on the stack; let it resolve before counting its counters.
	passUntilStackEmpty(t, e, 30)

	if got := e.G.Players[1].Life; got != 16 {
		t.Fatalf("defender life = %d, want 16 (2 first strike + 2 regular)", got)
	}
	if n := countChargeChanges(e, jitte, 2); n != 2 {
		t.Fatalf("logged %d CounterChange(CHARGE, +2) on the Jitte, want 2 (one per damage batch: the bearer dealt damage in both passes)", n)
	}
	if o := e.G.Obj(jitte); o.Counter("CHARGE") != 4 {
		t.Fatalf("jitte CHARGE counters = %d, want 4 (2 per damage step)", o.Counter("CHARGE"))
	}
	replayCheck(t, e, cfg)
}

// The CombatDamage$ half of the gate in isolation: the EQUIPPED creature deals
// NON-combat damage (its own DealDamage activation). The source matches
// ValidSource$ Creature.EquippedBy, so before the combatDamaging flag existed
// this shape could only have been kept quiet by the flag itself -- a Damage
// event from an ability resolution must never satisfy CombatDamage$ True.
func TestEquippedCreatureDealingNoncombatDamageGainsNoCharge(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := combatTriggerBoard(t, reg, []string{"Umezawa's Jitte"},
		[]string{
			"Name:Pinger\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nA:AB$ DealDamage | Cost$ 2 | ValidTgts$ Creature | NumDmg$ 1 | SpellDescription$ x\nOracle:x\n",
			"Name:Target Dummy\nManaCost:2\nTypes:Artifact Creature Golem\nPT:0/4\nOracle:x\n",
		}, nil, nil)
	// Park the clock at seat 0's first main phase the way linkBoard does (a
	// logged TurnChange/StepChange), so the bearer's activation is payable.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	jitte := findBattlefield(t, e, 0, "Umezawa's Jitte", 0)
	bearer := findBattlefield(t, e, 0, "Pinger", 0)
	dummy := findBattlefield(t, e, 0, "Target Dummy", 0)

	e.emit(events.Event{Kind: events.Attach, Obj: jitte, IDs: []state.ObjID{bearer}})

	addMana(t, e, 0, "CC")
	opt := abilityOption(t, e, bearer, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pinger target decision %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Obj == dummy {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no target option for the dummy: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 30)

	if o := e.G.Obj(dummy); o.Damage != 1 {
		t.Fatalf("dummy damage = %d, want 1 (the noncombat hit landed)", o.Damage)
	}
	if n := countChargeChanges(e, jitte, 2); n != 0 {
		t.Fatalf("non-combat DealDamage fired the CombatDamage$ True trigger %d time(s)", n)
	}
	replayCheck(t, e, cfg)
}

// A prevented combat hit (protection -> emit's Note substitution) never
// reaches checkTriggers as a Damage event, so the equipped creature's damage
// into a protection-bearer fires nothing -- even though the defender's own
// combat damage back at the bearer is real (its source is not equipped, so
// the ValidSource$ gate keeps it quiet as well).
func TestPreventedCombatDamageGainsNoCharge(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := combatTriggerBoard(t, reg, []string{"Umezawa's Jitte"},
		[]string{"Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"}, nil,
		[]string{"Name:Green Ward\nManaCost:2 W\nTypes:Creature Soldier\nPT:1/4\nK:Protection from Green\nOracle:x\n"})
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	jitte := findBattlefield(t, e, 0, "Umezawa's Jitte", 0)
	bearer := findBattlefield(t, e, 0, "Bear", 0)
	blocker := findBattlefield(t, e, 1, "Green Ward", 0)

	e.emit(events.Event{Kind: events.Attach, Obj: jitte, IDs: []state.ObjID{bearer}})

	e.askAttackers()
	submitAttackers(t, e, bearer)
	submitBlockers(t, e, blocker)

	if got := e.G.Obj(bearer).Damage; got != 1 {
		t.Fatalf("bearer damage = %d, want 1 (the blocker's hit is NOT prevented)", got)
	}
	if o := e.G.Obj(blocker); o.Zone != state.ZBattlefield || o.Damage != 0 {
		t.Fatalf("blocker zone %s damage %d -- the bearer's hit should have been prevented", o.Zone, o.Damage)
	}
	if n := countChargeChanges(e, jitte, 2); n != 0 {
		t.Fatalf("a prevented combat hit fired the CombatDamage$ True trigger %d time(s)", n)
	}
	replayCheck(t, e, cfg)
}

// Mode$ DamageDoneOnce gets the same once-per-batch gate DamageDealtOnce has,
// on its real compiled corpus script: Keeper of Fables' "Whenever one or more
// non-Human creatures you control deal combat damage to a player, draw a
// card." Two giants attack unblocked, so TWO Damage events land in the one
// damage batch -- the trigger draws exactly once ("one or more ...", the
// batch latch), not once per Damage event, and the two events' amounts
// accumulate into one referent (invisible here: DB$ Draw reads no amount).
func TestDamageDoneOnceFiresOncePerDamageStepOnRealCorpusScript(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := combatTriggerBoard(t, reg,
		[]string{"Keeper of Fables", "Hill Giant", "Hill Giant"}, nil, nil, nil)
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	keeper := findBattlefield(t, e, 0, "Keeper of Fables", 0)
	g1 := findBattlefield(t, e, 0, "Hill Giant", 0)
	g2 := findBattlefield(t, e, 0, "Hill Giant", 1)

	e.askAttackers()
	before := countDraws(e, 0)
	submitAttackers(t, e, g1, g2)
	// The trigger queued at the damage pass resolves once the engine grants
	// priority past combat; let the stack drain before counting.
	passUntilStackEmpty(t, e, 30)

	if got := e.G.Players[1].Life; got != 14 {
		t.Fatalf("defender life = %d, want 14 (two 3-power attackers)", got)
	}
	if n := countDraws(e, 0) - before; n != 1 {
		t.Fatalf("logged %d Draw events for seat 0 from combat, want 1 (DamageDoneOnce fires once per damage batch)", n)
	}
	if e.G.Obj(keeper) == nil {
		t.Fatal("keeper vanished")
	}
	replayCheck(t, e, cfg)
}

// The gate's flag in isolation, independent of ValidSource and of the stack
// (the Jitte script's ValidSource$ would mask the flag either way: during
// combat the stack is empty and non-combat ValidSource reads the resolving
// ability object, which never matches Creature.EquippedBy). This trigger
// carries CombatDamage$ True and nothing else, so the ONLY thing that can
// satisfy it is the combatDamaging flag: set (the state dealCombatDamage
// runs every assignment's emit under) it fires, unset (the ability-
// resolution state) it does not, same event shape, DamageDone so the
// once-per-batch latch never enters the comparison.
func TestCombatDamageFlagDrivesTheCombatDamageGate(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := combatTriggerBoard(t, reg, nil,
		[]string{
			"Name:Sentry\nManaCost:1 W\nTypes:Creature Soldier\nPT:1/1\n" +
				"T:Mode$ DamageDone | CombatDamage$ True | TriggerZones$ Battlefield | Execute$ TrigDraw\n" +
				"SVar:TrigDraw:DB$ Draw\nOracle:x\n",
			"Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n",
		}, nil, nil)
	_ = findBattlefield(t, e, 0, "Sentry", 0)
	bearer := findBattlefield(t, e, 0, "Bear", 0)

	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	base := countDraws(e, 0)

	// Combat leg: the flag set, e.damaging the bearer -- the state
	// dealCombatDamage runs every assignment's emit under.
	e.damaging = bearer
	e.combatDamaging = true
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
	e.combatDamaging = false
	e.damaging = 0
	e.priorityRound()
	passUntilStackEmpty(t, e, 30)
	if n := countDraws(e, 0) - base; n != 1 {
		t.Fatalf("combat-state Damage fired %d time(s), want 1", n)
	}

	// Non-combat leg: the identical event shape next turn, flag unset.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 3})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	e.damaging = bearer
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
	e.damaging = 0
	e.priorityRound()
	passUntilStackEmpty(t, e, 30)
	if n := countDraws(e, 0) - base; n != 1 {
		t.Fatalf("flag-unset Damage fired the CombatDamage$ True trigger %d more time(s), want 0", n-1)
	}
	replayCheck(t, e, cfg)
}

// countCounterChanges counts CounterChange events of one kind and per-emission
// amount on obj.
func countCounterChanges(e *Engine, obj state.ObjID, kind string, amount int32) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.CounterChange && ev.Obj == obj && ev.Counter == kind && ev.Amount == amount {
			n++
		}
	}
	return n
}

// countTokenCreates counts TokenCreate events owned by player p.
func countTokenCreates(e *Engine, p state.PlayerID) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.TokenCreate && ev.Player == p {
			n++
		}
	}
	return n
}

// answerRetortTarget answers the pending KTarget decision with the option
// naming want (the Screaming Nemesis retort asks "any other target" at
// placement).
func answerRetortTarget(t *testing.T, e *Engine, want state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want the retort's target decision", d)
	}
	for _, o := range d.Options {
		if o.Obj == want {
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
				t.Fatalf("submit retort target: %v", err)
			}
			return
		}
	}
	t.Fatalf("no target option for %d: %+v", want, d.Options)
}

const enrageCounterSrc = "Name:Riled Dino\nManaCost:2 G\nTypes:Creature Dinosaur\nPT:2/6\nK:Trample\n" +
	"T:Mode$ DamageDoneOnce | Execute$ TrigPump | ValidTarget$ Card.Self | TriggerZones$ Battlefield | TriggerDescription$ x\n" +
	"SVar:TrigPump:DB$ PutCounter | Defined$ Self | CounterType$ P1P1 | CounterNum$ X\n" +
	"SVar:X:TriggerCount$DamageAmount\nOracle:x\n"

const dealtOnceCounterSrc = "Name:Punisher\nManaCost:3 R\nTypes:Creature Goblin Warrior\nPT:4/6\nK:Trample\n" +
	"T:Mode$ DamageDealtOnce | CombatDamage$ True | ValidSource$ Card.Self | Execute$ TrigPump | TriggerZones$ Battlefield | TriggerDescription$ x\n" +
	"SVar:TrigPump:DB$ PutCounter | Defined$ Self | CounterType$ P1P1 | CounterNum$ X\n" +
	"SVar:X:TriggerCount$DamageAmount\nOracle:x\n"

// bearSrc is rules/additional_cost_offer_test.go's identical fixture, reused
// here for the two-blocker combat boards (same package).

// The once-per-batch gate's amount half, on the DONE side: the Riled Dino's
// Enrage-style DamageDoneOnce ("whenever CARDNAME is dealt damage, put that
// many +1/+1 counters on it") is dealt by TWO blockers in the one combat
// damage batch -- two separate Damage events, ONE trigger, and the trigger's
// TriggerCount$DamageAmount referent is the batch's accumulated total (4), not
// the first event's amount (2). Before the batch latch this card's trigger
// also never summed: the first hit fired it, the turn latch suppressed the
// second.
func TestDamageDoneOnceTriggerAmountIsTheBatchTotal(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := combatTriggerBoard(t, reg, nil, []string{enrageCounterSrc}, nil,
		[]string{bearSrc, bearSrc})
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	dino := findBattlefield(t, e, 0, "Riled Dino", 0)
	b1 := findBattlefield(t, e, 1, "Bear", 0)
	b2 := findBattlefield(t, e, 1, "Bear", 1)

	e.askAttackers()
	submitAttackers(t, e, dino)
	submitBlockers(t, e, b1, b2)
	// The hit-backs land synchronously inside the blocker submission; assert
	// the batch's raw total before the drain advances past the turn's cleanup
	// (which clears marked damage).
	if got := e.G.Obj(dino).Damage; got != 4 {
		t.Fatalf("dino damage = %d, want 4 (two 2/2 blockers hit back)", got)
	}
	drainCombatDamagePriority(t, e)
	passUntilStackEmpty(t, e, 30)

	if n := countCounterChanges(e, dino, "P1P1", 4); n != 1 {
		t.Fatalf("logged %d CounterChange(P1P1, +4) on the dino, want 1 (one trigger for the whole batch)", n)
	}
	if o := e.G.Obj(dino); o.Counter("P1P1") != 4 {
		t.Fatalf("dino P1P1 counters = %d, want 4 (the batch total, not the first hit's 2)", o.Counter("P1P1"))
	}
	replayCheck(t, e, cfg)
}

// The once-per-batch gate's amount half, on the DEALT side: the Punisher's
// DamageDealtOnce (CombatDamage$ True, ValidSource$ Card.Self) deals into TWO
// blockers in the one batch -- two separate Damage events, ONE trigger, and
// its TriggerCount$DamageAmount referent is the total that source dealt in
// the batch (4), matching Forge's per-source batch table.
func TestDamageDealtOnceTriggerAmountIsTheBatchTotal(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := combatTriggerBoard(t, reg, nil, []string{dealtOnceCounterSrc}, nil,
		[]string{bearSrc, bearSrc})
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	p := findBattlefield(t, e, 0, "Punisher", 0)
	b1 := findBattlefield(t, e, 1, "Bear", 0)
	b2 := findBattlefield(t, e, 1, "Bear", 1)

	e.askAttackers()
	submitAttackers(t, e, p)
	submitBlockers(t, e, b1, b2)
	drainCombatDamagePriority(t, e)
	passUntilStackEmpty(t, e, 30)

	if n := countCounterChanges(e, p, "P1P1", 4); n != 1 {
		t.Fatalf("logged %d CounterChange(P1P1, +4) on the Punisher, want 1 (one trigger for the whole batch)", n)
	}
	if o := e.G.Obj(p); o.Counter("P1P1") != 4 {
		t.Fatalf("Punisher P1P1 counters = %d, want 4 (the batch total, not the first hit's 2)", o.Counter("P1P1"))
	}
	replayCheck(t, e, cfg)
}

// Screaming Nemesis (real corpus script, DamageDoneOnce | ValidTarget$
// Card.Self, retort sized by TriggerCount$DamageAmount) fires on EACH of two
// separate damage events in the one turn -- the old once-per-turn latch held
// it to one. Each firing's retort lands at that event's own amount (2, then
// 3), which also proves the per-event referent amounts are not conflated.
func TestScreamingNemesisFiresPerDamageEvent(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := combatTriggerBoard(t, reg, []string{"Screaming Nemesis"}, nil, nil,
		[]string{"Name:Target Dummy\nManaCost:2\nTypes:Artifact Creature Golem\nPT:0/9\nOracle:x\n"})
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	nemesis := findBattlefield(t, e, 0, "Screaming Nemesis", 0)
	dummy := findBattlefield(t, e, 1, "Target Dummy", 0)

	// First separate damage event.
	e.emit(events.Event{Kind: events.Damage, Obj: nemesis, Amount: 2})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("pendingTriggers after 1st damage = %d, want 1", len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	answerRetortTarget(t, e, dummy)
	passUntilStackEmpty(t, e, 30)
	if o := e.G.Obj(dummy); o.Damage != 2 {
		t.Fatalf("dummy damage after 1st retort = %d, want 2", o.Damage)
	}

	// A second SEPARATE damage event, same turn: the trigger fires again.
	e.emit(events.Event{Kind: events.Damage, Obj: nemesis, Amount: 3})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("pendingTriggers after 2nd separate damage = %d, want 1 (the Once latch is per batch, not per turn)", len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	answerRetortTarget(t, e, dummy)
	passUntilStackEmpty(t, e, 30)
	if o := e.G.Obj(dummy); o.Damage != 5 {
		t.Fatalf("dummy damage after 2nd retort = %d, want 5 (2 + 3, each retort at its own event's amount)", o.Damage)
	}
	replayCheck(t, e, cfg)
}

// Raptor Hatchling (real corpus Enrage script, DamageDoneOnce | ValidTarget$
// Card.Self) fires on EACH of two separate damage events in the one turn --
// the old once-per-turn latch held it to one, so a second Shock in the same
// turn never made its second 3/3 Dinosaur.
func TestRaptorHatchlingFiresPerDamageEvent(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := combatTriggerBoard(t, reg, []string{"Raptor Hatchling"}, nil, nil, nil)
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	hatchling := findBattlefield(t, e, 0, "Raptor Hatchling", 0)

	before := countTokenCreates(e, 0)
	e.emit(events.Event{Kind: events.Damage, Obj: hatchling, Amount: 1})
	e.emit(events.Event{Kind: events.Damage, Obj: hatchling, Amount: 1})
	if len(e.pendingTriggers) != 2 {
		t.Fatalf("pendingTriggers after two separate damage events = %d, want 2 (one per batch)", len(e.pendingTriggers))
	}
	e.putTriggersOnStack() // two same-controller triggers ask for an order
	if d := e.Pending(); d == nil || d.Kind != decision.KTriggerOrder {
		t.Fatalf("pending = %+v, want the two triggers to ask for an order", d)
	}
	submit(t, e, 0, 1)
	if len(e.G.Stack) != 2 {
		t.Fatalf("stack = %v, want both triggers placed", e.G.Stack)
	}
	e.resolveTop()
	e.resolveTop()
	if n := countTokenCreates(e, 0) - before; n != 2 {
		t.Fatalf("logged %d TokenCreate events for seat 0, want 2 (one Enrage per damage batch)", n)
	}
	replayCheck(t, e, cfg)
}
