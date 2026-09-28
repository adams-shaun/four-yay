package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// threeSeatMain builds a three-seat game from cfg, starts it and parks at the
// active seat's Main1. It is the engine-level board the direction/hand-off
// tests need: two seats cannot tell "next seat" from "previous seat".
func threeSeatMain(t *testing.T, cfg Config) *Engine {
	t.Helper()
	e := New(seatZeroStart(cfg))
	e.Advance()
	toMain1(t, e)
	return e
}

// corpusDeck returns n copies of c followed by filler Mountains, so a corpus
// card can be a real deck card (genesis-created, and therefore reproducible
// by replayFromLog) rather than a test-time AddObject.
func corpusDeck(t *testing.T, c *cards.Card, n int) []*cards.Card {
	t.Helper()
	out := make([]*cards.Card, 0, 40)
	for i := 0; i < n; i++ {
		out = append(out, c)
	}
	for len(out) < 40 {
		out = append(out, mountainDeck(t, 1)...)
	}
	return out
}

// TestOrderOfSuccessionChainsDirectionAndPerRecipientChoices is the
// engine-level pin for the two-ask chain: Order of Succession's
// ChooseDirection root suspends on a real KChoose, resumes into its
// DBGainControl SubAbility, and then suspends AGAIN on the per-recipient
// choice -- which must be posed to the RECIPIENT (seat 2), not the caster,
// with the chosen creature (the second option) actually changing hands. The
// whole match then replays from the log alone.
func TestOrderOfSuccessionChainsDirectionAndPerRecipientChoices(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	order := searchCorpusCard(t, reg, "Order of Succession")
	bears := searchCorpusCard(t, reg, "Grizzly Bears")
	hill := searchCorpusCard(t, reg, "Hill Giant")
	seat0Deck := append([]*cards.Card{order, bears, bears}, mountainDeck(t, 37)...)
	cfg := seatZeroStart(Config{Seed: 90210,
		Names: []string{"a", "b", "c"},
		Decks: [][]*cards.Card{
			seat0Deck,
			corpusDeck(t, bears, 1),
			corpusDeck(t, hill, 1),
		},
		Tokens: reg.Tokens})
	e := threeSeatMain(t, cfg)
	// Ring "left" from seat 0 is [0, 1, 2]; the next player is seat 1 for
	// recipient 0, seat 2 for recipient 1, seat 0 for recipient 2. Seat 1's
	// ONE creature and seat 2's ONE creature need no ask; seat 0's TWO are
	// recipient 2's pool (the one ask).
	seat1Creature := placeInDeck(t, e, 1, bears, state.ZBattlefield)
	seat2Creature := placeInDeck(t, e, 2, hill, state.ZBattlefield)
	seat0A := placeInDeck(t, e, 0, bears, state.ZBattlefield)
	seat0B := placeInDeck(t, e, 0, bears, state.ZBattlefield)
	orderID := placeInDeck(t, e, 0, order, state.ZHand)
	for _, id := range []state.ObjID{seat1Creature, seat2Creature, seat0A, seat0B} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: %d not on the battlefield", id)
		}
	}
	// Precondition: every subject really starts under its owner, and the two
	// seat-0 creatures really are distinct objects (the choice matters).
	if e.G.Obj(seat0A).Controller != 0 || e.G.Obj(seat0B).Controller != 0 ||
		e.G.Obj(seat1Creature).Controller != 1 || e.G.Obj(seat2Creature).Controller != 2 {
		t.Fatalf("precondition: controllers = %d/%d/%d/%d, want 0/0/1/2",
			e.G.Obj(seat0A).Controller, e.G.Obj(seat0B).Controller,
			e.G.Obj(seat1Creature).Controller, e.G.Obj(seat2Creature).Controller)
	}
	if seat0A == seat0B {
		t.Fatal("precondition: the two seat-0 creatures are the same object")
	}

	addMana(t, e, 0, "UUUU") // 3U
	castFirst(t, e, "cast")
	if o := e.G.Obj(orderID); o.Zone != state.ZStack {
		t.Fatalf("precondition: Order of Succession zone = %s, want stack", o.Zone)
	}

	// The direction root suspends first.
	d := passUntilAsk(t, e)
	if d.Kind != decision.KChoose || d.ResumeKind != "choosedirection" {
		t.Fatalf("first ask = %+v, want the ChooseDirection ask", d)
	}
	left := optionByLabel(d.Options, "left")
	if left < 0 {
		t.Fatalf("direction options = %+v, want a left option", d.Options)
	}
	submitChoices(t, e, left)

	// Then the per-recipient ask, posed to seat 2 (its own chooser): its pool
	// is seat 0's two creatures.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "choice" {
		t.Fatalf("second ask = %+v, want the per-recipient choice", d)
	}
	if d.Player != 2 {
		t.Fatalf("per-recipient chooser = seat %d, want seat 2 (each recipient chooses for themself)", d.Player)
	}
	if len(d.Options) != 2 || d.Options[0].Obj != seat0A || d.Options[1].Obj != seat0B {
		t.Fatalf("per-recipient options = %+v, want [%d %d]", d.Options, seat0A, seat0B)
	}
	// Take the SECOND creature, so the choice -- not the first offer -- moves.
	submitChoices(t, e, d.Options[1].Index)

	if got := e.G.Obj(seat1Creature).Controller; got != 0 {
		t.Fatalf("recipient 0 (seat 0) gain controller = %d, want 0", got)
	}
	if got := e.G.Obj(seat2Creature).Controller; got != 1 {
		t.Fatalf("recipient 1 (seat 1) gain controller = %d, want 1", got)
	}
	if got := e.G.Obj(seat0B).Controller; got != 2 {
		t.Fatalf("recipient 2 (seat 2) gain controller = %d, want 2 (the chosen second option)", got)
	}
	if got := e.G.Obj(seat0A).Controller; got != 0 {
		t.Fatalf("unchosen seat-0 creature controller = %d, want 0", got)
	}
	if o := e.G.Obj(orderID); o.Zone != state.ZGraveyard {
		t.Fatalf("resolved Order of Succession zone = %s, want graveyard", o.Zone)
	}
	replayCheck(t, e, cfg)
}

// TestScrambleverseRandomTransfersAndUntaps drives Scrambleverse's real cast
// on three living seats: the seeded generator picks a controller for every
// nonland permanent (untapping them all through DBUntap), and the whole match
// replays byte-identically from the log alone -- which is what proves the
// random draws are the seeded host's and not ambient randomness.
func TestScrambleverseRandomTransfersAndUntaps(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	scramble := searchCorpusCard(t, reg, "Scrambleverse")
	bears := searchCorpusCard(t, reg, "Grizzly Bears")
	hill := searchCorpusCard(t, reg, "Hill Giant")
	cfg := seatZeroStart(Config{Seed: 5150,
		Names: []string{"a", "b", "c"},
		Decks: [][]*cards.Card{
			corpusDeck(t, scramble, 1),
			corpusDeck(t, bears, 1),
			corpusDeck(t, hill, 1),
		},
		Tokens: reg.Tokens})
	e := threeSeatMain(t, cfg)
	b1 := placeInDeck(t, e, 1, bears, state.ZBattlefield)
	h2 := placeInDeck(t, e, 2, hill, state.ZBattlefield)
	sID := placeInDeck(t, e, 0, scramble, state.ZHand)
	e.emit(events.Event{Kind: events.Tap, Obj: b1})
	e.emit(events.Event{Kind: events.Tap, Obj: h2})
	if !e.G.Obj(b1).Tapped || !e.G.Obj(h2).Tapped {
		t.Fatalf("precondition: tapped = %v/%v, want both tapped", e.G.Obj(b1).Tapped, e.G.Obj(h2).Tapped)
	}
	if e.G.Obj(b1).Controller != 1 || e.G.Obj(h2).Controller != 2 {
		t.Fatalf("precondition: controllers = %d/%d, want 1/2", e.G.Obj(b1).Controller, e.G.Obj(h2).Controller)
	}
	addMana(t, e, 0, "RRRRRRRR") // 6RR
	castFirst(t, e, "cast")
	if o := e.G.Obj(sID); o.Zone != state.ZStack {
		t.Fatalf("precondition: Scrambleverse zone = %s, want stack", o.Zone)
	}
	// Scrambleverse asks nothing mid-resolution: pass priority until it resolves.
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(sID); o.Zone != state.ZGraveyard {
		t.Fatalf("resolved Scrambleverse zone = %s, want graveyard", o.Zone)
	}
	if e.G.Obj(b1).Tapped || e.G.Obj(h2).Tapped {
		t.Fatalf("nonland permanents tapped = %v/%v, want both untapped (DBUntap)", e.G.Obj(b1).Tapped, e.G.Obj(h2).Tapped)
	}
	for _, id := range []state.ObjID{b1, h2} {
		if c := e.G.Obj(id).Controller; int(c) >= len(e.G.Players) {
			t.Fatalf("controller %d for %d is not a seat", c, id)
		}
	}
	replayCheck(t, e, cfg)
}

// orderSuccessionBoard seats Order of Succession plus the same three-seat
// creature board TestOrderOfSuccession... uses, and returns the engine, its
// config and the four creature ids.
func orderSuccessionBoard(t *testing.T) (*Engine, Config, state.ObjID, [4]state.ObjID) {
	t.Helper()
	reg := searchTestRegistry(t)
	order := searchCorpusCard(t, reg, "Order of Succession")
	bears := searchCorpusCard(t, reg, "Grizzly Bears")
	hill := searchCorpusCard(t, reg, "Hill Giant")
	seat0Deck := append([]*cards.Card{order, bears, bears}, mountainDeck(t, 37)...)
	cfg := seatZeroStart(Config{Seed: 90210,
		Names: []string{"a", "b", "c"},
		Decks: [][]*cards.Card{
			seat0Deck,
			corpusDeck(t, bears, 1),
			corpusDeck(t, hill, 1),
		},
		Tokens: reg.Tokens})
	e := threeSeatMain(t, cfg)
	seat1Creature := placeInDeck(t, e, 1, bears, state.ZBattlefield)
	seat2Creature := placeInDeck(t, e, 2, hill, state.ZBattlefield)
	seat0A := placeInDeck(t, e, 0, bears, state.ZBattlefield)
	seat0B := placeInDeck(t, e, 0, bears, state.ZBattlefield)
	orderID := placeInDeck(t, e, 0, order, state.ZHand)
	addMana(t, e, 0, "UUUU")
	castFirst(t, e, "cast")
	if o := e.G.Obj(orderID); o.Zone != state.ZStack {
		t.Fatalf("precondition: Order of Succession zone = %s, want stack", o.Zone)
	}
	return e, cfg, orderID, [4]state.ObjID{seat1Creature, seat2Creature, seat0A, seat0B}
}

// TestOrderOfSuccessionBotAnswersValidateAndResolve runs the deterministic
// bot policy's own answer through each new ask's validator on the live
// board: the direction ask and the per-recipient object ask must both accept
// the bot's answer, or the hosted bot would livelock re-submitting a rejected
// intent. The whole match then replays from the log alone.
func TestOrderOfSuccessionBotAnswersValidateAndResolve(t *testing.T) {
	t.Parallel()
	e, cfg, orderID, ids := orderSuccessionBoard(t)
	d := passUntilAsk(t, e)
	if d.Kind != decision.KChoose || d.ResumeKind != "choosedirection" {
		t.Fatalf("first ask = %+v, want the ChooseDirection ask", d)
	}
	bot := newTestBot(11)
	answers := 0
	for i := 0; i < 8; i++ {
		d = e.Pending()
		if d == nil {
			break
		}
		if d.Kind == decision.KPriority {
			break
		}
		if d.Kind != decision.KChoose || (d.ResumeKind != "choosedirection" && d.ResumeKind != "choice") {
			t.Fatalf("unexpected decision during the chain: %+v", d)
		}
		in := bot.answer(e, d)
		if err := d.Validate(in); err != nil {
			t.Fatalf("bot answer %v rejected by the %q ask validator: %v", in.Choices, d.ResumeKind, err)
		}
		if err := e.Submit(in); err != nil {
			t.Fatalf("submit bot answer: %v", err)
		}
		answers++
	}
	if answers < 2 {
		t.Fatalf("bot answered %d asks, want at least the direction and the per-recipient ask", answers)
	}
	if o := e.G.Obj(orderID); o.Zone != state.ZGraveyard {
		t.Fatalf("resolved Order of Succession zone = %s, want graveyard", o.Zone)
	}
	// Every creature the bot's picks moved now has a controller that is a
	// seat, and the two seat-0 creatures are still accounted for: no
	// permanent was dropped.
	for _, id := range ids {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("creature %d left the battlefield", id)
		}
		if c := e.G.Obj(id).Controller; int(c) >= len(e.G.Players) {
			t.Fatalf("controller %d for %d is not a seat", c, id)
		}
	}
	replayCheck(t, e, cfg)
}
