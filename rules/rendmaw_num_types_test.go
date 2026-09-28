// pred:Card.numTypesGE<n> — the card-type-count filter predicate — pinned
// end to end on the REAL corpus carrier Rendmaw, Creaking Nest (the only
// corpus file carrying `numTypes`, both triggers `ValidCard$
// Card.numTypesGE2` / `Land.numTypesGE2+YouCtrl`).
//
// The predicate itself is effects/filter.go's wordNumTypesGE word-kind;
// this file proves the trigger chain the predicate arms actually fires in a
// live engine: Rendmaw's ETB (ChangesZone Card.Self), its SpellCast arm on
// a cast two-type card, its LandPlayed arm on a played two-type land, and
// the single-type negative on both action arms. The "each player creates"
// rider rides TokenOwner$ Player (effects/token.go), which this task also
// implemented — every assertion below depends on the per-seat mint, so the
// owner distribution is asserted, not just the count.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// rendmawCorpusCard looks a real corpus card up by NAME.
func rendmawCorpusCard(t *testing.T, name string) *cards.Card {
	t.Helper()
	c, ok := testutil.CorpusRegistry(t).Lookup(name)
	if !ok {
		t.Fatalf("corpus missing %s", name)
	}
	return c
}

// rendmawGame builds a 2-seat engine from real corpus cards: Rendmaw, Alloy
// Myr (Artifact Creature — two card types), Darksteel Citadel (Artifact
// Land — two card types) and Grizzly Bears (Creature — one card type, the
// negative control) seeded into seat 0, mountains filling both decks, and
// the REAL corpus token registry (Rendmaw's TokenScript$ b_2_2_bird_flying).
func rendmawGame(t *testing.T) (*Engine, Config) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	deck := []*cards.Card{
		rendmawCorpusCard(t, "Rendmaw, Creaking Nest"),
		rendmawCorpusCard(t, "Alloy Myr"),
		rendmawCorpusCard(t, "Darksteel Citadel"),
		rendmawCorpusCard(t, "Grizzly Bears"),
	}
	deck = append(deck, mountainDeck(t, 40-len(deck))...)
	cfg := Config{Seed: 7, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{deck, mountainDeck(t, 40)},
		Tokens: reg.Tokens}
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	e.Advance()
	return e, cfg
}

// settleTriggers drains the engine's trigger queue AND any trigger ability
// already pushed onto the stack (Submit's own priority round auto-drains
// pending triggers onto the stack via putTriggersOnStack, so a trigger that
// queued inside a submitted intent is found on the stack, not in the queue).
func settleTriggers(t *testing.T, e *Engine, label string) {
	t.Helper()
	for i := 0; i < 40; i++ {
		if len(e.pendingTriggers) > 0 {
			e.putTriggersOnStack()
		}
		if len(e.G.Stack) == 0 {
			return
		}
		e.resolveTop()
	}
	t.Fatalf("%s: triggers did not settle, %d still pending", label, len(e.pendingTriggers))
}

// birdsOn counts seat p's battlefield Bird tokens.
func birdsOn(t *testing.T, e *Engine, p state.PlayerID) int {
	t.Helper()
	n := 0
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if o := e.G.Obj(id); o != nil && o.IsToken && o.Face() != nil && o.Face().Name == "Bird Token" {
			n++
		}
	}
	return n
}

// assertGoadedBirds asserts seat p holds exactly want Bird tokens, every one
// tapped, flying, and goaded permanently by Rendmaw's controller (the
// DBGoad sub's Duration$ Permanent ride) — the full rider chain, not just
// the mint count.
func assertGoadedBirds(t *testing.T, e *Engine, p state.PlayerID, want int, label string) {
	t.Helper()
	if got := birdsOn(t, e, p); got != want {
		t.Fatalf("%s: seat %d holds %d Bird tokens, want %d", label, p, got, want)
	}
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		o := e.G.Obj(id)
		if o == nil || !o.IsToken || o.Face() == nil || o.Face().Name != "Bird Token" {
			continue
		}
		if !o.Tapped {
			t.Fatalf("%s: seat %d's Bird token %d is not tapped", label, p, o.ID)
		}
		if o.Face().Power() != 2 || o.Face().Toughness() != 2 {
			t.Fatalf("%s: seat %d's Bird token is %d/%d, want 2/2", label, p, o.Face().Power(), o.Face().Toughness())
		}
		flying := false
		for _, k := range o.Face().Keywords {
			if k == "Flying" {
				flying = true
			}
		}
		if !flying {
			t.Fatalf("%s: seat %d's Bird token has no Flying", label, p)
		}
		if len(o.Goads) == 0 {
			t.Fatalf("%s: seat %d's Bird token %d carries no goad", label, p, o.ID)
		}
		goaded := false
		for _, ge := range o.Goads {
			if ge.Duration == "Permanent" && ge.Player == 0 {
				goaded = true
			}
		}
		if !goaded {
			t.Fatalf("%s: seat %d's Bird token %d goads %+v, want a Permanent-duration goad by seat 0", label, p, o.ID, o.Goads)
		}
	}
}

// rendmawSeedToHand moves the named card from seat p's library (or wherever
// it sits) into seat p's hand through a real logged MoveZone -- the opening
// hand is only 7 random cards, so the deck's named cards are usually still
// in the library.
func rendmawSeedToHand(t *testing.T, e *Engine, p state.PlayerID, c *cards.Card) state.ObjID {
	t.Helper()
	name := c.Faces[0].Name
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, p) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
				if z == state.ZHand {
					return id
				}
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZHand})
				e.pending = nil
				return id
			}
		}
	}
	t.Fatalf("seeded card %q not found in seat %d's library or hand", name, p)
	return 0
}

// castRendmawSpell casts the named hand card through the ordinary beginCast
// flow with the pool pre-funded, resolves it, and drains whatever triggers
// the cast queued. Precondition: the card really left the hand onto the
// stack and then off it.
func castRendmawSpell(t *testing.T, e *Engine, name, mana string) {
	t.Helper()
	addMana(t, e, 0, mana)
	id := rendmawSeedToHand(t, e, 0, rendmawCorpusCard(t, name))
	castMode(t, e, id, "")
	if o := e.G.Obj(id); o != nil && o.Zone == state.ZHand {
		t.Fatalf("%s never left the hand: cast not funded or not offered", name)
	}
	e.resolveTop()
	settleTriggers(t, e, "after resolving "+name)
}

// playRendmawLand plays the named land from seat 0's hand through the
// ordinary play_land priority option and drains the LandPlayed trigger.
func playRendmawLand(t *testing.T, e *Engine, name string) {
	t.Helper()
	toMain1(t, e)
	id := rendmawSeedToHand(t, e, 0, rendmawCorpusCard(t, name))
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision pending to play the land: %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "play_land" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no play_land option for %s among %+v", name, d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit play_land: %v", err)
	}
	settleTriggers(t, e, "after playing "+name)
}

func TestRendmawETBCreatesGoadedBirdsForEachPlayer(t *testing.T) {
	t.Parallel()
	e, cfg := rendmawGame(t)
	rendmaw := moveSeededCard(t, e, 0, rendmawCorpusCard(t, "Rendmaw, Creaking Nest"), state.ZBattlefield)
	if o := e.G.Obj(rendmaw); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Rendmaw not on the battlefield: %+v", o)
	}
	settleTriggers(t, e, "Rendmaw ETB")
	// Precondition for everything the multitype arms assert later: the ETB
	// mint already went through TokenOwner$ Player — one token PER SEAT.
	assertGoadedBirds(t, e, 0, 1, "Rendmaw ETB")
	assertGoadedBirds(t, e, 1, 1, "Rendmaw ETB")
	replayCheck(t, e, cfg)
}

func TestRendmawSpellCastArmFiresOnATwoTypeCard(t *testing.T) {
	t.Parallel()
	e, cfg := rendmawGame(t)
	moveSeededCard(t, e, 0, rendmawCorpusCard(t, "Rendmaw, Creaking Nest"), state.ZBattlefield)
	settleTriggers(t, e, "Rendmaw ETB")
	before0, before1 := birdsOn(t, e, 0), birdsOn(t, e, 1)

	// Alloy Myr: Artifact Creature — two real card types. Its cast must
	// fire the SpellCast arm (`ValidCard$ Card.numTypesGE2`).
	castRendmawSpell(t, e, "Alloy Myr", "CCC")
	assertGoadedBirds(t, e, 0, before0+1, "Alloy Myr cast")
	assertGoadedBirds(t, e, 1, before1+1, "Alloy Myr cast")

	// Grizzly Bears: Creature only — ONE card type. The SpellCast arm must
	// stay silent, and the count must not move.
	castRendmawSpell(t, e, "Grizzly Bears", "CG")
	if got := birdsOn(t, e, 0); got != before0+1 {
		t.Fatalf("single-type cast moved the token count: seat 0 at %d, want %d", got, before0+1)
	}
	if got := birdsOn(t, e, 1); got != before1+1 {
		t.Fatalf("single-type cast moved the token count: seat 1 at %d, want %d", got, before1+1)
	}
	replayCheck(t, e, cfg)
}

func TestRendmawLandPlayedArmFiresOnATwoTypeLand(t *testing.T) {
	t.Parallel()
	e, cfg := rendmawGame(t)
	moveSeededCard(t, e, 0, rendmawCorpusCard(t, "Rendmaw, Creaking Nest"), state.ZBattlefield)
	settleTriggers(t, e, "Rendmaw ETB")
	before0, before1 := birdsOn(t, e, 0), birdsOn(t, e, 1)

	// Darksteel Citadel: Artifact Land — two card types. Its play must fire
	// the LandPlayed arm (`ValidCard$ Land.numTypesGE2+YouCtrl`).
	playRendmawLand(t, e, "Darksteel Citadel")
	assertGoadedBirds(t, e, 0, before0+1, "Darksteel Citadel played")
	assertGoadedBirds(t, e, 1, before1+1, "Darksteel Citadel played")
	replayCheck(t, e, cfg)
}
