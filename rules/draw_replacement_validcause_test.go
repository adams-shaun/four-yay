package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// R:Event$ Draw ValidCause$ — the scoped-draw lane. Unpredictable Cyclone
// ("If a cycling ability of another nonland card would cause you to draw a
// card, instead exile cards from the top of your library until you exile a
// card that shares a card type with the cycled card...") is the corpus's only
// Draw-replacement line carrying ValidCause$, and the Draw matcher read every
// other parameter of the class but not this one: the replacement applied to
// EVERY draw its controller made — draw-step draws, "draw a card" spells,
// anything — because the cause was never consulted.

// cycloneSetup is the shared fixture: seat 0 at Main 1 with Unpredictable
// Cyclone on the battlefield and `top` seeded as seat 0's next library card.
func cycloneSetup(t *testing.T, top *cards.Card) (*Engine, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 743)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Unpredictable Cyclone"))
	topID := setupDrawLibrary(t, e, 0, top)
	return e, topID
}

// TestUnpredictableCycloneOrdinaryDrawIsNotReplaced is the negative leaf the
// report's probe measured as FAILING before the fix: with Unpredictable
// Cyclone out, a plain draw (no spell or ability on the stack) is not caused
// by any cycling ability, so it must NOT be replaced — exactly one Draw event
// is emitted and the card reaches the hand.
func TestUnpredictableCycloneOrdinaryDrawIsNotReplaced(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, top := cycloneSetup(t, mustCorpusCard(t, reg, "Grizzly Bears"))

	draws := countDraw(e)
	emitDraw(t, e, 0)
	if got := countDraw(e) - draws; got != 1 {
		t.Fatalf("ordinary draw events = %d, want 1 (Unpredictable Cyclone must not replace it)", got)
	}
	if o := e.G.Obj(top); o == nil || o.Zone != state.ZHand {
		t.Fatalf("ordinarily drawn card zone = %v, want hand", o)
	}
}

// TestUnpredictableCycloneCyclingDrawIsReplaced is the positive leaf: cycling
// a real nonland corpus card (Violent Impact, a Sorcery with K:Cycling:2)
// while Unpredictable Cyclone is out IS replaced — 0 Draw events for the
// cycle — and the DigUntil body runs, exiling the found card (FoundDestination$
// Exile) instead of drawing it. The cause is the minted cycling ability, whose
// Ability.Params["Keyword"] is "Cycling", and whose source card (Violent
// Impact) is a nonland.
func TestUnpredictableCycloneCyclingDrawIsReplaced(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, mustCorpusCard(t, reg, "Violent Impact"))
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Unpredictable Cyclone"))
	// Seed a Sorcery on top so the dig's "shares a card type with the cycled
	// card" filter deterministically finds exactly one card.
	found := setupDrawLibrary(t, e, 0, mustCorpusCard(t, reg, "Mind Rot"))
	id := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC] = 3

	opt := cyclingOption(t, e, id)
	draws := countDraw(e)
	e.beginActivation(0, opt)
	submitChoices(t, e, 0) // discard Violent Impact as the cycling cost
	if e.G.Obj(id).Zone != state.ZGraveyard {
		t.Fatalf("cycled card zone = %s, want graveyard", e.G.Obj(id).Zone)
	}
	e.resolveTop()

	if got := countDraw(e) - draws; got != 0 {
		t.Fatalf("cycling draw events = %d, want 0 (the cycling draw is replaced)", got)
	}
	// The DigUntil body ran: it exiled the card matching the cycled card's
	// type (FoundDestination$ Exile). The body's own DBRestRandomOrder then
	// returns the remembered cards to the library bottom, so the card is not
	// in exile at the end — the proof is the exile MOVE in the log.
	if !movedToExile(e, found) {
		t.Fatalf("the DigUntil body never exiled the found %q (draw was dropped, not replaced)",
			e.G.Obj(found).Face().Name)
	}
}

// TestUnpredictableCycloneNonCyclingAbilityDrawIsNotReplaced guards the cause
// gate from being "any activated ability": a draw caused by an ordinary
// activated ability of a nonland card (an inline artifact's "{T}: Draw a
// card") is NOT caused by a cycling ability, so it is not replaced.
func TestUnpredictableCycloneNonCyclingAbilityDrawIsNotReplaced(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	drawer := card(t, "Name:Drawer\nManaCost:1\nTypes:Artifact\n"+
		"A:AB$ Draw | Cost$ T | NumCards$ 1 | SpellDescription$ Draw a card.\nOracle:x\n")
	e := handEngine(t, drawer)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Unpredictable Cyclone"))
	id := e.G.Zone(state.ZHand, 0)[0]
	// Put the artifact straight onto the battlefield (handEngine leaves it in
	// hand; the {T} activation is what we need to drive).
	e.G.SetZone(state.ZHand, 0, nil)
	o := e.G.Obj(id)
	o.Zone = state.ZBattlefield
	o.SummonSick = false
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), id))
	e.staticEpoch, e.activeEpoch = -1, -1

	opt := cyclingOption(t, e, id)
	draws := countDraw(e)
	e.beginActivation(0, opt)
	e.resolveTop()
	if got := countDraw(e) - draws; got != 1 {
		t.Fatalf("non-cycling ability draw events = %d, want 1 (only a CYCLING cause admits the replacement)", got)
	}
}

// TestUnpredictableCycloneLandCyclingIsNotReplaced guards the other qualifier:
// Ziatora's Proving Ground is a LAND with cycling, so its cycling ability's
// source card is not a nonland and the `nonLand` qualifier must withhold the
// replacement — the draw happens normally.
func TestUnpredictableCycloneLandCyclingIsNotReplaced(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, mustCorpusCard(t, reg, "Ziatora's Proving Ground"))
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Unpredictable Cyclone"))
	id := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC] = 3

	opt := cyclingOption(t, e, id)
	draws := countDraw(e)
	e.beginActivation(0, opt)
	submitChoices(t, e, 0)
	e.resolveTop()
	if got := countDraw(e) - draws; got != 1 {
		t.Fatalf("land-cycling draw events = %d, want 1 (nonLand withholds the replacement)", got)
	}
}

// movedToExile reports whether obj has a logged MoveZone into exile.
func movedToExile(e *Engine, obj state.ObjID) bool {
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == obj && ev.To == state.ZExile {
			return true
		}
	}
	return false
}

// cyclingOption returns the "ability" priority option for obj, failing the
// test if none is offered (the cycling activation's own offer gate).
func cyclingOption(t *testing.T, e *Engine, obj state.ObjID) decision.Option {
	t.Helper()
	for _, o := range e.legalActions(e.G.Priority) {
		if o.Kind == "ability" && o.Obj == obj {
			return o
		}
	}
	t.Fatalf("no ability option offered for obj %d", obj)
	return decision.Option{}
}
