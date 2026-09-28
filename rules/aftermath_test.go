package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The Dusk // Dawn shape: the printed front face is the ordinary half, the
// ALTERNATE face carries K:Aftermath (all 27 corpus aftermath cards carry the
// keyword on the alternate face of an AlternateMode:Split card).
const duskDawnSrc = "Name:Dusk\nManaCost:2 W W\nTypes:Sorcery\n" +
	"A:SP$ DestroyAll | ValidCards$ Creature.powerGE3 | SpellDescription$ Destroy all creatures with power 3 or greater.\n" +
	"AlternateMode:Split\n" +
	"ALTERNATE\n" +
	"Name:Dawn\nManaCost:3 W W\nTypes:Sorcery\nK:Aftermath\n" +
	"A:SP$ ChangeZoneAll | ChangeType$ Creature.powerLE2+YouCtrl | Origin$ Graveyard | Destination$ Hand | SpellDescription$ Return all creature cards with power 2 or less from your graveyard to your hand.\n" +
	"Oracle:x\n"

const finishShapeSrc = "Name:Start\nManaCost:2 R\nTypes:Sorcery\n" +
	"A:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 3 | SpellDescription$ x\n" +
	"AlternateMode:Split\n" +
	"ALTERNATE\n" +
	"Name:Finish\nManaCost:2 B\nTypes:Sorcery\nK:Aftermath\n" +
	"A:SP$ Destroy | ValidTgts$ Creature | Cost$ 2 B Sac<1/Creature> | SpellDescription$ x\n" +
	"Oracle:x\n"

func aftermathOption(t *testing.T, e *Engine, id state.ObjID) *decision.Option {
	t.Helper()
	for _, o := range castOptions(t, e) {
		if o.Mode == "aftermath" && o.Obj == id {
			cp := o
			return &cp
		}
	}
	return nil
}

// TestAftermathCastsAlternateFaceFromGraveyardAndExiles: CR 702.85a -- the
// aftermath half of a split card is cast only from its owner's graveyard,
// for its own printed mana cost, and exiles after it resolves. The
// resolution must run the ALTERNATE face's spell (Dawn returns graveyard
// creatures to hand, which the primary half's DestroyAll could never do).
func TestAftermathCastsAlternateFaceFromGraveyardAndExiles(t *testing.T) {
	t.Parallel()
	bearSrc := "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e, cfg, dusk := newFixtureDeck(t, 60, duskDawnSrc, bearSrc)
	bear := addToGraveyard(t, e, 0, bearSrc)
	moveSeeded(t, e, 0, duskDawnSrc, state.ZGraveyard)
	addMana(t, e, 0, "WWWGG")
	am := aftermathOption(t, e, dusk)
	if am == nil {
		t.Fatal("aftermath not offered from the graveyard")
	}
	if am.Label != "Cast Dawn (aftermath)" {
		t.Fatalf("label %q", am.Label)
	}
	submitChoices(t, e, am.Index)
	o := e.G.Obj(dusk)
	if o.Zone != state.ZStack || o.FaceIdx != 1 || o.Face().Name != "Dawn" ||
		o.CastFlags&state.FlagAftermath == 0 {
		t.Fatalf("after paying: zone %s faceIdx %d name %s flags %+v",
			o.Zone, o.FaceIdx, o.Face().Name, o.CastFlags)
	}
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(dusk).Zone != state.ZExile {
		t.Fatalf("aftermath spell went to %s, want exile", e.G.Obj(dusk).Zone)
	}
	if e.G.Obj(bear).Zone != state.ZHand {
		t.Fatalf("Dawn did not return the graveyard creature: bear in %s", e.G.Obj(bear).Zone)
	}
	replayCheck(t, e, cfg)
}

// TestAftermathCounteredGoesToExile: CR 702.85a's "then exile it" covers
// every way the spell leaves the stack, including being countered (the
// flashback convention TestFlashbackedSpellCounteredGoesToExile pins).
func TestAftermathCounteredGoesToExile(t *testing.T) {
	t.Parallel()
	bearSrc := "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e, cfg, dusk := newFixtureDeck(t, 61, duskDawnSrc, bearSrc)
	moveSeeded(t, e, 0, duskDawnSrc, state.ZGraveyard)
	addMana(t, e, 0, "WWWGG")
	am := aftermathOption(t, e, dusk)
	if am == nil {
		t.Fatal("aftermath not offered from the graveyard")
	}
	submitChoices(t, e, am.Index)
	o := e.G.Obj(dusk)
	if o.Zone != state.ZStack || o.CastFlags&state.FlagAftermath == 0 {
		t.Fatalf("after paying: zone %s flags %+v", o.Zone, o.CastFlags)
	}
	// Counter it with a hand-built Counter effect against the stack object.
	effects.Resolve(e, &effects.Ctx{Controller: 1, Targets: []state.Target{{Obj: dusk}}, TargetsOffered: true},
		card(t, "Name:Counterspell\nManaCost:U U\nTypes:Instant\nA:SP$ Counter | ValidTgts$ Spell\nOracle:x\n").Faces[0].SpellAbility())
	if e.G.Obj(dusk).Zone != state.ZExile {
		t.Fatalf("countered aftermath spell went to %s, want exile", e.G.Obj(dusk).Zone)
	}
	replayCheck(t, e, cfg)
}

// TestAftermathNotOfferedFromHandOrWhileFaceIdxNonzero: the aftermath offer
// is a GRAVEYARD walk only -- the primary half keeps its ordinary hand cast
// and no aftermath option ever appears from the hand walk -- and a split card
// not showing its front face is never aftermath-cast.
func TestAftermathNotOfferedFromHandOrWhileFaceIdxNonzero(t *testing.T) {
	t.Parallel()
	e, _, dusk := newFixtureDeck(t, 62, duskDawnSrc)
	addMana(t, e, 0, "WWWGG")
	if am := aftermathOption(t, e, dusk); am != nil {
		t.Fatalf("aftermath offered from the hand walk: %+v", am)
	}
	// The ordinary hand cast of the primary face is still offered.
	var plain *decision.Option
	for _, o := range castOptions(t, e) {
		if o.Obj == dusk && o.Mode == "" {
			plain = &o
		}
	}
	if plain == nil {
		t.Fatal("primary half's ordinary hand cast not offered")
	}
	// Flip the graveyard card away from its front face: no aftermath offer.
	moveSeeded(t, e, 0, duskDawnSrc, state.ZGraveyard)
	e.emit(events.Event{Kind: events.FlipFace, Obj: dusk, Amount: 1})
	addMana(t, e, 0, "GG")
	if am := aftermathOption(t, e, dusk); am != nil {
		t.Fatalf("aftermath offered while FaceIdx != 0: %+v", am)
	}
}

// TestAftermathAdditionalCostFoldsAndIsCharged: a Finish-shaped face whose SP
// carries Cost$ 2 B Sac<1/Creature> is offered only when a sacrifice
// candidate exists (otherwise the cast flow would ask a sacrifice decision
// with no options), and the creature is actually sacrificed.
func TestAftermathAdditionalCostFoldsAndIsCharged(t *testing.T) {
	t.Parallel()
	bearSrc := "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	// No creature on the battlefield: the Sac part is unpayable, no offer.
	e1, _, card1 := newFixtureDeck(t, 63, finishShapeSrc)
	moveSeeded(t, e1, 0, finishShapeSrc, state.ZGraveyard)
	addMana(t, e1, 0, "BBGG")
	if am := aftermathOption(t, e1, card1); am != nil {
		t.Fatalf("aftermath offered with no sacrifice candidate: %+v", am)
	}
	// With a creature the option appears; casting it sacrifices the bear and
	// exiles the card after resolution.
	e2, cfg, card2 := newFixtureDeck(t, 64, finishShapeSrc, bearSrc)
	bear := putCreature(t, e2, 0, bearSrc)
	moveSeeded(t, e2, 0, finishShapeSrc, state.ZGraveyard)
	addMana(t, e2, 0, "BBGG")
	am := aftermathOption(t, e2, card2)
	if am == nil {
		t.Fatal("aftermath with a Sac additional cost not offered")
	}
	submitChoices(t, e2, am.Index)
	// The Sac cost part is asked BEFORE the 601.2c target choice (the
	// cast-flow's cost-first ask order), then the target -- target the bear.
	d := e2.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "sacrifice" {
		t.Fatalf("sacrifice decision %+v", d)
	}
	submitChoices(t, e2, 0)
	d = e2.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("target decision %+v", d)
	}
	submitChoices(t, e2, 0)
	if e2.G.Obj(card2).Zone != state.ZStack || e2.G.Obj(card2).Face().Name != "Finish" ||
		e2.G.Obj(card2).CastFlags&state.FlagAftermath == 0 {
		t.Fatalf("after paying: %+v", e2.G.Obj(card2))
	}
	if e2.G.Obj(bear).Zone != state.ZGraveyard {
		t.Fatalf("sacrifice cost not charged: bear in %s", e2.G.Obj(bear).Zone)
	}
	passUntilStackEmpty(t, e2, 20)
	if e2.G.Obj(card2).Zone != state.ZExile {
		t.Fatalf("aftermath spell went to %s, want exile", e2.G.Obj(card2).Zone)
	}
	replayCheck(t, e2, cfg)
}

// TestAftermathNotOfferedWithoutKeyword: the K:Aftermath gate is what keeps
// the offer off every other two-face Split card (Rooms must stay on the Room
// path, plain splits stay on the ordinary paths).
func TestAftermathNotOfferedWithoutKeyword(t *testing.T) {
	t.Parallel()
	src := "Name:Front\nManaCost:1 R\nTypes:Sorcery\n" +
		"A:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 1 | SpellDescription$ x\n" +
		"AlternateMode:Split\n" +
		"ALTERNATE\n" +
		"Name:Back\nManaCost:1 B\nTypes:Sorcery\n" +
		"A:SP$ Draw | Defined$ You | SpellDescription$ x\nOracle:x\n"
	e, _, id := newFixtureDeck(t, 65, src)
	addMana(t, e, 0, "BBG")
	moveSeeded(t, e, 0, src, state.ZGraveyard)
	e.Advance()
	if am := aftermathOption(t, e, id); am != nil {
		t.Fatalf("aftermath offered without the keyword: %+v", am)
	}
}
