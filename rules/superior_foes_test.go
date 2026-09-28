package rules

// ImprintOnHost$ (task param:api:Effect.ImprintOnHost): the DB$ Effect
// parameter that ties the created effect to its HOST card so a later
// `DB$ ChangeZone | Defined$ Imprinted | Origin$ Command | Destination$
// Exile` can exile the imprinted effect and end it. Forge's EffectEffect
// imprints the created effect TOKEN on the host card and moves the token to
// the Command zone; this build has no effect-token object, so the marker
// rides the registrations (state.ContinuousEffect.ImprintOnHost) and the
// idiom ends exactly those through Engine.EndImprintedEffects, wired from
// effects/zone.go's effChangeZone.
//
// The pin is the whole dig-and-play chain on the real corpus card Superior
// Foes of Spider-Man, whose oracle is "Whenever you cast a spell with mana
// value 4 or greater, you may exile the top card of your library. If you
// do, you may play that card until you exile another card with this
// creature." The second dig's trigger exiles the FIRST effect's token
// (Command zone -> Exile in Forge): the first dug card stays in exile but
// stops being playable, and the new dig's card becomes the playable one.
// Per Forge source (AbilityFactoryEffect.resolve): ImprintOnHost$ adds the
// effect token itself to the host's imprinted list and moveToCommand's it.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

const sfExiledBolt = "Name:Exiled Bolt\nManaCost:R\nTypes:Instant\nOracle:x\n"
const sfExiledBear = "Name:Exiled Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// TestSuperiorFoesSecondDigExilesTheFirstEffect pins the chain end to end:
// first big spell digs bolt to exile and its Effect grants the may-play;
// the second big spell's trigger exiles the imprinted FIRST effect (the
// grant ends), digs bear, and the new Effect grants only bear.
func TestSuperiorFoesSecondDigExilesTheFirstEffect(t *testing.T) {
	t.Parallel()
	reg := choiceCorpusRegistry(t)
	sf := choiceCorpusCard(t, "Superior Foes of Spider-Man")
	bigA := card(t, "Name:Big Spell A\nManaCost:3 G\nTypes:Instant\nOracle:x\n")
	bigB := card(t, "Name:Big Spell B\nManaCost:2 G G\nTypes:Instant\nOracle:x\n")
	e := corpusEngine(t, reg, []*cards.Card{sf, bigA, bigB}, nil)
	sfID := findCardObj(t, e, 0, "Superior Foes of Spider-Man", state.ZHand)
	bigAID := findCardObj(t, e, 0, "Big Spell A", state.ZHand)
	bigBID := findCardObj(t, e, 0, "Big Spell B", state.ZHand)
	// Precondition: all three spells are really in seat 0's hand, the zone
	// the casts below read.
	for _, id := range []state.ObjID{sfID, bigAID, bigBID} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand || o.Controller != 0 {
			t.Fatalf("hand precondition failed for %d: %+v", id, o)
		}
	}
	// The two dug cards, seeded on top of the library in dig order.
	seeded := seedLibraryTop(t, e, sfExiledBolt, sfExiledBear)
	bolt, bear := seeded[0], seeded[1]

	// Superior Foes enters (2 R).
	addMana(t, e, 0, "RRR")
	submitChoices(t, e, castOptionFor(t, e, sfID).Index)
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(sfID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Superior Foes zone=%v, want battlefield", o)
	}

	// Big Spell A (mana value 4): the cast trigger asks, we accept, the dig
	// exiles the top card (bolt) and the Effect registers the may-play
	// grant carrying the ImprintOnHost marker.
	addMana(t, e, 0, "GGGG")
	submitChoices(t, e, castOptionFor(t, e, bigAID).Index)
	trig := passPriorityUntil(t, e, decision.KTriggerOptional)
	submitChoices(t, e, optionIndexOfKind(t, trig, "yes"))
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(bolt); o == nil || o.Zone != state.ZExile {
		t.Fatalf("first dug card zone=%v, want exile", o)
	}
	grant := mayPlayGrantOn(e, bolt)
	if grant == nil {
		t.Fatalf("first Effect's may-play grant does not remember bolt: %+v", e.continuous)
	}
	if !grant.ImprintOnHost {
		t.Fatalf("grant ImprintOnHost=false, want the marker: %+v", grant)
	}
	// Precondition for the real assertion: bolt is genuinely playable from
	// exile through the grant (the state the second dig must end).
	addMana(t, e, 0, "R")
	if mayPlayOption(e.Pending(), bolt) == nil {
		t.Fatalf("first dug card not may-playable before the second dig: %+v", e.Pending().Options)
	}

	// Big Spell B: the second dig. Its trigger's ExileSelf ChangeZone
	// (`Defined$ Imprinted | Origin$ Command | Destination$ Exile`) must
	// exile the FIRST imprinted effect -- ending the bolt grant -- before
	// the new Effect registers for bear.
	addMana(t, e, 0, "GGGG")
	submitChoices(t, e, castOptionFor(t, e, bigBID).Index)
	trig = passPriorityUntil(t, e, decision.KTriggerOptional)
	submitChoices(t, e, optionIndexOfKind(t, trig, "yes"))
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(bolt); o == nil || o.Zone != state.ZExile {
		t.Fatalf("first dug card left exile across the second dig: %+v", o)
	}
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZExile {
		t.Fatalf("second dug card zone=%v, want exile", o)
	}
	if g := mayPlayGrantOn(e, bolt); g != nil {
		t.Fatalf("the first Effect's grant survived the second dig: %+v", g.Remembered)
	}
	if mayPlayGrantOn(e, bear) == nil {
		t.Fatalf("the second Effect's grant does not remember bear: %+v", e.continuous)
	}
	addMana(t, e, 0, "GG")
	d := e.Pending()
	if mayPlayOption(d, bolt) != nil {
		t.Fatalf("first dug card still may-playable after the second dig: %+v", d.Options)
	}
	if mayPlayOption(d, bear) == nil {
		t.Fatalf("second dug card not may-playable: %+v", d.Options)
	}
}

// TestSuperiorFoesDeclinedDigKeepsTheOldEffect pins the negative: the dig
// trigger is OptionalDecider$ You, so a declined election runs NOTHING --
// the old effect's grant stays live and the first dug card stays playable.
func TestSuperiorFoesDeclinedDigKeepsTheOldEffect(t *testing.T) {
	t.Parallel()
	reg := choiceCorpusRegistry(t)
	sf := choiceCorpusCard(t, "Superior Foes of Spider-Man")
	bigA := card(t, "Name:Big Spell A\nManaCost:3 G\nTypes:Instant\nOracle:x\n")
	bigB := card(t, "Name:Big Spell B\nManaCost:2 G G\nTypes:Instant\nOracle:x\n")
	e := corpusEngine(t, reg, []*cards.Card{sf, bigA, bigB}, nil)
	sfID := findCardObj(t, e, 0, "Superior Foes of Spider-Man", state.ZHand)
	bigAID := findCardObj(t, e, 0, "Big Spell A", state.ZHand)
	bigBID := findCardObj(t, e, 0, "Big Spell B", state.ZHand)
	seeded := seedLibraryTop(t, e, sfExiledBolt, sfExiledBear)
	bolt := seeded[0]

	addMana(t, e, 0, "RRR")
	submitChoices(t, e, castOptionFor(t, e, sfID).Index)
	passUntilStackEmpty(t, e, 20)
	addMana(t, e, 0, "GGGG")
	submitChoices(t, e, castOptionFor(t, e, bigAID).Index)
	trig := passPriorityUntil(t, e, decision.KTriggerOptional)
	submitChoices(t, e, optionIndexOfKind(t, trig, "yes"))
	passUntilStackEmpty(t, e, 20)
	if mayPlayGrantOn(e, bolt) == nil {
		t.Fatalf("precondition: no grant after the first dig: %+v", e.continuous)
	}

	// The declined election: the whole trigger body (ExileSelf included)
	// never runs, so the old effect is untouched.
	addMana(t, e, 0, "GGGG")
	submitChoices(t, e, castOptionFor(t, e, bigBID).Index)
	trig = passPriorityUntil(t, e, decision.KTriggerOptional)
	submitChoices(t, e, optionIndexOfKind(t, trig, "no"))
	passUntilStackEmpty(t, e, 20)
	if g := mayPlayGrantOn(e, bolt); g == nil {
		t.Fatalf("declined second dig still ended the first effect: %+v", e.continuous)
	}
	addMana(t, e, 0, "R")
	if mayPlayOption(e.Pending(), bolt) == nil {
		t.Fatalf("first dug card lost its may-play offer after a declined dig: %+v", e.Pending().Options)
	}
}
