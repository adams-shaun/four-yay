// K:Storied (CR 702.175, task storied1): the enduring-story latch and the
// statics gated on Condition$ EnduringStory, pinned on real corpus carriers.
//
// CR 702.175a makes "you have an enduring story for the rest of the game" a
// one-way designation (702.175b), exactly like Ascend's city's blessing: the
// threshold is checked once, and losing the permanents (or the Storied
// permanent itself) never revokes it. rules/storied.go's
// checkEnduringStoryGrants emits the latch from the same post-fold emit hook
// checkBlessingGrants uses; rules/layers.go's continuousConditionHolds reads
// it from the EnduringStory arm.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// qualifyingArtifact is a plain artifact permanent that adds one to the CR
// 702.175a count without carrying Storied itself.
const qualifyingArtifact = "Name:Story Artifact\nManaCost:0\nTypes:Artifact\nOracle:x\n"

// triggerStoriedGrant moves one permanent off seat 0's battlefield and back
// through real MoveZone events, so the engine's own post-fold hook runs
// checkEnduringStoryGrants against the folded board. onBoard/onBoardCard
// place permanents eventlessly, which is deliberate (they must not fire
// entry triggers); a fixture that needs the GRANT must cross an event that
// the hook filters, exactly as a live entry does.
func triggerStoriedGrant(t *testing.T, e *Engine, p state.PlayerID, id state.ObjID) {
	t.Helper()
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZGraveyard})
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZGraveyard, To: state.ZBattlefield})
}

// TestDainEnduringStoryPricesTheAttackTax pins CR 702.175a end to end on the
// real carrier: Dáin, Lord of the Iron Hills'
// `S:Mode$ CantAttackUnless | ValidCard$ Creature | Target$ You | Cost$ 1 |
// Condition$ EnduringStory`. With the enduring story held, an attack on
// Dáin's controller is priced {1} per attacker and the pool pays it; with no
// enduring story the same attack is free (no charge, no price label). Before
// the Storied work the gate failed closed, so the tax silently never
// appeared -- the reported symptom.
func TestDainEnduringStoryPricesTheAttackTax(t *testing.T) {
	t.Parallel()
	e, bear := attackPropSeat(t, "Dáin, Lord of the Iron Hills", 0)

	// Dáin himself is a legendary, so two artifacts complete CR 702.175a's
	// three. Both are placed eventlessly; the latch is then granted through
	// a real battlefield entry (the toggle below).
	first := onBoard(t, e, 0, qualifyingArtifact)
	onBoard(t, e, 0, qualifyingArtifact)
	if got := e.G.Players[0].EnduringStory; got {
		t.Fatal("precondition: enduring story set before the granting event ran")
	}
	triggerStoriedGrant(t, e, 0, first)
	if !e.G.Players[0].EnduringStory {
		t.Fatal("three qualifying permanents including real Storied Dáin did not grant the enduring story")
	}
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatal("precondition: attacker fixture is not on the battlefield")
	}

	// With the pool empty the pair must NOT be offered (the {1} is unpaid).
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected an attackers decision, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Player == 0 {
			t.Fatalf("charged pair offered with an empty pool: %+v", o)
		}
	}

	// One floating mana: the pair is offered at exactly {1} and paying it
	// empties the pool.
	floatMana(t, e, 1, "R")
	e.askAttackers()
	d = e.Pending()
	var opt *decision.Option
	for i := range d.Options {
		o := &d.Options[i]
		if o.Player == 0 && o.Obj == bear {
			opt = o
		}
	}
	if opt == nil {
		t.Fatalf("pair not offered although one mana can pay {1}: %+v", d.Options)
	}
	if opt.Label != "Attack with Runeclaw Bear at a (pay {1} per creature)" {
		t.Fatalf("Dáin attack price label %q, want the {1} charge", opt.Label)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("submit charged attack: %v", err)
	}
	if got := e.G.Players[1].Pool.Total(); got != 0 {
		t.Fatalf("pool after paying the {1} tax = %d, want 0", got)
	}
	if o := e.G.Obj(bear); !o.IsAttacking {
		t.Fatal("taxed attacker never declared")
	}
	drainCombatPriority(t, e)
}

// TestDainAttackIsFreeWithoutEnduringStory is the false side of the same
// switch: a board that never reaches CR 702.175a's three (Dáin plus one
// artifact only) leaves the gate false, so the attack is unpriced and free --
// and, crucially, is still OFFERED, proving the tax is absent rather than the
// attack being wrongly blocked.
func TestDainAttackIsFreeWithoutEnduringStory(t *testing.T) {
	t.Parallel()
	e, bear := attackPropSeat(t, "Dáin, Lord of the Iron Hills", 0)
	onBoard(t, e, 0, qualifyingArtifact) // two qualifying permanents, one short.
	if e.G.Players[0].EnduringStory {
		t.Fatal("precondition: enduring story set below the three-permanent threshold")
	}

	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected an attackers decision, got %+v", d)
	}
	var opt *decision.Option
	for i := range d.Options {
		o := &d.Options[i]
		if o.Player == 0 && o.Obj == bear {
			opt = o
		}
	}
	if opt == nil {
		t.Fatalf("free pair not offered: %+v", d.Options)
	}
	if opt.Label != "Attack with Runeclaw Bear at a" {
		t.Fatalf("attack priced although the enduring story is absent: label %q", opt.Label)
	}
}

// TestFiliContinuousPumpNeedsEnduringStory pins the S:Mode$ Continuous arm:
// Fíli the Pathfinder's `Affected$ Creature.YouCtrl | AddPower$ 1 |
// AddToughness$ 1 | Condition$ EnduringStory`. The gate holds only once the
// seat has the designation; before the granting event the pump is off.
func TestFiliContinuousPumpNeedsEnduringStory(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	// Fíli is Storied AND legendary, so it counts toward CR 702.175a itself.
	fili := onBoardCard(t, e, 0, mshCorpusCard(t, "Fíli the Pathfinder"))
	bear := onBoard(t, e, 0, "Name:Runeclaw Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	first := onBoard(t, e, 0, qualifyingArtifact)
	onBoard(t, e, 0, qualifyingArtifact)

	if got := e.Power(bear); got != 2 {
		t.Fatalf("precondition: Fíli's +1/+1 applied before the designation (power %d)", got)
	}
	if !e.HasKeyword(fili, "Storied") {
		t.Fatal("precondition: corpus Fíli does not carry K:Storied")
	}
	triggerStoriedGrant(t, e, 0, first)
	if !e.G.Players[0].EnduringStory {
		t.Fatal("Fíli plus two artifacts did not grant the enduring story")
	}
	if got := e.Power(bear); got != 3 {
		t.Fatalf("Fíli's conditional +1/+1 absent with the designation (power %d, want 3)", got)
	}
	if got := e.Toughness(bear); got != 3 {
		t.Fatalf("Fíli's conditional +1/+1 absent with the designation (toughness %d, want 3)", got)
	}
}

// TestEnduringStoryLatchSurvivesThePermanents pins CR 702.175b's one-way
// property and that it replays exactly: once granted, removing every
// qualifying permanent -- including the Storied permanent itself -- leaves
// the designation set, and the whole event stream round-trips.
//
// Unlike the price tests this fixture is built entirely through logged
// MoveZone events (newFixtureDeck + battlefield entries from hand), because
// replayCheck rebuilds the game from the log alone: an eventless onBoard
// placement is invisible to that rebuild.
func TestEnduringStoryLatchSurvivesThePermanents(t *testing.T) {
	t.Parallel()
	e, cfg, fili := newFixtureDeck(t, 719, "Name:Fili Probe\nManaCost:3 W\nTypes:Legendary Creature Dwarf Scout\nPT:2/2\n"+
		"K:Storied\nOracle:x\n", qualifyingArtifact, qualifyingArtifact)
	// The fixture card is bridged to hand; the two artifacts start in the
	// library. Bring all three onto the battlefield through MoveZone events.
	artifacts := 0
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		if artifacts == 2 {
			break
		}
		if o := e.G.Obj(id); o != nil && o.Face().Name == "Story Artifact" {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
			artifacts++
		}
	}
	if artifacts != 2 {
		t.Fatalf("precondition: found %d of the two fixture artifacts in the library", artifacts)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: fili, From: state.ZHand, To: state.ZBattlefield})
	if len(e.G.Zone(state.ZBattlefield, 0)) != 3 {
		t.Fatalf("precondition: %d permanents on the battlefield, want the three the threshold needs",
			len(e.G.Zone(state.ZBattlefield, 0)))
	}
	if !e.G.Players[0].EnduringStory {
		t.Fatal("precondition: designation not granted, latch not exercised")
	}

	// Remove every permanent seat 0 controls (the Storied Fíli included)
	// through real MoveZone events.
	for _, id := range append([]state.ObjID(nil), e.G.Zone(state.ZBattlefield, 0)...) {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZGraveyard})
	}
	if len(e.G.Zone(state.ZBattlefield, 0)) != 0 {
		t.Fatal("precondition: qualifying permanents still on the battlefield")
	}
	if !e.G.Players[0].EnduringStory {
		t.Fatal("enduring story revoked when the permanents left (CR 702.175b forbids this)")
	}
	replayCheck(t, e, cfg)
}
