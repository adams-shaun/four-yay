package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The imprintplayed end-to-end pin on Rashmi and Ragavan's REAL compiled
// corpus card: the first-spell trigger exiles the top card of an opponent's
// library and offers its free cast (`ValidSA$ Spell.cmcLTX` — the bound is
// the artifacts-you-control X). The `ImprintPlayed$ True` marker records
// each card the Play actually begins to play on the source's imprint list,
// and the chained `ConditionDefined$ Imprinted | ConditionPresent$ Card |
// ConditionCompare$ EQ0` gate on DBEffect reads that list to pick the arm:
//
//   - the card WAS cast (imprinted): DBEffect is denied — no MayPlay static,
//     so the exiled card is not offered a second, mana-costed cast;
//   - the Play was DECLINED (nothing imprinted): DBEffect registers the
//     STPlay "you may play the exiled card" static, and the fall-back
//     mana-costed cast from exile is offered — the oracle's "If you don't
//     cast it this way, you may cast it this turn".
//
// Before this task the gate was unresolved (the sub ran unconditionally) and
// the imprint was never recorded, so BOTH branches registered the static.
// Rashmi and Ragavan is in NO repo deck and NO legacy golden deck, so no
// chain head depends on this card (measured: grepping the carrier names
// against internal/testutil/decks/*.json returns nothing).

// rashmiTestEngine builds a 2-seat game with Rashmi and Ragavan plus a Sol
// Ring on seat 0's battlefield, an Ornithopter in seat 0's hand (the spell
// that trips the first-cast trigger), and the opponent's library topped with
// an Ornithopter (the exiled card; cmc 0 < the one artifact, so the free
// cast is offered). It returns the engine, config, Rashmi's id and the
// exiled opponent Ornithopter's id.
func rashmiTestEngine(t *testing.T, reg *cards.Registry) (*Engine, Config, state.ObjID, state.ObjID) {
	t.Helper()
	forest := searchCorpusCard(t, reg, "Forest")
	island := searchCorpusCard(t, reg, "Island")
	mountain := searchCorpusCard(t, reg, "Mountain")
	deck := []*cards.Card{
		searchCorpusCard(t, reg, "Rashmi and Ragavan"),
		searchCorpusCard(t, reg, "Sol Ring"),
		searchCorpusCard(t, reg, "Ornithopter"),
	}
	for i := 0; i < 8; i++ {
		deck = append(deck, forest, island)
	}
	for len(deck) < 40 {
		deck = append(deck, forest)
	}
	opp := []*cards.Card{searchCorpusCard(t, reg, "Ornithopter")}
	for len(opp) < 40 {
		opp = append(opp, mountain)
	}
	cfg := seatZeroStart(Config{Seed: 9211, Names: []string{"rashmi", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	rashmiID := searchMoveByName(t, e, "Rashmi and Ragavan", state.ZBattlefield)
	ringID := searchMoveByName(t, e, "Sol Ring", state.ZBattlefield)
	if ringID == 0 {
		t.Fatal("no Sol Ring on the battlefield")
	}
	myOrni := searchMoveByName(t, e, "Ornithopter", state.ZHand)
	// The opponent's Ornithopter must be the TOP card of their library when
	// the trigger's Dig resolves: if the opening hand took it, put it back
	// first, then pin the known top order.
	oppOrni := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZLibrary, 1) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Ornithopter" {
			oppOrni = id
		}
	}
	if oppOrni == 0 {
		// It went to the opponent's opening hand: return it to the library.
		found := false
		for _, id := range e.G.Zone(state.ZHand, 1) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Ornithopter" {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZLibrary})
				oppOrni = id
				found = true
				break
			}
		}
		if !found {
			t.Fatal("opponent Ornithopter absent from library and hand")
		}
	}
	order := []state.ObjID{oppOrni}
	for _, id := range e.G.Zone(state.ZLibrary, 1) {
		if id != oppOrni {
			order = append(order, id)
		}
	}
	e.emit(events.Event{Kind: events.LibraryOrder, Player: 1, IDs: order})
	// Cast seat 0's Ornithopter (cost 0): the first spell this turn, which
	// fires Rashmi's trigger.
	addMana(t, e, 0, "")
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == myOrni {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for the hand Ornithopter: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	// The trigger resolves above the spell: its Dig's ValidTgts$ Opponent
	// asks for the exile target first.
	d = passUntilNonPriority(t, e, 40)
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("after the cast: %+v, want the trigger's target ask", d)
	}
	tgtIdx := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			tgtIdx = o.Index
		}
	}
	if tgtIdx < 0 {
		t.Fatalf("target options = %+v, want the opponent among them", d.Options)
	}
	submitChoices(t, e, tgtIdx)
	// The Dig exiles the top card, the Treasure token is created, and the
	// DB$ Play's ask comes up: a single "Play Ornithopter" option, Min 0
	// (Optional$ True).
	d = passUntilNonPriority(t, e, 40)
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "play" {
		t.Fatalf("after the exile: %+v, want the DB$ Play ask", d)
	}
	if d.Player != 0 {
		t.Fatalf("play ask player = %d, want 0", d.Player)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != oppOrni {
		t.Fatalf("play options = %+v, want exactly the exiled Ornithopter %d", d.Options, oppOrni)
	}
	if d.Min != 0 {
		t.Fatalf("play ask Min = %d, want 0 (Optional$ True)", d.Min)
	}
	return e, cfg, rashmiID, oppOrni
}

// TestRashmiDeclinedPlayOffersTheFallbackCast is the "if you don't cast it
// this way" branch: the declined card stays in exile, nothing is imprinted,
// DBEffect's EQ0 gate registers the STPlay MayPlay static, and the
// mana-costed cast of the exiled card is offered this turn (and castable).
func TestRashmiDeclinedPlayOffersTheFallbackCast(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, rashmiID, oppOrni := rashmiTestEngine(t, reg)
	d := e.Pending()
	submitChoices(t, e) // the empty answer declines an Optional$ Play
	// The exiled card stayed in exile.
	if o := e.G.Obj(oppOrni); o.Zone != state.ZExile {
		t.Fatalf("declined card zone = %v, want exile", o.Zone)
	}
	// Nothing was imprinted on Rashmi.
	if o := e.G.Obj(rashmiID); len(o.Imprinted) != 0 {
		t.Fatalf("Rashmi's imprint list = %v, want empty", o.Imprinted)
	}
	// The EQ0 gate registered the MayPlay static from Rashmi.
	found := false
	for _, ce := range e.active() {
		if ce.MayPlay && ce.Source == rashmiID {
			found = true
		}
	}
	if !found {
		t.Fatalf("no MayPlay continuous effect from Rashmi after the decline: %+v", e.active())
	}
	// The fall-back cast is offered at priority.
	passUntilStackEmpty(t, e, 40)
	d = e.Pending()
	castIdx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == oppOrni {
			castIdx = o.Index
		}
	}
	if castIdx < 0 {
		t.Fatalf("no cast offer for the exiled Ornithopter: %+v", d.Options)
	}
	// ... and castable (cmc 0: no pool needed).
	submitChoices(t, e, castIdx)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(oppOrni); o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("cast exiled card zone/controller = %v/%d, want battlefield/0", o.Zone, o.Controller)
	}
	replayCheck(t, e, cfg)
}

// TestRashmiAcceptedPlayImprintsAndDeniesTheFallback is the "cast it this
// way" branch: the answered play begins the free cast, the card is recorded
// as imprinted on Rashmi (one events.Imprint), DBEffect's EQ0 gate denies
// the MayPlay static, and the cleanup clears the imprint list.
func TestRashmiAcceptedPlayImprintsAndDeniesTheFallback(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg, rashmiID, oppOrni := rashmiTestEngine(t, reg)
	submitChoices(t, e, 0) // play it
	passUntilStackEmpty(t, e, 40)
	// The card was played (free cast) and is now on seat 0's battlefield.
	if o := e.G.Obj(oppOrni); o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("played card zone/controller = %v/%d, want battlefield/0", o.Zone, o.Controller)
	}
	// Exactly one Imprint recorded the played card on Rashmi.
	imprints := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Imprint && ev.Obj == rashmiID && ev.Text == "" &&
			len(ev.IDs) == 1 && ev.IDs[0] == oppOrni {
			imprints++
		}
	}
	if imprints != 1 {
		t.Fatalf("Imprint events on Rashmi naming the played card = %d, want 1", imprints)
	}
	// The cleanup cleared the imprint list after the gate read it.
	if o := e.G.Obj(rashmiID); len(o.Imprinted) != 0 {
		t.Fatalf("Rashmi's imprint list after cleanup = %v, want empty", o.Imprinted)
	}
	// DBEffect was DENIED (imprint count 1 != EQ0): no MayPlay static from
	// Rashmi — the exiled card must not be offered a second cast.
	for _, ce := range e.active() {
		if ce.MayPlay && ce.Source == rashmiID {
			t.Fatalf("MayPlay static registered despite the played card: %+v", ce)
		}
	}
	passUntilStackEmpty(t, e, 40)
	d := e.Pending()
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == oppOrni {
			t.Fatalf("the played card was still offered for cast: %+v", d.Options)
		}
	}
	replayCheck(t, e, cfg)
}
