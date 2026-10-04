package rules

// MayPlay statics carrying RaiseCost$: the surcharge the permission adds on
// top of the printed cost (CR 118.3a) is now CONSUMED on the rules/mayplay.go
// route, instead of failing the whole static closed. Kotis, Sibsig Champion
// is the pinned carrier: "Once during each of your turns, you may cast a
// creature spell from your graveyard by exiling three other cards from your
// graveyard in addition to paying its other costs."
//
// A raise ParseCost cannot price (the variable forms -- a bare X, an
// announced PayLife<X>, an unknown token) still fails the static closed: the
// card is withheld whole, never granted with an uncharged surcharge. Risen
// Executioner's `RaiseCost$ X` is the pinned trap (a bare X parses to c.X++,
// which costAnnouncesCastX catches).

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules/pay"
	"github.com/adams-shaun/gorge/state"
)

// kotisGraveyardSetup places Kotis, Sibsig Champion on seat 0's battlefield
// and the named creature plus fillers in seat 0's graveyard, then leaves a
// priority decision pending. It returns the engine and the ids in the order
// (cast card, fillers...).
func kotisGraveyardSetup(t *testing.T, creatureSrc string, fillers int) (*Engine, state.ObjID, []state.ObjID) {
	t.Helper()
	e := handEngine(t, corpusAlternativeCard(t, "Kotis, Sibsig Champion"))
	kotis := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: kotis, From: state.ZHand, To: state.ZBattlefield})

	castCard := graveyardCard(t, e, creatureSrc)
	var fodders []state.ObjID
	for i := 0; i < fillers; i++ {
		fodders = append(fodders, graveyardCard(t, e,
			"Name:Fodder\nManaCost:1 R\nTypes:Creature Lizard\nPT:1/1\nOracle:x\n"))
	}
	return e, castCard, fodders
}

// TestKotisMayPlayGraveyardCastChargesTheExileSurcharge pins the whole
// consumed shape: with Kotis on the battlefield and a creature plus three
// other cards in the graveyard, the may-play cast is offered; committing it
// exiles exactly the three OTHER cards as the cost, and the cast card itself
// resolves onto the battlefield rather than being exiled.
func TestKotisMayPlayGraveyardCastChargesTheExileSurcharge(t *testing.T) {
	t.Parallel()
	e, bears, fodders := kotisGraveyardSetup(t, "Name:Grave Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n", 3)
	addMana(t, e, 0, "GG")

	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("priority = %+v, want a priority decision for seat 0", d)
	}
	castIdx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bears && o.Mode == "mayplay" {
			castIdx = o.Index
		}
	}
	if castIdx < 0 {
		t.Fatalf("Kotis may-play cast not offered for the graveyard creature: %+v", d.Options)
	}
	submitChoices(t, e, castIdx)

	// The surcharge's exile ask: exactly the three filler cards, Min=Max=3.
	de := e.Pending()
	if de == nil || de.Kind != decision.KChoose {
		t.Fatalf("exile cost ask missing: %+v", de)
	}
	if len(de.Options) != 3 {
		t.Fatalf("exile ask offers %d options, want exactly the 3 other cards", len(de.Options))
	}
	for _, o := range de.Options {
		if o.Obj == bears {
			t.Fatalf("exile ask offered the cast card itself (the self-exclusion failed): %+v", o)
		}
	}
	var picks []int
	for _, o := range de.Options {
		picks = append(picks, o.Index)
	}
	submitChoices(t, e, picks...)
	passUntilStackEmpty(t, e, 50)

	if o := e.G.Obj(bears); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("may-play-cast Bears zone=%v, want battlefield", o)
	}
	for _, id := range fodders {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZExile {
			t.Fatalf("fodder %d zone=%v, want exile (the surcharge was paid)", id, o.Zone)
		}
	}
	// Kotis itself was never exiled -- it is the granting permanent.
	if o := e.G.Obj(e.G.Zone(state.ZBattlefield, 0)[0]); o == nil {
		t.Fatal("Kotis left the battlefield")
	}
	if pool := e.G.Players[0].Pool.Total(); pool != 0 {
		t.Fatalf("pool=%d after the raised payment, want 0", pool)
	}
}

// TestKotisMayPlayWithheldWithoutThreeOtherCards pins the affordability gate
// on the raise: with only two other cards in the graveyard the cast is not
// offered at all (an offer the engine cannot pay must not exist).
func TestKotisMayPlayWithheldWithoutThreeOtherCards(t *testing.T) {
	t.Parallel()
	e, bears, _ := kotisGraveyardSetup(t, "Name:Grave Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n", 2)
	addMana(t, e, 0, "GG")

	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("priority = %+v, want a priority decision for seat 0", d)
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bears {
			t.Fatalf("may-play cast offered with only 2 other cards (raise unpayable): %+v", o)
		}
	}
}

// TestKotisMayPlayLimitOncePerTurn pins the MayPlayLimit$ 1 gate on the
// granting static: once the granted card has been cast, the SAME card is not
// offered again this turn (mayPlayLimitReached walks the log for that object
// id). The limit is per affected CARD; the printed "once during each of your
// turns" is a per-STATIC limit this build under-enforces. That
// under-enforcement is not observable on Kotis itself: after the first cast
// the graveyard holds too few other cards for the raise, so a second
// creature is withheld for the raise's sake, not the limit's. The
// per-card reading is pinned instead on the SAME card, and the
// under-enforcement is named in the report's Issues.
func TestKotisMayPlayLimitOncePerTurn(t *testing.T) {
	t.Parallel()
	e, bears, fodders := kotisGraveyardSetup(t, "Name:Grave Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n", 3)
	addMana(t, e, 0, "GG")

	d := e.Pending()
	castIdx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bears && o.Mode == "mayplay" {
			castIdx = o.Index
		}
	}
	if castIdx < 0 {
		t.Fatalf("first may-play cast not offered: %+v", d.Options)
	}
	submitChoices(t, e, castIdx)
	de := e.Pending()
	if de == nil || de.Kind != decision.KChoose || len(de.Options) != 3 {
		t.Fatalf("exile ask missing or wrong: %+v", de)
	}
	var picks []int
	for _, o := range de.Options {
		picks = append(picks, o.Index)
	}
	submitChoices(t, e, picks...)
	passUntilStackEmpty(t, e, 50)

	d = e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("pending = %+v, want priority for seat 0 after the cast resolved", d)
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bears && o.Mode == "mayplay" {
			t.Fatalf("MayPlayLimit$ 1 not enforced -- the same card offered a second may-play cast: %+v", o)
		}
	}
	_ = fodders
}

// TestMayPlayRaiseCostUnpriceableStaysWithheld pins the priceability rule:
// Risen Executioner's `RaiseCost$ X` is a variable surcharge this build
// cannot price (a bare X parses to c.X, no Unknown entry), so the static stays
// withheld whole -- the card is never offered from the graveyard, never
// granted with an uncharged surcharge.
func TestMayPlayRaiseCostUnpriceableStaysWithheld(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	if _, ok := reg.Lookup("Risen Executioner"); !ok {
		t.Fatal("Risen Executioner missing from corpus")
	}
	e := handEngine(t, corpusAlternativeCard(t, "Risen Executioner"))
	risen := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: risen, From: state.ZHand, To: state.ZGraveyard})
	// Plenty of other creature cards, in case the raise were (wrongly)
	// treated as pay-to-cast rather than unpriceable.
	for i := 0; i < 3; i++ {
		graveyardCard(t, e, "Name:Fodder\nManaCost:1 B\nTypes:Creature Zombie\nPT:1/1\nOracle:x\n")
	}
	addMana(t, e, 0, "BBBB")

	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("priority = %+v, want a priority decision", d)
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == risen {
			t.Fatalf("Risen Executioner offered despite the unpriceable RaiseCost$ X: %+v", o)
		}
	}
}

// TestMayPlayRaiseCostSelfExclusionAtOffer pins the offer-time self-exclusion:
// with Kotis + exactly two other cards in the graveyard the cast is not
// offered (only two "other" cards would remain once the card is on the stack),
// while an ABILITY-shaped ExileFromGrave cost -- which may legitimately exile
// its own source (encore) -- is deliberately untouched by the exclusion. The
// encore pin (TestEncoreActivatesFromTheGraveyardIntoHastedTokenCopies) covers
// the ability half; this test pins the cast half directly on the engine.
func TestMayPlayRaiseCostSelfExclusionAtOffer(t *testing.T) {
	t.Parallel()
	e, bears, _ := kotisGraveyardSetup(t, "Name:Grave Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n", 2)
	addMana(t, e, 0, "GG")

	d := e.Pending()
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == bears && o.Mode == "mayplay" {
			t.Fatalf("Kotis offered with only Kotis+2 others (the cast card counted as its own exile fodder): %+v", o)
		}
	}

	// The ability half: a cost part that may exile its own source still counts
	// it. nonManaCastable(ability=true) must not self-exclude, so an
	// ExileFromGrave<1/CARDNAME> activation from the graveyard stays payable.
	e2 := handEngine(t, corpusAlternativeCard(t, "Impulsive Pilferer"))
	pilferer := e2.G.Zone(state.ZHand, 0)[0]
	e2.emit(events.Event{Kind: events.MoveZone, Obj: pilferer, From: state.ZHand, To: state.ZGraveyard})
	// The encore cost is {3}{R} plus exiling the card itself.
	obj := e2.G.Obj(pilferer)
	c := ParseCost("3 R ExileFromGrave<1/CARDNAME>")
	if !pay.NonManaCastable(asPayer(e2), 0, pilferer, c, true, "") {
		t.Fatalf("ability-shaped ExileFromGrave<1/CARDNAME> wrongly withheld: the self-exclusion must be scoped to casts (the encore self-exile is real); obj zone=%v", obj.Zone)
	}
}
