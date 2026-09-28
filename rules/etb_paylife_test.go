package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The "as CARDNAME enters, pay any amount of life" ETB replacement family
// (Minion of the Wastes, Phyrexian Processor, Nameless Race): the corpus's
// `R:Event$ Moved | Destination$ Battlefield | ReplaceWith$ PayLife` whose
// body is a `Cost$ Mandatory PayLife<X>` api:StoreSVar line. This test file
// pins the corrected behaviour end to end:
//
//   - the payer ANNOUNCES X at the entry boundary (CR 614.12 / CR 601.2b),
//     the announced amount is paid as real life (CR 118.3), and the body
//     stores it (events.StoreSVar) so the card's characteristic-defining
//     P/T (or its token's P/T) reads it back.
//
// Before the fix the body ran for free: an `unimplemented API StoreSVar`
// Note, no life paid, and the stored default (LifePaidOnETB:Number$0) read
// as 0 so the card entered as a 0/0.

// enterWithPayLife emits the entry Move for a card in seat p's hand and
// answers the pending paylife announcement with x life, returning the
// object id. The move is emitted directly (not through moveSeededCard,
// which clears the parked ask) so the entry-boundary decision is
// observable.
func enterWithPayLife(t *testing.T, e *Engine, p state.PlayerID, c *cards.Card, x int) state.ObjID {
	t.Helper()
	var id state.ObjID
	for _, z := range []state.Zone{state.ZLibrary, state.ZHand} {
		for _, zid := range e.G.Zone(z, p) {
			if o := e.G.Obj(zid); o != nil && o.Face() != nil && o.Face().Name == c.Faces[0].Name {
				id = zid
			}
		}
	}
	if id == 0 {
		t.Fatalf("card %q not in seat %d's library or hand", c.Faces[0].Name, p)
	}
	// A live engine (tokenReplGame) holds a priority decision pending at
	// main1; applyETBChoiceReplacement refuses to park an entry while another
	// decision is outstanding, so clear it first -- the established direct-
	// entry test shape (adventure_test.go, altcast_test.go).
	e.pending = nil
	from := e.G.Obj(id).Zone
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: from, To: state.ZBattlefield})
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "etb" {
		t.Fatalf("entry ask = %+v, want a paylife ETB choice", d)
	}
	if len(d.Options) == 0 || d.Options[0].Kind != "paylife" {
		t.Fatalf("entry ask options = %+v, want paylife options", d.Options)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Amount == x {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no %d-life option offered: %+v", x, d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit %d life: %v", x, err)
	}
	return id
}

// TestMinionOfTheWastesEntersWithPaidLifePT is the filing card: a real corpus
// card whose ETB replacement stores the paid life as its characteristic
// defining P/T. Paying 5 must cost 5 life and make it a 5/5; the printed
// default would make it a 0/0 (and the SBA would bin it).
func TestMinionOfTheWastesEntersWithPaidLifePT(t *testing.T) {
	t.Parallel()
	pc := tokenReplCorpusCard(t, "Minion of the Wastes")
	e := handEngine(t, pc)
	id := e.G.Zone(state.ZHand, 0)[0]
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: Minion zone = %v, want hand", o)
	}
	lifeBefore := e.G.Players[0].Life
	if lifeBefore != 20 {
		t.Fatalf("precondition: starting life = %d, want 20", lifeBefore)
	}
	got := enterWithPayLife(t, e, 0, pc, 5)
	if got != id {
		t.Fatalf("entered object = %d, want %d", got, id)
	}
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("after entry: zone = %v, want battlefield", o)
	}
	// Precondition for the P/T assertion: the two values under comparison
	// (paid 5 vs the printed default 0) differ, and the life total moved.
	if life := e.G.Players[0].Life; life != lifeBefore-5 {
		t.Fatalf("life = %d, want %d (paid 5)", life, lifeBefore-5)
	}
	if stored, ok := o.RuntimeSVars["LifePaidOnETB"]; !ok || stored != 5 {
		t.Fatalf("LifePaidOnETB = %d (present=%v), want 5", stored, ok)
	}
	if p, tt := e.derivedScalar(id); p != 5 || tt != 5 {
		t.Fatalf("Minion P/T = %d/%d, want 5/5", p, tt)
	}
	// The body ran for real (not the unimplemented-API fallback).
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == "unimplemented API StoreSVar" {
			t.Fatal("StoreSVar still hit the unimplemented-API fallback")
		}
	}
}

// TestPhyrexianProcessorTokenUsesPaidLife pins the stored value surviving
// the entry and being read later: the Processor's {4},{T} token ability
// creates an X/X Phyrexian Minion where X is the life paid as it entered.
func TestPhyrexianProcessorTokenUsesPaidLife(t *testing.T) {
	t.Parallel()
	pc := tokenReplCorpusCard(t, "Phyrexian Processor")
	e, _ := tokenReplGame(t, 3, pc)
	toMain1(t, e)
	lifeBefore := e.G.Players[0].Life
	if lifeBefore != 20 {
		t.Fatalf("precondition: starting life = %d, want 20", lifeBefore)
	}
	id := enterWithPayLife(t, e, 0, pc, 4)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Processor not on the battlefield: %v", o)
	}
	if life := e.G.Players[0].Life; life != lifeBefore-4 {
		t.Fatalf("life = %d, want %d (paid 4)", life, lifeBefore-4)
	}
	// Activate {4},{T}: fund the four generic and submit the ability option.
	addMana(t, e, 0, "CCCC")
	opt := abilityOption(t, e, id, 0)
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	var tok *state.Object
	for _, zid := range e.G.Zone(state.ZBattlefield, 0) {
		if zz := e.G.Obj(zid); zz != nil && zz.Face() != nil && zz.Face().Name == "Phyrexian Minion Token" {
			tok = zz
		}
	}
	if tok == nil {
		t.Fatal("no Phyrexian Minion token was created")
	}
	// Precondition: the token script is a */* CDA face, so the printed read
	// is 0 and differs from the 4 the paid life must set.
	if tok.Face().Power() != 0 || tok.Face().Toughness() != 0 {
		t.Fatalf("precondition: token printed P/T = %d/%d, want the */* script's 0/0",
			tok.Face().Power(), tok.Face().Toughness())
	}
	if p, tt := e.derivedScalar(tok.ID); p != 4 || tt != 4 {
		t.Fatalf("token P/T = %d/%d, want 4/4 (the life paid as the Processor entered)", p, tt)
	}
}

// TestNamelessRacePayLifeIsCappedByXMax pins the bounded announcement: the
// card's `XMax$ Limit` caps X at the number of white nontoken permanents
// and white cards in opponents' graveyards (Oracle text). One of each gives
// a bound of 2, so the offered range is exactly 0..2 and paying 2 makes it
// a 2/2.
func TestNamelessRacePayLifeIsCappedByXMax(t *testing.T) {
	t.Parallel()
	nr := tokenReplCorpusCard(t, "Nameless Race")
	e := handEngine(t, nr)
	white := card(t, "Name:White Bear\nTypes:Creature Bear\nColors:White\nPT:2/2\nOracle:x\n")
	// One white nontoken permanent on the opponent's battlefield.
	perm := e.G.AddObject(white, 1)
	perm.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 1, append(e.G.Zone(state.ZBattlefield, 1), perm.ID))
	// One white card in the opponent's graveyard.
	grave := e.G.AddObject(white, 1)
	grave.Zone = state.ZGraveyard
	e.G.SetZone(state.ZGraveyard, 1, append(e.G.Zone(state.ZGraveyard, 1), grave.ID))

	id := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	d := e.Pending()
	if d == nil || len(d.Options) != 3 {
		t.Fatalf("precondition: options = %+v, want exactly 0..2 (XMax = 1 permanent + 1 card)", d)
	}
	want := 2
	idx := -1
	for _, o := range d.Options {
		if o.Amount == want {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no %d-life option: %+v", want, d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit %d life: %v", want, err)
	}
	if life := e.G.Players[0].Life; life != 18 {
		t.Fatalf("life = %d, want 18 (paid 2)", life)
	}
	if p, tt := e.derivedScalar(id); p != int32(want) || tt != int32(want) {
		t.Fatalf("Nameless Race P/T = %d/%d, want %d/%d", p, tt, want, want)
	}
}
