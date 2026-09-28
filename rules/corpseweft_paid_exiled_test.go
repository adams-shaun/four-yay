package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// corpseweft_paid_exiled_test.go is the real-corpus end-to-end pin for
// Corpseweft's paid-exile count. The card's ability is
//
//	AB$ Token | Cost$ 1 B ExileFromGrave<X/Creature> | XMin$ 1
//	  | TokenScript$ b_x_x_zombie_horror | TokenPower$ Y | TokenToughness$ Y
//	  | TokenTapped$ True
//	SVar:Y:ExiledCards$Amount/Twice
//
// so with exactly one creature exiled as the cost, the tapped Zombie Horror
// must be 2/2 and survive state-based actions. The count reads the activation's
// PAID exile list (effects/count.go refTargets' `ExiledCards` referent); before
// the fix that body failed closed to zero, the mint's dynamic P/T set 0/0, and
// the token was swept to the ceased zone. The script text stays in the
// gitignored .cards/ tree and is never committed.

// TestCorpseweftPaidExiledCards extends the XMin pin beside it
// (TestCorpseweftXMinParamOffersNoZeroExileX) with the token's fate: one
// creature exiled as payment makes a tapped 2/2 that is still on the
// battlefield after the stack empties.
func TestCorpseweftPaidExiledCards(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	cw := mustCorpusCard(t, reg, "Corpseweft")
	// Precondition: the corpus card's Token ability really is the dynamic-P/T
	// shape this test is about -- SVar Y is the `ExiledCards$Amount/Twice`
	// body and both token sides name it.
	found := false
	for _, ab := range cw.Faces[0].Abilities {
		if ab.API != "Token" || ab.Params["TokenPower"] != "Y" || ab.Params["TokenToughness"] != "Y" {
			continue
		}
		if cw.Faces[0].SVars["Y"] != "ExiledCards$Amount/Twice" {
			t.Fatalf("corpus Corpseweft SVar Y = %q, want ExiledCards$Amount/Twice", cw.Faces[0].SVars["Y"])
		}
		found = true
	}
	if !found {
		t.Fatal("corpus Corpseweft lost its dynamic TokenPower$/TokenToughness$ Y ability -- the pin below is vacuous")
	}
	bearCard := card(t, xMinParamBearSrc)
	cfg := seatZeroStart(Config{Seed: 95, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{
			append([]*cards.Card{cw, bearCard}, mountainDeck(t, 38)...),
			mountainDeck(t, 40),
		}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	cwID := findByName(e, "Corpseweft", 0)
	if cwID == 0 {
		t.Fatal("corpus Corpseweft not in seat 0's zones")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: cwID, From: e.G.Obj(cwID).Zone, To: state.ZBattlefield})
	var bearID state.ObjID
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 0) {
			if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Bear" {
				bearID = id
			}
		}
	}
	if bearID == 0 {
		t.Fatal("fixture Bear not in seat 0's hand or library")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: bearID, From: e.G.Obj(bearID).Zone, To: state.ZGraveyard})
	e.pending = nil
	e.Advance()
	// Preconditions: exactly ONE exilable creature (so the only legal X is 1
	// and exactly one card is paid) and the source on the battlefield.
	if bear := e.G.Obj(bearID); bear == nil || bear.Zone != state.ZGraveyard {
		t.Fatalf("setup: Bear = %+v, want in the graveyard", bear)
	}
	graveyardCreatures := 0
	for _, id := range e.G.Zone(state.ZGraveyard, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && hasTypeWord(o.Face().Types, "Creature") {
			graveyardCreatures++
		}
	}
	if graveyardCreatures != 1 {
		t.Fatalf("setup: seat 0's graveyard holds %d creatures, want exactly the one exilable Bear", graveyardCreatures)
	}
	if cw := e.G.Obj(cwID); cw == nil || cw.Zone != state.ZBattlefield {
		t.Fatalf("setup: Corpseweft = %+v, want on the battlefield", cw)
	}
	addMana(t, e, 0, "CB")
	opt := abilityOption(t, e, cwID, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Prompt != "Choose a value for X" {
		t.Fatalf("X ask = %+v, want the announcement decision", d)
	}
	if len(d.Options) != 1 || d.Options[0].Amount != 1 {
		t.Fatalf("X options = %+v, want exactly X = 1", d.Options)
	}
	chooseX(t, e, 1)
	ex := e.Pending()
	if ex == nil || ex.Kind != decision.KChoose || len(ex.Options) != 1 || ex.Options[0].Obj != bearID {
		t.Fatalf("exile-cost ask = %+v, want exactly the graveyard Bear offered", ex)
	}
	submitChoices(t, e, ex.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(bearID).Zone; got != state.ZExile {
		t.Fatalf("Bear zone after paying = %s, want exile", got)
	}
	// The mint event ran: the ability resolved.
	minted := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.TokenCreate && ev.Text == "b_x_x_zombie_horror" {
			minted = true
		}
	}
	// The event's Obj is left unset when the mint went through a CreateToken
	// plan, so the event is only evidence the ability resolved; the minted
	// object itself is located on the battlefield below.
	if !minted {
		t.Fatal("no Zombie Horror token mint event: the ability never resolved")
	}
	// THE assertion: the actual token is on the battlefield (a 0/0 dynamic
	// side would have been swept by state-based actions), tapped, and reads
	// the paid count doubled.
	tokID := state.ObjID(0)
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.IsToken && o.Face() != nil && o.Face().Name == "Zombie Horror Token" {
			tokID = id
		}
	}
	if tokID == 0 {
		t.Fatalf("no Zombie Horror token on the battlefield after resolution: the dynamic P/T side read zero and the token was swept")
	}
	tok := e.G.Obj(tokID)
	// Precondition: the script's PRINTED P/T differs from the 2/2 under test,
	// so the assertion below is reading the derived value, not the face.
	printedPow, printedTgh := int32(tok.Face().Power()), int32(tok.Face().Toughness())
	if printedPow == 2 && printedTgh == 2 {
		t.Fatalf("precondition: the token script is printed %d/%d, so this test cannot distinguish the derived 2/2", printedPow, printedTgh)
	}
	if !tok.Tapped {
		t.Fatal("Zombie Horror token is untapped, want TokenTapped$ True")
	}
	if got := e.Power(tokID); got != 2 {
		t.Fatalf("Zombie Horror derived power = %d, want 2 (twice the one exiled card)", got)
	}
	if got := e.Toughness(tokID); got != 2 {
		t.Fatalf("Zombie Horror derived toughness = %d, want 2 (twice the one exiled card)", got)
	}
	replayCheck(t, e, cfg)
}
