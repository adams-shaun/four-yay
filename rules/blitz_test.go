package rules

// kw:Blitz (CR 702.152) proof tests, driven by the real corpus card Sabin,
// Master Monk (`.cards/cardsfolder/s/sabin_master_monk.txt`):
//
//	K:Blitz:2 R R Discard<1/Card>
//	S:Mode$ Continuous | Affected$ Card.Self | MayPlay$ True |
//	  ValidSA$ Spell.Blitz | AffectedZone$ Graveyard | EffectZone$ Graveyard
//
// Sabin is the discriminating corpus form: its blitz cost is the only one
// carrying a non-mana part (Discard<1/Card>), and it is one of the two cards
// whose own static grants the graveyard blitz cast. Mezzio Mugger (K:Blitz:2 R,
// no discard, no graveyard static) is the control for the pure-mana form.
//
// The fixtures are never corpus .txt files (licensing): the discard fodder and
// the mountains come from mountainDeck/hand-authored cards.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// blitzEngine is altCostEngine with Sabin, Master Monk in seat 0's deck; the
// returned cfg lets every test replayCheck.
func blitzEngine(t *testing.T, seed uint64) (*Engine, Config) {
	t.Helper()
	e, cfg, _ := altCostEngine(t, seed, []string{"Sabin, Master Monk"}, nil, nil)
	return e, cfg
}

// castSabinBlitzed drives the end-to-end blitz cast from hand: it funds
// exactly the printed blitz cost {2}{R}{R} plus a hand card to discard, picks
// the (blitzed) option, pays the discard ask, drains the stack and returns
// the permanent's id. It asserts the offer existed (the mode is only ever
// produced by the Blitz path), that the cost was paid from the blitz pool, and
// that the provenance flag landed.
func castSabinBlitzed(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	id := findCardObj(t, e, 0, "Sabin, Master Monk", state.ZHand)
	// Precondition: the parser really kept the Blitz keyword with its full
	// parameter, or the offer below could never appear.
	f := e.G.Obj(id).Face()
	if !f.HasKeyword("Blitz") {
		t.Fatalf("setup: Sabin face lost the Blitz keyword: %v", f.Keywords)
	}
	if got, _ := f.KeywordParam("Blitz"); got != "2 R R Discard<1/Card>" {
		t.Fatalf("setup: Blitz param = %q, want the Discard form", got)
	}
	// {2}{R}{R} exactly -- not the printed {4}{R}: the (blitzed) offer exists
	// only because beginCast's "blitzed" case charged the keyword cost.
	addMana(t, e, 0, "RRCC")
	submitChoices(t, e, castModeOption(t, e, id, "blitzed"))
	// The Discard<1/Card> additional cost ask: exactly one hand card.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("blitz discard ask missing: %+v", d)
	}
	// Min=Max=1: exactly one card, any card (the Discard<1/Card> part). The
	// hand holds several Mountains, so the option LIST is longer than one --
	// the count question is Min/Max, not len(Options).
	if d.Min != 1 || d.Max != 1 || len(d.Options) == 0 {
		t.Fatalf("blitz discard ask = min %d max %d opts %d, want min/max 1 with options", d.Min, d.Max, len(d.Options))
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 40)
	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("blitzed Sabin in %s, want battlefield", o.Zone)
	}
	if o.CastFlags&state.FlagBlitzed == 0 {
		t.Fatalf("blitzed cast carries no FlagBlitzed: %+v", o.CastFlags)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("blitzed cast left %d mana; the blitz cost was not charged", got)
	}
	return id
}

// TestSabinBlitzCastsForItsCostDiscardsAndGainsHaste pins the offer/charge and
// the CR 702.152c haste rider on the real corpus card.
func TestSabinBlitzCastsForItsCostDiscardsAndGainsHaste(t *testing.T) {
	e, cfg := blitzEngine(t, 971)
	id := castSabinBlitzed(t, e)
	// Precondition: the haste assertion must be about a creature that entered
	// through the blitz cast, which the flag above proves.
	if !e.HasKeyword(id, "Haste") {
		t.Fatal("blitzed Sabin has no haste")
	}
	replayCheck(t, e, cfg)
}

// TestBlitzedCreatureDrawsWhenItDies pins CR 702.152c's "When this creature
// dies, draw a card": the runtime AddTrigger grant registered by blitzEnter
// fires for its own permanent's death and draws for the controller.
func TestBlitzedCreatureDrawsWhenItDies(t *testing.T) {
	e, cfg := blitzEngine(t, 972)
	id := castSabinBlitzed(t, e)
	// Precondition: the death trigger must be registered while the permanent
	// is alive -- a grant that never existed would make the draw vacuous.
	grants := 0
	for _, ce := range e.active() {
		if ce.AddTrigger != nil && ce.Source == id {
			grants++
		}
	}
	if grants != 1 {
		t.Fatalf("blitzed Sabin has %d dies-draw grants, want 1", grants)
	}
	// Precondition: both libraries still hold cards, so "drew a card" can
	// fail loudly rather than pass on an empty library.
	if len(e.G.Zone(state.ZLibrary, 0)) == 0 || len(e.G.Zone(state.ZLibrary, 1)) == 0 {
		t.Fatal("setup: a library is empty; the draw assertion could not fail")
	}
	hand0, hand1 := len(e.G.Zone(state.ZHand, 0)), len(e.G.Zone(state.ZHand, 1))

	// Kill it: lethal damage through the ordinary SBA path (the afterlife
	// test's shape).
	e.emit(events.Event{Kind: events.Damage, Obj: id, Amount: 99})
	e.checkStateBased()
	e.priorityRound()
	passUntilStackEmpty(t, e, 20)

	if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
		t.Fatalf("dead Sabin in %s, want graveyard (the draw assertion is vacuous)", got)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != hand0+1 {
		t.Fatalf("controller hand after the death = %d, want %d (the dies-draw did not fire)", got, hand0+1)
	}
	if got := len(e.G.Zone(state.ZHand, 1)); got != hand1 {
		t.Fatalf("opponent hand after the death = %d, want %d (the wrong player drew)", got, hand1)
	}
	_ = cfg
}

// TestBlitzedCreatureIsSacrificedAtTheNextEndStep pins CR 702.152d: the
// delayed sacrifice registered by blitzEnter resolves at the end step of the
// turn it entered and sends it to the graveyard.
func TestBlitzedCreatureIsSacrificedAtTheNextEndStep(t *testing.T) {
	e, cfg := blitzEngine(t, 973)
	id := castSabinBlitzed(t, e)
	// Precondition: the end-step sacrifice was registered at entry.
	found := false
	for _, d := range e.G.Delayed {
		if d.Source == id && d.Execute == "__kwBlitzSacrifice" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no __kwBlitzSacrifice registration for %d: %+v", id, e.G.Delayed)
	}
	driveToStepAll(t, e, e.G.Turn, 0, state.StepEnd)
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
		t.Fatalf("Sabin at the end step = %s, want graveyard (not sacrificed)", got)
	}
	if len(e.G.Delayed) != 0 {
		t.Fatalf("blitz sacrifice registration not consumed: %d", len(e.G.Delayed))
	}
	replayCheck(t, e, cfg)
}

// TestSabinGraveyardBlitzCastViaItsOwnStatic pins the graveyard permission:
// Sabin's own S: static (MayPlay$ True | ValidSA$ Spell.Blitz | AffectedZone$
// Graveyard) makes the blitz cast offered from the graveyard, and only the
// blitz cast -- the plain printed-cost cast is NOT granted.
func TestSabinGraveyardBlitzCastViaItsOwnStatic(t *testing.T) {
	e, cfg := blitzEngine(t, 974)
	id := findCardObj(t, e, 0, "Sabin, Master Monk", state.ZGraveyard)
	if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
		t.Fatalf("setup: card zone = %v, want graveyard", got)
	}
	// Precondition: the grant really is live for the card in the graveyard.
	if _, ok := e.mayPlayGrant(0, id); !ok {
		t.Fatal("setup: Sabin's own MayPlay$ Spell.Blitz static does not grant from the graveyard")
	}
	// Precondition: a hand card exists for the Discard<1/Card> part, so the
	// offer below is withheld for the right reason if it is missing.
	if len(e.G.Zone(state.ZHand, 0)) == 0 {
		t.Fatal("setup: empty hand; the blitz discard could not be paid")
	}
	addMana(t, e, 0, "RRCC")
	// Exactly one cast option: the blitzed one. The static names Spell.Blitz,
	// not the plain cast, so there must be no plain mayplay offer.
	blitzed, plain := 0, 0
	for _, o := range e.Pending().Options {
		if o.Kind != "cast" || o.Obj != id {
			continue
		}
		switch o.Mode {
		case "blitzed":
			blitzed++
		case "mayplay", "":
			plain++
		}
	}
	if blitzed != 1 {
		t.Fatalf("graveyard blitz option count = %d, want 1: %+v", blitzed, e.Pending().Options)
	}
	if plain != 0 {
		t.Fatalf("plain graveyard cast offered despite ValidSA$ Spell.Blitz: %+v", e.Pending().Options)
	}
	submitChoices(t, e, castModeOption(t, e, id, "blitzed"))
	submitChoices(t, e, 0) // pay the discard
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(id); o.Zone != state.ZBattlefield || o.CastFlags&state.FlagBlitzed == 0 {
		t.Fatalf("graveyard blitz cast: zone=%s flags=%d", o.Zone, o.CastFlags)
	}
	if o := e.G.Obj(id); o.Zone != state.ZBattlefield || o.CastFlags&state.FlagBlitzed == 0 {
		t.Fatalf("graveyard blitz cast: zone=%s flags=%d", o.Zone, o.CastFlags)
	}
	if !e.HasKeyword(id, "Haste") {
		t.Fatal("graveyard-blitzed Sabin has no haste")
	}
	replayCheck(t, e, cfg)
}

// TestBlitzGraveyardOfferAbsentWithoutTheStatic is the control: a Blitz
// creature in the graveyard with NO MayPlay static gets no graveyard blitz
// offer, so the offer above is attributable to Sabin's static rather than to
// the keyword alone.
func TestBlitzGraveyardOfferAbsentWithoutTheStatic(t *testing.T) {
	e, _, _ := altCostEngine(t, 975, []string{"Mezzio Mugger"}, nil, nil)
	id := findCardObj(t, e, 0, "Mezzio Mugger", state.ZGraveyard)
	if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
		t.Fatalf("setup: card zone = %v, want graveyard", got)
	}
	if !e.G.Obj(id).Face().HasKeyword("Blitz") {
		t.Fatal("setup: Mezzio Mugger lost its Blitz keyword")
	}
	if _, ok := e.mayPlayGrant(0, id); ok {
		t.Fatal("setup: Mezzio Mugger unexpectedly has a graveyard may-play grant")
	}
	addMana(t, e, 0, "RRCC")
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == id {
			t.Fatalf("graveyard cast offered without a MayPlay static: %+v", o)
		}
	}
}
