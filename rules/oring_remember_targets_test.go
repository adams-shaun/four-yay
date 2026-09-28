package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Behaviour leaves for the remember-targets/delayed-trigger census params:
// ChangeZone's RememberTargets$/ForgetOtherTargets$ (the O-Ring shape,
// Journey to Nowhere / Leonin Relic-Warder), DelayedTrigger's
// RememberObjects$ and ValidPlayer$ and ChangeZone's ExileFaceDown$
// (Necropotence). Every protagonist here is a REAL compiled corpus card --
// the O-Ring return is the canonical shape the corpus spells, so the pins
// are end-to-end on the real scripts, driven through the ordinary
// trigger/stack/priority machinery (delayed_test.go's harness style).

// acceptOptionalTrigger answers a pending trigger_optional ask with Yes (the
// OptionalDecider$ You contract's accepting half), fatal when nothing
// optional is pending.
func acceptOptionalTrigger(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTriggerOptional || len(d.Options) == 0 || d.Options[0].Kind != "yes" {
		t.Fatalf("expected an optional-trigger ask, got %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)
}

// moveCorpusCard moves a real card out of seat p's hand/library into to and
// returns its id -- moveSeeded's shape for a corpus card, which has no
// script text to name it by.
func moveCorpusCard(t *testing.T, e *Engine, name string, p state.PlayerID, to state.Zone) state.ObjID {
	t.Helper()
	toMain1(t, e)
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, p) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: to})
				e.pending = nil
				e.Advance() // drain any ETB trigger the entry just fired
				return id
			}
		}
	}
	t.Fatalf("corpus card %q not found in seat %d's library or hand", name, p)
	return 0
}

// oringConfig builds a 2-seat game: seat 0's deck leads with the corpus
// protagonist, seat 1's with the named target cards (the exile fodder), both
// padded with Mountains. The toss is advanced until it starts seat 0, like
// newFixtureDeck.
func oringConfig(t *testing.T, seed uint64, protagonist string, targets ...string) (*Engine, Config) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	prot := mustCorpusCard(t, reg, protagonist)
	deck1 := make([]*cards.Card, 0, len(targets))
	for _, name := range targets {
		deck1 = append(deck1, mustCorpusCard(t, reg, name))
	}
	build := func(s uint64) Config {
		return Config{Seed: s, Names: []string{"a", "b"},
			Decks: [][]*cards.Card{
				append([]*cards.Card{prot}, mountainDeck(t, 39)...),
				append(deck1, mountainDeck(t, 40-len(deck1))...),
			}}
	}
	cfg := seatZeroStart(build(seed))
	e := New(cfg)
	e.Advance()
	return e, cfg
}

// TestJourneyToNowhereReturnsTheExiledCreature is the O-Ring canonical
// shape, end to end on the real corpus script: the ETB trigger's
// RememberTargets$ True remembers the exiled creature on the source's
// persistent list, and the leave-battlefield trigger's `Defined$ Remembered`
// -- which reads the CARD's list, not this build's event capture -- returns
// it under its owner's control. A second enter/leave pair exercises
// ForgetOtherTargets$ True.
func TestJourneyToNowhereReturnsTheExiledCreature(t *testing.T) {
	t.Parallel()
	e, cfg := oringConfig(t, 63, "Journey to Nowhere", "Hill Giant", "Grizzly Bears")
	giant := moveCorpusCard(t, e, "Hill Giant", 1, state.ZBattlefield)
	bears := moveCorpusCard(t, e, "Grizzly Bears", 1, state.ZBattlefield)
	journey := moveCorpusCard(t, e, "Journey to Nowhere", 0, state.ZBattlefield)

	// The ETB trigger asks for the exile target; exile the Giant.
	passUntilKind(t, e, decision.KTarget, 40)
	submitTarget(t, e, giant)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(giant); o == nil || o.Zone != state.ZExile {
		t.Fatalf("giant zone = %v, want exile after the ETB resolved", zoneName(o))
	}
	jObj := e.G.Obj(journey)
	if jObj == nil || len(jObj.Remembered) != 1 || jObj.Remembered[0].Obj != giant {
		t.Fatalf("journey's persistent Remembered = %v, want exactly the exiled giant", jObj.Remembered)
	}

	// Journey leaves: the return trigger returns the Giant under its
	// owner's control (seat 1 -- the deck it was dealt from).
	e.emit(events.Event{Kind: events.MoveZone, Obj: journey,
		From: state.ZBattlefield, To: state.ZGraveyard})
	e.pending = nil
	e.Advance()
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(giant); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("giant zone=%v controller=%d, want battlefield under its owner (seat 1)",
			zoneName(o), o.Controller)
	}

	// Re-entering and exiling a SECOND creature exercises ForgetOtherTargets$
	// True: the card's list must hold exactly the new exile, not both.
	e.emit(events.Event{Kind: events.MoveZone, Obj: journey,
		From: state.ZGraveyard, To: state.ZBattlefield})
	e.pending = nil
	e.Advance()
	passUntilKind(t, e, decision.KTarget, 40)
	submitTarget(t, e, bears)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(bears); o == nil || o.Zone != state.ZExile {
		t.Fatalf("bears zone = %v, want exile after the second ETB resolved", zoneName(o))
	}
	jObj = e.G.Obj(journey)
	if jObj == nil || len(jObj.Remembered) != 1 || jObj.Remembered[0].Obj != bears {
		t.Fatalf("after ForgetOtherTargets the card's Remembered = %v, want exactly the bears", jObj.Remembered)
	}

	// And leaving again returns the bears, not the (battlefield) giant.
	e.emit(events.Event{Kind: events.MoveZone, Obj: journey,
		From: state.ZBattlefield, To: state.ZGraveyard})
	e.pending = nil
	e.Advance()
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(bears); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("bears zone = %v, want battlefield after the second return", zoneName(o))
	}
	if o := e.G.Obj(giant); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the first exile's giant must not move on the second return: %v", zoneName(o))
	}
	replayCheck(t, e, cfg)
}

// TestLeoninRelicWarderReturnsTheExiledArtifact drives the same O-Ring shape
// on the real Relic-Warder script -- a narrower target filter
// (Artifact,Enchantment) and an optional trigger aside, the remember/return
// contract is the one Journey pins.
func TestLeoninRelicWarderReturnsTheExiledArtifact(t *testing.T) {
	t.Parallel()
	e, cfg := oringConfig(t, 64, "Leonin Relic-Warder", "Sol Ring")
	ring := moveCorpusCard(t, e, "Sol Ring", 1, state.ZBattlefield)
	warder := moveCorpusCard(t, e, "Leonin Relic-Warder", 0, state.ZBattlefield)

	// The ETB trigger is OPTIONAL (OptionalDecider$ You). The engine asks
	// the target first, then -- after the trigger reaches the stack -- the
	// apply? election: answer target, then Yes.
	passUntilKind(t, e, decision.KTarget, 40)
	submitTarget(t, e, ring)
	passUntilKind(t, e, decision.KTriggerOptional, 40)
	acceptOptionalTrigger(t, e)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(ring); o == nil || o.Zone != state.ZExile {
		t.Fatalf("Sol Ring zone = %v, want exile", zoneName(o))
	}

	// The Warder leaves: the artifact returns under its owner's control.
	e.emit(events.Event{Kind: events.MoveZone, Obj: warder,
		From: state.ZBattlefield, To: state.ZGraveyard})
	e.pending = nil
	e.Advance()
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(ring); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("Sol Ring zone=%v controller=%d, want battlefield under its owner",
			zoneName(o), o.Controller)
	}
	replayCheck(t, e, cfg)
}

// TestFlickerwispBlinkRemembersTheObject is the blink-remembers pattern on
// the REAL corpus script: the ETB exile's RememberChanged$ captures the
// permanent, DelayedTrigger's RememberObjects$ RememberedLKI is the read the
// census names, and the end-step delayed trigger returns the exiled
// permanent.
func TestFlickerwispBlinkRemembersTheObject(t *testing.T) {
	t.Parallel()
	e, cfg := oringConfig(t, 65, "Flickerwisp", "Hill Giant")
	giant := moveCorpusCard(t, e, "Hill Giant", 1, state.ZBattlefield)
	moveCorpusCard(t, e, "Flickerwisp", 0, state.ZBattlefield)
	passUntilKind(t, e, decision.KTarget, 40)
	submitTarget(t, e, giant)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(giant); o == nil || o.Zone != state.ZExile {
		t.Fatalf("giant zone = %v, want exile after the ETB resolved", zoneName(o))
	}
	if len(e.G.Delayed) != 1 {
		t.Fatalf("expected one delayed registration, got %d", len(e.G.Delayed))
	}
	// The registration captures the resolving chain's Remembered, which also
	// holds the trigger's own self-capture (Flickerwisp entered the
	// battlefield); the bounce's Origin$ Exile precondition skips that
	// entry, so the contract is that the exiled giant IS remembered.
	hasGiant := false
	for _, tgt := range e.G.Delayed[0].Remembered {
		if tgt.Obj == giant {
			hasGiant = true
		}
	}
	if !hasGiant {
		t.Fatalf("registration Remembered = %v, want it to hold the exiled giant", e.G.Delayed[0].Remembered)
	}
	driveToEndStep(t, e)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(giant); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("giant zone = %v, want battlefield after the end-step return", zoneName(o))
	}
	if len(e.G.Delayed) != 0 {
		t.Fatalf("registration should be consumed by firing, got %d", len(e.G.Delayed))
	}
	replayCheck(t, e, cfg)
}

// TestNecropotenceExilesFaceDownAndReturnsNextEndStep is Necropotence's two
// census reads end to end: the activated ability exiles the top card FACE
// DOWN (ExileFaceDown$ True -- state the view redacts to everyone but the
// exiling controller), and the delayed trigger carries ValidPlayer$ You, so
// it does NOT fire at an opponent's end step -- it fires at the controller's
// NEXT end step and puts the card into its controller's hand.
func TestNecropotenceExilesFaceDownAndReturnsNextEndStep(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	necro := mustCorpusCard(t, reg, "Necropotence")
	build := func(s uint64) Config {
		return Config{Seed: s, Names: []string{"a", "b"},
			Decks: [][]*cards.Card{
				append([]*cards.Card{necro}, mountainDeck(t, 39)...),
				mountainDeck(t, 40),
			}}
	}
	cfg := seatZeroStart(build(66))
	e := New(cfg)
	e.Advance()
	n := moveCorpusCard(t, e, "Necropotence", 0, state.ZBattlefield)

	// Activate during seat 1's turn (seat 1's first turn is turn 2 -- the
	// TurnChange counter advances every hand-off): drive there, wait for seat
	// 0's priority (the decision's Player names the decider), and pay the
	// life.
	driveToStepAll(t, e, 2, 1, state.StepMain1)
	passUntilKind(t, e, decision.KPriority, 40)
	for i := 0; i < 20; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision pending")
		}
		if d.Player == 0 {
			break
		}
		submitPass(t, e)
	}
	opt := abilityOption(t, e, n, 0)
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 40)

	// The top card of seat 0's library is exiled face down, and one
	// registration is pending.
	exiled := 0
	var exiledID state.ObjID
	for _, id := range e.G.Zone(state.ZExile, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name != "Necropotence" {
			exiled++
			exiledID = id
		}
	}
	if exiled != 1 || e.G.Obj(exiledID) == nil || !e.G.Obj(exiledID).FaceDown {
		t.Fatalf("want exactly one face-down seat-0 exile, got exiled=%d facedown=%v", exiled,
			e.G.Obj(exiledID) != nil && e.G.Obj(exiledID).FaceDown)
	}
	if e.G.Players[0].Life != 19 {
		t.Fatalf("life = %d, want 19 after the PayLife<1> cost", e.G.Players[0].Life)
	}
	if len(e.G.Delayed) != 1 {
		t.Fatalf("want one pending registration, got %d", len(e.G.Delayed))
	}
	if dt := e.G.Delayed[0]; len(dt.Remembered) != 1 || dt.Remembered[0].Obj != exiledID {
		t.Fatalf("registration Remembered = %v, want exactly the exiled card", dt.Remembered)
	}

	// Seat 1's end step must NOT consume the registration: the trigger's
	// ValidPlayer$ You fails there, and it stays pending for the controller's
	// own next end step.
	driveToStepAll(t, e, 2, 1, state.StepEnd)
	if o := e.G.Obj(exiledID); o == nil || o.Zone != state.ZExile || !o.FaceDown {
		t.Fatalf("seat 1's end step must not fire the ValidPlayer$ You trigger; card %v", o)
	}
	pushes := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.DelayedPush {
			pushes++
		}
	}
	if pushes != 0 {
		t.Fatalf("no DelayedPush may fire at an opponent's end step, got %d", pushes)
	}

	// Seat 0's next end step fires it: the card goes to its controller's hand
	// and is face up again there.
	driveToStepAll(t, e, 3, 0, state.StepEnd)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(exiledID); o == nil || o.Zone != state.ZHand || o.FaceDown {
		t.Fatalf("exiled card zone=%v facedown=%v, want the controller's hand face up",
			zoneName(o), o.FaceDown)
	}
	replayCheck(t, e, cfg)
}
