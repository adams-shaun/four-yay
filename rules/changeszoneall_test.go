package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// trig:ChangesZoneAll, the batch "whenever one or more ... enter/die" trigger
// mode, pinned end to end on its two report carriers (Tocasia's Welcome,
// Morbid Opportunist -- issues agent-20260918T213615Z-a44e8e5d and
// agent-20260918T213615Z-e9b0fa3b). Both are real corpus cards, so no Forge
// script text is committed here (the search_library_test.go convention).
//
// The engine implements the mode BATCH-OF-ONE: it evaluates per emitted
// MoveZone event, exactly like ChangesZone, and the once-per-turn behaviour
// of a mass operation comes from the cards' ActivationLimit$ 1. The cards
// here carry that limit, so the observable behaviour is exact; the recorded
// narrowing for the un-limited lines (86 of 126) lives in the report.

// zallEngine deals seat 0 a deck led by opener0, seat 1 a deck led by
// opener1 (both padded with Grizzly Bears to 40), starts the game with seat
// 0 active at main 1, and leaves the openers in their opening hands (the
// deal is in deck order).
func zallEngine(t *testing.T, reg *cards.Registry, opener0, opener1 []string) (*Engine, Config) {
	t.Helper()
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	build := func(openers []string) []*cards.Card {
		deck := make([]*cards.Card, 0, 40)
		for _, name := range openers {
			deck = append(deck, searchCorpusCard(t, reg, name))
		}
		for len(deck) < 40 {
			deck = append(deck, bear)
		}
		return deck
	}
	cfg := seatZeroStart(Config{Seed: 9107, Names: []string{"zall", "opponent"},
		Decks: [][]*cards.Card{build(opener0), build(opener1)}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// zallEnter moves the first object named name in player p's hand or library
// onto p's battlefield (the fixture ETB: a raw MoveZone emit, the way the
// planar/referents fixtures enter permanents).
func zallEnter(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, p) {
			o := e.G.Obj(id)
			if o != nil && o.Face() != nil && o.Face().Name == name {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZBattlefield})
				return id
			}
		}
	}
	t.Fatalf("fixture %q absent from player %d hand/library", name, p)
	return 0
}

// zallDie moves a battlefield object to its owner's graveyard (the fixture
// death: a raw MoveZone emit whose LKI the matcher reads, CR 603.10a).
func zallDie(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	o := e.G.Obj(id)
	if o == nil {
		t.Fatalf("object %d missing", id)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: state.ZGraveyard})
}

// zallDrain pushes every pending trigger onto the stack and resolves the
// stack empty. With a single trigger per entry (these fixtures' shape) the
// drain never asks a real decision; putTriggersOnStack may return true on a
// stale priority decision left over from the fixture setup, which is not an
// ask.
func zallDrain(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 50; i++ {
		if d := e.Pending(); d != nil && d.Kind == decision.KChoose && d.ResumeKind == "search_confirm" {
			// A pending Optional$ hidden-origin search now poses Forge's
			// confirmAction gate before the fetch list (task
			// optional-library-confirm); e.resolveTop below cannot answer it,
			// so accept -- the deterministic "do the may" answer -- and keep
			// the pre-confirmation drain behaviour.
			if len(d.Options) == 0 {
				t.Fatalf("search confirmation with no option to take: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
				t.Fatalf("submit search confirmation: %v", err)
			}
			continue
		}
		if len(e.pendingTriggers) > 0 {
			e.putTriggersOnStack()
			continue
		}
		if len(e.G.Stack) == 0 {
			return
		}
		e.resolveTop()
	}
	t.Fatalf("trigger drain did not settle: %d pending, %d on the stack", len(e.pendingTriggers), len(e.G.Stack))
}

// zallDraws counts the draw events a player has received so far.
func zallDraws(t *testing.T, e *Engine, p state.PlayerID) int {
	t.Helper()
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Draw && ev.Player == p {
			n++
		}
	}
	return n
}

// TestTocasiasWelcomeDrawsOncePerTurn is the reported card end to end:
// "Whenever one or more creatures you control with mana value 3 or less
// enter, draw a card. This ability triggers only once each turn."
//
// Pinned, in order on one turn (draw counts are DELTAS over the baseline,
// because the opening-hand deal is itself seven draw events):
//  1. a mv-4 creature entering draws nothing (the cmcLE3 half);
//  2. a mv-2 creature entering draws exactly one card;
//  3. a mass entry -- several more mv-2 creatures in the same turn -- still
//     draws exactly one (the ActivationLimit$ 1 latch; several matching
//     entries queue exactly one trigger);
//  4. an OPPONENT's creature entering draws nothing (YouCtrl).
func TestTocasiasWelcomeDrawsOncePerTurn(t *testing.T) {
	reg := searchTestRegistry(t)
	e, _ := zallEngine(t, reg,
		[]string{"Tocasia's Welcome", "Grizzly Bears", "Colossapede", "Grizzly Bears", "Grizzly Bears"},
		[]string{"Grizzly Bears", "Forest"})
	oppID := zallEnter(t, e, 0, "Tocasia's Welcome")
	if o := e.G.Obj(oppID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the Welcome did not enter: %+v", o)
	}
	base := zallDraws(t, e, 0)

	// (1) a mv-4 creature entering draws nothing.
	colossa := zallEnter(t, e, 0, "Colossapede")
	zallDrain(t, e)
	if n := zallDraws(t, e, 0) - base; n != 0 {
		t.Fatalf("mv-4 entry drew %d card(s), want 0 (cmcLE3)", n)
	}
	if got := e.G.Obj(colossa).Zone; got != state.ZBattlefield {
		t.Fatalf("Colossapede zone = %s, want battlefield", got)
	}

	// (2) a mv-2 creature entering draws exactly one.
	zallEnter(t, e, 0, "Grizzly Bears")
	zallDrain(t, e)
	if n := zallDraws(t, e, 0) - base; n != 1 {
		t.Fatalf("mv-2 entry drew %d card(s), want exactly 1", n)
	}

	// (3) the once-per-turn latch: a mass entry in the same turn queues no
	// further trigger.
	for i := 0; i < 2; i++ {
		zallEnter(t, e, 0, "Grizzly Bears")
		if len(e.pendingTriggers) != 0 {
			t.Fatalf("ActivationLimit$ 1 did not latch: %d trigger(s) queued on a later entry", len(e.pendingTriggers))
		}
		zallDrain(t, e)
	}
	if n := zallDraws(t, e, 0) - base; n != 1 {
		t.Fatalf("mass entry drew %d card(s) total, want exactly 1 (once each turn)", n)
	}

	// (4) an opponent's creature entering is not "you control".
	oppBear := zallEnter(t, e, 1, "Grizzly Bears")
	zallDrain(t, e)
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("an opponent's entry queued %d trigger(s), want 0", len(e.pendingTriggers))
	}
	if n := zallDraws(t, e, 0) - base; n != 1 {
		t.Fatalf("draws after the opponent's entry = %d, want still 1", n)
	}
	if got := e.G.Obj(oppBear).Zone; got != state.ZBattlefield {
		t.Fatalf("opponent bear zone = %s, want battlefield", got)
	}
}

// TestMorbidOpportunistDrawsOncePerTurn is the sibling carrier end to end:
// "Whenever one or more other creatures die, draw a card. This ability
// triggers only once each turn." The spec is ValidCards$ Creature.Other |
// Origin$ Battlefield | Destination$ Graveyard, where Other is Forge's
// not-the-source predicate (effects/filter.go) -- any creature other than
// the Opportunist itself, either side's.
//
// Pinned, on one turn:
//  1. a creature dying draws exactly one card;
//  2. a second creature dying the same turn draws nothing more (the
//     ActivationLimit$ 1 latch -- the mass-death shape);
//  3. a NON-creature (a land) dying neither queues nor draws;
//  4. the Opportunist ITSELF dying queues nothing (Other excludes the
//     source); and on a fresh engine, an OPPONENT's creature dying draws
//     one (Other does not exclude the other side's creatures).
func TestMorbidOpportunistDrawsOncePerTurn(t *testing.T) {
	reg := searchTestRegistry(t)
	e, _ := zallEngine(t, reg,
		[]string{"Morbid Opportunist", "Grizzly Bears", "Grizzly Bears", "Forest"},
		[]string{"Grizzly Bears", "Forest"})
	zallEnter(t, e, 0, "Morbid Opportunist")
	bear1 := zallEnter(t, e, 0, "Grizzly Bears")
	bear2 := zallEnter(t, e, 0, "Grizzly Bears")
	forest := zallEnter(t, e, 0, "Forest")
	base := zallDraws(t, e, 0)

	// (1) one creature dying draws exactly one.
	zallDie(t, e, bear1)
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("one creature dying queued %d trigger(s), want 1", len(e.pendingTriggers))
	}
	zallDrain(t, e)
	if n := zallDraws(t, e, 0) - base; n != 1 {
		t.Fatalf("a creature dying drew %d card(s), want exactly 1", n)
	}

	// (2) a second creature dying the same turn is latched off.
	zallDie(t, e, bear2)
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("ActivationLimit$ 1 did not latch: %d trigger(s) queued on the second death", len(e.pendingTriggers))
	}
	zallDrain(t, e)
	if n := zallDraws(t, e, 0) - base; n != 1 {
		t.Fatalf("draws after the second death = %d, want still 1", n)
	}

	// (3) a non-creature dying neither queues nor draws.
	zallDie(t, e, forest)
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("a land dying queued %d trigger(s), want 0", len(e.pendingTriggers))
	}
	if n := zallDraws(t, e, 0) - base; n != 1 {
		t.Fatalf("draws after the land death = %d, want still 1", n)
	}

	// (4a) the Opportunist itself dying is not "other creatures".
	opportunist := e.G.Zone(state.ZBattlefield, 0)[0]
	zallDie(t, e, opportunist)
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("the source's own death queued %d trigger(s), want 0", len(e.pendingTriggers))
	}

	// (4b) fresh engine: an OPPONENT's creature dying is "other creatures".
	e2, _ := zallEngine(t, reg,
		[]string{"Morbid Opportunist"},
		[]string{"Grizzly Bears"})
	zallEnter(t, e2, 0, "Morbid Opportunist")
	base2 := zallDraws(t, e2, 0)
	oppBear := zallEnter(t, e2, 1, "Grizzly Bears")
	zallDie(t, e2, oppBear)
	if len(e2.pendingTriggers) != 1 {
		t.Fatalf("an opponent creature dying queued %d trigger(s), want 1", len(e2.pendingTriggers))
	}
	zallDrain(t, e2)
	if n := zallDraws(t, e2, 0) - base2; n != 1 {
		t.Fatalf("an opponent creature dying drew %d card(s), want exactly 1", n)
	}
}

// TestMerryWardenOfIsengardBathesArtifactsOncePerTurn is the deck card the
// original report named (issue agent-20260918T202223Z-7054b9c3): "Whenever
// one or more artifacts you control enter, create a 1/1 white Soldier
// creature token with lifelink. This ability triggers only once each
// turn." -- ValidCards$ Artifact.YouCtrl | Destination$ Battlefield with
// ActivationLimit$ 1, one of the 40 exact lines.
//
// Pinned, in order on one turn:
//  1. TWO artifacts entering together create exactly ONE 1/1 white Soldier
//     token with lifelink (the batch semantics -- one trigger per group,
//     asserted by battlefield zone contents, not by a log string);
//  2. a second artifact batch in the SAME turn creates no further token
//     (the ActivationLimit$ 1 latch);
//  3. an OPPONENT's artifact entering creates nothing (YouCtrl).
func TestMerryWardenOfIsengardBathesArtifactsOncePerTurn(t *testing.T) {
	reg := searchTestRegistry(t)
	e, _ := zallEngine(t, reg,
		[]string{"Merry, Warden of Isengard", "Sol Ring", "Tormod's Crypt", "Mox Amber", "Forest"},
		[]string{"Sol Ring", "Grizzly Bears", "Forest"})
	zallEnter(t, e, 0, "Merry, Warden of Isengard")
	// Merry's Partner-with ETB must settle before the artifact batch. Pippin
	// is absent from this fixture, so target its controller; the no-candidate
	// search then resolves without another decision. Otherwise its independent
	// ETB trigger pollutes the batch trigger count this test is about.
	if !e.putTriggersOnStack() {
		t.Fatal("Merry Partner-with ETB did not enter the trigger drain")
	}
	targetAsk := e.Pending()
	if targetAsk == nil || targetAsk.Kind != decision.KTarget {
		t.Fatalf("Merry Partner-with ETB decision = %+v, want target player", targetAsk)
	}
	self := -1
	for _, o := range targetAsk.Options {
		if o.Kind == "player" && o.Player == 0 {
			self = o.Index
		}
	}
	if self < 0 {
		t.Fatalf("Merry Partner-with ETB offers no controller target: %+v", targetAsk.Options)
	}
	submitChoices(t, e, self)
	zallDrain(t, e)

	// (1) two artifacts entering together -> exactly one token.
	zallEnter(t, e, 0, "Sol Ring")
	zallEnter(t, e, 0, "Tormod's Crypt")
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("two artifacts entering queued %d trigger(s), want 1 (one per batch)", len(e.pendingTriggers))
	}
	zallDrain(t, e)
	if n := countTokensNamedOnSeat(t, e, 0, "Soldier Token"); n != 1 {
		t.Fatalf("a two-artifact batch created %d Soldier token(s), want exactly 1", n)
	}
	var tok *state.Object
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Soldier Token" {
			tok = o
			break
		}
	}
	if tok == nil {
		t.Fatal("no Soldier Token object on seat 0's battlefield")
	}
	f := tok.Face()
	if !slices.Contains(f.Types, "Creature") || !slices.Contains(f.Types, "Soldier") {
		t.Fatalf("token types = %v, want Creature Soldier", f.Types)
	}
	if f.PT != "1/1" {
		t.Fatalf("token PT = %q, want 1/1", f.PT)
	}
	if !slices.Contains(f.Keywords, "Lifelink") {
		t.Fatalf("token keywords = %v, want Lifelink", f.Keywords)
	}

	// (2) a second artifact batch in the same turn is latched off.
	zallEnter(t, e, 0, "Mox Amber")
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("ActivationLimit$ 1 did not latch: %d trigger(s) queued on the second batch", len(e.pendingTriggers))
	}
	zallDrain(t, e)
	if n := countTokensNamedOnSeat(t, e, 0, "Soldier Token"); n != 1 {
		t.Fatalf("after the second batch seat 0 holds %d Soldier token(s), want still 1", n)
	}

	// (3) an opponent's artifact entering is not "you control".
	oppRing := zallEnter(t, e, 1, "Sol Ring")
	zallDrain(t, e)
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("an opponent's artifact entering queued %d trigger(s), want 0", len(e.pendingTriggers))
	}
	if n := countTokensNamedOnSeat(t, e, 0, "Soldier Token"); n != 1 {
		t.Fatalf("after the opponent's artifact seat 0 holds %d Soldier token(s), want still 1", n)
	}
	if got := e.G.Obj(oppRing).Zone; got != state.ZBattlefield {
		t.Fatalf("opponent Sol Ring zone = %s, want battlefield", got)
	}
}

// TestChangesZoneAllPrimitiveIsRegistered pins the census half of the fix:
// the mode is declared supported and all pinned carriers -- the two report
// cards and Merry, Warden of Isengard, the deck card the original issue
// named -- carry no unregistered primitive at all. Merry's
// DECK-CONSTRUCTION keyword K:Partner with (CR 903.13c) is registered
// alongside its "kw:Partner" sibling in the trigger_match.go non-API list:
// the engine's partnerHead/partnerPairOK seat the named pair, so the
// registration asserts the corpus shape is understood.
func TestChangesZoneAllPrimitiveIsRegistered(t *testing.T) {
	if !effects.Supported()["trig:ChangesZoneAll"] {
		t.Fatal(`effects.Supported() is missing "trig:ChangesZoneAll"`)
	}
	reg := searchTestRegistry(t)
	for _, name := range []string{"Tocasia's Welcome", "Morbid Opportunist", "Merry, Warden of Isengard"} {
		c := searchCorpusCard(t, reg, name)
		for _, prim := range c.Primitives() {
			if !effects.Supported()[prim] {
				t.Fatalf("%s carries an unsupported primitive %q", name, prim)
			}
		}
	}
}
