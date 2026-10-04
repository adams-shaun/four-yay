package rules

// The rv2c cost-modifier parity task: RaiseCost/ReduceCost/SetCost read the
// same parameters Forge's CostAdjustment reads (ValidSpell$, SVar Amount$,
// Color$, IgnoreGeneric$, MinMana$, EffectZone$ host-self statics, IsPresent$,
// RaiseTo$), and ParseCost prices the monocolour-hybrid (twobrid),
// hybrid-Phyrexian and snow symbols as real alternative payments. Every
// fixture below embeds the real Forge script line(s) of the card it probes,
// copied from the corpus (never a committed .txt — the licensing rule).

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

const steelshaperSrc = "Name:Auriok Steelshaper\nManaCost:1 W\nTypes:Creature Human Soldier\nPT:1/1\n" +
	"S:Mode$ ReduceCost | ValidCard$ Card | ValidSpell$ Activated.Equip | Activator$ You | Amount$ 1 | Description$ Equip costs you pay cost {1} less.\n" +
	"Oracle:Equip costs you pay cost {1} less.\n"

const equiperSrc = "Name:Kyoshi Battle Fan\nManaCost:3\nTypes:Artifact Creature Kitsune\nPT:1/3\n" +
	"K:Equip:2\nOracle:Equip {2}\n"

const thaliaRv2cSrc = "Name:Thalia, Guardian of Thraben\nManaCost:1 W\nTypes:Legendary Creature Human Soldier\nPT:2/1\n" +
	"S:Mode$ RaiseCost | ValidCard$ Card.nonCreature | Type$ Spell | Amount$ 1 | Description$ Noncreature spells cost {1} more to cast.\n" +
	"Oracle:Noncreature spells cost {1} more to cast.\n"

const rakdosSrc = "Name:Rakdos, Lord of Riots\nManaCost:B B R R\nTypes:Legendary Creature Demon\nPT:6/6\n" +
	"S:Mode$ ReduceCost | ValidCard$ Creature | Type$ Spell | Activator$ You | Amount$ X | Description$ Creature spells you cast cost {1} less to cast for each 1 life your opponents have lost this turn.\n" +
	"SVar:X:Count$LifeOppsLostThisTurn\n" +
	"Oracle:Creature spells you cast cost {1} less to cast for each 1 life your opponents have lost this turn.\n"

const heraldRv2cSrc = "Name:Herald of War\nManaCost:3 W W\nTypes:Creature Angel\nPT:3/3\n" +
	"S:Mode$ ReduceCost | ValidCard$ Angel,Human | Type$ Spell | Activator$ You | Amount$ X | Description$ Angels and Humans you cast cost 1 less for each +1/+1 counter on CARDNAME.\n" +
	"SVar:X:Count$CardCounters.P1P1\n" +
	"Oracle:Angel spells and Human spells you cast cost {1} less to cast for each +1/+1 counter on Herald of War.\n"

const ghaltaSrc = "Name:Ghalta, Primal Hunger\nManaCost:10 G G\nTypes:Legendary Creature Elder Dinosaur\nPT:12/12\n" +
	"S:Mode$ ReduceCost | ValidCard$ Card.Self | Type$ Spell | Amount$ X | EffectZone$ All | Description$ This spell costs {X} less to cast, where X is the total power of creatures you control.\n" +
	"SVar:X:Count$Valid Creature.YouCtrl$CardPower\n" +
	"Oracle:This spell costs {X} less to cast, where X is the total power of creatures you control.\n"

const hydraRv2cSrc = "Name:Khalni Hydra\nManaCost:G G G G G G G G\nTypes:Creature Hydra\nPT:8/8\n" +
	"S:Mode$ ReduceCost | ValidCard$ Card.Self | Type$ Spell | Color$ G | Amount$ X | EffectZone$ All | Description$ This spell costs {G} less to cast for each green creature you control.\n" +
	"SVar:X:Count$Valid Creature.Green+YouCtrl\n" +
	"Oracle:This spell costs {G} less to cast for each green creature you control.\n"

const heartstoneSrc = "Name:Heartstone\nManaCost:3\nTypes:Artifact\n" +
	"S:Mode$ ReduceCost | ValidCard$ Creature | Type$ Ability | Amount$ 1 | MinMana$ 1 | AffectedZone$ Battlefield | Description$ Activated abilities of creatures cost {1} less to activate. This effect can't reduce the mana in that cost to less than one mana.\n" +
	"Oracle:Activated abilities of creatures cost {1} less to activate. This effect can't reduce the mana in that cost to less than one mana.\n"

const biomancersFamiliarSrc = "Name:Biomancer's Familiar\nManaCost:G U\nTypes:Creature Mutant\nPT:2/2\n" +
	"S:Mode$ ReduceCost | ValidCard$ Creature.YouCtrl | Type$ Ability | Amount$ 2 | MinMana$ 1 | AffectedZone$ Battlefield | Description$ Activated abilities of creatures you control cost {2} less to activate. This effect can't reduce the mana in that cost to less than one mana.\n" +
	"Oracle:Activated abilities of creatures you control cost {2} less to activate. This effect can't reduce the mana in that cost to less than one mana.\n"

const costlyCreatureAbilitySrc = "Name:Costly Creature\nManaCost:3 G\nTypes:Creature Beast\nPT:3/3\n" +
	"A:AB$ Pump | Cost$ 3 | Defined$ Self | NumAtt$ +1 | NumDef$ +1 | SpellDescription$ CARDNAME gets +1/+1 until end of turn.\n" +
	"Oracle:{3}: CARDNAME gets +1/+1 until end of turn.\n"

const trinisphereSrc = "Name:Trinisphere\nManaCost:3\nTypes:Artifact\n" +
	"S:Mode$ SetCost | ValidCard$ Card | Type$ Spell | Amount$ 3 | RaiseTo$ True | IsPresent$ Card.Self+untapped | Description$ As long as CARDNAME is untapped, each spell that would cost less than three mana to cast costs three mana to cast.\n" +
	"Oracle:As long as Trinisphere is untapped, each spell that would cost less than three mana to cast costs three mana to cast.\n"

const spectralProcessionSrc = "Name:Spectral Procession\nManaCost:2W 2W 2W\nTypes:Sorcery\n" +
	"A:SP$ Token | TokenAmount$ 3 | TokenScript$ w_1_1_spirit_flying | TokenOwner$ You | SpellDescription$ Create three 1/1 white Spirit creature tokens with flying.\n" +
	"Oracle:Create three 1/1 white Spirit creature tokens with flying.\n"

const icehideGolemSrc = "Name:Icehide Golem\nManaCost:S\nTypes:Snow Artifact Creature Golem\nPT:2/2\n" +
	"Oracle:({S} can be paid with one mana from a snow source.)\n"

const snowWastesSrc = "Name:Snow-Covered Wastes\nManaCost:no cost\nTypes:Basic Snow Land\n" +
	"A:AB$ Mana | Cost$ T | Produced$ C | SpellDescription$ Add {C}.\n" +
	"Oracle:{T}: Add {C}.\n"

const borealCentaurSrc = "Name:Boreal Centaur\nManaCost:1 G\nTypes:Snow Creature Centaur Warrior\nPT:2/2\n" +
	"A:AB$ Pump | Cost$ S | Defined$ Self | NumAtt$ +1 | NumDef$ +1 | ActivationLimit$ 1 | SpellDescription$ CARDNAME gets +1/+1 until end of turn. Activate only once each turn.\n" +
	"Oracle:{S}: Boreal Centaur gets +1/+1 until end of turn. Activate only once each turn.\n"

const ajaniSleeperSrc = "Name:Ajani Sleeper Agent\nManaCost:1 G GWP W\nTypes:Legendary Planeswalker Ajani\nLoyalty:4\n" +
	"Oracle:Compleated\n"

const colorlessHybridSrc = "Name:Colourless Hybrid\nManaCost:C/W\nTypes:Artifact\nOracle:x\n"

const twinVeilSrc = "Name:Twin Veil\nManaCost:W/U W/U\nTypes:Instant\n" +
	"A:SP$ Pump | ValidTgts$ Creature | NumAtt$ +1 | NumDef$ +1\nOracle:x\n"

const targetSrc = "Name:Target\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// whiteReducerSrc mirrors Davriel, Soul Broker's real cost-reduction line
// (SVar:CostBLess in .cards/cardsfolder/d/davriel_soul_broker.txt) with the
// colour set to W, so the {W/U}{W/U} announcement shape has a reducer whose
// pip it can legally assign to either of its pips.
const whiteReducerSrc = "Name:White Reducer\nManaCost:1 W\nTypes:Creature Human Cleric\nPT:1/2\n" +
	"S:Mode$ ReduceCost | ValidCard$ Card | Type$ Spell | Activator$ You | Amount$ 1 | Color$ W | Description$ Spells you cast cost {W} less to cast.\n" +
	"Oracle:x\n"

const testBearSrc = "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// castByName returns seat p's cast option for the named card.
func castByName(t *testing.T, e *Engine, p state.PlayerID, name string) *decision.Option {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("not at priority for %s: %+v", name, d)
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj != 0 && e.G.Obj(o.Obj) != nil && e.G.Obj(o.Obj).Face() != nil &&
			e.G.Obj(o.Obj).Face().Name == name {
			return &o
		}
	}
	return nil
}

// reduceOf returns the single evaluated ReduceCost amount a cast of id by p
// draws, failing if there is not exactly one reduction.
func reduceOf(t *testing.T, e *Engine, p state.PlayerID, id state.ObjID) int32 {
	t.Helper()
	mods := e.costModifiers(p, id, spellScope(""))
	var n int32
	for _, red := range mods.Reduces {
		n += red.Generic + red.Colored.Total()
	}
	return n
}

// TestAuriokSteelshaperDiscountsOnlyEquip pins ValidSpell$: the reduction
// applies to the Equip activation (whose expansion carries Keyword$ Equip)
// and to nothing else — a {1}{W} creature cast is NOT discounted even though
// the static's ValidCard$ Card would admit every card.
func TestAuriokSteelshaperDiscountsOnlyEquip(t *testing.T) {
	t.Parallel()
	e, cfg, fan := newFixtureDeck(t, 61, equiperSrc, thaliaRv2cSrc, steelshaperSrc)
	if got := e.AbilityCosts(0, fan); len(got) != 1 || got[0] != "2" {
		t.Fatalf("equip costs before the Steelshaper: %v, want [2]", got)
	}
	putCreature(t, e, 0, equiperSrc)
	putCreature(t, e, 0, steelshaperSrc)
	if got := e.AbilityCosts(0, fan); len(got) != 1 || got[0] != "1" {
		t.Fatalf("equip costs with the Steelshaper out: %v, want [1]", got)
	}
	// And the discount is real money: one colourless mana activates the
	// two-mana equip.
	addMana(t, e, 0, "C")
	if kinds(e.legalActions(0))["ability"] != 1 {
		t.Fatal("the discounted equip should be activatable with one mana")
	}
	replayCheck(t, e, cfg)

	// The probe: Thalia {1}{W} from a pool of exactly {W} — no discount.
	e2, _, _ := newFixtureDeck(t, 62, thaliaRv2cSrc, steelshaperSrc)
	putCreature(t, e2, 0, steelshaperSrc)
	addMana(t, e2, 0, "W")
	if castByName(t, e2, 0, "Thalia, Guardian of Thraben") != nil {
		t.Fatal("Steelshaper must not discount a plain creature cast (ValidSpell$ Activated.Equip)")
	}
}

// TestBiomancersFamiliarOnlyReducesItsControllersCreatures pins the
// controller-relative ValidCard$ context. The Familiar belongs to seat 1, so
// its Creature.YouCtrl filter must not reduce seat 0's creature activation;
// the same static does reduce seat 1's creature activation. In particular,
// the payer is only the action actor -- it must never become the `You` used
// to evaluate a static's ValidCard$.
func TestBiomancersFamiliarOnlyReducesItsControllersCreatures(t *testing.T) {
	e, cfg, _ := newFixtureDeck(t, 621, testBearSrc)
	ours := putToken(t, e, 0, costlyCreatureAbilitySrc, state.ZBattlefield)
	theirs := putToken(t, e, 1, costlyCreatureAbilitySrc, state.ZBattlefield)
	putToken(t, e, 1, biomancersFamiliarSrc, state.ZBattlefield)

	if got := e.AbilityCosts(0, ours); len(got) != 1 || got[0] != "3" {
		t.Fatalf("opponent's Familiar reduced our creature activation to %v, want [3]", got)
	}
	if got := e.AbilityCosts(1, theirs); len(got) != 1 || got[0] != "1" {
		t.Fatalf("Familiar did not reduce its controller's creature activation to %v, want [1]", got)
	}
	replayCheck(t, e, cfg)
}

// TestRakdosReduceCostEvaluatesTheLifeSVar pins the SVar Amount$: the
// reduction is Count$LifeOppsLostThisTurn — 0 with no life lost (the old
// parseAmount fallback made it 1), 2 after an opponent lost 2 life.
func TestRakdosReduceCostEvaluatesTheLifeSVar(t *testing.T) {
	t.Parallel()
	e, cfg, _ := newFixtureDeck(t, 63, testBearSrc, rakdosSrc)
	_ = putCreature(t, e, 0, rakdosSrc)
	var bear state.ObjID
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(id).Face().Name == "Bear" {
			bear = id
		}
	}
	if bear == 0 {
		t.Fatal("bear not in hand")
	}
	addMana(t, e, 0, "G")
	if got := reduceOf(t, e, 0, bear); got != 0 {
		t.Fatalf("reduction with no life lost = %d, want 0", got)
	}
	if castByName(t, e, 0, "Bear") != nil {
		t.Fatal("Bear {1}{G} with only {G} must not be castable when the reduction is 0")
	}
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: -2})
	e.priorityRound()
	if got := reduceOf(t, e, 0, bear); got != 2 {
		t.Fatalf("reduction after 2 life lost = %d, want 2", got)
	}
	if castByName(t, e, 0, "Bear") == nil {
		t.Fatal("Bear should be castable now: the reduction 2 covers the generic")
	}
	replayCheck(t, e, cfg)
}

// TestHeraldOfWarReducesByItsCounters pins Count$CardCounters: the reduction
// tracks the +1/+1 counters ON the herald itself, live as they accumulate.
func TestHeraldOfWarReducesByItsCounters(t *testing.T) {
	t.Parallel()
	angelSrc := "Name:Angel\nManaCost:3 W\nTypes:Creature Angel\nPT:2/2\nOracle:x\n"
	e, cfg, _ := newFixtureDeck(t, 64, angelSrc, heraldRv2cSrc)
	herald := putCreature(t, e, 0, heraldRv2cSrc)
	var angel state.ObjID
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(id).Face().Name == "Angel" {
			angel = id
		}
	}
	if angel == 0 {
		t.Fatal("angel not in hand")
	}
	addMana(t, e, 0, "W")
	if got := reduceOf(t, e, 0, angel); got != 0 {
		t.Fatalf("reduction with no counters = %d, want 0", got)
	}
	if castByName(t, e, 0, "Angel") != nil {
		t.Fatal("Angel {3}{W} with only {W} must not be castable at 0 counters")
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: herald, Counter: "P1P1", Amount: 3})
	e.priorityRound()
	if got := reduceOf(t, e, 0, angel); got != 3 {
		t.Fatalf("reduction with 3 counters = %d, want 3", got)
	}
	if castByName(t, e, 0, "Angel") == nil {
		t.Fatal("Angel should be castable: three counters cover the generic 3")
	}
	replayCheck(t, e, cfg)
}

// TestGhaltaSelfReductionFromHand pins the host-self static: Ghalta's own
// EffectZone$ All reduction is live while the card is still IN HAND, so 18
// power of creatures turns {10}{G}{G} into {G}{G}.
func TestGhaltaSelfReductionFromHand(t *testing.T) {
	t.Parallel()
	bigSrc := "Name:Big\nManaCost:6 G\nTypes:Creature Beast\nPT:9/9\nOracle:x\n"
	e, cfg, ghalta := newFixtureDeck(t, 65, ghaltaSrc, bigSrc, bigSrc)
	putCreature(t, e, 0, bigSrc)
	putCreature(t, e, 0, bigSrc)
	addMana(t, e, 0, "GG")
	if got := reduceOf(t, e, 0, ghalta); got != 18 {
		t.Fatalf("Ghalta reduction with 18 power out = %d, want 18", got)
	}
	opt := castByName(t, e, 0, "Ghalta, Primal Hunger")
	if opt == nil {
		t.Fatal("Ghalta must be castable from hand with GG and 18 power out")
	}
	submitChoices(t, e, opt.Index)
	if e.G.Obj(ghalta).Zone != state.ZStack {
		t.Fatalf("Ghalta on %s, want stack", e.G.Obj(ghalta).Zone)
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("pool after the cast = %d, want 0", e.G.Players[0].Pool.Total())
	}
	replayCheck(t, e, cfg)
}

// TestColorReduceRemovesColoredPips pins Color$: Khalni Hydra's reduction is
// one GREEN pip per green creature (SVar count), not a generic discount.
func TestColorReduceRemovesColoredPips(t *testing.T) {
	t.Parallel()
	greenSrc := "Name:Elvish\nManaCost:G\nTypes:Creature Elf Druid\nPT:1/1\nOracle:x\n"
	e, cfg, hydra := newFixtureDeck(t, 66, hydraRv2cSrc, greenSrc, greenSrc)
	putCreature(t, e, 0, greenSrc)
	putCreature(t, e, 0, greenSrc)
	addMana(t, e, 0, "GGGGGG")
	if got := reduceOf(t, e, 0, hydra); got != 2 {
		t.Fatalf("Khalni Hydra reduction with 2 green creatures = %d, want 2 (green pips)", got)
	}
	mods := e.costModifiers(0, hydra, spellScope(""))
	if mods.Reduces[0].Colored[state.MG] != 2 || mods.Reduces[0].Generic != 0 {
		t.Fatalf("the reduction must name 2 green pips, got %+v", mods.Reduces[0])
	}
	opt := castByName(t, e, 0, "Khalni Hydra")
	if opt == nil {
		t.Fatal("Khalni Hydra must be castable with 6 green after a 2-pip colour reduction")
	}
	submitChoices(t, e, opt.Index)
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("pool after the cast = %d, want 0", e.G.Players[0].Pool.Total())
	}
	replayCheck(t, e, cfg)
}

// TestMinManaFloorKeepsOneMana pins MinMana$: Heartstone's reduction can't
// reduce a creature ability's activation cost below one mana.
func TestMinManaFloorKeepsOneMana(t *testing.T) {
	t.Parallel()
	pumperSrc := "Name:Pumper\nManaCost:1 G\nTypes:Creature Beast\nPT:1/1\n" +
		"A:AB$ Pump | Cost$ 1 | Defined$ Self | NumAtt$ +1 | SpellDescription$ +1/+0.\n" +
		"Oracle:x\n"
	e, _, pumper := newFixtureDeck(t, 67, pumperSrc, heartstoneSrc)
	putCreature(t, e, 0, pumperSrc)
	putCreature(t, e, 0, heartstoneSrc)
	if got := e.AbilityCosts(0, pumper); len(got) != 1 || got[0] != "1" {
		t.Fatalf("Heartstone's reduction with MinMana$ 1 leaves %v, want [1]", got)
	}
	e.G.Players[0].Pool[state.MC] = 0
	e.priorityRound()
	if kinds(e.legalActions(0))["ability"] != 0 {
		t.Fatal("the floored ability still costs one mana; with an empty pool it must not be offered")
	}
	e.G.Players[0].Pool[state.MC] = 1
	e.priorityRound()
	if kinds(e.legalActions(0))["ability"] != 1 {
		t.Fatal("the floored ability should be activatable with one mana")
	}
}

// TestTrinisphereSetCostFloor pins SetCost (RaiseTo$ True + IsPresent$): an
// untapped Trinisphere raises every cheaper spell's total mana to 3; a tapped
// one raises nothing.
func TestTrinisphereSetCostFloor(t *testing.T) {
	t.Parallel()
	zapSrc := "Name:Zap\nManaCost:1\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n"
	e, cfg, zap := newFixtureDeck(t, 68, zapSrc, trinisphereSrc)
	putCreature(t, e, 0, trinisphereSrc)
	addMana(t, e, 0, "C")
	if castByName(t, e, 0, "Zap") != nil {
		t.Fatal("a {1} spell under an untapped Trinisphere costs {3}; one mana must not be enough")
	}
	addMana(t, e, 0, "CC")
	opt := castByName(t, e, 0, "Zap")
	if opt == nil {
		t.Fatal("the Trinisphere-raised Zap should be castable with 3 mana")
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Zap's target decision: %+v", d)
	}
	submitChoices(t, e, 0)
	if e.G.Obj(zap).Zone != state.ZStack {
		t.Fatalf("Zap on %s, want stack", e.G.Obj(zap).Zone)
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("pool after paying the raised cost = %d, want 0", e.G.Players[0].Pool.Total())
	}
	replayCheck(t, e, cfg)

	// A tapped Trinisphere stops applying (IsPresent$ Card.Self+untapped).
	e3, _, _ := newFixtureDeck(t, 69, zapSrc, trinisphereSrc)
	tri3 := putCreature(t, e3, 0, trinisphereSrc)
	e3.emit(events.Event{Kind: events.Tap, Obj: tri3})
	addMana(t, e3, 0, "C")
	if castByName(t, e3, 0, "Zap") == nil {
		t.Fatal("a tapped Trinisphere must not raise the cost")
	}
}

// TestTrinisphereSetCostDoesNotRaiseTwobridGenericFace pins CR 202.4b at
// the SetCost boundary. {2/W} already has mana value 2 before its face is
// announced; Trinisphere therefore adds only one generic to its generic-face
// payment, letting exactly {C}{C}{C} pay the resulting {3}.
func TestTrinisphereSetCostDoesNotRaiseTwobridGenericFace(t *testing.T) {
	t.Parallel()
	if got := ParseCost("2/W").CMC(); got != 2 {
		t.Fatalf("{2/W} mana value = %d, want 2", got)
	}
	prowlerSrc := "Name:Prowler\nManaCost:2/W\nTypes:Creature Cat\nPT:2/1\nOracle:x\n"
	e, cfg, prowler := newFixtureDeck(t, 681, prowlerSrc, trinisphereSrc)
	putCreature(t, e, 0, trinisphereSrc)
	addMana(t, e, 0, "CCC")

	opt := castByName(t, e, 0, "Prowler")
	if opt == nil {
		t.Fatal("{2/W} under Trinisphere must be castable with exactly {C}{C}{C}")
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 1 || d.Options[0].Kind != "pay_generic" {
		t.Fatalf("{2/W} generic-face choice under Trinisphere = %+v, want only pay_generic", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	if e.G.Obj(prowler).Zone != state.ZStack || e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("after {3} generic payment: zone=%s pool=%v, want stack and empty pool",
			e.G.Obj(prowler).Zone, e.G.Players[0].Pool)
	}
	replayCheck(t, e, cfg)

	// CR 601.2b on the OFFER side: with {W}{W} neither announced face
	// completes the raised cost — the white face announces to {W}+{2} and
	// the generic face to {3} — so the cast must not be offered at all. The
	// composed unresolved cost ({2/W}+{1}, CR 202.4b's generic-face mana
	// value) IS payable from {W}{W} by its white face plus one generic,
	// which is exactly why the offer gate must enumerate the pip faces.
	e2, _, _ := newFixtureDeck(t, 682, prowlerSrc, trinisphereSrc)
	putCreature(t, e2, 0, trinisphereSrc)
	addMana(t, e2, 0, "WW")
	if castByName(t, e2, 0, "Prowler") != nil {
		t.Fatal("{2/W} under an untapped Trinisphere with only {W}{W} has no completing announcement; the cast must not be offered")
	}

	// With {W}{W}{W} both faces complete, so the cast IS offered and the
	// white face announced first still finishes the raised {W}+{2}.
	e3, cfg3, prowler3 := newFixtureDeck(t, 683, prowlerSrc, trinisphereSrc)
	putCreature(t, e3, 0, trinisphereSrc)
	addMana(t, e3, 0, "WWW")
	opt3 := castByName(t, e3, 0, "Prowler")
	if opt3 == nil {
		t.Fatal("{2/W} under an untapped Trinisphere with {W}{W}{W} must be castable by either face")
	}
	submitChoices(t, e3, opt3.Index)
	d3 := e3.Pending()
	if d3 == nil || d3.Kind != decision.KChoose || len(d3.Options) != 2 || d3.Options[0].Kind != "pay_W" || d3.Options[1].Kind != "pay_generic" {
		t.Fatalf("{2/W} pip under Trinisphere with WWW: %+v, want both faces (each completes the raised cost)", d3)
	}
	submitChoices(t, e3, d3.Options[0].Index)
	if e3.G.Obj(prowler3).Zone != state.ZStack || e3.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("after {W}+{2} payment: zone=%s pool=%v, want stack and empty pool",
			e3.G.Obj(prowler3).Zone, e3.G.Players[0].Pool)
	}
	replayCheck(t, e3, cfg3)
}

// TestSpectralProcessionTwobridPayments pins the monocolour hybrid: each
// {2/W} pip is announced as one decision, payable by one white OR two
// generic (CR 107.4e).
func TestSpectralProcessionTwobridPayments(t *testing.T) {
	t.Parallel()
	// All white: three announcements, each paid white.
	e, cfg, proc := newFixtureDeck(t, 70, spectralProcessionSrc)
	addMana(t, e, 0, "WWW")
	opt := castByName(t, e, 0, "Spectral Procession")
	if opt == nil {
		t.Fatal("Spectral Procession must be payable with WWW")
	}
	submitChoices(t, e, opt.Index)
	for i := 0; i < 3; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "pay_W" {
			t.Fatalf("pip %d: %+v", i, d)
		}
		// With exactly WWW there is one legal completion -- every pip paid
		// white -- so the generic face (2 mana each) is NOT offered: choosing
		// it on any pip would leave the rest of the cost unpayable.
		if len(d.Options) != 1 {
			t.Fatalf("pip %d: options %+v, want only pay_W (the generic face cannot complete WWW)", i, d.Options)
		}
		submitChoices(t, e, 0)
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("pool after WWW payment = %d, want 0", e.G.Players[0].Pool.Total())
	}
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(proc).Zone != state.ZGraveyard {
		t.Fatalf("resolved procession on %s, want graveyard", e.G.Obj(proc).Zone)
	}
	replayCheck(t, e, cfg)

	// Six generic (no white at all): each pip announced, each paid generic.
	e2, cfg2, proc2 := newFixtureDeck(t, 71, spectralProcessionSrc)
	addMana(t, e2, 0, "CCCCCC")
	opt2 := castByName(t, e2, 0, "Spectral Procession")
	if opt2 == nil {
		t.Fatal("Spectral Procession must be payable with six generic mana")
	}
	submitChoices(t, e2, opt2.Index)
	for i := 0; i < 3; i++ {
		d := e2.Pending()
		if d == nil || d.Kind != decision.KChoose || len(d.Options) != 1 || d.Options[0].Kind != "pay_generic" {
			t.Fatalf("generic pip %d: %+v", i, d)
		}
		submitChoices(t, e2, 0)
	}
	if e2.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("pool after generic payment = %d, want 0", e2.G.Players[0].Pool.Total())
	}
	passUntilStackEmpty(t, e2, 20)
	if e2.G.Obj(proc2).Zone != state.ZGraveyard {
		t.Fatalf("resolved procession on %s, want graveyard", e2.G.Obj(proc2).Zone)
	}
	replayCheck(t, e2, cfg2)

	// Two white and four generic: a mixed payment also reaches the tokens.
	e3, _, _ := newFixtureDeck(t, 72, spectralProcessionSrc)
	addMana(t, e3, 0, "WCCCC")
	if castByName(t, e3, 0, "Spectral Procession") == nil {
		t.Fatal("the mixed W+4C pool should pay one pip white and two generic")
	}
	// Three mana short of three pips: W pays one, and only two generic are
	// left for the other two {2/W} faces — not offered at all.
	e4, _, _ := newFixtureDeck(t, 73, spectralProcessionSrc)
	addMana(t, e4, 0, "WCC")
	if castByName(t, e4, 0, "Spectral Procession") != nil {
		t.Fatal("three mana cannot pay three {2/W} pips")
	}
}

// TestTwobridGenericFaceOnlyWhenFeasible pins the CR 601.2b legality question
// manaAsk now enforces: a monocolour hybrid's generic face is offered only
// when some assignment of the still-unsettled pips makes the whole cost
// payable. A single {2/W} with exactly {W} is paid only one way (the colour
// face), so the generic face is withheld; with {W}{W} the generic face is
// genuinely payable, so it is offered and selecting it casts the spell cleanly
// end to end rather than stranding the cast in an abort.
func TestTwobridGenericFaceOnlyWhenFeasible(t *testing.T) {
	t.Parallel()
	prowlerSrc := "Name:Prowler\nManaCost:2W\nTypes:Creature Cat\nPT:2/1\nOracle:x\n"

	// Exactly {W}: only the colour face completes the {2/W} pip.
	e, cfg, prowler := newFixtureDeck(t, 80, prowlerSrc)
	addMana(t, e, 0, "W")
	opt := castByName(t, e, 0, "Prowler")
	if opt == nil {
		t.Fatal("Prowler {2/W} must be castable with {W}")
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("pip decision: %+v", d)
	}
	if len(d.Options) != 1 || d.Options[0].Kind != "pay_W" {
		t.Fatalf("with only {W} the generic face is not feasible; options %+v", d.Options)
	}
	submitChoices(t, e, d.Options[0].Index)
	if e.G.Obj(prowler).Zone != state.ZStack {
		t.Fatalf("Prowler on %s, want stack", e.G.Obj(prowler).Zone)
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("pool after {W} payment = %d, want 0", e.G.Players[0].Pool.Total())
	}
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(prowler).Zone != state.ZBattlefield {
		t.Fatalf("resolved prowler on %s, want battlefield", e.G.Obj(prowler).Zone)
	}
	replayCheck(t, e, cfg)

	// {W}{W}: either face completes the cast; the generic face is offered and
	// selecting it casts the spell cleanly (no abort at targetAsk).
	e2, cfg2, prowler2 := newFixtureDeck(t, 81, prowlerSrc)
	addMana(t, e2, 0, "WW")
	opt2 := castByName(t, e2, 0, "Prowler")
	if opt2 == nil {
		t.Fatal("Prowler {2/W} must be castable with {W}{W}")
	}
	submitChoices(t, e2, opt2.Index)
	d2 := e2.Pending()
	if d2 == nil || d2.Kind != decision.KChoose {
		t.Fatalf("pip decision 2: %+v", d2)
	}
	genIdx := -1
	for _, o := range d2.Options {
		if o.Kind == "pay_generic" {
			genIdx = o.Index
		}
	}
	if genIdx < 0 {
		t.Fatalf("the generic face must be offered when {W}{W} can pay it: %+v", d2.Options)
	}
	submitChoices(t, e2, genIdx)
	if e2.G.Obj(prowler2).Zone != state.ZStack {
		t.Fatalf("Prowler (generic face) on %s, want stack", e2.G.Obj(prowler2).Zone)
	}
	if e2.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("pool after generic payment = %d, want 0", e2.G.Players[0].Pool.Total())
	}
	passUntilStackEmpty(t, e2, 20)
	if e2.G.Obj(prowler2).Zone != state.ZBattlefield {
		t.Fatalf("resolved prowler2 on %s, want battlefield", e2.G.Obj(prowler2).Zone)
	}
	replayCheck(t, e2, cfg2)
}

// TestTwobridAnnouncementSeesRaiseCost proves the flexible-pip announcement
// uses the same total-cost composition as payment. With Thalia out, {2/W}
// from {W}{W} has exactly one completion: pay white for the pip and the other
// white for Thalia's tax. Its generic face would make the final cost {3}, so
// CR 601.2b must not offer it and choosing the legal face must reach stack.
func TestTwobridAnnouncementSeesRaiseCost(t *testing.T) {
	t.Parallel()
	spellSrc := "Name:Taxed Prowler\nManaCost:2/W\nTypes:Sorcery\nOracle:x\n"
	e, cfg, spell := newFixtureDeck(t, 82, spellSrc, thaliaRv2cSrc)
	putCreature(t, e, 0, thaliaRv2cSrc)
	addMana(t, e, 0, "WW")

	opt := castByName(t, e, 0, "Taxed Prowler")
	if opt == nil {
		t.Fatal("{W}{W} must pay the white face plus Thalia's raise")
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("pip decision: %+v", d)
	}
	if len(d.Options) != 1 || d.Options[0].Kind != "pay_W" {
		t.Fatalf("generic {2} face plus Thalia is unpayable from {W}{W}; options %+v", d.Options)
	}
	submitChoices(t, e, d.Options[0].Index)
	if e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("Taxed Prowler on %s, want stack", e.G.Obj(spell).Zone)
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("pool after white face plus Thalia raise = %d, want 0", e.G.Players[0].Pool.Total())
	}
	replayCheck(t, e, cfg)
}

// TestSnowCostPaidOnlyBySnowMana pins {S} (CR 107.4h): the pip is payable
// only by a mana a snow permanent produced, and paying it consumes the snow
// unit (the pool slot and the parallel tally together).
func TestSnowCostPaidOnlyBySnowMana(t *testing.T) {
	t.Parallel()
	e, cfg, golem := newFixtureDeck(t, 74, icehideGolemSrc, snowWastesSrc)
	wastes := moveSeeded(t, e, 0, snowWastesSrc, state.ZBattlefield)
	addMana(t, e, 0, "C")
	if castByName(t, e, 0, "Icehide Golem") != nil {
		t.Fatal("a plain colourless mana must not pay {S}")
	}
	// Use the normal mana-ability path, not a hand-written ManaAdd: effMana
	// must inspect the snow source and tag its output as snow mana.
	activateMana(t, e, wastes)
	if e.G.Players[0].Pool[state.MC] != 2 || e.G.Players[0].Snow[state.MC] != 1 {
		t.Fatalf("pool %+v snow %+v after the snow tap", e.G.Players[0].Pool, e.G.Players[0].Snow)
	}
	opt := castByName(t, e, 0, "Icehide Golem")
	if opt == nil {
		t.Fatal("the snow colourless mana must pay {S}")
	}
	submitChoices(t, e, opt.Index)
	if e.G.Obj(golem).Zone != state.ZStack {
		t.Fatalf("golem on %s, want stack", e.G.Obj(golem).Zone)
	}
	// The snow unit was consumed; the plain unit remains.
	if e.G.Players[0].Pool[state.MC] != 1 || e.G.Players[0].Snow[state.MC] != 0 {
		t.Fatalf("after paying {S}: pool %+v snow %+v, want one plain colourless left",
			e.G.Players[0].Pool, e.G.Players[0].Snow)
	}
	replayCheck(t, e, cfg)

	// The same pip gates an ability activation: Boreal Centaur's {S} pump.
	e2, cfg2, _ := newFixtureDeck(t, 75, borealCentaurSrc)
	centaur := putCreature(t, e2, 0, borealCentaurSrc)
	addMana(t, e2, 0, "G")
	e2.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "SG", Amount: 1})
	e2.priorityRound()
	if e2.G.Players[0].Pool[state.MG] != 2 || e2.G.Players[0].Snow[state.MG] != 1 {
		t.Fatalf("centaur pool %+v snow %+v", e2.G.Players[0].Pool, e2.G.Players[0].Snow)
	}
	if kinds(e2.legalActions(0))["ability"] != 1 {
		t.Fatal("the {S} pump should be activatable with snow mana in the pool")
	}
	// And without the snow unit the same pool cannot pay it.
	e3, _, _ := newFixtureDeck(t, 76, borealCentaurSrc)
	addMana(t, e3, 0, "G")
	if kinds(e3.legalActions(0))["ability"] != 0 {
		t.Fatal("plain green mana must not pay a {S} activation")
	}
	_ = centaur
	replayCheck(t, e2, cfg2)
}

// TestColorlessHybridCostsParseAndPay covers the {C/W} half of CR 107.4e.
// Unlike generic mana, the colourless face is a strict {C} pip: a white mana
// pays the other face, but an unrelated coloured mana does not. The cast path
// also proves pay_C is dispatched as an announced flexible-pip payment rather
// than falling through and repeating its decision.
func TestColorlessHybridCostsParseAndPay(t *testing.T) {
	t.Parallel()
	c := ParseCost("C/W")
	if c.Generic != 0 || len(c.Hybrid) != 1 || c.Hybrid[0] != (ManaPair{A: 'C', B: 'W'}) {
		t.Fatalf("ParseCost(\"C/W\") = %+v, want one C/W hybrid", c)
	}
	if !poolCanPay(c, pool(0, 0, 0, 0, 0, 1)) || !poolCanPay(c, pool(1, 0, 0, 0, 0, 0)) {
		t.Fatal("C/W must be payable by either C or W")
	}
	if poolCanPay(c, pool(0, 1, 0, 0, 0, 0)) {
		t.Fatal("C/W must not be payable by U")
	}

	e, cfg, card := newFixtureDeck(t, 83, colorlessHybridSrc)
	addMana(t, e, 0, "C")
	opt := castByName(t, e, 0, "Colourless Hybrid")
	if opt == nil {
		t.Fatal("C/W must be castable from a C pool")
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 1 || d.Options[0].Kind != "pay_C" {
		t.Fatalf("C/W payment decision = %+v, want only pay_C", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	if e.G.Obj(card).Zone != state.ZStack || e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("after C payment: zone=%s pool=%v, want stack and empty pool", e.G.Obj(card).Zone, e.G.Players[0].Pool)
	}
	replayCheck(t, e, cfg)
}

// TestColorReductionSeesTheAnnouncedHybridFace ensures Color$ is applied to
// the resolved face, not to an opaque hybrid symbol. The {W/U} spell is free
// by its white half under the real reduction, while its blue half remains
// unaffordable from an empty pool.
func TestColorReductionSeesTheAnnouncedHybridFace(t *testing.T) {
	t.Parallel()
	spellSrc := "Name:Hybrid Spell\nManaCost:W/U\nTypes:Sorcery\nOracle:x\n"
	reducerSrc := "Name:White Reducer\nManaCost:2\nTypes:Artifact\n" +
		"S:Mode$ ReduceCost | ValidCard$ Card | Type$ Spell | Color$ W | Amount$ 1 | Description$ White spells cost {W} less.\nOracle:x\n"
	e, cfg, spell := newFixtureDeck(t, 84, spellSrc, reducerSrc)
	putCreature(t, e, 0, reducerSrc)
	e.priorityRound()
	opt := castByName(t, e, 0, "Hybrid Spell")
	if opt == nil {
		t.Fatal("the W face of {W/U} reduced by Color$ W must be castable for no mana")
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 1 || d.Options[0].Kind != "pay_W" {
		t.Fatalf("hybrid face decision = %+v, want only the reduced white face", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	if e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("reduced hybrid spell on %s, want stack", e.G.Obj(spell).Zone)
	}
	replayCheck(t, e, cfg)
}

// TestValidTargetModifierRepricesBeforePayment covers the target-dependent
// half of CostAdjustment. The W half of {W/U} is free only when the spell
// targets the reducer. The second, ordinary permanent must not appear in the
// target menu: with no mana it would reprice to an unaffordable W cost and
// used to reverse the already-announced cast after the player selected it.
func TestValidTargetModifierRepricesBeforePayment(t *testing.T) {
	t.Parallel()
	spellSrc := "Name:Targeted Growth\nManaCost:W/U\nTypes:Sorcery\n" +
		"A:SP$ Pump | ValidTgts$ Permanent | NumAtt$ +1 | NumDef$ +1\nOracle:x\n"
	reducerSrc := "Name:Target Discount\nManaCost:2\nTypes:Artifact\n" +
		"S:Mode$ ReduceCost | ValidTarget$ Card.Self | Activator$ You | Type$ Spell | Color$ W | Amount$ 1 | Description$ Spells you cast that target CARDNAME cost {W} less.\nOracle:x\n"
	otherSrc := "Name:Other Permanent\nManaCost:1\nTypes:Artifact\nOracle:x\n"
	e, cfg, spell := newFixtureDeck(t, 85, spellSrc, reducerSrc, otherSrc)
	reducer := putCreature(t, e, 0, reducerSrc)
	other := putCreature(t, e, 0, otherSrc)
	e.priorityRound()
	opt := castByName(t, e, 0, "Targeted Growth")
	if opt == nil {
		t.Fatal("a target earning a Color$ reduction must make the W face offerable with no mana")
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 1 || d.Options[0].Kind != "pay_W" {
		t.Fatalf("hybrid payment decision = %+v, want only pay_W", d)
	}
	submitChoices(t, e, d.Options[0].Index)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("target decision = %+v", d)
	}
	idx := -1
	for _, target := range d.Options {
		if target.Obj == reducer {
			idx = target.Index
		}
		if target.Obj == other {
			t.Fatalf("unaffordable nonmatching target was offered: %+v", d.Options)
		}
	}
	if idx < 0 || len(d.Options) != 1 {
		t.Fatalf("the reducer must be the sole affordable target: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	if e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("targeted spell on %s, want stack", e.G.Obj(spell).Zone)
	}
	replayCheck(t, e, cfg)
}

// TestHybridPhyrexianCostsParseAndPay pins the three-part compleated symbol:
// one pip payable by either colour or two life.
func TestHybridPhyrexianCostsParseAndPay(t *testing.T) {
	t.Parallel()
	// Forge also emits the same symbol P-first on the real Lukka, Bound to
	// Ruin script; normalise that spelling before payment choices are built.
	pfirst := ParseCost("PRG")
	if len(pfirst.HybridPhyrexian) != 1 || pfirst.HybridPhyrexian[0] != (HybridPhyrexian{A: 'R', B: 'G'}) {
		t.Fatalf("ParseCost(\"PRG\") = %+v, want one R/G/P hybrid-Phyrexian pip", pfirst)
	}
	if !poolCanPay(pfirst, pool(0, 0, 0, 1, 0, 0)) || !payable(pfirst, state.Mana{}, state.Mana{}, [7]state.Mana{}, 2) {
		t.Fatal("PRG must be payable by R or by two life")
	}

	c := ParseCost("1 G GWP W")
	if c.Generic != 1 || c.Colored[state.MG] != 1 || c.Colored[state.MW] != 1 ||
		len(c.HybridPhyrexian) != 1 || c.HybridPhyrexian[0] != (HybridPhyrexian{A: 'G', B: 'W'}) {
		t.Fatalf("ParseCost(\"1 G GWP W\") = %+v", c)
	}
	if !payable(c, pool(1, 0, 0, 0, 2, 1), state.Mana{}, [7]state.Mana{}, 0) {
		t.Fatal("W + GG + a green face + generic must pay the cost with no life")
	}
	// Two life pays the compleated face when no colour has a spare unit left
	// for it: the colour pips take their own units, the pip goes to life, and
	// the generic lands on the colourless unit.
	pay, ok := resolveMana(c, pool(1, 0, 0, 0, 1, 1), state.Mana{}, [7]state.Mana{}, 10, nil)
	if !ok || pay.LifeSpent != 2 {
		t.Fatalf("W+G+C with life must pay the compleated pip with two life: %+v ok=%v", pay, ok)
	}
	if !payable(c, pool(1, 0, 0, 0, 1, 1), state.Mana{}, [7]state.Mana{}, 10) {
		t.Fatal("the same pool is payable with life offered")
	}
	if payable(c, pool(1, 0, 0, 0, 1, 1), state.Mana{}, [7]state.Mana{}, 1) {
		t.Fatal("one life is not enough for the compleated face")
	}
	// Pool with no white at all, life offered: the pip goes to life.
	c2 := ParseCost("GWP")
	pay2, ok2 := resolveMana(c2, state.Mana{}, state.Mana{}, [7]state.Mana{}, 2, nil)
	if !ok2 || pay2.LifeSpent != 2 {
		t.Fatalf("GWP with an empty pool and 2 life = %+v ok=%v, want two life", pay2, ok2)
	}
	if !payable(c2, state.Mana{}, state.Mana{}, [7]state.Mana{}, 2) {
		t.Fatal("GWP must be payable with exactly two life")
	}
	if poolCanPay(c2, state.Mana{}) {
		t.Fatal("GWP is not pool-payable by nothing")
	}
}

// TestAffectedZoneScopesAbilityModifier pins AffectedZone$ on a Type$
// Ability reduction: a static scoped to Graveyard abilities must not reach a
// battlefield ability.
func TestAffectedZoneScopesAbilityModifier(t *testing.T) {
	t.Parallel()
	pumperSrc := "Name:Pumper\nManaCost:1 G\nTypes:Creature Beast\nPT:1/1\n" +
		"A:AB$ Pump | Cost$ 2 | Defined$ Self | NumAtt$ +1 | SpellDescription$ +1/+0.\n" +
		"Oracle:x\n"
	convergenceSrc := "Name:Convergence\nManaCost:3\nTypes:Artifact\n" +
		"S:Mode$ ReduceCost | ValidCard$ Card.YouOwn | Type$ Ability | Amount$ 2 | MinMana$ 1 | AffectedZone$ Graveyard | Description$ Activated abilities of cards in your graveyard cost {2} less to activate.\n" +
		"Oracle:x\n"
	e, _, pumper := newFixtureDeck(t, 77, pumperSrc, convergenceSrc)
	putCreature(t, e, 0, convergenceSrc)
	// The battlefield ability is untouched by the graveyard-scoped static.
	if got := e.AbilityCosts(0, pumper); len(got) != 1 || got[0] != "2" {
		t.Fatalf("battlefield ability costs under a graveyard-scoped reducer: %v, want [2]", got)
	}
}

// TestCheckSVarGatesTheReduction pins CheckSVar$/SVarCompare$ (Monk Class's
// real "second spell" line): the reduction holds only when the SVar
// evaluation matches the comparison, and the count itself is the
// Count$ThisTurnCast_Card.YouCtrl log fold.
func TestCheckSVarGatesTheReduction(t *testing.T) {
	t.Parallel()
	dualcastSrc := "Name:Dualcast\nManaCost:2\nTypes:Enchantment\n" +
		"S:Mode$ ReduceCost | ValidCard$ Card | Type$ Spell | Activator$ You | Amount$ 1 | CheckSVar$ YouCastThisTurn | SVarCompare$ EQ1 | Description$ The second spell you cast each turn costs {1} less to cast.\n" +
		"SVar:YouCastThisTurn:Count$ThisTurnCast_Card.YouCtrl\n" +
		"Oracle:The second spell you cast each turn costs {1} less to cast.\n"
	boltSrc := "Name:Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n"
	e, cfg, _ := newFixtureDeck(t, 78, testBearSrc, dualcastSrc, boltSrc)
	putCreature(t, e, 0, dualcastSrc)
	var bear state.ObjID
	for _, id := range e.G.Zone(state.ZHand, 0) {
		switch e.G.Obj(id).Face().Name {
		case "Bear":
			bear = id
		}
	}
	addMana(t, e, 0, "RG")
	if got := reduceOf(t, e, 0, bear); got != 0 {
		t.Fatalf("first-spell reduction = %d, want 0 (YouCastThisTurn is 0, not EQ1)", got)
	}
	// Cast the counted first spell for real (Bolt, targeted and resolved).
	opt := castByName(t, e, 0, "Bolt")
	if opt == nil {
		t.Fatal("Bolt should be castable with {R}")
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Bolt's target decision: %+v", d)
	}
	submitChoices(t, e, 0)
	passUntilStackEmpty(t, e, 20)
	addMana(t, e, 0, "G")
	if got := reduceOf(t, e, 0, bear); got != 1 {
		t.Fatalf("second-spell reduction = %d, want 1", got)
	}
	if castByName(t, e, 0, "Bear") == nil {
		t.Fatal("Bear should be castable: the discount 1 covers the generic")
	}
	replayCheck(t, e, cfg)
}

// TestConditionPlayerTurnGatesTheReduction pins Condition$ PlayerTurn /
// NotPlayerTurn (Discontinuity's real line): the reduction applies only on
// the caster's own turn.
func TestConditionPlayerTurnGatesTheReduction(t *testing.T) {
	t.Parallel()
	discontinuitySrc := "Name:Discontinuity\nManaCost:2 U U\nTypes:Instant\n" +
		"S:Mode$ ReduceCost | Condition$ PlayerTurn | ValidCard$ Card.Self | Amount$ 1 | Color$ 2 U U | Type$ Spell | EffectZone$ All | Description$ During your turn, CARDNAME costs {2}{U}{U} less to cast.\n" +
		"Oracle:During your turn, CARDNAME costs {2}{U}{U} less to cast.\n"
	e, cfg, disc := newFixtureDeck(t, 79, discontinuitySrc)
	// Discontinuity's `Color$ 2 U U` has four reduction slots, not three:
	// the numeric token is two generic pips and the two U tokens are blue
	// pips. On its controller's turn it is therefore free, not a {1} spell.
	e.G.Active = 0
	if got := reduceOf(t, e, 0, disc); got != 4 {
		t.Fatalf("reduction on your turn = %d, want 4 ({2}{U}{U})", got)
	}
	if opt := castByName(t, e, 0, "Discontinuity"); opt == nil {
		t.Fatal("Discontinuity must be castable for no mana on its controller's turn")
	} else {
		submitChoices(t, e, opt.Index)
		if e.G.Obj(disc).Zone != state.ZStack {
			t.Fatalf("free Discontinuity on %s, want stack", e.G.Obj(disc).Zone)
		}
	}
	replayCheck(t, e, cfg)

	// A fresh game on the opponent's turn receives no reduction.
	e2, _, disc2 := newFixtureDeck(t, 79, discontinuitySrc)
	e2.G.Active = 1
	if got := reduceOf(t, e2, 0, disc2); got != 0 {
		t.Fatalf("reduction on the opponent's turn = %d, want 0", got)
	}
}

// TestExactPipAnnouncementScope keeps the legacy local resource menus only
// for a completely unmodified ordinary hybrid/Phyrexian cost. Every modifier
// composition requires whole-cost feasibility: generic modifiers change the
// shared remainder just as Color$/floor modifiers do.
// TestColorReductionAssignedToLaterHybridPip pins the CR 601.2f/601.2b rule
// the controller ruling names (instance 2): a Color$ reduction is legally
// assignable to a LATER pip, so the announcement menus must evaluate the
// whole cost over full assignments, never each pip against the modifiers in
// announcement order. {W/U}{W/U} under a {W} reduction with only {U} in the
// pool: announcing U first and W second reduces the W half and charges {U}.
// The per-pip composition this replaces (the reduction applied while the
// second pip was still unresolved) saw no W pip, spilled the reduction to
// generic and starved the strict {U} slot against the live pip — rejecting
// the legal U-first sequence from the menu. No card-specific code: the same
// shared primitive (costMods.feasibleAny) that gates the offer enumerates
// every announcement menu.
func TestColorReductionAssignedToLaterHybridPip(t *testing.T) {
	e, cfg, spell := newFixtureDeck(t, 801, twinVeilSrc, whiteReducerSrc)
	putCreature(t, e, 0, whiteReducerSrc)
	target := putToken(t, e, 1, targetSrc, state.ZBattlefield)
	addMana(t, e, 0, "U")

	// OFFER side: U-then-reduced-W is a legal full assignment, so the cast
	// must be offered even though the pool carries no white at all.
	opt := castByName(t, e, 0, "Twin Veil")
	if opt == nil {
		t.Fatal("{W/U}{W/U} under a {W} reduction with {U} must be castable: U then the reduced W is a legal assignment")
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("first hybrid payment decision = %+v", d)
	}
	firstW, firstU := -1, -1
	for _, choice := range d.Options {
		switch choice.Kind {
		case "pay_W":
			firstW = choice.Index
		case "pay_U":
			firstU = choice.Index
		}
	}
	if firstW < 0 || firstU < 0 {
		t.Fatalf("both faces of the first {W/U} pip must be offered (each has a completing assignment): %+v", d.Options)
	}
	// Choose the formerly-rejected U-first face.
	submitChoices(t, e, firstU)
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("second hybrid payment decision = %+v", d)
	}
	secondW := -1
	for _, choice := range d.Options {
		if choice.Kind == "pay_W" {
			secondW = choice.Index
		}
		if choice.Kind == "pay_U" {
			t.Fatalf("the second {U} face strands {U}{U} and must not be offered: %+v", d.Options)
		}
	}
	if secondW < 0 {
		t.Fatalf("the second pip's reduced W face must be offered: %+v", d.Options)
	}
	submitChoices(t, e, secondW)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("target decision after the U-then-W announcement = %+v", d)
	}
	targetIndex := -1
	for _, choice := range d.Options {
		if choice.Obj == target {
			targetIndex = choice.Index
		}
	}
	if targetIndex < 0 {
		t.Fatalf("target must remain available after the legal announcement: %+v", d.Options)
	}
	submitChoices(t, e, targetIndex)
	if e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("Twin Veil after the legal announcement is in %s, want stack", e.G.Obj(spell).Zone)
	}
	// The composed total was {U}{W} minus the {W} reduction = {U}: the single
	// blue unit pays it and nothing else moves.
	if e.G.Players[0].Pool.Total() != 0 || e.G.Players[0].Pool[state.MU] != 0 {
		t.Fatalf("pool after paying the reduced total = %v, want empty", e.G.Players[0].Pool)
	}
	replayCheck(t, e, cfg)
}

// TestUnmodifiedPhyrexianWholeCostMenu pins that unifying every announcement
// menu on the one shared feasibility primitive (no more shape-gated legacy
// local menu) keeps the unmodified ordinary-Phyrexian menu legal end to end:
// with only {B} and ample life, committing the black face strands the {1}
// generic (the old locally-affordable menu offered it and then aborted at
// payCast), so both pips must be announced through the life face and the
// cast completes as {B} + 4 life.
func TestUnmodifiedPhyrexianWholeCostMenu(t *testing.T) {
	dismemberSrc := "Name:Dismember\nManaCost:1 BP BP\nTypes:Instant\n" +
		"A:SP$ Pump | ValidTgts$ Creature | NumAtt$ -5 | NumDef$ -5\nOracle:x\n"
	e, cfg, spell := newFixtureDeck(t, 802, dismemberSrc, targetSrc)
	target := putToken(t, e, 1, targetSrc, state.ZBattlefield)
	addMana(t, e, 0, "B")

	opt := castByName(t, e, 0, "Dismember")
	if opt == nil {
		t.Fatal("unmodified Dismember with {B} and ample life must be castable")
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("first Phyrexian payment decision = %+v", d)
	}
	// Only the life face completes ({B} committed strands the generic {1});
	// the locally-affordable black face must not be on the menu.
	for _, choice := range d.Options {
		if choice.Kind == "pay_B" {
			t.Fatalf("committing black for the first pip strands the generic {1} and must not be offered: %+v", d.Options)
		}
		if choice.Kind != "pay_life" {
			t.Fatalf("unexpected option %+v", choice)
		}
	}
	if len(d.Options) != 1 {
		t.Fatalf("first menu = %+v, want only the life face", d.Options)
	}
	submitChoices(t, e, d.Options[0].Index)
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("second Phyrexian payment decision = %+v", d)
	}
	for _, choice := range d.Options {
		if choice.Kind == "pay_B" {
			t.Fatalf("the second black face also strands the generic {1} and must not be offered: %+v", d.Options)
		}
		if choice.Kind != "pay_life" {
			t.Fatalf("unexpected option %+v", choice)
		}
	}
	if len(d.Options) != 1 {
		t.Fatalf("second menu = %+v, want only the life face", d.Options)
	}
	submitChoices(t, e, d.Options[0].Index)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("target decision after legal payment announcement = %+v", d)
	}
	targetIndex := -1
	for _, choice := range d.Options {
		if choice.Obj == target {
			targetIndex = choice.Index
		}
	}
	if targetIndex < 0 {
		t.Fatalf("target must remain available: %+v", d.Options)
	}
	submitChoices(t, e, targetIndex)
	if e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("Dismember after completing payment is in %s, want stack", e.G.Obj(spell).Zone)
	}
	if e.G.Players[0].Pool.Total() != 0 || e.G.Players[0].Life != 16 {
		t.Fatalf("payment pool=%v life=%d, want empty pool and 16 life", e.G.Players[0].Pool, e.G.Players[0].Life)
	}
	replayCheck(t, e, cfg)
}

// TestThaliaPhyrexianAnnouncementFiltersUnpayableFace proves the actual
// manaAsk menus, not just their selection predicate. Thalia raises Dismember
// from {1}{B/P}{B/P} to {2}{B/P}{B/P}; with {B}{B}{B} and ample life, choosing
// black for the first pip remains legal only because the second must be paid
// with life. The second menu must therefore withhold its locally-affordable
// black face and the remaining legal sequence must complete payment.
func TestThaliaPhyrexianAnnouncementFiltersUnpayableFace(t *testing.T) {
	dismemberSrc := "Name:Dismember\nManaCost:1 BP BP\nTypes:Instant\n" +
		"A:SP$ Pump | ValidTgts$ Creature | NumAtt$ -5 | NumDef$ -5\nOracle:x\n"
	e, cfg, spell := newFixtureDeck(t, 791, dismemberSrc, thaliaRv2cSrc, targetSrc)
	putCreature(t, e, 0, thaliaRv2cSrc)
	target := putToken(t, e, 1, targetSrc, state.ZBattlefield)
	addMana(t, e, 0, "BBB")

	opt := castByName(t, e, 0, "Dismember")
	if opt == nil {
		t.Fatal("Dismember raised by Thalia must remain castable with BBB and two life")
	}
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("first Phyrexian payment decision = %+v", d)
	}
	firstBlack := -1
	for _, choice := range d.Options {
		if choice.Kind == "pay_B" {
			firstBlack = choice.Index
		}
	}
	if firstBlack < 0 {
		t.Fatalf("first payment must allow black before taking the legal life face: %+v", d.Options)
	}
	submitChoices(t, e, firstBlack)

	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("second Phyrexian payment decision = %+v", d)
	}
	life := -1
	for _, choice := range d.Options {
		if choice.Kind == "pay_B" {
			t.Fatalf("second black face strands Thalia-raised Dismember and must not be offered: %+v", d.Options)
		}
		if choice.Kind == "pay_life" {
			life = choice.Index
		}
	}
	if life < 0 {
		t.Fatalf("second payment must retain the completing life face: %+v", d.Options)
	}
	submitChoices(t, e, life)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("target decision after legal payment announcement = %+v", d)
	}
	targetIndex := -1
	for _, choice := range d.Options {
		if choice.Obj == target {
			targetIndex = choice.Index
		}
	}
	if targetIndex < 0 {
		t.Fatalf("target must remain available after legal payment announcement: %+v", d.Options)
	}
	submitChoices(t, e, targetIndex)
	if e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("Dismember after completing payment is in %s, want stack", e.G.Obj(spell).Zone)
	}
	if e.G.Players[0].Pool.Total() != 0 || e.G.Players[0].Life != 18 {
		t.Fatalf("Dismember payment pool=%v life=%d, want empty pool and 18 life", e.G.Players[0].Pool, e.G.Players[0].Life)
	}
	replayCheck(t, e, cfg)
}

// TestRaiseCostManaShapeAddsPips pins the RaiseCost Cost\$ raise (Andradite
// Leech's real line): the additional cost is whole mana — the raised spell
// owes the extra {B} pip, and a non-black spell is untouched.
func TestRaiseCostManaShapeAddsPips(t *testing.T) {
	t.Parallel()
	blightSrc := "Name:Blight\nManaCost:B\nTypes:Sorcery\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n"
	leechSrc := "Name:Andradite Leech\nManaCost:2 B\nTypes:Creature Leech\nPT:2/2\n" +
		"A:AB$ Pump | Cost$ B | Defined$ Self | NumAtt$ +1 | NumDef$ +1 | SpellDescription$ CARDNAME gets +1/+1 until end of turn.\n" +
		"S:Mode$ RaiseCost | ValidCard$ Card.Black | Activator$ You | Type$ Spell | Cost$ B | Description$ Black spells you cast cost {B} more to cast.\n" +
		"Oracle:x\n"
	e, cfg, _ := newFixtureDeck(t, 80, blightSrc, leechSrc)
	putCreature(t, e, 0, leechSrc)
	addMana(t, e, 0, "B")
	if castByName(t, e, 0, "Blight") != nil {
		t.Fatal("a {B} spell raised by {B} needs two black; one must not be enough")
	}
	addMana(t, e, 0, "B")
	opt := castByName(t, e, 0, "Blight")
	if opt == nil {
		t.Fatal("the raised {B}{B} should be castable with two black")
	}
	submitChoices(t, e, opt.Index)
	if d := e.Pending(); d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Blight's target decision: %+v", d)
	}
	submitChoices(t, e, 0)
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("pool after paying the raised cost = %d, want 0", e.G.Players[0].Pool.Total())
	}
	replayCheck(t, e, cfg)
	// A non-black spell is untouched by the ValidCard$ Card.Black raise.
	leech2Src := "Name:Andradite Leech\nManaCost:2 B\nTypes:Creature Leech\nPT:2/2\n" +
		"S:Mode$ RaiseCost | ValidCard$ Card.Black | Activator$ You | Type$ Spell | Cost$ B | Description$ Black spells you cast cost {B} more to cast.\n" +
		"Oracle:x\n"
	e2, _, _ := newFixtureDeck(t, 81, "Name:Filler\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n", leech2Src)
	putCreature(t, e2, 0, leech2Src)
	addMana(t, e2, 0, "R")
	if castByName(t, e2, 0, "Filler") == nil {
		t.Fatal("a red spell is not black: the {B} raise must not touch it")
	}
}
