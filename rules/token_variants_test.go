package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Tests for the token/zone-change entry variants this task read:
//
//   - `Token.TokenTapped$ True` (Army of the Damned's thirteen tapped
//     Zombies) -- the token enters and then carries the same "entered
//     tapped" Tap event every other Tapped$ zone-change path emits;
//   - `Token.TokenPower$`/`Token.TokenToughness$` (Skyclave Apparition's
//     X/X Illusion, X the exiled card's mana value) -- a dynamic P/T set as
//     a permanent layer-7b SubSet continuous effect on the token itself;
//   - `ChangeZoneAll.Tapped$ True` (Splendid Reclamation's "Return all land
//     cards from your graveyard to the battlefield tapped") -- the mass
//     move's battlefield entries each carry the entry tap.
//
// All three cards are repo-deck cards (their census labels were retired in
// knownUnsupportedParams by this work), so no legacy golden head depends on
// any of them by construction -- re-checked against TestHeads below.

// castSpellNamed funds the pool, moves the named card to seat 0's hand, and
// casts it, passing through resolution; it returns whatever decision is
// pending once the stack is empty (for these no-target spells, priority).
func castSpellNamed(t *testing.T, e *Engine, reg *cards.Registry, name, symbols string) *decision.Decision {
	t.Helper()
	searchCorpusCard(t, reg, name) // fail loudly if the corpus lost the card
	id := searchMoveByName(t, e, name, state.ZHand)
	addMana(t, e, 0, symbols)
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for %s: %+v", name, d.Options)
	}
	submitChoices(t, e, idx)
	return passUntilResolved(t, e, 20)
}

// stackEmpty reports whether no object anywhere sits on the stack.
func stackEmpty(e *Engine) bool {
	for i := range e.G.Objs {
		if e.G.Objs[i].Zone == state.ZStack {
			return false
		}
	}
	return true
}

// passUntilResolved drives a cast through resolution: pass every priority
// decision until the stack is empty and priority is back, stopping early on
// a mid-resolution non-priority ask (returned to the caller). A seat with a
// mandatory non-pass priority action fails loudly.
func passUntilResolved(t *testing.T, e *Engine, limit int) *decision.Decision {
	t.Helper()
	for i := 0; i < limit && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision pending (game over: %v)", e.G.Over)
		}
		if d.Kind != decision.KPriority {
			return d
		}
		if stackEmpty(e) {
			return d
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit pass: %v", err)
		}
	}
	t.Fatalf("stack never emptied within %d passes", limit)
	return nil
}

// enteredTappedTaps collects the log's "entered tapped" Tap events.
func enteredTappedTaps(t *testing.T, e *Engine) []events.Event {
	t.Helper()
	var out []events.Event
	for _, ev := range e.L.Events {
		if ev.Kind == events.Tap && ev.Text == "entered tapped" {
			out = append(out, ev)
		}
	}
	return out
}

// TestTokenTappedCreatesTappedTokens pins TokenTapped$ True on the real
// corpus card: Army of the Damned's thirteen 2/2 black Zombie tokens all
// enter TAPPED, each through its own "entered tapped" Tap event, and the
// log-only replay reproduces the whole board.
func TestTokenTappedCreatesTappedTokens(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Army of the Damned")
	d := castSpellNamed(t, e, reg, "Army of the Damned", "BBBBBBBB")
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("after Army of the Damned resolved: %+v, want priority", d)
	}
	tokens := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o == nil || !o.IsToken {
			continue
		}
		tokens++
		if !o.Tapped {
			t.Fatalf("token %d (%v) entered untapped, want TokenTapped$ True", id, o.Face().Name)
		}
	}
	if tokens != 13 {
		t.Fatalf("battlefield holds %d tokens, want 13 (TokenAmount$ 13)", tokens)
	}
	taps := enteredTappedTaps(t, e)
	if len(taps) != 13 {
		t.Fatalf("log carries %d \"entered tapped\" Tap events, want 13", len(taps))
	}
	for _, ev := range taps {
		o := e.G.Obj(ev.Obj)
		if o == nil || !o.IsToken || o.Zone != state.ZBattlefield {
			t.Fatalf("entered-tapped event names non-token object %d: %+v", ev.Obj, o)
		}
	}
	replayCheck(t, e, cfg)
}

// skyclaveEngine deals seat 0 Skyclave Apparition and Murder (plus basics),
// and puts a Grizzly Bears on seat 1's battlefield -- the mana-value-2
// nonland nontoken permanent the ETB exile will take, so the leave trigger's
// token is an X/X Illusion with X = 2 created by the BEAR's owner (seat 1).
func skyclaveEngine(t *testing.T, reg *cards.Registry) (*Engine, Config, state.ObjID) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	bear := searchCorpusCard(t, reg, "Grizzly Bears")
	deck := []*cards.Card{
		searchCorpusCard(t, reg, "Skyclave Apparition"),
		searchCorpusCard(t, reg, "Murder"),
	}
	for i := 0; i < 8; i++ {
		deck = append(deck, forest, mountain)
	}
	for len(deck) < 40 {
		deck = append(deck, bear)
	}
	opp := append([]*cards.Card{bear}, make([]*cards.Card, 0, 39)...)
	for len(opp) < 40 {
		opp = append(opp, mountain)
	}
	cfg := seatZeroStart(Config{Seed: 9210, Names: []string{"apparition", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	bearID := state.ObjID(0)
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 1) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Grizzly Bears" {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZBattlefield})
				e.pending = nil
				bearID = id
				break
			}
		}
		if bearID != 0 {
			break
		}
	}
	if bearID == 0 {
		t.Fatal("no Grizzly Bears in seat 1's library to stage")
	}
	e.priorityRound()
	return e, cfg, bearID
}

// TestTokenDynamicPTSkyclaveApparition pins TokenPower$/TokenToughness$ end
// to end on the real card: Skyclave Apparition enters, its ETB exiles seat
// 1's Grizzly Bears (mana value 2), Murder destroys the Apparition, and the
// leave trigger creates a 2/2 blue Illusion token OWNED BY THE BEAR'S OWNER
// (TokenOwner$ RememberedOwner) with the derived P/T of exactly 2/2.
func TestTokenDynamicPTSkyclaveApparition(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, bearID := skyclaveEngine(t, reg)

	// Enter + exile the bear through the ETB trigger's target ask.
	id := searchMoveByName(t, e, "Skyclave Apparition", state.ZHand)
	addMana(t, e, 0, "WWW")
	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("after Skyclave Apparition entered: %+v, want the ETB exile target ask", d)
	}
	tIdx := -1
	for _, o := range d.Options {
		if o.Obj == bearID {
			tIdx = o.Index
		}
	}
	if tIdx < 0 {
		t.Fatalf("the staged bear %d is not offered as an exile target: %+v", bearID, d.Options)
	}
	submitChoices(t, e, tIdx)
	if d = e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("after the exile answer: %+v, want priority", d)
	}
	if d = passUntilResolved(t, e, 20); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("while the exile trigger resolved: %+v, want priority", d)
	}
	if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZExile {
		t.Fatalf("exiled bear = %+v, want exile", o)
	}

	// Destroy the Apparition with Murder; the leave trigger runs.
	murder := searchMoveByName(t, e, "Murder", state.ZHand)
	addMana(t, e, 0, "BBB")
	d = e.Pending()
	cIdx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == murder {
			cIdx = o.Index
		}
	}
	if cIdx < 0 {
		t.Fatalf("no cast option for Murder: %+v", d.Options)
	}
	submitChoices(t, e, cIdx)
	if d = e.Pending(); d == nil || d.Kind != decision.KTarget {
		t.Fatalf("after casting Murder: %+v, want the kill target ask", d)
	}
	tIdx = -1
	for _, o := range d.Options {
		if o.Obj == id {
			tIdx = o.Index
		}
	}
	if tIdx < 0 {
		t.Fatalf("Skyclave Apparition %d not offered to Murder: %+v", id, d.Options)
	}
	submitChoices(t, e, tIdx)
	if d = passUntilResolved(t, e, 20); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("after Murder resolved: %+v, want priority", d)
	}
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("destroyed Skyclave Apparition = %+v, want graveyard", o)
	}

	// The leave trigger's token: seat 1's battlefield, blue Illusion, 2/2.
	var tok *state.Object
	tokID := state.ObjID(0)
	for _, tid := range e.G.Zone(state.ZBattlefield, 1) {
		o := e.G.Obj(tid)
		if o != nil && o.IsToken {
			tok, tokID = o, tid
			break
		}
	}
	if tok == nil {
		t.Fatalf("no token on seat 1's battlefield; events: %+v", e.L.Events[len(e.L.Events)-6:])
	}
	if tok.Owner != 1 {
		t.Fatalf("Illusion token owner = %d, want 1 (TokenOwner$ RememberedOwner)", tok.Owner)
	}
	if tok.Face() == nil || !strings.Contains(tok.Face().Name, "Illusion") {
		t.Fatalf("token face = %+v, want the u_x_x_illusion Illusion", tok.Face())
	}
	der := e.Derived(tokID)
	if der.Power != 2 || der.Toughness != 2 {
		t.Fatalf("Illusion token derived P/T = %d/%d, want 2/2 (X = the bear's mana value)", der.Power, der.Toughness)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unrecognized TokenOwner") {
			t.Fatalf("TokenOwner$ RememberedOwner fell back with a Note: %q", ev.Text)
		}
		if ev.Kind == events.Note && strings.Contains(ev.Text, "TokenPower$") {
			t.Fatalf("dynamic token P/T degraded to a Note: %q", ev.Text)
		}
	}
	replayCheck(t, e, cfg)
}

// TestChangeZoneAllTappedReturnsGraveyardLandsTapped pins Tapped$ True on
// the real corpus card: Splendid Reclamation returns every land card from
// seat 0's graveyard to the battlefield TAPPED, each through its own
// "entered tapped" Tap event.
func TestChangeZoneAllTappedReturnsGraveyardLandsTapped(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Splendid Reclamation")
	var grave []state.ObjID
	for _, name := range []string{"Forest", "Mountain", "Forest"} {
		id := searchMoveByName(t, e, name, state.ZGraveyard)
		grave = append(grave, id)
	}
	d := castSpellNamed(t, e, reg, "Splendid Reclamation", "GGGG")
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("after Splendid Reclamation resolved: %+v, want priority", d)
	}
	for _, gid := range grave {
		o := e.G.Obj(gid)
		if o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("returned land %d = %+v, want battlefield", gid, o)
		}
		if !o.Tapped {
			t.Fatalf("returned land %d (%v) entered untapped, want Tapped$ True", gid, o.Face().Name)
		}
	}
	taps := enteredTappedTaps(t, e)
	if len(taps) != len(grave) {
		t.Fatalf("log carries %d \"entered tapped\" Tap events, want %d", len(taps), len(grave))
	}
	// Nothing else moved: the battlefield holds exactly the returned lands
	// (the fixture staged no other permanents).
	if n := len(e.G.Zone(state.ZBattlefield, 0)); n != len(grave) {
		t.Fatalf("seat 0 battlefield holds %d permanents, want %d", n, len(grave))
	}
	replayCheck(t, e, cfg)
}
