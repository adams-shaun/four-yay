package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

const floodedStrandSrc = "Name:Flooded Strand\nManaCost:no cost\nTypes:Land\n" +
	"A:AB$ ChangeZone | Cost$ T PayLife<1> Sac<1/CARDNAME> | Origin$ Library | Destination$ Battlefield | ChangeType$ Plains,Island | SpellDescription$ Search your library for a Plains or Island card, put it onto the battlefield, then shuffle.\n" +
	"Oracle:{T}, Pay 1 life, Sacrifice Flooded Strand: Search your library for a Plains or Island card, put it onto the battlefield, then shuffle.\n"

const wastelandSrc = "Name:Wasteland\nManaCost:no cost\nTypes:Land\n" +
	"A:AB$ Mana | Cost$ T | Produced$ C | SpellDescription$ Add {C}.\n" +
	"A:AB$ Destroy | ValidTgts$ Land.nonBasic | TgtPrompt$ Select target nonbasic land. | Cost$ T Sac<1/CARDNAME> | SpellDescription$ Destroy target nonbasic land.\n" +
	"Oracle:{T}: Add {C}.\\n{T}, Sacrifice Wasteland: Destroy target nonbasic land.\n"

const plainsSrc = "Name:Plains\nTypes:Basic Land Plains\nOracle:x\n"
const nonbasicLandSrc = "Name:Target Land\nTypes:Land\nOracle:x\n"

// TestFloodedStrandSelfSacrificeSkipsTheSingletonChoice follows the actual
// fetchland flow: activation pays life and sacrifices the Strand without a
// singleton sacrifice KChoose, then the library search remains a real choice.
func TestFloodedStrandSelfSacrificeSkipsTheSingletonChoice(t *testing.T) {
	t.Parallel()
	e, cfg, strand := newFixtureDeck(t, 76, floodedStrandSrc, plainsSrc)
	plains := moveSeeded(t, e, 0, plainsSrc, state.ZLibrary)
	moveSeeded(t, e, 0, floodedStrandSrc, state.ZBattlefield)
	e.Advance()

	life := e.G.Players[0].Life
	opt := abilityOption(t, e, strand, 0)
	submitChoices(t, e, opt.Index)
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		t.Fatalf("Flooded Strand posed a sacrifice choice instead of proceeding to priority: %+v", d)
	}
	if got := e.G.Obj(strand).Zone; got != state.ZGraveyard {
		t.Fatalf("Flooded Strand zone = %s, want Graveyard after paying its cost", got)
	}
	if got := e.G.Players[0].Life; got != life-1 {
		t.Fatalf("life = %d, want %d after Flooded Strand's PayLife<1>", got, life-1)
	}

	passUntilKind(t, e, decision.KChoose, 4)
	d := e.Pending()
	if d == nil || d.ResumeKind != "search" {
		t.Fatalf("search decision = %+v", d)
	}
	plainsIndex := -1
	for _, o := range d.Options {
		if o.Obj == plains {
			plainsIndex = o.Index
		}
	}
	if plainsIndex < 0 {
		t.Fatalf("Flooded Strand search did not offer Plains %d: %+v", plains, d.Options)
	}
	submitChoices(t, e, plainsIndex)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(plains).Zone; got != state.ZBattlefield {
		t.Fatalf("chosen Plains zone = %s, want Battlefield", got)
	}
	// Flooded Strand's Forge script has no Tapped$ parameter, matching its
	// oracle text: the fetched land enters untapped.
	if e.G.Obj(plains).Tapped {
		t.Fatal("Flooded Strand fetched Plains tapped without a Tapped$ parameter")
	}
	replayCheck(t, e, cfg)
}

// TestWastelandSelfSacrificeLeavesItsTargetChoice verifies that only the
// forced self-payment is skipped; Wasteland's ordinary target decision still
// happens before its source is sacrificed.
func TestWastelandSelfSacrificeLeavesItsTargetChoice(t *testing.T) {
	t.Parallel()
	e, cfg, wasteland := newFixtureDeck(t, 77, wastelandSrc, nonbasicLandSrc)
	target := moveSeeded(t, e, 0, nonbasicLandSrc, state.ZBattlefield)
	moveSeeded(t, e, 0, wastelandSrc, state.ZBattlefield)
	e.Advance()

	opt := abilityOption(t, e, wasteland, 1)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Wasteland target decision = %+v, want KTarget (not sacrifice KChoose)", d)
	}
	targetIndex := -1
	for _, o := range d.Options {
		if o.Obj == target {
			targetIndex = o.Index
		}
	}
	if targetIndex < 0 {
		t.Fatalf("Wasteland did not offer target land %d: %+v", target, d.Options)
	}
	submitChoices(t, e, targetIndex)
	if got := e.G.Obj(wasteland).Zone; got != state.ZGraveyard {
		t.Fatalf("Wasteland zone = %s, want Graveyard after target selection", got)
	}
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(target).Zone; got != state.ZGraveyard {
		t.Fatalf("Wasteland target zone = %s, want Graveyard after resolution", got)
	}
	replayCheck(t, e, cfg)
}

// TestOrdinarySacrificeCostStillAsks pins the boundary: a creature filter is
// not a source reference, even if this fixture currently has only one match.
func TestOrdinarySacrificeCostStillAsks(t *testing.T) {
	t.Parallel()
	e, _, rites := newFixtureDeck(t, 78, villageRitesSrc, bearSrc)
	bear := putCreature(t, e, 0, bearSrc)
	addMana(t, e, 0, "B")

	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == rites {
			submitChoices(t, e, o.Index)
			break
		}
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 1 || d.Options[0].Kind != "sacrifice" || d.Options[0].Obj != bear {
		t.Fatalf("Village Rites sacrifice decision = %+v, want its creature choice", d)
	}
}

// TestNicknameSelfSacrificeSkipsTheSingletonChoice covers Forge's second
// spelling for a bare source reference.
func TestNicknameSelfSacrificeSkipsTheSingletonChoice(t *testing.T) {
	t.Parallel()
	const src = "Name:Nickname Source\nTypes:Artifact\nA:AB$ GainLife | Cost$ Sac<1/NICKNAME> | Defined$ You | LifeAmount$ 1\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 79, src)
	moveSeeded(t, e, 0, src, state.ZBattlefield)
	e.Advance()

	opt := abilityOption(t, e, id, 0)
	submitChoices(t, e, opt.Index)
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		t.Fatalf("NICKNAME self-sacrifice wrongly posed a choice: %+v", d)
	}
	if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
		t.Fatalf("NICKNAME source zone = %s, want Graveyard", got)
	}
	replayCheck(t, e, cfg)
}
