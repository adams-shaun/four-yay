package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The PlayCost$ gate (task playcost1): a DB$ Play's PlayCost$ parameter is
// the "rather than paying its mana cost" alternative (CR 118.9), priced per
// chosen card with the ConvertedManaCost placeholder substituted by the
// card's own mana value. Pinned on the real corpus carriers in both
// directions -- the YES arm pays the alternative and never mana, an
// unpayable alternative hard-declines with a Note, and an unpriceable token
// (the corpus's one PlayCost$ SuspendCost, The Face of Boe) never degrades to
// a mana fallback.

// energyPlayFixture is the Amped Raptor cast-provenance fixture driven one
// step further: the raptor is cast from hand, the exile-until found the
// injected Grizzly Bears, and the resolution is suspended on the DB$ Play
// ask (the YES arm of the energy-cast offer). The pool is EMPTY (the
// {1}{R} the raptor itself cost was paid at castMode), and the entry
// trigger granted exactly 2 {E}.
func energyPlayFixture(t *testing.T) (*Engine, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	raptor := corpusAlternativeCard(t, "Amped Raptor")
	bear := corpusAlternativeCard(t, "Grizzly Bears")
	e, _ := castProvEngine(t, reg, raptor)
	bearID := putTopOfLibrary(t, e, bear, 0)
	raptorID := e.G.Zone(state.ZHand, 0)[0]
	// Raw ManaAdd emits (no priority re-ask): beginCast pays from the pool
	// directly, so the committed cast needs no live priority decision.
	for _, r := range "CR" {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: string(r), Amount: 1})
	}
	castMode(t, e, raptorID, "")
	resolveOffStack(t, e, raptorID)
	drainProvTriggers(t, e)
	if n := e.G.Players[0].Counter("ENERGY"); n != 2 {
		t.Fatalf("entry energy = %d, want 2 before the play ask", n)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "play" {
		t.Fatalf("expected the play ask after the exile-until, got %+v", d)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != bearID {
		t.Fatalf("the play ask offers %+v, want exactly the exiled found card", d.Options)
	}
	return e, bearID
}

func TestAmpedRaptorPlayCostDeclinesWhenTheEnergyCannotBePaid(t *testing.T) {
	t.Parallel()
	e, bearID := energyPlayFixture(t)
	// Strip the 2 {E} the entry granted: the payer cannot cover the
	// alternative, so the YES answer must NOT begin a cast that settles
	// short -- it declines with a loud Note and the card stays in exile.
	e.emit(events.Event{Kind: events.PlayerCounterChange, Player: 0, Counter: "ENERGY", Amount: -2})
	submitChoices(t, e, 0)
	passUntilStackEmpty(t, e, 60)
	if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZExile {
		t.Fatalf("the unpayable alternative still cast the card: zone %v", o)
	}
	if n := e.G.Players[0].Counter("ENERGY"); n != 0 {
		t.Fatalf("energy after the declined play = %d, want 0 (nothing partial was spent)", n)
	}
	if !hasNote(e, "alternative cost cannot be paid") {
		t.Fatalf("no Note named the unpayable alternative")
	}
}

func TestBeginPlayFixedGenericPlayCostPaysTheTokenNotTheManaValue(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bear := corpusAlternativeCard(t, "Grizzly Bears")
	e, _ := castProvEngine(t, reg, bear)
	bearID := e.G.Zone(state.ZHand, 0)[0]
	// Blue Mage's Cane's PlayCost$ 3 shape: a fixed generic token. The pool
	// holds exactly 3; the bear's mana value (2) must NOT be substituted --
	// the token is literal, so exactly 3 generic is charged.
	for range 3 {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "R", Amount: 1})
	}
	e.beginPlay(0, bearID, false, "3", false, false)
	if hasEvent(e, events.PutOnStack, bearID) == false {
		t.Fatalf("the fixed-generic PlayCost did not begin a cast (no stack push)")
	}
	if pool := e.G.Players[0].Pool.Total(); pool != 0 {
		t.Fatalf("mana pool after the {3} alternative = %d, want 0", pool)
	}
	resolveOffStack(t, e, bearID)
	if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the played card is %v, want on the battlefield", o)
	}
	if hasNote(e, "declined") {
		t.Fatalf("the payable fixed-generic alternative was declined")
	}
}

func TestBeginPlayPayLifePlayCostSubstitutesTheManaValue(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bear := corpusAlternativeCard(t, "Grizzly Bears")
	e, _ := castProvEngine(t, reg, bear)
	bearID := e.G.Zone(state.ZHand, 0)[0]
	// Anrakyr the Traveller's PlayCost$ PayLife<ConvertedManaCost> shape:
	// pay life equal to the card's mana value (2), never its mana cost.
	e.beginPlay(0, bearID, false, "PayLife<ConvertedManaCost>", false, false)
	if !hasEvent(e, events.PutOnStack, bearID) {
		t.Fatalf("the PayLife alternative did not begin a cast (no stack push)")
	}
	if life := e.G.Players[0].Life; life != 18 {
		t.Fatalf("life after the PayLife<Cmc> alternative = %d, want 18", life)
	}
	resolveOffStack(t, e, bearID)
	if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the played card is %v, want on the battlefield", o)
	}

	// A payer who cannot cover the substituted life declines: the cast is
	// never begun and the card stays in hand.
	e2, _ := castProvEngine(t, reg, bear)
	bear2 := e2.G.Zone(state.ZHand, 0)[0]
	e2.G.Players[0].Life = 1
	e2.beginPlay(0, bear2, false, "PayLife<ConvertedManaCost>", false, false)
	if hasEvent(e2, events.PutOnStack, bear2) {
		t.Fatalf("an unpayable PayLife alternative began a cast")
	}
	if o := e2.G.Obj(bear2); o == nil || o.Zone != state.ZHand {
		t.Fatalf("the declined PayLife play moved the card: %v", o)
	}
	if !hasNote(e2, "alternative cost cannot be paid") {
		t.Fatalf("no Note named the unpayable PayLife alternative")
	}
}

func TestPlayCostPricingAndHardDecline(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	bear := corpusAlternativeCard(t, "Grizzly Bears")

	// The pricing grammar: ConvertedManaCost substitutes the face's mana
	// value (2 for Grizzly Bears) and the token lands in the Cost field its
	// cast-flow consumer already reads.
	c, ok := pricePlayCost(bear.Faces[0], "PayEnergy<ConvertedManaCost>")
	if !ok || len(c.Energy) != 1 || c.Energy[0].N != 2 {
		t.Fatalf("pricePlayCost(PayEnergy<ConvertedManaCost>) = %+v, %v; want one Energy part N=2", c, ok)
	}
	c, ok = pricePlayCost(bear.Faces[0], "PayLife<ConvertedManaCost>")
	if !ok || c.Life != 2 {
		t.Fatalf("pricePlayCost(PayLife<ConvertedManaCost>) = %+v, %v; want Life=2", c, ok)
	}
	c, ok = pricePlayCost(bear.Faces[0], "3")
	if !ok || c.Generic != 3 {
		t.Fatalf("pricePlayCost(3) = %+v, %v; want Generic=3", c, ok)
	}
	c, ok = pricePlayCost(bear.Faces[0], "Discard<1/Card>")
	if !ok || len(c.Discard) != 1 || c.Discard[0].Spec != "Card" {
		t.Fatalf("pricePlayCost(Discard<1/Card>) = %+v, %v; want one Discard part Card", c, ok)
	}
	if _, ok := pricePlayCost(bear.Faces[0], "SuspendCost"); ok {
		t.Fatalf("pricePlayCost(SuspendCost) priced; want the hard decline")
	}
	if _, ok := pricePlayCost(bear.Faces[0], "LifeTotalHalfUp"); ok {
		t.Fatalf("pricePlayCost(LifeTotalHalfUp) priced; want the hard decline")
	}

	// beginPlay hard-declines an unpriceable token: no cast begun, one loud
	// Note naming the token, the card stays where it was.
	e, _ := castProvEngine(t, reg, bear)
	bearID := e.G.Zone(state.ZHand, 0)[0]
	before := len(e.L.Events)
	e.beginPlay(0, bearID, false, "SuspendCost", false, false)
	if e.cast != nil {
		t.Fatalf("an unpriceable PlayCost began a cast")
	}
	if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZHand {
		t.Fatalf("the declined play moved the card: zone %v", o)
	}
	if !hasNote(e, "SuspendCost") {
		t.Fatalf("no Note named the unpriceable PlayCost token")
	}
	notes := 0
	for _, ev := range e.L.Events[before:] {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "declined") {
			notes++
		}
	}
	if notes != 1 {
		t.Fatalf("unpriceable PlayCost emitted %d decline Notes, want 1", notes)
	}
}
