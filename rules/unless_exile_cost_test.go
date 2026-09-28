package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The Exile-head UnlessCost$ ticket. Grip of Amnesia is the corpus carrier:
// `A:SP$ Counter | ... | UnlessCost$ ExileFromGrave<1/All> | SubAbility$
// DBDraw` -- "Counter target spell unless its controller exiles all cards
// from their graveyard. Draw a card." Before this ticket ParseUnlessCost had
// no Exile arm, so the whole cost parsed to Cost{}, false, the resolution arm
// hard-declined, and the target spell's controller was NEVER offered a way to
// save the spell: the counter always succeeded even when the player wanted to
// exile their graveyard. The fix models the Exile head on the unless path.

// TestParseUnlessCostExileComponents pins the strict parser boundary for the
// Exile head: the fixed zone-headed forms the corpus carries are priceable
// (and carry the graveyard/hand zone the cast-cost parser assigns), while the
// spellings the exileCost regex does not cover -- ExileFromStack, bare Exile
// and an unfolded X amount -- stay hard declines.
func TestParseUnlessCostExileComponents(t *testing.T) {
	t.Parallel()
	if c, ok := ParseUnlessCost("ExileFromGrave<1/Card>"); !ok || len(c.Exile) != 1 ||
		c.Exile[0].N != 1 || c.Exile[0].Spec != "Card" || c.Exile[0].Zone != state.ZGraveyard {
		t.Fatalf("ExileFromGrave<1/Card> = %+v ok=%v, want one graveyard Card part", c, ok)
	}
	if c, ok := ParseUnlessCost("ExileFromGrave<1/All>"); !ok || len(c.Exile) != 1 ||
		c.Exile[0].N != 1 || c.Exile[0].Spec != "All" || c.Exile[0].Zone != state.ZGraveyard {
		t.Fatalf("ExileFromGrave<1/All> = %+v ok=%v, want one graveyard All part", c, ok)
	}
	if c, ok := ParseUnlessCost("ExileFromGrave<2/Card>"); !ok || c.Exile[0].N != 2 {
		t.Fatalf("ExileFromGrave<2/Card> = %+v ok=%v, want N=2", c, ok)
	}
	if c, ok := ParseUnlessCost("ExileAnyGrave<1/Card.TriggeredNewCard>"); !ok || len(c.Exile) != 1 ||
		c.Exile[0].Zone != state.ZGraveyard {
		t.Fatalf("ExileAnyGrave<1/...> = %+v ok=%v, want one graveyard part", c, ok)
	}
	// FromHand keeps the hand zone (CostPart.Zone zero reads as the hand).
	if c, ok := ParseUnlessCost("ExileFromHand<1/Card>"); !ok || c.Exile[0].Zone != 0 {
		t.Fatalf("ExileFromHand<1/Card> = %+v ok=%v, want the hand zone (zero)", c, ok)
	}
	// A ";" alternation folds to the "," MatchesSpec uses, like every other
	// component head.
	if c, ok := ParseUnlessCost("ExileFromGrave<1/Creature;Enchantment>"); !ok || c.Exile[0].Spec != "Creature,Enchantment" {
		t.Fatalf("alternation spec = %+v ok=%v, want folded Creature,Enchantment", c, ok)
	}
	// The Mandatory marker strips cleanly before the Exile arm (Egon, God of
	// Death carries both together).
	if c, ok := ParseUnlessCost("Mandatory ExileFromGrave<1/Card>"); !ok || len(c.Exile) != 1 {
		t.Fatalf("Mandatory ExileFromGrave<1/Card> = %+v ok=%v, want the marker stripped and one part", c, ok)
	}
	// Heads the exileCost regex does not cover stay hard declines.
	if _, ok := ParseUnlessCost("ExileFromStack<1/Card.Self>"); ok {
		t.Fatal("ExileFromStack priced; it must hard-decline")
	}
	if _, ok := ParseUnlessCost("Exile<1/Creature.!token>"); ok {
		t.Fatal("bare Exile priced; it must hard-decline")
	}
	if _, ok := ParseUnlessCost("ExileFromGrave<X/Card>"); ok {
		t.Fatal("an unfolded ExileFromGrave<X/...> priced; it must hard-decline")
	}
}

// gripOfAmnesiaFixture casts a creature (seat 0) and then Grip of Amnesia
// (seat 0) at it, target-selecting the creature spell, returning the engine,
// the countered creature's id, the Grip object id, and the ids of the cards
// seeded into seat 0's graveyard (graveCards of them).
func gripOfAmnesiaFixture(t *testing.T, graveCards int) (*Engine, state.ObjID, state.ObjID, []state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e, gripID, bearID := counterFixture(t, reg, "Grip of Amnesia", "Grizzly Bears")

	// Precondition for the whole-zone assertion: seed exactly graveCards into
	// seat 0's graveyard via logged events, so replay can rebuild the game.
	// counterFixture left the post-target priority pending; the MoveZone
	// events below do not disturb it, so drainUntilUnlessPay can continue.
	var seeded []state.ObjID
	for _, id := range append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...) {
		if len(seeded) >= graveCards {
			break
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZGraveyard})
		seeded = append(seeded, id)
	}
	if got := len(e.G.Zone(state.ZGraveyard, 0)); got != graveCards {
		t.Fatalf("fixture graveyard = %d cards, want %d", got, graveCards)
	}
	return e, bearID, gripID, seeded
}

// TestGripOfAmnesiaOffersAndPaysWholeGraveyardExile drives the real corpus
// card end to end: the counter's unless window must OFFER the exile as a
// payable option (not the decline-only list the defect produced), paying must
// exile EVERY card in the payer's graveyard (not a single pick), and the
// countered creature spell survives.
func TestGripOfAmnesiaOffersAndPaysWholeGraveyardExile(t *testing.T) {
	t.Parallel()
	e, bearID, _, seeded := gripOfAmnesiaFixture(t, 3)

	// Precondition: three graveyard cards, so "all" (3) is strictly more
	// than the one-card pick part.N spells.
	if len(seeded) != 3 {
		t.Fatalf("fixture seeded %d graveyard cards, want 3 so whole-zone differs from a single pick", len(seeded))
	}

	pay := drainUntilUnlessPay(t, e, 30)
	if pay == nil {
		t.Fatal("no unless_pay ask posed for Grip of Amnesia")
	}
	if len(pay.Options) != 2 {
		t.Fatalf("Grip's ExileFromGrave<1/All> window offered %d options (%+v), want pay+decline", len(pay.Options), pay.Options)
	}
	if pay.Player != 0 {
		t.Fatalf("unless_pay payer = seat %d, want the countered spell's controller (seat 0)", pay.Player)
	}

	mark := len(e.L.Events)
	submitChoices(t, e, pay.Options[0].Index)
	passUntilStackEmpty(t, e, 20)

	exiles := 0
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.MoveZone && ev.To == state.ZExile && ev.Text == "exiled as a cost" {
			exiles++
		}
	}
	if exiles != 3 {
		t.Fatalf("exile-as-cost events = %d, want 3 (the WHOLE graveyard, not a single pick)", exiles)
	}
	for _, id := range seeded {
		if z := e.G.Obj(id).Zone; z != state.ZExile {
			t.Fatalf("seeded graveyard card %d zone = %s, want the whole graveyard exiled", id, z)
		}
	}
	// Paying means the counter does NOT counter: the creature resolves onto
	// the battlefield.
	if z := e.G.Obj(bearID).Zone; z != state.ZBattlefield {
		t.Fatalf("paying the exile should save the spell: Bear zone = %s, want Battlefield", z)
	}
}

// TestGripOfAmnesiaDeclineCountersAndDraws is the mirror: declining leaves the
// seeded graveyard cards untouched, counters the spell, and still runs the
// SubAbility draw.
func TestGripOfAmnesiaDeclineCountersAndDraws(t *testing.T) {
	t.Parallel()
	e, bearID, _, seeded := gripOfAmnesiaFixture(t, 3)

	before := countDraw(e)
	pay := drainUntilUnlessPay(t, e, 30)
	if pay == nil {
		t.Fatal("no unless_pay ask posed for Grip of Amnesia")
	}
	// Precondition: the draw count is sampled before, so the decline branch's
	// own draw is observable as a change (asserted below).
	submitChoices(t, e, pay.Options[len(pay.Options)-1].Index)
	passUntilStackEmpty(t, e, 20)

	for _, id := range seeded {
		if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
			t.Fatalf("decline moved seeded graveyard card %d to %s, want the untouched graveyard", id, z)
		}
	}
	if z := e.G.Obj(bearID).Zone; z != state.ZGraveyard {
		t.Fatalf("declining should counter the spell: Bear zone = %s, want Graveyard", z)
	}
	if got := countDraw(e) - before; got != 1 {
		t.Fatalf("Grip's SubAbility drew %d cards, want 1", got)
	}
}

// TestGripOfAmnesiaEmptyGraveyardIsDeclineOnly pins the count half of the
// whole-zone read: the token still demands part.N (1) cards, so an empty
// graveyard cannot pay and the window offers only the decline.
func TestGripOfAmnesiaEmptyGraveyardIsDeclineOnly(t *testing.T) {
	t.Parallel()
	e, bearID, _, _ := gripOfAmnesiaFixture(t, 0)
	if got := len(e.G.Zone(state.ZGraveyard, 0)); got != 0 {
		t.Fatalf("fixture graveyard = %d, want 0", got)
	}

	pay := drainUntilUnlessPay(t, e, 30)
	if pay == nil {
		t.Fatal("no unless_pay ask posed for Grip of Amnesia")
	}
	// Precondition: the offer gate judged it unpayable, so only the decline
	// remains -- and this differs from the 3-card case above (2 options).
	if len(pay.Options) != 1 {
		t.Fatalf("an empty-graveyard exile offered %d options (%+v), want decline-only", len(pay.Options), pay.Options)
	}
	mark := len(e.L.Events)
	submitChoices(t, e, pay.Options[0].Index)
	passUntilStackEmpty(t, e, 20)
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.MoveZone && ev.To == state.ZExile && ev.Text == "exiled as a cost" {
			t.Fatalf("the unpayable exile still moved a card: %+v", ev)
		}
	}
	if z := e.G.Obj(bearID).Zone; z != state.ZGraveyard {
		t.Fatalf("an unpayable ExileFromGrave<1/All> must still counter: Bear zone = %s, want Graveyard", z)
	}
}

// TestPayUnlessCostRefusesExileSynchronously pins that the synchronous charge
// site never pays an Exile part for free: beginUnlessPayment owns it, exactly
// like a sacrifice or a discard.
func TestPayUnlessCostRefusesExileSynchronously(t *testing.T) {
	t.Parallel()
	e := stealEngine(t, 744)
	// Precondition: the payer has a graveyard card that a leaked synchronous
	// pay would have exiled.
	grave := e.G.Zone(state.ZGraveyard, 0)
	if len(grave) == 0 {
		// Seed one so the test cannot pass vacuously on an empty zone.
		for _, id := range append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...) {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZGraveyard})
			break
		}
	}
	if len(e.G.Zone(state.ZGraveyard, 0)) == 0 {
		t.Fatal("precondition: no graveyard card to protect")
	}
	cost := Cost{Exile: []CostPart{{N: 1, Spec: "Card", Zone: state.ZGraveyard}}}
	if e.payUnlessCost(0, cost, &effects.Ctx{Controller: 0}, 0) {
		t.Fatal("payUnlessCost paid an Exile part synchronously; beginUnlessPayment must own it")
	}
}
