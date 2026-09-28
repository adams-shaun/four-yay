package rules

import (
	"reflect"

	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// firstAttackFixture places a corpus first-attack carrier on seat 0's
// battlefield alongside a ready tapped bear (the untap the carriers'
// TrigUntap delivers has something to untap).
func firstAttackFixture(t *testing.T, name, path string) (*Engine, state.ObjID, state.ObjID) {
	t.Helper()
	card := mshCorpusCardPath(t, name, path)
	e := combatEngine(t)
	src := onBoardCard(t, e, 0, card)
	bear := onBoardReady(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.G.Obj(bear).Tapped = true
	return e, src, bear
}

// firstAttackFire emits one DeclareAttackers naming src and asserts the
// trigger queued and resolved (the tapped bear untapped), the shape every
// carrier's printed text promises for the first attack of the turn.
func firstAttackFire(t *testing.T, e *Engine, src, bear state.ObjID) {
	t.Helper()
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{src}})
	if len(e.pendingTriggers) == 0 {
		t.Fatal("first attack did not queue the FirstAttack trigger")
	}
	e.putTriggersOnStack()
	e.resolveTop()
	if e.G.Obj(bear).Tapped {
		t.Fatal("the first attack's untap did not resolve")
	}
}

// TestAureliaFirstAttackFiresOncePerTurn pins the FirstAttack$ trigger gate
// on the real corpus card (aurelia_the_warleader): the first attack in a turn
// fires and untaps, a second attack in the SAME turn (extra-combat shape --
// extra combats share g.Turn and must not reset the count) does not fire, and
// next turn's first attack fires again. Before the gate existed the trigger
// fired on EVERY DeclareAttackers, the loop the ap1 reporter measured.
func TestAureliaFirstAttackFiresOncePerTurn(t *testing.T) {
	t.Parallel()
	e, src, bear := firstAttackFixture(t, "Aurelia, the Warleader", "a/aurelia_the_warleader.txt")
	firstAttackFire(t, e, src, bear)

	// Second attack in the same turn: silent, and the bear stays untapped.
	e.G.Obj(bear).Tapped = true
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{src}})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("second attack in the same turn queued %d triggers, want 0", len(e.pendingTriggers))
	}
	if !e.G.Obj(bear).Tapped {
		t.Fatal("the second attack untapped creatures; the trigger must be silent")
	}

	// Next turn's first attack fires again.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.G.Obj(bear).Tapped = true
	firstAttackFire(t, e, src, bear)
}

// firstAttackPending counts the queued triggers whose SA is the carriers'
// TrigUntap Execute sub (DB$ UntapAll / DB$ Untap). The queued SA is the
// EXECUTE sub, not the trigger line, so FirstAttack$ cannot be read off it --
// and Scourge's Dethrone keyword trigger (DB$ PutCounter) correctly fires on
// every attack, so the count of ALL pending triggers is not the assertion.
func firstAttackPending(e *Engine) int {
	n := 0
	for _, pt := range e.pendingTriggers {
		if pt.SA != nil && (pt.SA.API == "UntapAll" || pt.SA.API == "Untap") {
			n++
		}
	}
	return n
}

// answerTriggerOrders answers every pending trigger-order decision with the
// offered order -- no reordering is under test here (Scourge's Dethrone
// keyword trigger fires alongside its FirstAttack trigger, and a group of
// two asks).
func answerTriggerOrders(t *testing.T, e *Engine) {
	t.Helper()
	for {
		d := e.Pending()
		if d == nil || d.Kind != decision.KTriggerOrder {
			return
		}
		idx := make([]int, 0, len(d.Options))
		for _, o := range d.Options {
			idx = append(idx, o.Index)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: idx}); err != nil {
			t.Fatalf("submit trigger order: %v", err)
		}
	}
}

// TestFirstAttackFiresOncePerTurnGodoAndScourge runs the fire-once-then-silent
// table over the other three carriers, proving the gate lives on the Mode$
// Attacks matcher, not on one card's params. Scourge's unread Condition$
// AttackedPlayerWithMostLife and Fear's unread Delirium$ stay unread (out of
// scope), so the fire/silence assertions pin on the queued FirstAttack SA
// itself, not on the untap -- Fear's Untap execution needs a target ask.
func TestFirstAttackFiresOncePerTurnGodoAndScourge(t *testing.T) {
	t.Parallel()
	carriers := []struct {
		name string
		path string
	}{
		{"Godo, Bandit Warlord", "g/godo_bandit_warlord.txt"},
		{"Scourge of the Throne", "s/scourge_of_the_throne.txt"},
		{"Fear of Missing Out", "f/fear_of_missing_out.txt"},
	}
	for _, c := range carriers {
		t.Run(c.name, func(t *testing.T) {
			card := mshCorpusCardPath(t, c.name, c.path)
			var e *Engine
			var src state.ObjID
			if c.name == "Fear of Missing Out" {
				// Supply the real intervening-if's four types so this test
				// isolates the once-per-turn gate.
				e, _, src = gateFixture(t, 964, c.name,
					deliriumBearSrc, deliriumPlainsSrc, deliriumBoltSrc, deliriumOathSrc)
				e.emit(events.Event{Kind: events.MoveZone, Obj: src, From: state.ZHand, To: state.ZBattlefield})
				millExtras(t, e, "Delirium Bear", "Delirium Plains", "Delirium Bolt", "Delirium Oath")
				e.G.Step = state.StepDeclareAttackers
			} else {
				e = combatEngine(t)
				src = onBoardCard(t, e, 0, card)
			}
			e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{src}})
			if firstAttackPending(e) == 0 {
				t.Fatalf("%s: first attack did not queue the FirstAttack trigger", c.name)
			}
			e.putTriggersOnStack()
			answerTriggerOrders(t, e)
			e.resolveTop()

			e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{src}})
			if n := firstAttackPending(e); n != 0 {
				t.Fatalf("%s: second attack in the same turn queued %d FirstAttack triggers, want 0", c.name, n)
			}

			e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
			e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{src}})
			if firstAttackPending(e) == 0 {
				t.Fatalf("%s: next turn's first attack did not queue the FirstAttack trigger", c.name)
			}
		})
	}
}

// TestFirstAttackSecondAttackerInSameEventStillFires proves the gate keys on
// the MATCHED attacker's own count, not the event size: a fresh two-attacker
// event where the source attacks alongside others still fires, because each
// attacker's count is 1 after the fold.
func TestFirstAttackSecondAttackerInSameEventStillFires(t *testing.T) {
	t.Parallel()
	e, src, bear := firstAttackFixture(t, "Aurelia, the Warleader", "a/aurelia_the_warleader.txt")
	fox := onBoardReady(t, e, 0, "Name:Fox\nManaCost:1 W\nTypes:Creature Fox\nPT:2/2\nOracle:x\n")
	e.G.Obj(bear).Tapped = true
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{src, fox}})
	if len(e.pendingTriggers) == 0 {
		t.Fatal("a two-attacker event with a first-time source did not queue the trigger")
	}
	e.putTriggersOnStack()
	e.resolveTop()
	if e.G.Obj(bear).Tapped {
		t.Fatal("the multi-attacker first attack did not untap")
	}

	// The now-twice-attacked source in a fresh event: silent. (ValidCard$
	// Creature.Self means the trigger can only ever fire for its OWN source's
	// attacks, so the per-source property is exactly this shape: the event
	// size never matters, the matched source's own count does.)
	e.G.Obj(bear).Tapped = true
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{src}})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("second attack queued %d triggers, want 0", len(e.pendingTriggers))
	}
}

// TestFirstAttackReplaysExactly replays the fire-once game through a Clone:
// the queued triggers and resolution must be byte-identical, the
// TestAttackersDeclaredReplaysExactly precedent.
func TestFirstAttackReplaysExactly(t *testing.T) {
	t.Parallel()
	card := mshCorpusCardPath(t, "Aurelia, the Warleader", "a/aurelia_the_warleader.txt")
	e := combatEngine(t)
	src := onBoardCard(t, e, 0, card)
	bear := onBoardReady(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.G.Obj(bear).Tapped = true
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{src}})
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{src}})
	clone := e.Clone()
	for _, eng := range []*Engine{e, clone} {
		eng.putTriggersOnStack()
		eng.resolveTop()
	}
	if !reflect.DeepEqual(e.L.Events, clone.L.Events) {
		t.Fatal("clone diverged across the FirstAttack resolution")
	}
	if !reflect.DeepEqual(e.G, clone.G) {
		t.Fatal("clone game state diverged across the FirstAttack resolution")
	}
}

// TestFirstAttackLoopContinuesPastANonFirstMatch pins the ValidCard match
// loop's restructure: a non-first matching attacker must NOT veto the event --
// a later matching attacker whose own count is 1 still fires. Corpus-unreachable
// today (all four carriers use ValidCard$ Creature.Self, so the source is the
// only possible match), so pinned synthetically on a non-Self spec via
// ParseBytes (no fixture file on disk).
func TestFirstAttackLoopContinuesPastANonFirstMatch(t *testing.T) {
	t.Parallel()
	c, ds := cards.ParseBytes("first_attack_observer.txt", []byte(
		"Name:FirstAttackObserver\nManaCost:2 W\nTypes:Creature Angel\nPT:4/4\nOracle:x\n"+
			"T:Mode$ Attacks | ValidCard$ Creature | TriggerZones$ Battlefield | Execute$ TrigUntap | FirstAttack$ True | TriggerDescription$ Whenever a creature attacks for the first time each turn, untap all creatures you control.\n"+
			"SVar:TrigUntap:DB$ UntapAll | ValidCards$ Creature.YouCtrl\n"))
	if len(ds) != 0 {
		t.Fatalf("parse observer: %v", ds)
	}
	if ds = c.Link(); len(ds) != 0 {
		t.Fatalf("link observer: %v", ds)
	}
	e := combatEngine(t)
	src := onBoardCard(t, e, 0, c)
	_ = src // the trigger's source is never an attacker in this fixture
	bear1 := onBoardReady(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	bear2 := onBoardReady(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	// bear1's first attack fires.
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{bear1}})
	if len(e.pendingTriggers) == 0 {
		t.Fatal("first attack did not queue the trigger")
	}
	e.putTriggersOnStack()
	e.resolveTop()

	// A fresh event naming the now-twice-attacked bear1 FIRST: its gate
	// failure must continue the loop, not veto bear2's first attack.
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{bear1, bear2}})
	if len(e.pendingTriggers) == 0 {
		t.Fatal("a non-first matching attacker vetoed the event; a later first-time attacker must still fire")
	}
}
