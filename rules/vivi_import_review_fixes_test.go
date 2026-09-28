package rules

// The review round's fixes on top of the vivi-ornitier-cedh import reads
// (verdict-r2's findings), each pinned on the carrier the finding named:
//
//	static delayed-trigger scan   Insist (the "next creature spell can't be
//	                              countered" promise family, ~50 corpus cards)
//	Reveal.Random$ × NumCards$    Rise // Fall's Fall half (the only corpus
//	                              Reveal line with NumCards$ above one)
//	Draw.OptionalDecider$ seat    Vex (Defined$ TargetedController; the
//	                              Opponent carriers' draw sub is a separate
//	                              no-op gap, see the report's Issues)
//	Draw.OptionalDecider$ unknown synthetic (no corpus line carries an
//	                              unresolvable value; the fail-closed arm
//	                              still must not regress the mandatory draw)

import (
	"strings"

	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// castReviewed funds seat's pool, casts the card named id (no targets), and
// passes both seats so the stack resolves.
func castReviewed(t *testing.T, e *Engine, seat state.PlayerID, id state.ObjID, fund string) {
	t.Helper()
	addMana(t, e, seat, fund)
	d := e.Pending()
	if d == nil || d.Player != seat {
		t.Fatalf("seat %d priority expected, got %+v", seat, d)
	}
	co := viviOption(d, "cast", id)
	if co == nil {
		t.Fatalf("no cast option for obj %d: %+v", id, d.Options)
	}
	submitChoices(t, e, co.Index)
	viviPass(t, e)
	viviPass(t, e)
}

// TestTwoStaticDelayedSpellCastPromisesBothFire pins the collect-then-fire
// scan: with TWO live "the next creature spell you cast this turn can't be
// countered" promises (Insist cast twice in one turn — the ordinary shape,
// ~50 corpus carriers), ONE creature cast must fire BOTH without the
// index-out-of-range panic the live-slice splice produced.
func TestTwoStaticDelayedSpellCastPromisesBothFire(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := viviEngine(t, reg,
		[]*cards.Card{viviCard(t, reg, "Insist"), viviCard(t, reg, "Insist"), viviCard(t, reg, "Grizzly Bears")},
		[]*cards.Card{viviCard(t, reg, "Opt")})
	var insists []state.ObjID
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Insist" {
			insists = append(insists, id)
		}
	}
	// Lift library copies until two sit in the hand. The zone is RE-FETCHED
	// per lift: a MoveZone's Apply mutates the library slice in place, so a
	// single range can shift past the next copy.
	for len(insists) < 2 {
		var found state.ObjID
		for _, id := range e.G.Zone(state.ZLibrary, 0) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Insist" {
				found = id
				break
			}
		}
		if found == 0 {
			break
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: found, From: state.ZLibrary, To: state.ZHand})
		insists = append(insists, found)
	}
	if len(insists) != 2 {
		t.Fatalf("expected two Insist objects in hand, got %v", insists)
	}
	first, second := insists[0], insists[1]
	castReviewed(t, e, 0, first, "G")
	castReviewed(t, e, 0, second, "G")
	live := 0
	for _, dt := range e.G.Delayed {
		if dt.EventMode == "SpellCast" {
			live++
		}
	}
	if live != 2 {
		t.Fatalf("two promises expected after two Insists, got %d", live)
	}
	bears := moveByName(t, e, 0, "Grizzly Bears", state.ZHand)
	// THE pin: the cast's scan collects both matches and fires them after the
	// scan — no slice splice mid-range, no panic, no skipped registration.
	castReviewed(t, e, 0, bears, "GG")
	for _, dt := range e.G.Delayed {
		if dt.EventMode == "SpellCast" {
			t.Fatalf("both promises must be consumed by one creature cast, %d left (id %d turn %d)",
				dt.ID, dt.Source, dt.MaxTurn)
		}
	}
	if o := e.G.Obj(bears); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Grizzly Bears zone=%v, want battlefield", o)
	}
	replayCheck(t, e, cfg)
}

// randTwoSrc is Fall's reveal line (Rise // Fall, the only corpus Reveal
// line whose NumCards$ exceeds one), authored inline — the corpus .txt is
// GPL and never committed.
const randTwoSrc = "Name:Randtwo\nManaCost:B R\nTypes:Sorcery\n" +
	"A:SP$ Reveal | ValidTgts$ Player | Random$ True | NumCards$ 2 | RememberRevealed$ True | SubAbility$ DBDiscard\n" +
	"SVar:DBDiscard:DB$ Discard | Mode$ Defined | Defined$ Targeted | DefinedCards$ ValidHand Card.nonLand+IsRemembered\n" +
	"Oracle:Target player reveals two cards at random from their hand, then discards each nonland card revealed this way.\n"

// TestRandomRevealCountFollowsNumCards pins the count: the Random$ narrowing
// runs AFTER the count is resolved, so NumCards$ 2 reveals (and the chained
// discard acts on) TWO distinct cards — the pre-fix build narrowed the pool
// to one card first and revealed one.
func TestRandomRevealCountFollowsNumCards(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	opt := viviCard(t, reg, "Opt")
	seat1 := make([]*cards.Card, 0, 40)
	for i := 0; i < 40; i++ {
		seat1 = append(seat1, opt)
	}
	deck0 := append([]*cards.Card{card(t, randTwoSrc)}, mountainDeck(t, 39)...)
	cfg := seatZeroStart(Config{Seed: 42, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{deck0, seat1},
		Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	rand := moveByName(t, e, 0, "Randtwo", state.ZHand)
	addMana(t, e, 0, "BR")
	d := e.Pending()
	co := viviOption(d, "cast", rand)
	if co == nil {
		t.Fatalf("no Randtwo cast: %+v", d.Options)
	}
	submitChoices(t, e, co.Index)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the player target ask, got %+v", d)
	}
	tgt := viviOption(d, "player", 1)
	if tgt == nil {
		t.Fatalf("no seat-1 player option: %+v", d.Options)
	}
	submitChoices(t, e, tgt.Index)
	viviPass(t, e)
	viviPass(t, e)
	// THE pin: exactly one public reveal Note, carrying TWO DISTINCT seat-1
	// hand ids — the pre-fix narrowing narrowed the pool to one card first
	// and revealed one. (The chained discard sub is the Discard Mode$ Defined
	// stand-in — a defined-PLAYER target with DefinedCards$ moves nothing —
	// a gap already recorded in AGENTS.md's approximations table, out of
	// scope here.) The reveal moves nothing, so seat 1's hand is unchanged.
	if got := len(e.G.Zone(state.ZHand, 1)); got != 7 {
		t.Fatalf("seat 1 hand %d, want 7 (a reveal moves nothing)", got)
	}
	var revealed []state.ObjID
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Player == 1 && len(ev.IDs) > 0 {
			revealed = append(revealed, ev.IDs...)
		}
	}
	if len(revealed) != 2 || revealed[0] == revealed[1] {
		t.Fatalf("two distinct revealed cards expected, got %v", revealed)
	}
	for _, id := range revealed {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZHand || o.Controller != 1 {
			t.Fatalf("revealed card %d not in seat 1's hand: %+v", id, o)
		}
	}
	replayCheck(t, e, cfg)
}

// TestOptionalDeciderAskResolvesTheNamedDecider pins the decider identity:
// Vex's "that spell's controller may draw a card" — OptionalDecider$
// TargetedController — asks the TARGETED spell's controller (seat 0), not
// the counter's caster (seat 1, where the pre-fix read posed it).
func TestOptionalDeciderAskResolvesTheNamedDecider(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg := viviEngine(t, reg,
		[]*cards.Card{viviCard(t, reg, "Opt")},
		[]*cards.Card{viviCard(t, reg, "Vex")})
	opt := moveByName(t, e, 0, "Opt", state.ZHand)
	addMana(t, e, 0, "U")
	d := e.Pending()
	co := viviOption(d, "cast", opt)
	if co == nil {
		t.Fatalf("no Opt cast: %+v", d.Options)
	}
	submitChoices(t, e, co.Index)
	viviPass(t, e)
	vex := moveByName(t, e, 1, "Vex", state.ZHand)
	addMana(t, e, 1, "UCC")
	d = e.Pending()
	if d == nil || d.Player != 1 {
		t.Fatalf("seat 1 priority expected, got %+v", d)
	}
	co = viviOption(d, "cast", vex)
	if co == nil {
		t.Fatalf("no Vex cast: %+v", d.Options)
	}
	submitChoices(t, e, co.Index)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the spell target ask, got %+v", d)
	}
	tgt := viviOption(d, "spell", opt)
	if tgt == nil {
		t.Fatalf("no Opt stack option: %+v", d.Options)
	}
	submitChoices(t, e, tgt.Index)
	viviPass(t, e)
	viviPass(t, e)
	// THE pin: the ask goes to seat 0 — the countered spell's controller —
	// and a yes draws for seat 0.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Player != 0 || d.ResumeKind != "draw_optional" {
		t.Fatalf("expected the draw_optional ask for seat 0 (the targeted controller), got %+v", d)
	}
	hand0 := len(e.G.Zone(state.ZHand, 0))
	submitChoices(t, e, 0) // yes — draw
	if got := len(e.G.Zone(state.ZHand, 0)); got != hand0+1 {
		t.Fatalf("seat 0 hand %d, want %d", got, hand0+1)
	}
	replayCheck(t, e, cfg)
}

// unresolvableDeciderSrc carries an OptionalDecider$ value the selector
// grammar does not carry. No corpus line does either (the values are You,
// True, Opponent, TargetedController, TriggeredCardController — the first
// two keep asking the controller, the rest resolve), so the fail-closed arm
// is corpus-unreachable and pinned on this synthetic.
const unresolvableDeciderSrc = "Name:Optdec\nManaCost:U\nTypes:Instant\n" +
	"A:SP$ Draw | Defined$ You | NumCards$ 1 | OptionalDecider$ UnmodelledSel\n" +
	"Oracle:Draw a card.\n"

// TestOptionalDeciderUnresolvableDrawsMandatory pins the fail-closed arm: an
// unresolvable decider keeps the pre-ask mandatory draw and one loud Note —
// it neither poses a controller ask (the pre-fix wrong-seat behaviour) nor
// wedges.
func TestOptionalDeciderUnresolvableDrawsMandatory(t *testing.T) {
	t.Parallel()
	deck0 := append([]*cards.Card{card(t, unresolvableDeciderSrc)}, mountainDeck(t, 39)...)
	cfg := seatZeroStart(Config{Seed: 42, Names: []string{"a", "b"},
		Decks:  [][]*cards.Card{deck0, mountainDeck(t, 40)},
		Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	id := moveByName(t, e, 0, "Optdec", state.ZHand)
	addMana(t, e, 0, "U")
	d := e.Pending()
	co := viviOption(d, "cast", id)
	if co == nil {
		t.Fatalf("no Optdec cast: %+v", d.Options)
	}
	submitChoices(t, e, co.Index)
	viviPass(t, e)
	viviPass(t, e)
	// No ask anywhere — the draw went through mandatory, one Note names the
	// value.
	d = e.Pending()
	if d != nil && d.Kind != decision.KPriority {
		t.Fatalf("no ask expected after the resolution, got %+v", d)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != 8 {
		t.Fatalf("seat 0 hand %d, want 8 (the pre-ask mandatory draw)", got)
	}
	noted := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unmodelled Draw OptionalDecider$ UnmodelledSel") {
			noted = true
		}
	}
	if !noted {
		t.Fatal("no loud Note for the unresolvable decider")
	}
	replayCheck(t, e, cfg)
}
