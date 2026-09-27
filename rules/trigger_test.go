package rules

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const etbDrawSrc = `Name:Scribe
ManaCost:1 U
Types:Creature Human
PT:1/1
T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigDraw | TriggerDescription$ draw
SVar:TrigDraw:DB$ Draw | NumCards$ 1 | Defined$ You
Oracle:x
`

func TestEnterTheBattlefieldTriggerGoesOnTheStackAndResolves(t *testing.T) {
	e := layerEngine(t)
	before := len(e.G.Zone(state.ZHand, 0))
	id := onBoard(t, e, 0, etbDrawSrc)
	// onBoard bypasses events, so fire the zone change explicitly.
	e.G.SetZone(state.ZBattlefield, 0, nil)
	e.G.Obj(id).Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), id))
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})

	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the triggered ability", e.G.Stack)
	}
	e.resolveTop()
	if got := len(e.G.Zone(state.ZHand, 0)); got != before+1 {
		t.Fatalf("hand = %d, want %d after the ETB draw", got, before+1)
	}
}

func TestTriggerDoesNotFireForOtherObjects(t *testing.T) {
	e := layerEngine(t)
	watcher := onBoard(t, e, 0, etbDrawSrc)
	_ = watcher
	other := e.G.AddObject(card(t, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 0)
	other.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{other.ID})
	e.emit(events.Event{Kind: events.MoveZone, Obj: other.ID,
		From: state.ZHand, To: state.ZBattlefield})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 0 {
		t.Fatalf("Card.Self trigger fired for a different object: %v", e.G.Stack)
	}
}

func TestTriggerZonesGateFiring(t *testing.T) {
	// A graveyard-only trigger must not fire while the card is on the
	// battlefield.
	src := `Name:Ghoul
ManaCost:B
Types:Creature Zombie
PT:1/1
T:Mode$ Phase | Phase$ Upkeep | TriggerZones$ Graveyard | Execute$ TrigLose | TriggerDescription$ x
SVar:TrigLose:DB$ LoseLife | LifeAmount$ 1 | Defined$ You
Oracle:x
`
	e := layerEngine(t)
	id := onBoard(t, e, 0, src)
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 0 {
		t.Fatal("a graveyard trigger fired from the battlefield")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id,
		From: state.ZBattlefield, To: state.ZGraveyard})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatal("the graveyard trigger did not fire from the graveyard")
	}
}

// TestZoneSpecContainsMatchesSplitSemantics pins the invariant the hand-rolled,
// allocation-free zoneSpecContains (which replaced strings.Split in zoneGate)
// relies on: for every TriggerZones$ string, it must report exactly the same
// membership as the old strings.Split+TrimSpace+ParseZone loop did -- including
// the edge shapes that hand-rolled scanners get wrong (a trailing/leading comma
// making a phantom graveyard part, a doubled comma in the middle, whitespace
// after a separator). The reference computation below IS the old code, so a
// regression that drops a part or mishandles an empty segment fails here.
func TestZoneSpecContainsMatchesSplitSemantics(t *testing.T) {
	candidates := []state.Zone{
		state.ZBattlefield, state.ZLibrary, state.ZGraveyard,
		state.ZExile, state.ZStack, state.ZHand,
	}
	for _, tc := range []struct {
		spec string
	}{
		{"Battlefield"},
		{"Battlefield,Library"},
		{"Library,Graveyard"},
		{"Stack"},
		{"Graveyard,Exile,Library"},
		{"Battlefield,"},         // trailing empty part -> graveyard.
		{",Battlefield"},         // leading empty part -> graveyard.
		{"Battlefield,,Library"}, // doubled separator -> phantom graveyard.
		{"Graveyard, Stack"},     // a space; the reference trims, ParseZone does too.
	} {
		want := map[state.Zone]bool{}
		for _, z := range strings.Split(tc.spec, ",") {
			want[effects.ParseZone(strings.TrimSpace(z))] = true
		}
		for _, zone := range candidates {
			if got := zoneSpecContains(tc.spec, zone); got != want[zone] {
				t.Errorf("zoneSpecContains(%q, %s) = %v, want %v (split semantics)",
					tc.spec, zone, got, want[zone])
			}
		}
	}
}

// TestMultiZoneTriggerZonesFiresFromEachZone is the end-to-end guard that a
// comma-separated TriggerZones$ triggers from every listed zone, not just the
// first. The allocation-free scanner must still recognize the second and later
// parts of the spec; a last-part-drop regression would make a Library,Graveyard
// trigger fire only from the library.
func TestMultiZoneTriggerZonesFiresFromEachZone(t *testing.T) {
	src := `Name:Ghost
ManaCost:B
Types:Creature Spirit
PT:1/1
T:Mode$ Phase | Phase$ Upkeep | TriggerZones$ Library,Graveyard | Execute$ TrigLose | TriggerDescription$ x
SVar:TrigLose:DB$ LoseLife | LifeAmount$ 1 | Defined$ You
Oracle:x
`
	for _, zone := range []state.Zone{state.ZLibrary, state.ZGraveyard} {
		e := layerEngine(t)
		obj := e.G.AddObject(card(t, src), 0)
		id := obj.ID
		obj.Zone = zone
		e.G.SetZone(zone, 0, []state.ObjID{id})
		e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
		e.putTriggersOnStack()
		if len(e.G.Stack) != 1 {
			t.Errorf("Library,Graveyard trigger did not fire from %s", zone)
		}
	}
}

func TestSimultaneousTriggersStackInAPNAPOrder(t *testing.T) {
	e := layerEngine(t)
	// Two upkeep triggers, one per seat. APNAP means the active player's goes
	// on the stack first and therefore resolves last.
	src := `Name:Bell
ManaCost:W
Types:Enchantment
T:Mode$ Phase | Phase$ Upkeep | Execute$ TrigGain | TriggerDescription$ x
SVar:TrigGain:DB$ GainLife | LifeAmount$ 1 | Defined$ You
Oracle:x
`
	a := onBoard(t, e, 0, src)
	b := onBoard(t, e, 1, src)
	e.G.Active = 0
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 2 {
		t.Fatalf("stack = %v, want two triggers", e.G.Stack)
	}
	if e.G.Obj(e.G.Stack[0]).Source != a || e.G.Obj(e.G.Stack[1]).Source != b {
		t.Fatal("triggers were not stacked in APNAP order")
	}
}

func TestReplacementRedirectsTheEvent(t *testing.T) {
	src := `Name:Phoenix
ManaCost:2 R
Types:Creature Phoenix
PT:2/2
R:Event$ Moved | Destination$ Graveyard | ValidCard$ Card.Self | ReplaceWith$ RepExile | Description$ x
SVar:RepExile:DB$ ChangeZone | Origin$ Battlefield | Destination$ Exile | Defined$ Self
Oracle:x
`
	e := layerEngine(t)
	id := onBoard(t, e, 0, src)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id,
		From: state.ZBattlefield, To: state.ZGraveyard})
	if e.G.Obj(id).Zone != state.ZExile {
		t.Fatalf("zone = %s, want exile", e.G.Obj(id).Zone)
	}
	if len(e.G.Zone(state.ZGraveyard, 0)) != 0 {
		t.Fatal("the replaced move still reached the graveyard")
	}
}

func TestReplacementAppliesOnlyOncePerEvent(t *testing.T) {
	// A replacement that re-emits a matching event must not re-trigger itself
	// into an infinite loop.
	src := `Name:Loop
ManaCost:1
Types:Artifact
R:Event$ Moved | Destination$ Graveyard | ValidCard$ Card.Self | ReplaceWith$ RepAgain | Description$ x
SVar:RepAgain:DB$ ChangeZone | Origin$ Battlefield | Destination$ Graveyard | Defined$ Self
Oracle:x
`
	e := layerEngine(t)
	id := onBoard(t, e, 0, src)
	done := make(chan struct{})
	go func() {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id,
			From: state.ZBattlefield, To: state.ZGraveyard})
		close(done)
	}()
	<-done
	if e.G.Obj(id).Zone != state.ZGraveyard {
		t.Fatalf("zone = %s", e.G.Obj(id).Zone)
	}
}

// --- Additional coverage beyond the brief's mandatory six, for the five
// trigger modes those tests don't otherwise exercise, and for the totality
// concerns (cascade bound, SVar threading, F3 compliance) the brief calls
// out by name. ---

func TestSpellCastTriggerMatchesValidCardAndPlayer(t *testing.T) {
	src := `Name:Watcher
ManaCost:1 U
Types:Creature Wizard
PT:1/1
T:Mode$ SpellCast | ValidCard$ Instant | ValidActivatingPlayer$ Opponent | Execute$ TrigDraw | TriggerDescription$ x
SVar:TrigDraw:DB$ Draw | NumCards$ 1 | Defined$ You
Oracle:x
`
	e := layerEngine(t)
	onBoard(t, e, 0, src)
	bolt := card(t, "Name:Shock\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n")
	spell := e.G.AddObject(bolt, 1)
	spell.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 1, []state.ObjID{spell.ID})
	e.emit(events.Event{Kind: events.PutOnStack, Obj: spell.ID, Player: 1,
		From: state.ZHand, To: state.ZStack})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 2 { // the spell itself, plus the trigger
		t.Fatalf("stack = %v, want the spell plus one trigger", e.G.Stack)
	}
}

func TestSpellCastTriggerIgnoresATriggeredAbilityEnteringTheStack(t *testing.T) {
	// A SpellCast trigger with a bare "Any" filter must not fire for a
	// triggered ability object (no Face -- Ruling F3) entering the stack;
	// only an actual spell (a card) counts as "casting a spell".
	src := `Name:Watcher
ManaCost:1 U
Types:Creature Wizard
PT:1/1
T:Mode$ SpellCast | ValidCard$ Any | Execute$ TrigDraw | TriggerDescription$ x
SVar:TrigDraw:DB$ Draw | NumCards$ 1 | Defined$ You
Oracle:x
`
	src2 := `Name:Herald
ManaCost:W
Types:Creature Human
PT:1/1
T:Mode$ Phase | Phase$ Upkeep | Execute$ TrigGain | TriggerDescription$ x
SVar:TrigGain:DB$ GainLife | LifeAmount$ 1 | Defined$ You
Oracle:x
`
	e := layerEngine(t)
	onBoard(t, e, 0, src)
	onBoard(t, e, 0, src2)
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want only the Phase trigger", e.G.Stack)
	}
	if e.G.Obj(e.G.Stack[0]).Ability == nil {
		t.Fatal("expected the stack object to be an ability")
	}
}

func TestAttacksTriggerFiresForTheDeclaredAttacker(t *testing.T) {
	src := `Name:Raider
ManaCost:1 R
Types:Creature Goblin
PT:2/2
T:Mode$ Attacks | ValidCard$ Card.Self | Execute$ TrigPump | TriggerDescription$ x
SVar:TrigPump:DB$ Pump | Defined$ Self | NumAtt$ +1
Oracle:x
`
	e := layerEngine(t)
	id := onBoard(t, e, 0, src)
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{id}})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the attack trigger", e.G.Stack)
	}
}

func TestAttacksTriggerDoesNotFireForANonAttacker(t *testing.T) {
	src := `Name:Raider
ManaCost:1 R
Types:Creature Goblin
PT:2/2
T:Mode$ Attacks | ValidCard$ Card.Self | Execute$ TrigPump | TriggerDescription$ x
SVar:TrigPump:DB$ Pump | Defined$ Self | NumAtt$ +1
Oracle:x
`
	e := layerEngine(t)
	id := onBoard(t, e, 0, src)
	other := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	_ = id
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{other}})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v, want no trigger for a non-attacker", e.G.Stack)
	}
}

func TestDamageDoneTriggerMatchesValidTarget(t *testing.T) {
	src := `Name:Vampire
ManaCost:1 B
Types:Creature Vampire
PT:2/2
T:Mode$ DamageDone | ValidTarget$ Player | Execute$ TrigGain | TriggerDescription$ x
SVar:TrigGain:DB$ GainLife | LifeAmount$ 1 | Defined$ You
Oracle:x
`
	e := layerEngine(t)
	onBoard(t, e, 0, src)
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the damage trigger", e.G.Stack)
	}
}

func TestDamageDealtOnceFiresPerDamageBatch(t *testing.T) {
	src := `Name:Vampire
ManaCost:1 B
Types:Creature Vampire
PT:2/2
T:Mode$ DamageDealtOnce | ValidTarget$ Player | Execute$ TrigGain | TriggerDescription$ x
SVar:TrigGain:DB$ GainLife | LifeAmount$ 1 | Defined$ You
Oracle:x
`
	e := layerEngine(t)
	onBoard(t, e, 0, src)
	// Two SEPARATE non-combat Damage events are two damage batches (Forge
	// dealDamage brackets each call's damage; nothing here brackets them
	// together), so the trigger fires per event. The old once-per-turn latch
	// suppressed the second firing -- this test used to pin exactly that and
	// was flipped by the per-batch fix.
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
	if len(e.pendingTriggers) != 2 {
		t.Fatalf("pendingTriggers = %d, want two (one per separate damage batch)", len(e.pendingTriggers))
	}
	e.putTriggersOnStack() // two same-controller triggers ask for an order
	if d := e.Pending(); d == nil || d.Kind != decision.KTriggerOrder {
		t.Fatalf("pending = %+v, want the two triggers to ask for an order", d)
	}
	submit(t, e, 0, 1)
	if len(e.G.Stack) != 2 {
		t.Fatalf("stack = %v, want both triggers placed", e.G.Stack)
	}
	e.resolveTop() // clear the stack so the next check counts only new triggers.
	e.resolveTop()
	// A third event, a turn later, fires again.
	e.G.Turn++
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the trigger to fire again next turn", e.G.Stack)
	}
}

// A damage producer can nest inside another batch owner (for example, an
// effect or replacement that emits damage while resolving a DamageAll). The
// inner bracket is part of the outer simultaneous batch, so its End must not
// clear the outer latch: all three events below queue one trigger totaling 6.
func TestNestedDamageBatchesKeepOuterOnceLatch(t *testing.T) {
	src := `Name:Vampire
ManaCost:1 B
Types:Creature Vampire
PT:2/2
T:Mode$ DamageDealtOnce | ValidTarget$ Player | Execute$ TrigGain | TriggerDescription$ x
SVar:TrigGain:DB$ GainLife | LifeAmount$ 1 | Defined$ You
Oracle:x
`
	e := layerEngine(t)
	onBoard(t, e, 0, src)

	e.BeginDamageBatch()
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 1})
	e.BeginDamageBatch()
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 2})
	e.EndDamageBatch()
	if !e.damageBatchOpen || e.damageBatchDepth != 1 {
		t.Fatalf("inner End closed the outer batch: open=%v depth=%d", e.damageBatchOpen, e.damageBatchDepth)
	}
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 3})
	e.EndDamageBatch()

	if e.damageBatchOpen || e.damageBatchDepth != 0 {
		t.Fatalf("outer End left batch open=%v depth=%d", e.damageBatchOpen, e.damageBatchDepth)
	}
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("pendingTriggers = %d, want 1 for the nested outer batch", len(e.pendingTriggers))
	}
	if got := e.pendingTriggers[0].Ctx.TriggerContext.TriggerAmount; got != 6 {
		t.Fatalf("trigger damage amount = %d, want 6 (1 + 2 + 3 in one batch)", got)
	}
}

// Negative Damage repairs clear marked damage during cleanup/regeneration;
// they are not damage dealt. Both Once modes must ignore the repair, rather
// than adding another trigger after the positive damage that it clears.
func TestDamageOnceDoesNotTriggerOnNegativeDamageRepair(t *testing.T) {
	src := `Name:Vampire
ManaCost:1 B
Types:Creature Vampire
PT:2/2
T:Mode$ DamageDealtOnce | ValidTarget$ Card.Self | Execute$ TrigGain | TriggerDescription$ x
T:Mode$ DamageDoneOnce | ValidTarget$ Card.Self | Execute$ TrigGain | TriggerDescription$ x
SVar:TrigGain:DB$ GainLife | LifeAmount$ 1 | Defined$ You
Oracle:x
`
	e := layerEngine(t)
	id := onBoard(t, e, 0, src)
	e.emit(events.Event{Kind: events.Damage, Obj: id, Amount: 2})
	if len(e.pendingTriggers) != 2 {
		t.Fatalf("positive damage queued %d Once triggers, want 2", len(e.pendingTriggers))
	}

	// This is the same negative Damage shape cleanupBody emits to remove the
	// two marked points. It must leave the positive-damage queue untouched.
	e.emit(events.Event{Kind: events.Damage, Obj: id, Amount: -2})
	if len(e.pendingTriggers) != 2 {
		t.Fatalf("negative damage repair queued %d Once triggers, want 0 additional", len(e.pendingTriggers)-2)
	}
	if got := e.G.Obj(id).Damage; got != 0 {
		t.Fatalf("damage after repair = %d, want 0", got)
	}
}

func TestBecomesTargetTriggerFiresWhenTargeted(t *testing.T) {
	src := `Name:Ward
ManaCost:1 W
Types:Creature Soldier
PT:1/3
T:Mode$ BecomesTarget | ValidTarget$ Card.Self | Execute$ TrigGain | TriggerDescription$ x
SVar:TrigGain:DB$ GainLife | LifeAmount$ 1 | Defined$ You
Oracle:x
`
	e := layerEngine(t)
	id := onBoard(t, e, 0, src)
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: 999, IDs: []state.ObjID{id}})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the becomes-target trigger", e.G.Stack)
	}
}

func TestLandPlayedTriggerFiresOnlyForLands(t *testing.T) {
	src := `Name:Ranger
ManaCost:1 G
Types:Creature Human
PT:2/2
T:Mode$ LandPlayed | ValidCard$ Land | Execute$ TrigGain | TriggerDescription$ x
SVar:TrigGain:DB$ GainLife | LifeAmount$ 1 | Defined$ You
Oracle:x
`
	e := layerEngine(t)
	onBoard(t, e, 0, src)
	land := e.G.AddObject(card(t, "Name:Forest\nTypes:Basic Land Forest\nOracle:x\n"), 0)
	land.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{land.ID})
	e.emit(events.Event{Kind: events.MoveZone, Obj: land.ID, From: state.ZHand, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.LandPlayed, Player: 0})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the land-played trigger", e.G.Stack)
	}
}

func TestLandPlayedTriggerIgnoresTheSeparateLandPlayedEvent(t *testing.T) {
	// The bookkeeping LandPlayed event carries no Obj, so it must never by
	// itself satisfy a LandPlayed trigger's ValidCard$.
	src := `Name:Ranger
ManaCost:1 G
Types:Creature Human
PT:2/2
T:Mode$ LandPlayed | ValidCard$ Land | Execute$ TrigGain | TriggerDescription$ x
SVar:TrigGain:DB$ GainLife | LifeAmount$ 1 | Defined$ You
Oracle:x
`
	e := layerEngine(t)
	onBoard(t, e, 0, src)
	e.emit(events.Event{Kind: events.LandPlayed, Player: 0})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v, want no trigger from the bookkeeping event alone", e.G.Stack)
	}
}

func TestTriggerCascadeIsBounded(t *testing.T) {
	// A trigger that fires in response to its own effect must not queue
	// forever: maxTriggerFires caps how many times one (source, index) pair
	// can enqueue, even across many repeats of the same event.
	src := `Name:Looper
ManaCost:1 B
Types:Creature Horror
PT:1/1
T:Mode$ DamageDone | ValidSource$ Card.Self | Execute$ TrigGain | TriggerDescription$ x
SVar:TrigGain:DB$ GainLife | LifeAmount$ 1 | Defined$ You
Oracle:x
`
	e := layerEngine(t)
	id := onBoard(t, e, 0, src)
	// Put the object itself on top of the stack so damageSource() names it.
	e.G.Stack = []state.ObjID{id}
	for i := 0; i < maxTriggerFires+50; i++ {
		e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 1})
	}
	if got := len(e.pendingTriggers); got != maxTriggerFires {
		t.Fatalf("pendingTriggers = %d, want the cascade capped at %d", got, maxTriggerFires)
	}
}

func TestDiesTriggerFiresWithDefaultTriggerZones(t *testing.T) {
	// A "dies" trigger's own source leaves the battlefield as part of the
	// very event it must react to: by the time checkTriggers runs, the
	// object's current zone is already the graveyard, so a naive zoneGate
	// checking only the post-move zone would never see it "on the
	// battlefield" and would never fire. zoneGate must also accept the zone
	// the object was just in for the specific event that moved it.
	src := `Name:Imp
ManaCost:B
Types:Creature Imp
PT:1/1
T:Mode$ ChangesZone | Origin$ Battlefield | Destination$ Graveyard | ValidCard$ Card.Self | Execute$ TrigLose | TriggerDescription$ x
SVar:TrigLose:DB$ LoseLife | LifeAmount$ 1 | Defined$ You
Oracle:x
`
	e := layerEngine(t)
	id := onBoard(t, e, 0, src)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id,
		From: state.ZBattlefield, To: state.ZGraveyard})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the dies trigger", e.G.Stack)
	}
}

func TestReplacementDoesNotFireForAnUnrelatedDestination(t *testing.T) {
	src := `Name:Phoenix
ManaCost:2 R
Types:Creature Phoenix
PT:2/2
R:Event$ Moved | Destination$ Graveyard | ValidCard$ Card.Self | ReplaceWith$ RepExile | Description$ x
SVar:RepExile:DB$ ChangeZone | Origin$ Battlefield | Destination$ Exile | Defined$ Self
Oracle:x
`
	e := layerEngine(t)
	id := onBoard(t, e, 0, src)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id,
		From: state.ZBattlefield, To: state.ZHand})
	if e.G.Obj(id).Zone != state.ZHand {
		t.Fatalf("zone = %s, want hand: an unrelated destination must not be replaced", e.G.Obj(id).Zone)
	}
}

// --- Fix round 1: Findings 1-4. These specifically verify the effect of a
// resolved trigger lands on the *correct* object -- Defined$ You/Player (as
// every test above uses) read c.Controller and are immune to both Ruling
// T20-b (Ctx.Source) and T20-c (Ctx.Remembered), which is exactly why the
// first round's 246 passing tests gave false confidence. NumAtt$ here is
// always a literal, never an SVar reference, so these are not entangled
// with the separate effects/count.go gaps the report already flags for
// Piledriver/Mimic.

func TestTriggerEffectAppliesToTheSourceNotTheAbilityWrapper(t *testing.T) {
	// Ruling T20-b regression: resolveTop's ability branch used to build
	// Ctx.Source from the transient stack-object id instead of o.Source, so
	// Defined$ Self -- the single most common Defined$ value in real
	// trigger scripts -- pointed at the wrapper rather than the permanent,
	// and the pump silently applied to nothing.
	src := `Name:Bully
ManaCost:1 R
Types:Creature Goblin
PT:2/2
T:Mode$ Attacks | ValidCard$ Card.Self | Execute$ TrigPump | TriggerDescription$ x
SVar:TrigPump:DB$ Pump | Defined$ Self | NumAtt$ +3 | NumDef$ +3
Oracle:x
`
	e := layerEngine(t)
	id := onBoard(t, e, 0, src)
	if got := e.Power(id); got != 2 {
		t.Fatalf("power before attacking = %d, want 2", got)
	}
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{id}})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the attack trigger queued", e.G.Stack)
	}
	e.resolveTop()
	if got := e.Power(id); got != 5 {
		t.Fatalf("power after the trigger resolved = %d, want 5 (2 base + 3 pump landed on Bully itself)", got)
	}
}

func TestTriggerEffectAppliesToTheRememberedObject(t *testing.T) {
	// Ruling T20-c regression: events.Move's zone-leave reset used to wipe
	// Object.Remembered on the very MoveZone that placed the ability on the
	// stack (ZStack falls into Move's "leaving play" default case, the same
	// as every non-battlefield destination), so Defined$ Remembered always
	// read back empty and the pump silently applied to nothing.
	src := `Name:Watcher
ManaCost:1 U
Types:Creature Wizard
PT:1/1
T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Creature.Other | Execute$ TrigPump | TriggerDescription$ x
SVar:TrigPump:DB$ Pump | Defined$ Remembered | NumAtt$ +1 | NumDef$ +1
Oracle:x
`
	e := layerEngine(t)
	watcher := onBoard(t, e, 0, src)
	other := e.G.AddObject(card(t, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), 0)
	other.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{other.ID})
	e.emit(events.Event{Kind: events.MoveZone, Obj: other.ID, From: state.ZHand, To: state.ZBattlefield})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the ChangesZone trigger queued", e.G.Stack)
	}
	if got := e.G.Obj(e.G.Stack[0]).Remembered; len(got) != 1 || got[0].Obj != other.ID {
		t.Fatalf("Remembered = %v, want [{Obj: %d}] (the entering Bear, not Watcher)", got, other.ID)
	}
	e.resolveTop()
	if got := e.Power(other.ID); got != 3 {
		t.Fatalf("bear power = %d, want 3 (2 base + 1 pump landed on the remembered object)", got)
	}
	if got := e.Power(watcher); got != 1 {
		t.Fatalf("watcher power = %d, want unchanged 1 -- the pump must not land on the trigger's own source", got)
	}
}

// TestReplayFromLogAloneReconstructsTriggeredAbilities is Ruling T20-a's
// regression test: a game log folded into a fresh Game with no rules.Engine
// behind it -- exactly what a real replay-from-log-alone does -- must
// reconstruct the same state a live game reached, triggered abilities
// included. Before the fix, the ability wrapper's ObjID was assigned by a
// direct, unlogged Game.AddObject call; a log-only reconstruction never
// learned that ID existed, so the MoveZone that placed it on the stack
// silently no-op'd (events.Move's "if o == nil { return }" guard) and the
// replayed stack permanently diverged from the live one.
func TestReplayFromLogAloneReconstructsTriggeredAbilities(t *testing.T) {
	// The trigger-bearing creature must reach the battlefield through
	// ordinary logged events, not the onBoard test helper every other test
	// in this file uses -- onBoard's whole point is to bypass the log
	// (its own doc comment says so), which is exactly what this test must
	// not do: it is specifically checking that replaying the log *alone*
	// reconstructs the live game, so the Scribe has to actually be part of
	// a deck (genesis's own AddObject calls, which replayFromLog
	// independently reconstructs the same way) and move by MoveZone events.
	scribe := card(t, etbDrawSrc)
	deck0 := append([]*cards.Card{scribe}, mountainDeck(t, 39)...)
	cfg := Config{Seed: 7, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{deck0, mountainDeck(t, 40)}}
	e := New(cfg)

	// Genesis's shuffle puts the Scribe somewhere in player 0's library or
	// opening hand; find it deterministically rather than depending on
	// where the shuffle happened to land it.
	var id state.ObjID
	for _, cand := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(cand).Face().Name == "Scribe" {
			id = cand
			break
		}
	}
	if id == 0 {
		for _, cand := range e.G.Zone(state.ZLibrary, 0) {
			if e.G.Obj(cand).Face().Name == "Scribe" {
				id = cand
				break
			}
		}
		if id == 0 {
			t.Fatal("Scribe not found in player 0's library or hand")
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the triggered ability queued", e.G.Stack)
	}
	e.resolveTop()
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack = %v, want the ability resolved off the stack", e.G.Stack)
	}
	afterTrigger := len(e.L.Events)

	// Drive ordinary turn structure on top of the trigger too, so the
	// replayed log also has to reconstruct Priority/Passes/StepChange/
	// TurnChange correctly, not just the one trigger.
	passAll(t, e, 60)

	fresh := replayFromLog(t, cfg, e.L.Events)
	if diff := diffGames(e.G, fresh); diff != "" {
		t.Fatalf("replay from the log alone diverged from the live game:\n%s", diff)
	}

	// Control: dropping the trigger's own final event (the ability
	// wrapper's move from the stack to exile) must be detected as a
	// divergence, or this test could pass vacuously.
	truncated := replayFromLog(t, cfg, e.L.Events[:afterTrigger-1])
	if diff := diffGames(e.G, truncated); diff == "" {
		t.Fatal("control: a truncated log should diverge from the live game, but diffGames found no difference")
	}
}

func TestDiesTriggerSeesTheCreatureAsItWas(t *testing.T) {
	// A creature with a dies trigger and two +1/+1 counters: Move clears the
	// counters, so only last known information can say it had them.
	src := "Name:Geist\nManaCost:G G\nTypes:Creature Spirit\nPT:2/1\n" +
		"T:Mode$ ChangesZone | Origin$ Battlefield | Destination$ Graveyard | ValidCard$ Card.Self | Execute$ TrigNote | TriggerDescription$ When CARDNAME dies, note.\n" +
		"SVar:TrigNote:DB$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"
	e, _, id := newFixtureDeck(t, 3, src)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "P1P1", Amount: 2})
	e.pendingTriggers = nil
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZGraveyard})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("%d pending triggers", len(e.pendingTriggers))
	}
	lki := e.pendingTriggers[0].Ctx.LKI
	if lki == nil || lki.ID != id || lki.Zone != state.ZBattlefield || lki.Counter("P1P1") != 2 {
		t.Fatalf("LKI %+v", lki)
	}
	if e.G.Obj(id).Counter("P1P1") != 0 {
		t.Fatal("the live object should have lost its counters on leaving")
	}
	lki.AddCounter("P1P1", 5)
	if e.G.Obj(id).Counter("P1P1") != 0 {
		t.Fatal("LKI shares a Counters array with the live object")
	}
}

// TestETBTriggerLKIDoesNotAliasCountersEnteringPlay is the aliasing check
// TestDiesTriggerSeesTheCreatureAsItWas above cannot actually make: a move
// OUT of the battlefield resets Counters to nil (events/apply.go's Move,
// default case) regardless of whether emit's LKI capture aliased them, so
// mutating lki.Counters there can never reach the live object's Counters --
// it's already nil by the time the assertion runs, deep copy or not. Moving
// INTO the battlefield is the one shape where Move does NOT reset Counters
// (Move's ZBattlefield case only sets SummonSick/Damage/Timestamp), so the
// live object keeps whatever counters it already had, and a shallow LKI
// copy (a bare `cp := *o` with no independent Counters backing array) would
// let mutating the LKI's copy mutate the live object's counters too. Fix
// round 1, Minor 3.
func TestETBTriggerLKIDoesNotAliasCountersEnteringPlay(t *testing.T) {
	src := "Name:Geist\nManaCost:G G\nTypes:Creature Spirit\nPT:2/1\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigNote | TriggerDescription$ x\n" +
		"SVar:TrigNote:DB$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"
	e, _, id := newFixtureDeck(t, 3, src)
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "P1P1", Amount: 2})
	e.pendingTriggers = nil
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("%d pending triggers", len(e.pendingTriggers))
	}
	lki := e.pendingTriggers[0].Ctx.LKI
	if lki == nil || lki.Counter("P1P1") != 2 {
		t.Fatalf("LKI %+v", lki)
	}
	if e.G.Obj(id).Counter("P1P1") != 2 {
		t.Fatal("entering the battlefield should not reset counters (Move's ZBattlefield case)")
	}
	lki.AddCounter("P1P1", 5)
	if e.G.Obj(id).Counter("P1P1") != 2 {
		t.Fatal("LKI aliases the live object's Counters array")
	}
}

func TestAttacksTriggerRemembersTheDefendingPlayer(t *testing.T) {
	ids := []state.ObjID{4, 5}
	got := triggerRemembered(events.Event{Kind: events.DeclareAttackers, Player: 2, IDs: ids}, 9)
	if len(got) != 3 || got[0].Obj != 4 || got[1].Obj != 5 || !got[2].IsPlayer || got[2].Player != 2 {
		t.Fatalf("%v", got)
	}
}

// replayFromLog reconstructs a Game from cfg's decks (replicating rules.New's
// own unlogged genesis AddObject calls, in the same per-deck order) plus
// every event in log, applied via events.Apply directly -- no rules.Engine
// involved, exactly what a real replay-from-log-alone would do. Tokens is
// wired the same way New itself wires it (g.Tokens = cfg.Tokens), so a
// TokenCreate event in the log resolves against the same table live play
// used -- previously missing here, latent until Task 9's cast_test.go
// fixture helper (newFixtureDeck's own doc comment already named the
// general hazard: an object introduced any way other than via cfg.Decks is
// invisible to this function) started relying on it for replay fidelity.
// Starting life, commander identity/cast counters and the match-wide commander
// damage-vector size are genesis bookkeeping rather than logged state, so they
// are reconstructed from Config and the event stream just as New initializes
// them. Commander seating uses legalCommandersFor, the same gate as New.
func replayFromLog(t *testing.T, cfg Config, log []events.Event) *state.Game {
	t.Helper()
	life := int32(20)
	if cfg.StartingLife > 0 {
		life = cfg.StartingLife
	}
	g := state.NewGameLife(cfg.Names, life)
	g.Tokens = cfg.Tokens
	cmdIDs := make([][]state.ObjID, len(cfg.Names))
	totalCmd := 0
	for i := range cfg.Names {
		if i >= len(cfg.Decks) {
			continue
		}
		deck := cfg.Decks[i]
		p := state.PlayerID(i)
		ids := make([]state.ObjID, 0, len(deck))
		for _, c := range deck {
			ids = append(ids, g.AddObject(c, p).ID)
		}
		for _, idx := range cfg.legalCommandersFor(i, len(deck), deck) {
			cmdIDs[i] = append(cmdIDs[i], ids[idx])
		}
		totalCmd += len(cmdIDs[i])
		g.SetZone(state.ZLibrary, p, ids)
		if i < len(cfg.Sideboards) {
			sb := make([]state.ObjID, 0, len(cfg.Sideboards[i]))
			for _, c := range cfg.Sideboards[i] {
				o := g.AddObject(c, p)
				o.Zone = state.ZSideboard
				sb = append(sb, o.ID)
			}
			g.SetZone(state.ZSideboard, p, sb)
		}
		if i < len(cfg.PlanarDecks) {
			for _, c := range cfg.PlanarDecks[i] {
				g.AddObject(c, p)
			}
		}
	}
	for _, ev := range log {
		events.Apply(g, ev)
	}
	for p := range g.Players {
		if len(cmdIDs[p]) > 0 {
			g.Players[p].Commanders = append([]state.ObjID(nil), cmdIDs[p]...)
			g.Players[p].CmdCasts = make([]int32, len(cmdIDs[p]))
			for _, ev := range log {
				if ev.Kind != events.PutOnStack || ev.Player != state.PlayerID(p) || ev.From != state.ZCommand {
					continue
				}
				for k, id := range cmdIDs[p] {
					if id == ev.Obj {
						g.Players[p].CmdCasts[k]++
						break
					}
				}
			}
		}
		if totalCmd > 0 {
			g.Players[p].CmdDamage = make([]int32, totalCmd)
		}
	}
	return g
}

// diffGames reports every structural difference between two games, or ""
// if they match exactly. Mirrors events.TestApplyIsPure's own comparison
// shape (Players, Objs, Stack, each zone, and the turn-structure scalars)
// rather than a whole-struct reflect.DeepEqual, so a mismatch is reported
// with enough detail to diagnose.
// stripTransientAbilities removes from g the footprint a triggered ability's
// own came-and-gone stack object leaves relative to base: the transient Objs
// slot(s) appended after the base snapshot, their zone membership, and the
// NextID increment(s) they consumed. It is the honest whole-game diff for CR
// 603.5, where a declined optional trigger DID go on the stack and then
// depart to exile having done nothing -- so the diff cannot be literally
// empty, but everything OTHER than the ability's own footprint must be
// byte-identical.
func stripTransientAbilities(base, g *state.Game) {
	if len(g.Objs) <= len(base.Objs) {
		return
	}
	// The transient objects were appended after the base snapshot's last one
	// (nothing else is created between the two snapshots on the paths that
	// use this helper). Record each one's id and current zone/owner, drop the
	// zone membership, then trim the slots and restore NextID so diffGames
	// sees only the durable game.
	type transient struct {
		id    state.ObjID
		zone  state.Zone
		owner state.PlayerID
	}
	var ts []transient
	for i := len(base.Objs); i < len(g.Objs); i++ {
		o := &g.Objs[i]
		ts = append(ts, transient{id: o.ID, zone: o.Zone, owner: o.Controller})
	}
	for _, t := range ts {
		if t.zone == state.ZStack {
			kept := g.Stack[:0]
			for _, id := range g.Stack {
				if id != t.id {
					kept = append(kept, id)
				}
			}
			g.Stack = kept
			continue
		}
		z := g.Zone(t.zone, t.owner)
		kept := z[:0]
		for _, id := range z {
			if id != t.id {
				kept = append(kept, id)
			}
		}
		g.SetZone(t.zone, t.owner, kept)
	}
	g.Objs = g.Objs[:len(base.Objs)]
	g.NextID = base.NextID
}

func diffGames(a, b *state.Game) string {
	var diffs []string
	if !reflect.DeepEqual(a.Players, b.Players) {
		diffs = append(diffs, fmt.Sprintf("players: %+v vs %+v", a.Players, b.Players))
	}
	n := len(a.Objs)
	if len(b.Objs) > n {
		n = len(b.Objs)
	}
	for i := 0; i < n; i++ {
		var ao, bo any
		if i < len(a.Objs) {
			ao = a.Objs[i]
		}
		if i < len(b.Objs) {
			bo = b.Objs[i]
		}
		if !reflect.DeepEqual(ao, bo) {
			diffs = append(diffs, fmt.Sprintf("obj[%d]: %+v vs %+v", i+1, ao, bo))
		}
	}
	// Stack and per-zone ID lists use slices.Equal, not reflect.DeepEqual.
	// events.remove (events/apply.go) always allocates via make, even for a
	// zero-length result, so a zone that just lost its last object holds a
	// non-nil empty slice; state.Game.Clone's append-based copy normalises
	// that same empty zone to nil. slices.Equal compares length and elements
	// only, so it correctly treats nil and empty as the same zone; DeepEqual
	// does not.
	if !slices.Equal(a.Stack, b.Stack) {
		diffs = append(diffs, fmt.Sprintf("stack: %v vs %v", a.Stack, b.Stack))
	}
	for p := state.PlayerID(0); int(p) < len(a.Players); p++ {
		for _, z := range []state.Zone{state.ZLibrary, state.ZHand, state.ZBattlefield, state.ZGraveyard, state.ZExile} {
			az, bz := a.Zone(z, p), b.Zone(z, p)
			if !slices.Equal(az, bz) {
				diffs = append(diffs, fmt.Sprintf("p%d:%s: %v vs %v", p, z, az, bz))
			}
		}
	}
	// Ruling F6 (Task 27 fix round 1): Winner, Draw and NextID are compared
	// too. They were missing, so "a whole-game diff of empty" claimed more
	// than this helper delivered -- most sharply for Draw, which Task 22
	// added precisely because Winner's zero value is a real seat, so a game
	// that ended in a draw compared equal to one seat 0 won. Task 24 is the
	// replay task and inherits this harness; a replay comparison that cannot
	// see who won is a bad foundation to hand it.
	if a.Turn != b.Turn || a.Active != b.Active || a.Clock != b.Clock ||
		a.Priority != b.Priority || a.Passes != b.Passes || a.Step != b.Step ||
		a.Over != b.Over || a.Winner != b.Winner || a.Draw != b.Draw || a.NextID != b.NextID {
		diffs = append(diffs, fmt.Sprintf(
			"scalars: turn=%d/%d active=%d/%d clock=%d/%d priority=%d/%d passes=%d/%d step=%s/%s over=%v/%v winner=%d/%d draw=%v/%v nextid=%d/%d",
			a.Turn, b.Turn, a.Active, b.Active, a.Clock, b.Clock,
			a.Priority, b.Priority, a.Passes, b.Passes, a.Step, b.Step, a.Over, b.Over,
			a.Winner, b.Winner, a.Draw, b.Draw, a.NextID, b.NextID))
	}
	return strings.Join(diffs, "\n")
}
