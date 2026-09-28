package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// rendmawEngine builds a 3-seat engine (seats 0/1/2, toss pinned to seat 0)
// whose token table is the full compiled corpus's, so the real Bird token
// script (b_2_2_bird_flying) is mintable.
func rendmawEngine(t *testing.T, reg *cards.Registry) *Engine {
	t.Helper()
	return New(seatZeroStart(Config{Seed: 7, Names: []string{"a", "b", "c"},
		Decks:  [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40)},
		Tokens: reg.Tokens}))
}

// birdTokens reports every Bird token on the battlefield, per seat.
func birdTokens(t *testing.T, e *Engine, p state.PlayerID) []*state.Object {
	t.Helper()
	var out []*state.Object
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		o := e.G.Obj(id)
		if o.IsToken {
			out = append(out, o)
		}
	}
	return out
}

// TestRendmawEachPlayerCreatesAGoadedBirdToken pins the TokenOwner$ Player
// arm of effToken END TO END on the real compiled corpus card: Rendmaw,
// Creaking Nest's "each player creates a tapped 2/2 black Bird creature
// token with flying. The tokens are goaded for the rest of the game."
// Before the fix the bare spelling fell to the unrecognised-owner default
// and ONE Bird landed on Rendmaw's controller's battlefield (and only that
// one was goaded). The pin drives the real ETB trigger (ChangesZone into
// the battlefield) through the ordinary trigger drain and resolution, so
// the whole chain is the compiled card's own: DB$ Token (TokenOwner$
// Player, TokenTapped$ True, RememberTokens$ True) -> DB$ Goad (Defined$
// Remembered, Duration$ Permanent) -> DB$ Cleanup.
func TestRendmawEachPlayerCreatesAGoadedBirdToken(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := rendmawEngine(t, reg)
	src := e.G.AddObject(mustCorpusCard(t, reg, "Rendmaw, Creaking Nest"), 0)
	// The logged entry fires the compiled ChangesZone trigger exactly as a
	// real cast does.
	e.emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: state.ZLibrary, To: state.ZBattlefield})
	// putTriggersOnStack returns whether an ASK is pending, not whether a
	// trigger was pushed -- the pushed trigger is what matters here.
	e.putTriggersOnStack()
	if len(e.G.Zone(state.ZStack, 0)) == 0 {
		t.Fatal("Rendmaw's ETB trigger never went on the stack")
	}
	e.resolveTop()

	// Three players, three Bird tokens: one on each seat's battlefield,
	// each owned and controlled by that seat, tapped (TokenTapped$ True),
	// and goaded by Rendmaw's controller (seat 0) for the rest of the game
	// (Duration$ Permanent -- no expiry, CR 701.38b's two conditions read
	// forever here).
	for _, p := range []state.PlayerID{0, 1, 2} {
		toks := birdTokens(t, e, p)
		if len(toks) != 1 {
			t.Fatalf("seat %d's battlefield holds %d tokens, want exactly one Bird", p, len(toks))
		}
		bird := toks[0]
		if bird.Owner != p || bird.Controller != p {
			t.Fatalf("seat %d's token: owner=%d controller=%d, want both %d", p, bird.Owner, bird.Controller, p)
		}
		if bird.Face() == nil || bird.Face().Name != "Bird Token" {
			t.Fatalf("seat %d's token is %q, want the Bird token face", p, bird.Face().Name)
		}
		if !bird.Tapped {
			t.Fatalf("seat %d's Bird token is untapped, want entered tapped (TokenTapped$ True)", p)
		}
		if len(bird.Goads) != 1 {
			t.Fatalf("seat %d's Bird token goads = %+v, want exactly one goad entry", p, bird.Goads)
		}
		ge := bird.Goads[0]
		if ge.Player != 0 {
			t.Fatalf("seat %d's Bird token goaded by %d, want Rendmaw's controller 0", p, ge.Player)
		}
		if ge.Duration != "Permanent" {
			t.Fatalf("seat %d's Bird token goad duration = %q, want Permanent (\"for the rest of the game\")", p, ge.Duration)
		}
		if !e.hasActiveGoad(bird) || !e.goadedBy(bird, 0) {
			t.Fatalf("seat %d's Bird token is not an active goad of seat 0", p)
		}
		if e.goadedBy(bird, 1) || e.goadedBy(bird, 2) {
			t.Fatalf("seat %d's Bird token goaded by a seat that did not control Rendmaw", p)
		}
	}
	// The goad survives a turn boundary: "for the rest of the game" is not
	// a this-turn rider.
	e.Advance()
	for _, p := range []state.PlayerID{0, 1, 2} {
		for _, bird := range birdTokens(t, e, p) {
			if len(bird.Goads) == 0 {
				t.Fatalf("seat %d's Bird token lost its goad at the turn boundary", p)
			}
		}
	}
}

// TestRendmawSpellCastTriggerAlsoGoadsPerPlayer: the SECOND trigger shape
// (Mode$ SpellCast, "whenever you play a card with two or more card
// types") resolves the same DBTokens SVar, so it goes through the same
// per-player arm. Put a two-card-type spell on the stack by seat 0 while
// Rendmaw is out and every seat creates its Bird.
func TestRendmawSpellCastTriggerAlsoGoadsPerPlayer(t *testing.T) {
	t.Parallel()
	reg := sharedCorpus(t)
	e := rendmawEngine(t, reg)
	src := e.G.AddObject(mustCorpusCard(t, reg, "Rendmaw, Creaking Nest"), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: src.ID, From: state.ZLibrary, To: state.ZBattlefield})
	// A two-type spell: artifact creature.
	sp := e.G.AddObject(card(t, "Name:Toy Trooper\nTypes:Artifact Creature\nPT:1/1\nOracle:x\n"), 0)
	e.emit(events.Event{Kind: events.PutOnStack, Obj: sp.ID, Player: 0, From: state.ZHand, To: state.ZStack})
	e.putTriggersOnStack()
	// Both of Rendmaw's lines are pending on this sequence now that
	// numTypesGE2 is a real filter predicate (main's filter work): the ETB
	// line queued at the entry's MoveZone and the SpellCast line queued at
	// this PutOnStack, so the drain poses the CR 603.3b trigger_order ask
	// before placing either. Answer it keeping the offered order; both arms
	// resolve the same DBTokens SVar, so the per-player assertions below
	// hold whichever instance lands on top.
	if d := e.Pending(); d != nil && d.Kind == decision.KTriggerOrder {
		choices := make([]int, 0, len(d.Options))
		for _, o := range d.Options {
			choices = append(choices, o.Index)
		}
		submitChoices(t, e, choices...)
	}
	if len(e.G.Zone(state.ZStack, 0)) == 0 {
		t.Fatal("Rendmaw's SpellCast trigger never went on the stack")
	}
	e.resolveTop()
	for _, p := range []state.PlayerID{0, 1, 2} {
		toks := birdTokens(t, e, p)
		if len(toks) != 1 {
			t.Fatalf("seat %d's battlefield holds %d tokens, want one Bird per player", p, len(toks))
		}
		if len(toks[0].Goads) != 1 {
			t.Fatalf("seat %d's Bird token goads = %+v, want one (goaded for the rest of the game)", p, toks[0].Goads)
		}
	}
}
