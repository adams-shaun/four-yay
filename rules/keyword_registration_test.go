package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Registered keywords must each have a behaviour test naming them; this
// pins the M2r registrations against the tests that prove them.
func TestRegisteredKeywordsAreHonoured(t *testing.T) {
	sup := effects.Supported()
	for kw, proof := range map[string]string{
		"kw:Flash":          "TestFlashCreatureIsCastableOffTurn",
		"kw:Foretell":       "TestForetellActionOfferedOnYourTurnAtTwoGeneric",
		"kw:Indestructible": "TestIndestructibleSurvivesLethalDamageAndDestroy",
		"kw:Devoid":         "TestDevoidCreatureIsColourless",
		"kw:Undying":        "TestUndyingReturnsOnceWithACounter",
		"kw:Persist":        "TestPersistUsesRealCorpusCard",
		"kw:Evolve":         "TestEvolveGrowsOnlyForBiggerCreatures",
		"kw:Exalted":        "TestExaltedPumpsALoneAttackerAndProwessPumpsOnNoncreatureSpells",
		"kw:Dethrone":       "TestDethroneCountsOnlyTheAttackedPlayersLife",
		"kw:Training":       "TestTrainingCounterOnAttackWithABiggerCreature",
		"kw:Melee":          "TestTitaniaMeleeCountsDistinctAttackedOpponents",
		"kw:Mentor":         "TestMentorCounterOnLesserPowerAttacker",
		"kw:Riot":           "TestRiotAndHideawayUseRealCorpusCards",
		"kw:Hideaway":       "TestRiotAndHideawayUseRealCorpusCards",
		"kw:Extort":         "TestExtortUsesRealCorpusCard",
		"kw:Dredge":         "TestDredgeUsesRealCorpusCard",
		"kw:Myriad":         "TestMyriadUsesRealCorpusCard",
		"kw:Soulbond":       "TestSoulbondUsesRealCorpusCard",
		"kw:Unleash":        "TestUnleashChoicePosedAndCounterApplied",
		"kw:Prowess":        "TestExaltedPumpsALoneAttackerAndProwessPumpsOnNoncreatureSpells",
		"kw:Ward":           "TestWardVeinRipperCountersAnUnpaidTargetingSpell",
		"kw:Echo":           "TestEchoKarmicGuidePaysEchoOrIsSacrificed",
		"kw:Annihilator":    "TestAnnihilatorArtisanSacrificesThePrintedAmount",
		"kw:Mobilize":       "TestMobilizeUsesRealCorpusCard",
		"kw:Crew":           "TestCrewAvengersQuinjetAnimatesUntilEndOfTurn",
		"kw:Afterlife":      "TestAfterlifeTitheTakerMintsSpiritTokens",
		"kw:Fabricate":      "TestFabricateAngelOfInventionChoosesServosOrCounters",
		"kw:Double Strike":  "TestDoubleStrikeFearAndShadowUseCorpusCombatKeywords",
		"kw:Fear":           "TestDoubleStrikeFearAndShadowUseCorpusCombatKeywords",
		"kw:Shadow":         "TestDoubleStrikeFearAndShadowUseCorpusCombatKeywords",
		"kw:Horsemanship":   "TestHorsemanshipCanBlockOnlyHorsemanshipAttackers",
		"kw:Skulk":          "TestSkulkBlocksOnlyGreaterPowerBlockers",
		"kw:Intimidate":     "TestIntimidateBlocksOnlyArtifactsAndSharedColors",
		"kw:Landwalk":       "TestLandwalkEvadesDefenderLands",
		"kw:Protection":     "TestProtectionUsesAllLiveColourQualities",
		"kw:Flanking":       "TestFlankingKnightOfTheHolyNimbusDebuffsBlockers",
		"kw:Split second":   "TestVATSSplitSecondWithholdsCastsAndActivations",
		"kw:Devour":         "TestGorgerWurmDevourAsksAndCountsTheSacrificed",
		"kw:Gravestorm":     "TestOminousHarvestGravestormCopiesPerDeath",
		"kw:Toxic":          "TestToxicIxhelAddsPoisonOnCombatDamage",
		"kw:Infect":         "TestInfectDamageToCreatureIsMinusOneCountersNotMarkedDamage",
		"kw:Wither":         "TestWitherVillagePillagersETBDamageAllDealsCounters",
		"kw:Battle cry":     "TestBattleCryPumpsOtherAttackingCreatures",
		"kw:Graft":          "TestLlanowarRebornGraftEntersWithCounterAndMayMoveIt",
	} {
		if !sup[kw] {
			t.Errorf("%s is not registered (proof test: %s)", kw, proof)
		}
	}
}

func TestIndestructibleSurvivesLethalDamageAndDestroy(t *testing.T) {
	// Printed Indestructible survives lethal damage (SBA) and a Destroy
	// effect; a Destroy against a creature that GAINED Indestructible via a
	// Pump also does nothing (Host.HasKeyword reads derived keywords).
	e, cfg, id := newFixtureDeck(t, 5, "Name:Rock\nManaCost:1\nTypes:Creature Wall\nPT:0/3\nK:Indestructible\nOracle:x\n")
	_ = cfg
	// Put it onto the battlefield the way the fixture helpers do, then hit it.
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.Damage, Obj: id, Amount: 5})
	e.checkStateBased()
	if e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatal("indestructible creature died to lethal damage")
	}
	ctx := &effects.Ctx{Source: id, Controller: 0, Targets: []state.Target{{Obj: id}}}
	effects.Resolve(e, ctx, &cards.SA{Kind: "DB", API: "Destroy", Params: map[string]string{}})
	if e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatal("indestructible creature was destroyed")
	}
	// Granted: a plain bear pumped with KW$ Indestructible.
	e2, _, bear := newFixtureDeck(t, 6, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e2.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZHand, To: state.ZBattlefield})
	effects.Resolve(e2, &effects.Ctx{Source: bear, Controller: 0, Targets: []state.Target{{Obj: bear}}},
		&cards.SA{Kind: "DB", API: "Pump", Params: map[string]string{"KW": "Indestructible"}})
	effects.Resolve(e2, &effects.Ctx{Source: bear, Controller: 0, Targets: []state.Target{{Obj: bear}}},
		&cards.SA{Kind: "DB", API: "Destroy", Params: map[string]string{}})
	if e2.G.Obj(bear).Zone != state.ZBattlefield {
		t.Fatal("Destroy ignored granted Indestructible")
	}
}

func TestDevoidCreatureIsColourless(t *testing.T) {
	e, _, id := newFixtureDeck(t, 7, "Name:Breaker\nManaCost:6 G\nTypes:Creature Eldrazi\nK:Devoid\nOracle:x\n")
	if got := effects.ColorsOf(e.G.Obj(id)); got != "" {
		t.Fatalf("devoid creature has colours %q", got)
	}
}
