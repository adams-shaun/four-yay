// Ticket agent-20260923T065617Z-9b7a6efa: the CR 704.5k world rule. If two or
// more permanents have the supertype world, all except the one that has had
// the world supertype for the shortest amount of time are put into their
// owners' graveyards; on a tie for the shortest amount of time, all of them
// are. The rule is GLOBAL (every world permanent, whatever its controller)
// and DETERMINISTIC: unlike the CR 704.5j legend rule it poses no choice, so
// it is an automatic SBA (rules/sba.go worldRule) and the survivor is the most
// recently entered (largest Object.Timestamp) permanent.
//
// The first version of this ticket implemented the rule as a per-controller
// controller choice -- the legend rule's twin -- which the CR text at
// .superpowers/research/cr/MagicCompRules-20260807.txt:10492-10495 flatly
// contradicts. These tests pin the CR text.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// worldEnchantment is a plain World enchantment source. World is a supertype
// read from the DERIVED type list, exactly as the legend rule reads Legendary.
func worldEnchantment(name string) string {
	return "Name:" + name + "\nManaCost:2 B\nTypes:World Enchantment\nOracle:x\n"
}

// moveWorldSeeded fields one World enchantment for seat p and returns its id.
func moveWorldSeeded(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	src := worldEnchantment(name)
	id := moveSeeded(t, e, p, src, state.ZBattlefield)
	o := e.G.Obj(id)
	// The precondition the world rule reads: the object is on the
	// battlefield with the World supertype in its derived list. A vacuous
	// setup must fail loudly, not pass silently.
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("fixture: world permanent %d not on the battlefield (zone %v)", id, o)
	}
	if !worldUnderLayers(e, id) {
		t.Fatalf("fixture: %s is not a World permanent in its derived types; CR 704.5k cannot fire", o.Face().Name)
	}
	return id
}

// worldMoveText returns the Text of the object's latest battlefield->graveyard
// MoveZone, and whether it has one. Unlike the legend helper it does not fail
// on absence, so a "nothing happened" assertion can use it.
func worldMoveText(e *Engine, id state.ObjID) (string, bool) {
	for i := len(e.L.Events) - 1; i >= 0; i-- {
		ev := e.L.Events[i]
		if ev.Kind == events.MoveZone && ev.Obj == id &&
			ev.From == state.ZBattlefield && ev.To == state.ZGraveyard {
			return ev.Text, true
		}
	}
	return "", false
}

// worldBatchWitness is a World enchantment whose leaves-the-battlefield
// trigger watches EVERY World permanent departure, so the world-rule batch
// (rules/sba.go worldRule, CR 704.5k) must give both tied departing sources
// the same pre-departure board -- 4 queued triggers (each source observing
// both departures), not the 3 a missing snapshot produces. Its twin is
// walkerBatchWitness in rules/sba_batch_test.go for the zero-loyalty batch.
const worldBatchWitness = "Name:Batch world\nManaCost:2 B\nTypes:World Enchantment\n" +
	"T:Mode$ ChangesZone | Origin$ Battlefield | Destination$ Graveyard | ValidCard$ Enchantment | Execute$ Gain\n" +
	"SVar:Gain:DB$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"

// TestWorldRuleTieSendsAllUsesPreDepartureBoard pins CR 704.3/603.10a for the
// world rule's simultaneous departure batch: two tied World permanents both
// depart, and each departing source's leaves-the-battlefield trigger must
// observe BOTH departures off the same pre-departure board. Without the
// trigger-board snapshot around the loop, the source moved first is already
// gone when the second move is matched, so only 3 triggers queue. The test
// asserts the tie precondition (equal Timestamps) and the World supertype so
// a vacuous setup fails loudly.
func TestWorldRuleTieSendsAllUsesPreDepartureBoard(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	a := onBoard(t, e, 0, worldBatchWitness)
	b := onBoard(t, e, 0, worldBatchWitness)
	// Precondition: both are World permanents on the battlefield, and their
	// timestamps tie (so both depart -- a unique-newest survivor would leave
	// only one source to observe, defeating the pin).
	for _, id := range []state.ObjID{a, b} {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("fixture: world batch member %d not on the battlefield (zone %v)", id, o)
		}
		if !o.Face().IsWorld() {
			t.Fatalf("fixture: %q printed face is not World", o.Face().Name)
		}
	}
	stamp := e.G.Obj(a).Timestamp
	e.G.Obj(b).Timestamp = stamp
	if e.G.Obj(a).Timestamp != e.G.Obj(b).Timestamp {
		t.Fatalf("fixture: timestamps %d/%d differ; the tie is not exercised",
			e.G.Obj(a).Timestamp, e.G.Obj(b).Timestamp)
	}
	e.checkStateBased()
	for _, id := range []state.ObjID{a, b} {
		if o := e.G.Obj(id); o.Zone != state.ZGraveyard {
			t.Fatalf("tied world permanent %d stayed in %v; a tie sends ALL of them", id, o.Zone)
		}
	}
	// Each of the two sources observes both departures: 4 queued triggers.
	if len(e.pendingTriggers) != 4 {
		t.Fatalf("queued %d triggers, want 4 (both sources observe both departures)", len(e.pendingTriggers))
	}
	seen := map[[2]state.ObjID]bool{}
	for _, pt := range e.pendingTriggers {
		lki := pt.Ctx.LKI
		if lki == nil || lki.Zone != state.ZBattlefield {
			t.Fatalf("missing pre-batch LKI: %+v", lki)
		}
		key := [2]state.ObjID{pt.Source, lki.ID}
		if seen[key] {
			t.Fatalf("duplicate observation: %v", key)
		}
		seen[key] = true
	}
	if e.triggerBefore != nil {
		t.Fatal("look-back leaked beyond batch")
	}
}

// TestWorldRuleNewestSurvives pins CR 704.5k's deterministic core: with two
// world permanents, the one that has had the supertype for the shortest amount
// of time (the most recently entered, largest Object.Timestamp) survives and
// every other is put into its owner's graveyard with Text "world rule". No
// decision is posed -- the rule is not a choice.
//
// It fails with the fix reverted: nothing is binned and both stay forever.
func TestWorldRuleNewestSurvives(t *testing.T) {
	t.Parallel()
	e, _, _ := newFixtureDeck(t, 93, worldEnchantment("Nether Void"), worldEnchantment("Nether Void"))
	older := moveWorldSeeded(t, e, 0, "Nether Void")
	younger := moveWorldSeeded(t, e, 0, "Nether Void")
	// Precondition: the two really are ordered by entry -- without a strict
	// ordering the "newest survives" assertion is vacuous.
	if e.G.Obj(older).Timestamp >= e.G.Obj(younger).Timestamp {
		t.Fatalf("fixture: older Timestamp %d not < younger Timestamp %d",
			e.G.Obj(older).Timestamp, e.G.Obj(younger).Timestamp)
	}
	e.checkStateBased()
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		t.Fatalf("the world rule posed a choice %+v; CR 704.5k is deterministic", d)
	}
	if o := e.G.Obj(younger); o.Zone != state.ZBattlefield {
		t.Fatalf("the newest world permanent left the battlefield (zone %v); it alone must survive", o.Zone)
	}
	if o := e.G.Obj(older); o.Zone != state.ZGraveyard {
		t.Fatalf("the older world permanent stayed in %v; CR 704.5k puts it into its owner's graveyard", o.Zone)
	}
	if text, ok := worldMoveText(e, older); !ok || text != "world rule" {
		t.Fatalf("older world permanent departed with Text %q (found %v), want \"world rule\"", text, ok)
	}
}

// TestWorldRuleGlobalAcrossControllers pins CR 704.5k's global scope: the rule
// counts every world permanent on the battlefield, not one controller's. Seat
// 0's older world permanent is put into its graveyard because seat 1's is
// newer -- the case the false per-controller implementation left untouched.
func TestWorldRuleGlobalAcrossControllers(t *testing.T) {
	t.Parallel()
	// Seat 0's deck: the fixture named Nether Void; seat 1's deck: Nether
	// Void. Both are fielded and the rule applies across them.
	e, _, _ := newFixtureDeckWithOpponentCard(t, 97,
		worldEnchantment("Nether Void"), worldEnchantment("Nether Void"), worldEnchantment("Nether Void"))
	seat0 := moveWorldSeeded(t, e, 0, "Nether Void")
	seat1 := moveWorldSeeded(t, e, 1, "Nether Void")
	// Precondition: two different controllers, and seat 1's is strictly newer.
	if e.G.Obj(seat0).Controller == e.G.Obj(seat1).Controller {
		t.Fatalf("fixture: both world permanents share a controller %d; the global assertion is vacuous", e.G.Obj(seat0).Controller)
	}
	if e.G.Obj(seat1).Timestamp <= e.G.Obj(seat0).Timestamp {
		t.Fatalf("fixture: seat 1's Timestamp %d not > seat 0's %d; seat 1 must hold the supertype more briefly",
			e.G.Obj(seat1).Timestamp, e.G.Obj(seat0).Timestamp)
	}
	e.checkStateBased()
	if o := e.G.Obj(seat1); o.Zone != state.ZBattlefield {
		t.Fatalf("the newest world permanent (seat 1) left the battlefield (zone %v); it must survive", o.Zone)
	}
	if o := e.G.Obj(seat0); o.Zone != state.ZGraveyard {
		t.Fatalf("seat 0's OLDER world permanent stayed in %v; CR 704.5k is global and bins it", o.Zone)
	}
	if text, ok := worldMoveText(e, seat0); !ok || text != "world rule" {
		t.Fatalf("cross-controller departure Text %q (found %v), want \"world rule\"", text, ok)
	}
}

// TestWorldRuleTieSendsAll pins CR 704.5k's tie clause: "In the event of a tie
// for the shortest amount of time, all are put into their owners' graveyards."
// The two world permanents are given the SAME Timestamp so neither has held the
// supertype longer, and both must depart -- including the one a newest-wins
// reading would have kept.
func TestWorldRuleTieSendsAll(t *testing.T) {
	t.Parallel()
	e, _, _ := newFixtureDeck(t, 99, worldEnchantment("The Abyss"), worldEnchantment("The Abyss"))
	a := moveWorldSeeded(t, e, 0, "The Abyss")
	b := moveWorldSeeded(t, e, 0, "The Abyss")
	// The tie: neither has held the supertype longer. Writing the timestamp
	// directly is a test-only fixture (the engine assigns it from Game.Clock
	// on entry, so two entries in one instant share it).
	stamp := e.G.Obj(a).Timestamp
	e.G.Obj(b).Timestamp = stamp
	if e.G.Obj(a).Timestamp != e.G.Obj(b).Timestamp {
		t.Fatalf("fixture: timestamps %d/%d differ; the tie is not exercised",
			e.G.Obj(a).Timestamp, e.G.Obj(b).Timestamp)
	}
	e.checkStateBased()
	for _, id := range []state.ObjID{a, b} {
		if o := e.G.Obj(id); o.Zone != state.ZGraveyard {
			t.Fatalf("tied world permanent %d stayed in %v; a tie sends ALL of them", id, o.Zone)
		}
		if text, ok := worldMoveText(e, id); !ok || text != "world rule" {
			t.Fatalf("tied world permanent %d departed with Text %q (found %v), want \"world rule\"", id, text, ok)
		}
	}
	if worlds := e.worldPermanents(); len(worlds) != 0 {
		t.Fatalf("a tie left %d world permanents on the battlefield: %v", len(worlds), worlds)
	}
}

// TestWorldRuleSinglePermanentIsUntouched asserts a lone world permanent is
// not a duplicate: nothing departs and no decision arrives. The positive
// control in the same test fields a second world permanent and asserts the
// rule then fires, so the absence assertions cannot pass with the world rule
// unregistered.
func TestWorldRuleSinglePermanentIsUntouched(t *testing.T) {
	t.Parallel()
	e, _, _ := newFixtureDeck(t, 94, worldEnchantment("Concordant Crossroads"), worldEnchantment("Concordant Crossroads"))
	only := moveWorldSeeded(t, e, 0, "Concordant Crossroads")
	// Precondition: it is the only world permanent.
	if worlds := e.worldPermanents(); len(worlds) != 1 || worlds[0] != only {
		t.Fatalf("fixture: world permanents = %v, want exactly [%d]", worlds, only)
	}
	e.checkStateBased()
	if o := e.G.Obj(only); o.Zone != state.ZBattlefield {
		t.Fatalf("a lone world permanent left the battlefield (zone %v)", o.Zone)
	}
	if d := e.Pending(); d != nil {
		t.Fatalf("a lone world permanent posed a decision: %+v", d)
	}
	// Positive control: a second world permanent makes it a duplicate and the
	// handler runs.
	second := moveWorldSeeded(t, e, 0, "Concordant Crossroads")
	e.checkStateBased()
	if o := e.G.Obj(second); o.Zone != state.ZBattlefield {
		t.Fatalf("positive control: the newer world permanent left the battlefield (zone %v)", o.Zone)
	}
	if o := e.G.Obj(only); o.Zone != state.ZGraveyard {
		t.Fatalf("positive control: the older world permanent stayed in %v; the handler did not run", o.Zone)
	}
}

// TestWorldRuleReadsDerivedSupertype pins that the world scan reads the
// DERIVED type list, not the printed face: two same-named PRINTED-non-World
// enchantments under one controller become a world duplicate set only because
// a layer-4 static (CR 613.1c AddTypes$ World) grants the supertype. The
// precondition asserts the printed faces are NOT World while the derived lists
// ARE, so a scan that read the printed face alone cannot pass.
func TestWorldRuleReadsDerivedSupertype(t *testing.T) {
	t.Parallel()
	const grant = "Name:Worldmaker\nManaCost:2 U\nTypes:Artifact\n" +
		"S:Mode$ Continuous | Affected$ Enchantment.YouCtrl | AddTypes$ World | Description$ x\nOracle:x\n"
	plain := "Name:Gravity Sphere\nManaCost:2 R\nTypes:Enchantment\nOracle:x\n"
	e, _, _ := newFixtureDeck(t, 95, grant, plain, plain)
	moveSeeded(t, e, 0, grant, state.ZBattlefield)
	id1 := moveSeeded(t, e, 0, plain, state.ZBattlefield)
	id2 := moveSeeded(t, e, 0, plain, state.ZBattlefield)
	for _, id := range []state.ObjID{id1, id2} {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("fixture: permanent %d not on the battlefield (zone %v)", id, o)
		}
		if o.Face().IsWorld() {
			t.Fatalf("fixture: printed face of %q already is World; the derived-read assertion is vacuous", o.Face().Name)
		}
		if !worldUnderLayers(e, id) {
			t.Fatalf("fixture: the layer-4 AddTypes$ World grant is not live for %d (derived %v)", id, e.Derived(id).Types)
		}
	}
	e.checkStateBased()
	if o := e.G.Obj(id2); o.Zone != state.ZBattlefield {
		t.Fatalf("the newer derived-World member left the battlefield (zone %v); it must survive", o.Zone)
	}
	if o := e.G.Obj(id1); o.Zone != state.ZGraveyard {
		t.Fatalf("the older derived-World member stayed in %v; the scan missed the granted supertype", o.Zone)
	}
	if text, ok := worldMoveText(e, id1); !ok || text != "world rule" {
		t.Fatalf("derived-World departure Text %q (found %v), want \"world rule\"", text, ok)
	}
}

// TestWorldAndLegendRulesAreOrthogonal: one legendary pair and one world pair
// in one pass. The legend rule asks its controller first; answering it lets
// the Submit tail's next SBA pass apply the world rule automatically (no
// second ask). Each rule emits its own departure Text ("legend rule" / "world
// rule") and neither is mistaken for the other.
func TestWorldAndLegendRulesAreOrthogonal(t *testing.T) {
	t.Parallel()
	legend := "Name:Legend Twin\nManaCost:2 G\nTypes:Legendary Creature Bear\nPT:5/5\nOracle:x\n"
	world := worldEnchantment("The Abyss")
	e, _, _ := newFixtureDeck(t, 96, legend, legend, world, world)
	l1 := moveSeeded(t, e, 0, legend, state.ZBattlefield)
	l2 := moveSeeded(t, e, 0, legend, state.ZBattlefield)
	w1 := moveWorldSeeded(t, e, 0, "The Abyss")
	w2 := moveWorldSeeded(t, e, 0, "The Abyss")
	// Precondition: both pairs are real duplicate sets.
	if o := e.G.Obj(l1); o.Zone != state.ZBattlefield || !o.Face().IsLegendary() {
		t.Fatalf("fixture: legendary pair member %d not a battlefield legend (zone %v)", l1, o.Zone)
	}
	if e.G.Obj(w1).Timestamp >= e.G.Obj(w2).Timestamp {
		t.Fatalf("fixture: world pair is not ordered by entry")
	}
	e.checkStateBased()
	// The legend rule is asked first; the world rule waits (the pass halts on
	// the parked legend batch before reaching the world step).
	dl := legendPending(t, e, 0, l1, l2)
	if e.legendBatch == nil {
		t.Fatalf("the legend ask arrived with no legend batch parked")
	}
	if o := e.G.Obj(w1); o.Zone != state.ZBattlefield {
		t.Fatalf("the world rule ran under the outstanding legend ask; it must wait for the answer")
	}
	submitKeep(t, e, dl, 0)
	// The legend answer's Submit tail re-scans and applies the world rule.
	if o := e.G.Obj(l1); o.Zone != state.ZBattlefield {
		t.Fatalf("kept legend in %v, want battlefield", o.Zone)
	}
	if o := e.G.Obj(l2); o.Zone != state.ZGraveyard {
		t.Fatalf("binned legend in %v, want graveyard", o.Zone)
	}
	if text, ok := worldMoveText(e, l2); !ok || text != "legend rule" {
		t.Fatalf("binned legend departed with Text %q (found %v), want \"legend rule\"", text, ok)
	}
	if o := e.G.Obj(w2); o.Zone != state.ZBattlefield {
		t.Fatalf("the newest world permanent left the battlefield (zone %v); it must survive", o.Zone)
	}
	if o := e.G.Obj(w1); o.Zone != state.ZGraveyard {
		t.Fatalf("the older world permanent stayed in %v; the world rule did not run after the legend answer", o.Zone)
	}
	if text, ok := worldMoveText(e, w1); !ok || text != "world rule" {
		t.Fatalf("world departure Text %q (found %v), want \"world rule\"", text, ok)
	}
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		t.Fatalf("a KChoose is still pending after both rules settled: %+v", d)
	}
}
