package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The suspected-designation suite (task alterattr1). The end-to-end pin is
// the real corpus carrier Nelly Borca, Impulsive Accuser; the status
// mechanics are unit pins on the events.AlterAttribute fold the primitive
// shares with every carrier.

// nellyEngine builds a 3-seat engine with Nelly Borca (seat 0), a bear of
// her own, and a victim bear under seat 2, all on the battlefield, ready to
// declare attackers. All three seats share one deck so each also carries the
// other seats' cards -- harmless, since only the named cards are moved.
func nellyEngine(t *testing.T) (*Engine, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	nelly, ok := reg.Lookup("Nelly Borca, Impulsive Accuser")
	if !ok {
		t.Fatal("corpus card Nelly Borca, Impulsive Accuser not found")
	}
	bear, ok := reg.Lookup("Runeclaw Bear")
	if !ok {
		t.Fatal("corpus card Runeclaw Bear not found")
	}
	deck := append(mountainDeck(t, 40), nelly, bear)
	e := New(seatZeroStart(Config{Seed: 42, Names: []string{"accuser", "bystander", "defender"},
		Decks: [][]*cards.Card{deck, deck, deck}, Tokens: reg.Tokens}))
	e.Advance()
	attacker := crAbortMove(t, e, 0, "Nelly Borca, Impulsive Accuser", state.ZBattlefield)
	own := onBoardCard(t, e, 0, card(t, "Name:Own Bear\nTypes:Creature\nPT:1/1\nOracle:synthetic\n"))
	victim := crAbortMove(t, e, 2, "Runeclaw Bear", state.ZBattlefield)
	return e, attacker, own, victim
}

// passRound answers n "pass" priority decisions in a row -- the table
// goes once around, and the last pass resolves the waiting trigger (the same
// three-pass shape the Master of Diversion pin uses at three seats). A
// non-priority decision in the window is a resolution asking something it
// must not ask here.
func passRound(t *testing.T, e *Engine, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("priority window ended after %d of %d passes", i, n)
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("want only priority decisions in the pass window, got %v at seq %d", d.Kind, d.Seq)
		}
		pass := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				pass = o.Index
			}
		}
		if pass < 0 {
			t.Fatalf("priority decision has no pass option: %+v", d)
		}
		crAbortAnswer(t, e, "pass", pass)
	}
}

// TestNellyBorcaSuspectsTargetAndGoadsAllSuspected pins the real carrier end
// to end: Nelly's attack trigger poses the suspect ask at placement
// (ValidTgts$ Creature), the answer marks the creature suspected, and the
// chained DB$ Goad (Defined$ Valid Creature.IsSuspected) goads EVERY
// suspected creature on the battlefield -- the victim here, plus a second
// creature suspected earlier, still suspected when the second attack
// resolves. Creatures nobody suspected are not goaded.
func TestNellyBorcaSuspectsTargetAndGoadsAllSuspected(t *testing.T) {
	t.Parallel()
	e, attacker, own, victim := nellyEngine(t)
	if e.G.Obj(victim).Suspected || len(e.G.Obj(victim).Goads) != 0 {
		t.Fatal("precondition: the victim must start unsuspcted and ungoaded")
	}
	if e.HasKeyword(victim, "Menace") {
		t.Fatal("precondition: the victim must not have menace before being suspected")
	}
	e.pending = nil
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	e.askAttackers()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected the attack decision, got %+v", d)
	}
	pick := -1
	for _, o := range d.Options {
		if o.Obj == attacker && o.Player == 2 {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatal("Nelly's attack against seat 2 not offered")
	}
	crAbortAnswer(t, e, "Nelly attack", pick)

	// The suspect ask: a KTarget over the battlefield's creatures.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want the suspect target ask at placement, got %+v", d)
	}
	tpick := -1
	for _, o := range d.Options {
		if o.Obj == victim {
			tpick = o.Index
		}
	}
	if tpick < 0 {
		t.Fatalf("suspect ask does not offer the victim: %+v", d.Options)
	}
	crAbortAnswer(t, e, "Nelly suspect target", tpick)
	passRound(t, e, 3)

	if !e.G.Obj(victim).Suspected {
		t.Fatal("the answered suspect ask did not mark the victim suspected")
	}
	if e.G.Obj(own).Suspected || e.G.Obj(attacker).Suspected {
		t.Fatal("a creature nobody targeted became suspected")
	}
	if e.HasKeyword(own, "Menace") {
		t.Fatal("an unsuspcted creature gained menace")
	}
	if !e.HasKeyword(victim, "Menace") {
		t.Fatal("a suspected creature does not have menace")
	}
	if len(e.G.Obj(victim).Goads) != 1 {
		t.Fatalf("suspected creature goads = %+v, want exactly one from the chained DB$ Goad", e.G.Obj(victim).Goads)
	}
	ge := e.G.Obj(victim).Goads[0]
	if ge.Player != 0 {
		t.Fatalf("goad relationship names player %d, want Nelly's controller 0", ge.Player)
	}
	if ge.Source != attacker {
		t.Fatalf("goad source = %d, want Nelly %d", ge.Source, attacker)
	}
	if len(e.G.Obj(own).Goads) != 0 {
		t.Fatal("an unsuspcted creature was goaded")
	}

	// A second attack on a later turn: the chained goad covers EVERY
	// suspected creature -- the new victim AND the one suspected above.
	e.pending = nil
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 3})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	e.askAttackers()
	d = e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected the second attack decision, got %+v", d)
	}
	pick = -1
	for _, o := range d.Options {
		if o.Obj == attacker && o.Player == 2 {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatal("second attack against seat 2 not offered")
	}
	crAbortAnswer(t, e, "Nelly second attack", pick)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("want the second suspect ask, got %+v", d)
	}
	tpick = -1
	for _, o := range d.Options {
		if o.Obj == own {
			tpick = o.Index
		}
	}
	if tpick < 0 {
		t.Fatalf("second suspect ask does not offer Own Bear: %+v", d.Options)
	}
	crAbortAnswer(t, e, "Nelly second suspect", tpick)
	passRound(t, e, 3)

	if !e.G.Obj(own).Suspected {
		t.Fatal("the second suspect ask did not mark its target")
	}
	if len(e.G.Obj(own).Goads) != 1 || len(e.G.Obj(victim).Goads) != 1 {
		t.Fatalf("second attack's goad must cover every suspected creature; own=%v victim=%v",
			e.G.Obj(own).Goads, e.G.Obj(victim).Goads)
	}
}

// TestSuspectedCreatureCannotBlock pins CR 702.157b's can't block half and
// the removal arm (Activate$ False) that lifts it. Precondition asserts make
// the before-state load-bearing: the same creature CAN block before it is
// suspected and cannot after.
func TestSuspectedCreatureCannotBlock(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	blocker := onBoardCard(t, e, 1, card(t, "Name:Victim\nTypes:Creature\nPT:2/2\nOracle:synthetic\n"))
	attacker := onBoardCard(t, e, 0, card(t, "Name:Raider\nTypes:Creature\nPT:2/2\nOracle:synthetic\n"))
	e.G.Obj(attacker).IsAttacking = true
	e.G.Obj(attacker).Attacking = 1
	if !e.canBlock(blocker, attacker) {
		t.Fatal("precondition: an ordinary creature must be able to block")
	}
	e.emit(events.Event{Kind: events.AlterAttribute, Obj: blocker, Text: "Suspected", Amount: 1})
	if !e.G.Obj(blocker).Suspected {
		t.Fatal("the AlterAttribute fold did not set the designation")
	}
	if e.canBlock(blocker, attacker) {
		t.Fatal("a suspected creature was allowed to block")
	}
	e.emit(events.Event{Kind: events.AlterAttribute, Obj: blocker, Text: "Suspected", Amount: -1})
	if e.G.Obj(blocker).Suspected {
		t.Fatal("the removal arm (Activate$ False) did not clear the designation")
	}
	if !e.canBlock(blocker, attacker) {
		t.Fatal("the cleared designation still blocks")
	}
}

// TestSuspectedEndsOnLeaveBattlefieldAndControlChange pins the two CR
// 702.157b end conditions on the folds every object crosses: leaving the
// battlefield, and another player gaining control (a same-controller
// ControlChange is no change and keeps it).
func TestSuspectedEndsOnLeaveBattlefieldAndControlChange(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	cre := onBoardCard(t, e, 0, card(t, "Name:Victim\nTypes:Creature\nPT:2/2\nOracle:synthetic\n"))
	e.emit(events.Event{Kind: events.AlterAttribute, Obj: cre, Text: "Suspected", Amount: 1})
	if !e.G.Obj(cre).Suspected {
		t.Fatal("precondition: the designation must be set")
	}
	e.emit(events.Event{Kind: events.ControlChange, Obj: cre, Player: 1})
	if e.G.Obj(cre).Suspected {
		t.Fatal("the designation survived another player gaining control")
	}
	e.emit(events.Event{Kind: events.AlterAttribute, Obj: cre, Text: "Suspected", Amount: 1})
	e.emit(events.Event{Kind: events.ControlChange, Obj: cre, Player: 1})
	if !e.G.Obj(cre).Suspected {
		t.Fatal("a same-controller ControlChange cleared the designation")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: cre, From: state.ZBattlefield,
		To: state.ZGraveyard, Player: 1})
	if e.G.Obj(cre).Suspected {
		t.Fatal("the designation survived leaving the battlefield")
	}
}

// TestGainControlAllValidSuspectedAndGoaded pins the predicate side the Hot
// Pursuit end-combat trigger rides: DB$ GainControl | AllValid$
// Creature.IsGoaded,Creature.IsSuspected takes exactly the goaded and the
// suspected creatures (the union, in the deterministic scan), and the
// control change itself clears the suspected designation (CR 702.157b).
func TestGainControlAllValidSuspectedAndGoaded(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	watcher := onBoardCard(t, e, 0, card(t, `Name:Seizure
Types:Enchantment
T:Mode$ Phase | Phase$ Upkeep | ValidPlayer$ You | Execute$ Seize
SVar:Seize:DB$ GainControl | AllValid$ Creature.IsGoaded,Creature.IsSuspected
Oracle:synthetic AllValid probe
`))
	goaded := onBoardCard(t, e, 1, card(t, "Name:Goaded\nTypes:Creature\nPT:2/2\nOracle:synthetic\n"))
	suspected := onBoardCard(t, e, 1, card(t, "Name:Suspected\nTypes:Creature\nPT:2/2\nOracle:synthetic\n"))
	untouched := onBoardCard(t, e, 1, card(t, "Name:Untouched\nTypes:Creature\nPT:2/2\nOracle:synthetic\n"))
	e.emit(events.Event{Kind: events.Goad, Obj: goaded, Player: 0})
	e.emit(events.Event{Kind: events.AlterAttribute, Obj: suspected, Text: "Suspected", Amount: 1})
	if !effects.MatchesObjectCtx(e.G, "Creature.IsGoaded", e.G.Obj(goaded), effects.SpecContext{}) {
		t.Fatal("IsGoaded predicate does not match the goaded creature")
	}
	if !effects.MatchesObjectCtx(e.G, "Creature.IsSuspected", e.G.Obj(suspected), effects.SpecContext{}) {
		t.Fatal("IsSuspected predicate does not match the suspected creature")
	}
	if effects.MatchesObjectCtx(e.G, "Creature.IsGoaded", e.G.Obj(untouched), effects.SpecContext{}) ||
		effects.MatchesObjectCtx(e.G, "Creature.IsSuspected", e.G.Obj(untouched), effects.SpecContext{}) {
		t.Fatal("a plain creature matched a goaded/suspected predicate")
	}
	if e.G.Obj(goaded).Controller != 1 || e.G.Obj(suspected).Controller != 1 {
		t.Fatal("precondition: the goaded and suspected creatures must belong to seat 1")
	}

	e.pending = nil
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("queued triggers = %d, want the watcher's upkeep trigger", len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	e.resolveTop()
	if e.G.Obj(goaded).Controller != 0 || e.G.Obj(suspected).Controller != 0 {
		t.Fatalf("AllValid$ GainControl took goaded=%d suspected=%d, want both under seat 0",
			e.G.Obj(goaded).Controller, e.G.Obj(suspected).Controller)
	}
	if e.G.Obj(untouched).Controller != 1 {
		t.Fatal("AllValid$ GainControl took a creature that was neither goaded nor suspected")
	}
	if e.G.Obj(suspected).Suspected {
		t.Fatal("the suspected designation survived the control change")
	}
	if e.G.Obj(watcher).Controller != 0 {
		t.Fatal("the resolving source itself changed controller")
	}
}

// TestAlterAttributeUnsupportedAttributeStaysLoud pins the scoping: a body
// naming an attribute the engine does not model (Commander, the corpus's
// remaining population -- Prepared, Harnessed, Suspected and Solved are
// modelled now) records
// the loud Note and moves nothing -- the Manifest/Cloak out-of-shape
// convention.
func TestAlterAttributeUnsupportedAttributeStaysLoud(t *testing.T) {
	t.Parallel()
	e := combatEngine(t)
	watcher := onBoardCard(t, e, 0, card(t, `Name:Preparer
Types:Creature
PT:1/1
T:Mode$ Phase | Phase$ Upkeep | ValidPlayer$ You | Execute$ Prep
SVar:Prep:DB$ AlterAttribute | Defined$ Self | Attributes$ Commander
Oracle:synthetic unsupported-attribute probe
`))
	e.pending = nil
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("queued triggers = %d, want the watcher's upkeep trigger", len(e.pendingTriggers))
	}
	e.putTriggersOnStack()
	e.resolveTop()
	found := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "not modelled") {
			found = true
		}
		if ev.Kind == events.AlterAttribute {
			t.Fatal("an unmodelled attribute emitted a designation event")
		}
	}
	if !found {
		t.Fatal("the unsupported attribute emitted no loud Note")
	}
	if e.G.Obj(watcher).Suspected {
		t.Fatal("the unsupported body set the suspected designation")
	}
}
