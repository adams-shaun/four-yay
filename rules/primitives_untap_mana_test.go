package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The five primitives this ticket implements, each proven on a real card
// script (the exact Forge lines, inlined per the licensing rule -- never a
// .cards .txt):
//
//	api:Untap              Basalt Monolith
//	api:ManaReflected      Exotic Orchard, Fellwar Stone, Chrome Mox
//	stat:ManaConvert       Chromatic Orrery, Quicksilver Elemental
//	stat:UntapOtherPlayer  Endbringer
//	kw:Cumulative upkeep   Mystic Remora
//
// The monolith/vault R:Event$ Untap replacement is a separate, unsupported
// primitive and is intentionally not retired by this ticket.

const basaltMonolithScript = "Name:Basalt Monolith\nManaCost:3\nTypes:Artifact\n" +
	"R:Event$ Untap | ValidCard$ Card.Self | ValidStepTurnToController$ You | Layer$ CantHappen | Description$ This artifact doesn't untap during your untap step.\n" +
	"A:AB$ Mana | Cost$ T | Produced$ C | Amount$ 3 | SpellDescription$ Add {C}{C}{C}.\n" +
	"A:AB$ Untap | Cost$ 3 | SpellDescription$ Untap this artifact.\nOracle:x\n"

const exoticOrchardScript = "Name:Exotic Orchard\nManaCost:no cost\nTypes:Land\n" +
	"A:AB$ ManaReflected | Cost$ T | ColorOrType$ Color | Valid$ Land.OppCtrl | ReflectProperty$ Produce | SpellDescription$ Add one mana of any color that a land an opponent controls could produce.\n" +
	"Oracle:x\n"

const chromaticOrreryScript = "Name:Chromatic Orrery\nManaCost:7\nTypes:Legendary Artifact\n" +
	"S:Mode$ ManaConvert | ValidPlayer$ You | ManaConversion$ AnyType->AnyColor | Description$ You may spend mana as though it were mana of any color.\n" +
	"A:AB$ Mana | Cost$ T | Produced$ C | Amount$ 5 | SpellDescription$ Add {C}{C}{C}{C}{C}.\n" +
	"A:AB$ Draw | Cost$ 5 T | NumCards$ X | SpellDescription$ Draw a card for each color among permanents you control.\n" +
	"SVar:X:Count$Valid Permanent.YouCtrl$Colors\nOracle:x\n"

const quicksilverElementalScript = "Name:Quicksilver Elemental\nManaCost:3 U U\nTypes:Creature Elemental\nPT:3/4\n" +
	"S:Mode$ ManaConvert | ValidPlayer$ You | ValidCard$ Card.Self | ValidSA$ Activated | ManaConversion$ Blue->AnyColor | Description$ You may spend blue mana as though it were mana of any color to pay the activation costs of CARDNAME's abilities.\n" +
	"A:AB$ Effect | Cost$ U | ValidTgts$ Creature | TgtZone$ Battlefield | TgtPrompt$ Select target creature card | StaticAbilities$ STSteal | RememberLKI$ Targeted | Duration$ UntilHostLeavesPlayOrEOT | SpellDescription$ CARDNAME gains all activated abilities of target creature until end of turn.\n" +
	"SVar:STSteal:Mode$ Continuous | Affected$ Card.EffectSource | GainsAbilitiesOfDefined$ RememberedLKI | Description$ EFFECTSOURCE gains all activated abilities of that card until end of turn.\nOracle:x\n"

const endbringerScript = "Name:Endbringer\nManaCost:5 C\nTypes:Creature Eldrazi\nPT:5/5\n" +
	"S:Mode$ UntapOtherPlayer | ValidCard$ Card.Self | Description$ Untap CARDNAME during each other player's untap step.\n" +
	"A:AB$ DealDamage | Cost$ T | ValidTgts$ Any | NumDmg$ 1 | SpellDescription$ CARDNAME deals 1 damage to any target.\nOracle:x\n"

const mysticRemoraScript = "Name:Mystic Remora\nManaCost:U\nTypes:Enchantment\n" +
	"K:Cumulative upkeep:1\n" +
	"T:Mode$ SpellCast | ValidCard$ Card.nonCreature | ValidActivatingPlayer$ Opponent | TriggerZones$ Battlefield | Execute$ TrigDraw | TriggerDescription$ Whenever an opponent casts a noncreature spell, you may draw a card unless that player pays {4}.\n" +
	"SVar:TrigDraw:DB$ Draw | Defined$ You | UnlessCost$ 4 | UnlessPayer$ TriggeredActivator | NumCards$ 1 | OptionalDecider$ You\nOracle:x\n"

const chromeMoxScript = "Name:Chrome Mox\nManaCost:0\nTypes:Artifact\n" +
	"T:Mode$ ChangesZone | ValidCard$ Card.Self | Origin$ Any | Destination$ Battlefield | OptionalDecider$ You | Execute$ TrigExile | TriggerDescription$ Imprint — When CARDNAME enters, you may exile a nonartifact, nonland card from your hand.\n" +
	"SVar:TrigExile:DB$ ChangeZone | Imprint$ True | Origin$ Hand | Destination$ Exile | ChangeType$ Card.nonArtifact+nonLand | ChangeNum$ 1\n" +
	"A:AB$ ManaReflected | Cost$ T | Valid$ Defined.Imprinted | ColorOrType$ Color | ReflectProperty$ Is | SpellDescription$ Add one mana of any of the exiled card's colors.\nOracle:x\n"

const manaFlareScript = "Name:Mana Flare\nManaCost:2 R\nTypes:Enchantment\n" +
	"T:Mode$ TapsForMana | ValidCard$ Land | Execute$ TrigMana | TriggerZones$ Battlefield | Static$ True | TriggerDescription$ Whenever a player taps a land for mana, that player adds one mana of any type that land produced.\n" +
	"SVar:TrigMana:DB$ ManaReflected | ColorOrType$ Type | ReflectProperty$ Produced | Defined$ TriggeredActivator\nOracle:x\n"

const incubationDruidScript = "Name:Incubation Druid\nManaCost:1 G\nTypes:Creature Elf Druid\nPT:0/2\n" +
	"A:AB$ PutCounter | Cost$ 3 G G | Adapt$ 3\n" +
	"A:AB$ ManaReflected | Cost$ T | ColorOrType$ Type | Valid$ Land.YouCtrl | Amount$ IncubationAmount | ReflectProperty$ Produce | SpellDescription$ Add one mana of any type that a land you control could produce. If CARDNAME has a +1/+1 counter on it, add three mana of that type instead.\n" +
	"SVar:Y:Count$Valid Card.Self+counters_GE1_P1P1\n" +
	"SVar:IncubationAmount:Count$Compare Y GE1.3.1\nOracle:x\n"

const tazriStalwartSurvivorScript = "Name:Tazri, Stalwart Survivor\nManaCost:W U B R G\nTypes:Legendary Creature Human Warrior\nPT:3/3\n" +
	"S:Mode$ Continuous | Affected$ Creature.YouCtrl | AddAbility$ Mana | Description$ Each creature you control has {T}: Add one mana of any of this creature's colors. Spend this mana only to activate an ability of a creature. Activate only if this creature has another activated ability.\n" +
	"SVar:Mana:AB$ ManaReflected | Cost$ T | Valid$ Defined.Self | ColorOrType$ Color | ReflectProperty$ Is | RestrictValid$ Activated.Creature+inZoneBattlefield | IsPresent$ Card.Self+hasAbility Activated.otherAbility | SpellDescription$ Add one mana of any of this creature's colors. Spend this mana only to activate an ability of a creature. Activate only if this creature has another activated ability.\n" +
	"A:AB$ Mill | Cost$ W U B R G T | NumCards$ 5 | RememberMilled$ True\nOracle:x\n"

const ancestralRecallScript = "Name:Ancestral Recall\nManaCost:U\nTypes:Instant\n" +
	"A:SP$ Draw | NumCards$ 3 | SpellDescription$ Draw three cards.\nOracle:x\n"

const giantGrowthScript = "Name:Giant Growth\nManaCost:G\nTypes:Instant\n" +
	"A:SP$ Pump | ValidTgts$ Creature | NumAtt$ 3 | NumDef$ 3 | SpellDescription$ Target creature gets +3/+3 until end of turn.\nOracle:x\n"

func mountainScript() string {
	return "Name:Mountain\nTypes:Basic Land Mountain\n" +
		"A:AB$ Mana | Cost$ T | Produced$ R | SpellDescription$ Add {R}.\nOracle:x\n"
}

func forestScript() string {
	return "Name:Forest\nTypes:Basic Land Forest\n" +
		"A:AB$ Mana | Cost$ T | Produced$ G | SpellDescription$ Add {G}.\nOracle:x\n"
}

// untapOptions finds the option kinds of a pending priority decision.
func optionKinds(d *decision.Decision) map[string]int {
	if d == nil {
		return nil
	}
	m := map[string]int{}
	for _, o := range d.Options {
		m[o.Kind]++
	}
	return m
}

// hasEvent reports whether the log carries an event of kind with the given
// object (obj 0 matches any).
func (e *Engine) hasEvent(kind events.Kind, obj state.ObjID) bool {
	for _, ev := range e.L.Events {
		if ev.Kind == kind && (obj == 0 || ev.Obj == obj) {
			return true
		}
	}
	return false
}

// TestBasaltMonolithUntap proves api:Untap on Basalt Monolith's real script.
// A stun counter replaces its paid untap, just as it replaces an untap-step
// untap (CR 122.1d); after it is consumed the ability untaps normally.
func TestBasaltMonolithUntap(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	basalt := onBoard(t, e, 0, basaltMonolithScript)
	e.emit(events.Event{Kind: events.Tap, Obj: basalt})
	if !e.G.Obj(basalt).Tapped {
		t.Fatal("fixture: Basalt should start tapped")
	}

	// api:Untap: the {3} ability is offered as an ordinary ability and
	// untaps on resolution.
	e.G.Players[0].Pool[state.MC] = 3
	e.priorityRound()
	if kinds(e.Pending().Options)["ability"] == 0 {
		t.Fatalf("Basalt's untap ability not offered: %+v", e.Pending().Options)
	}
	castFirst(t, e, "ability")
	passUntilStackEmpty(t, e, 8)
	if e.G.Obj(basalt).Tapped {
		t.Fatal("Basalt's {3} untap did not untap it")
	}

	// A stun counter replaces the first paid untap: it is removed and Basalt
	// remains tapped. The second activation has no stun left and untaps it.
	e.emit(events.Event{Kind: events.Tap, Obj: basalt})
	e.emit(events.Event{Kind: events.CounterChange, Obj: basalt, Counter: "STUN", Amount: 1})
	e.G.Players[0].Pool[state.MC] = 3
	e.priorityRound()
	castFirst(t, e, "ability")
	passUntilStackEmpty(t, e, 8)
	if !e.G.Obj(basalt).Tapped || e.G.Obj(basalt).Counter("STUN") != 0 {
		t.Fatalf("stun did not replace paid untap: tapped=%v stun=%d", e.G.Obj(basalt).Tapped, e.G.Obj(basalt).Counter("STUN"))
	}
	e.G.Players[0].Pool[state.MC] = 3
	e.priorityRound()
	castFirst(t, e, "ability")
	passUntilStackEmpty(t, e, 8)
	if e.G.Obj(basalt).Tapped {
		t.Fatal("Basalt did not untap after its stun counter was removed")
	}
}

func TestCloudOfFaeriesUntapUpToSelection(t *testing.T) {
	t.Parallel()
	const cloud = "Name:Cloud of Faeries\nManaCost:1 U\nTypes:Creature Faerie\nPT:1/1\n" +
		"T:Mode$ ChangesZone | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigUntap\n" +
		"SVar:TrigUntap:DB$ Untap | UntapUpTo$ True | UntapType$ Land | Amount$ 2\nOracle:x\n"
	e := handEngine(t)
	lands := []state.ObjID{onBoard(t, e, 0, mountainScript()), onBoard(t, e, 0, forestScript()), onBoard(t, e, 0, mountainScript())}
	for _, id := range lands {
		e.emit(events.Event{Kind: events.Tap, Obj: id})
	}
	o := e.G.AddObject(card(t, cloud), 0)
	o.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), o.ID))
	e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZHand, To: state.ZBattlefield})
	e.putTriggersOnStack()
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.ResumeKind != "untap" || d.Min != 0 || d.Max != 2 || len(d.Options) != 3 {
		t.Fatalf("Cloud of Faeries untap choice = %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{1}}); err != nil {
		t.Fatal(err)
	}
	for i, id := range lands {
		if got, want := e.G.Obj(id).Tapped, i != 1; got != want {
			t.Fatalf("land %d tapped=%v want %v", i, got, want)
		}
	}
}

// TestEndbringerUntapsDuringOtherPlayersUntapSteps proves
// stat:UntapOtherPlayer on Endbringer's real script: it untaps during every
// OTHER player's untap step (and a neighbouring permanent without the static
// does not), and during its own controller's untap step it untaps normally.
func TestEndbringerUntapsDuringOtherPlayersUntapSteps(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	endbringer := onBoard(t, e, 1, endbringerScript)
	plain := onBoard(t, e, 1, mountainScript())
	e.emit(events.Event{Kind: events.Tap, Obj: endbringer})
	e.emit(events.Event{Kind: events.Tap, Obj: plain})

	// Seat 0's untap step: Endbringer untaps, the plain land does not.
	e.G.Turn = 2
	e.beginTurn(0)
	if e.G.Obj(endbringer).Tapped {
		t.Fatal("Endbringer did not untap during an other player's untap step")
	}
	if !e.G.Obj(plain).Tapped {
		t.Fatal("a permanent without the static untapped during a foreign untap step")
	}
	if !e.hasEvent(events.Untap, endbringer) {
		t.Fatal("Endbringer's foreign untap-step untap emitted no Untap event")
	}

	// Seat 1's own untap step: everything untaps normally.
	e.emit(events.Event{Kind: events.Tap, Obj: endbringer})
	e.emit(events.Event{Kind: events.Tap, Obj: plain})
	e.G.Turn = 3
	e.beginTurn(1)
	if e.G.Obj(endbringer).Tapped || e.G.Obj(plain).Tapped {
		t.Fatal("the controller's own untap step did not untap its permanents")
	}
}

// TestExoticOrchardReflectedMana proves api:ManaReflected on Exotic Orchard's
// real script: one opponent land with one producible colour adds that colour
// with no ask; a second land widens the set and the activation asks
// (chooseManaColor); with nothing to reflect the activation is not offered.
// Fellwar Stone carries the identical script shape and Chrome Mox the
// Defined.Imprinted one, so one test covers the primitive's three real
// forms.
func TestExoticOrchardReflectedMana(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	orchard := onBoard(t, e, 0, exoticOrchardScript)
	e.priorityRound()
	if optionKinds(e.Pending())["activate"] != 0 {
		t.Fatal("Exotic Orchard offered an activation with nothing to reflect")
	}

	_ = onBoard(t, e, 1, mountainScript())
	e.priorityRound()
	if optionKinds(e.Pending())["activate"] != 1 {
		t.Fatalf("Exotic Orchard's activation not offered with an opponent land: %+v", e.Pending().Options)
	}
	castFirst(t, e, "activate")
	if e.G.Players[0].Pool[state.MR] != 1 {
		t.Fatalf("Exotic Orchard should have added one R, pool %+v", e.G.Players[0].Pool)
	}

	// A second opponent land of a different colour widens the candidate set;
	// the activation now asks, and the answer's colour is what is added.
	// (The first activation's {T} tapped the Orchard; the fixture untaps it
	// so the same source can be activated again.)
	e.emit(events.Event{Kind: events.Untap, Obj: orchard})
	_ = onBoard(t, e, 1, forestScript())
	e.priorityRound()
	castFirst(t, e, "activate")
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("expected a colour choice, got %+v", d)
	}
	if len(d.Options) != 2 || d.Options[0].Label != "Add R" || d.Options[1].Label != "Add G" {
		t.Fatalf("colour options wrong: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{1}}); err != nil {
		t.Fatalf("submit colour: %v", err)
	}
	if e.G.Players[0].Pool[state.MG] != 1 {
		t.Fatalf("Exotic Orchard should have added one G after the ask, pool %+v", e.G.Players[0].Pool)
	}
	_ = orchard
}

// TestFellwarStoneAndChromeMoxReflectedShapes pins the other two
// ManaReflected forms: Fellwar Stone is Exotic Orchard's twin (same
// candidates); Chrome Mox's Valid$ Defined.Imprinted resolves through the
// Defined machinery, finds no imprint record in this build, and is therefore
// not offered at all (an ability that can only resolve into a Note must not
// be offered).
func TestReflectingPoolTypeReflectsOnlyProducedColourless(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	pool := onBoard(t, e, 0, "Name:Reflecting Pool\nManaCost:no cost\nTypes:Land\n"+
		"A:AB$ ManaReflected | Cost$ T | ColorOrType$ Type | Valid$ Land.YouCtrl | ReflectProperty$ Produce | SpellDescription$ Add one mana of any type that a land you control could produce.\nOracle:x\n")
	sa := e.G.Obj(pool).Face().Abilities[0]
	ctx := &effects.Ctx{Source: pool, Controller: 0, SVars: e.G.Obj(pool).Face().SVars}
	if got := effects.ManaReflectedCandidates(e, ctx, sa); len(got) != 0 {
		t.Fatalf("Reflecting Pool alone reflected mana: %v", got)
	}
	_ = onBoard(t, e, 0, mountainScript())
	if got := effects.ManaReflectedCandidates(e, ctx, sa); len(got) != 1 || got[0] != "R" {
		t.Fatalf("Reflecting Pool plus Mountain = %v, want [R]", got)
	}
	_ = onBoard(t, e, 0, "Name:Wastes\nTypes:Basic Land\nA:AB$ Mana | Cost$ T | Produced$ C | SpellDescription$ Add {C}.\nOracle:x\n")
	if got := effects.ManaReflectedCandidates(e, ctx, sa); len(got) != 2 || got[0] != "R" || got[1] != "C" {
		t.Fatalf("Reflecting Pool plus Mountain and Wastes = %v, want [R C]", got)
	}
	// Type's colourless candidate is a real mana choice, not merely an offer:
	// choose it through the activation path and prove the answer reaches ManaAdd.
	e.priorityRound()
	d := e.Pending()
	poolOpt := -1
	for _, opt := range d.Options {
		if opt.Kind == "activate" && opt.Obj == pool {
			poolOpt = opt.Index
			break
		}
	}
	if poolOpt < 0 {
		t.Fatalf("Reflecting Pool activation missing: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{poolOpt}}); err != nil {
		t.Fatal(err)
	}
	d = e.Pending()
	if d == nil || len(d.Options) != 2 || d.Options[1].Label != "Add C" {
		t.Fatalf("Reflecting Pool Type choice = %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{1}}); err != nil {
		t.Fatal(err)
	}
	if got := e.G.Players[0].Pool[state.MC]; got != 1 {
		t.Fatalf("Reflecting Pool's chosen C = %d, want 1", got)
	}
}

func TestFellwarStoneAndChromeMoxReflectedShapes(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	onBoard(t, e, 0, "Name:Fellwar Stone\nManaCost:2\nTypes:Artifact\n"+
		"A:AB$ ManaReflected | Cost$ T | ColorOrType$ Color | Valid$ Land.OppCtrl | ReflectProperty$ Produce | SpellDescription$ Add one mana of any color that a land an opponent controls could produce.\nOracle:x\n")
	onBoard(t, e, 1, mountainScript())
	e.priorityRound()
	if optionKinds(e.Pending())["activate"] != 1 {
		t.Fatalf("Fellwar Stone's activation not offered: %+v", e.Pending().Options)
	}
	castFirst(t, e, "activate")
	if e.G.Players[0].Pool[state.MR] != 1 {
		t.Fatalf("Fellwar Stone should have added one R, pool %+v", e.G.Players[0].Pool)
	}

	e2 := handEngine(t)
	chromeObj := e2.G.AddObject(card(t, chromeMoxScript), 0)
	chromeObj.Zone = state.ZHand
	e2.G.SetZone(state.ZHand, 0, append(e2.G.Zone(state.ZHand, 0), chromeObj.ID))
	chrome := chromeObj.ID
	blue := e2.G.AddObject(card(t, ancestralRecallScript), 0)
	blue.Zone = state.ZHand
	green := e2.G.AddObject(card(t, giantGrowthScript), 0)
	green.Zone = state.ZHand
	e2.G.SetZone(state.ZHand, 0, append(e2.G.Zone(state.ZHand, 0), blue.ID, green.ID))
	// Drive Chrome Mox's REAL ETB trigger through the stack. Two eligible
	// cards force the resumed KChoose path; choose the second and prove a
	// clone replays the same answer byte-for-byte.
	e2.emit(events.Event{Kind: events.MoveZone, Obj: chrome, From: state.ZHand, To: state.ZBattlefield})
	e2.putTriggersOnStack()
	e2.resolveTop()
	d := e2.Pending()
	if d == nil || d.Kind != decision.KTriggerOptional {
		t.Fatalf("Chrome Mox optional trigger did not ask at resolution: %+v", d)
	}
	if err := e2.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatalf("accept imprint trigger: %v", err)
	}
	d = e2.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 || d.ResumeKind != "imprint" {
		t.Fatalf("expected two-card imprint choice, got %+v", d)
	}
	clone := e2.Clone()
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{1}}
	if err := e2.Submit(in); err != nil {
		t.Fatalf("choose second imprint: %v", err)
	}
	if err := clone.Submit(in); err != nil {
		t.Fatalf("replay second imprint on clone: %v", err)
	}
	if diff := diffGames(e2.G, clone.G); diff != "" {
		t.Fatalf("imprint replay diverged:\n%s", diff)
	}
	if got := e2.G.Obj(chrome).Imprinted; len(got) != 1 || got[0] != green.ID {
		t.Fatalf("Chrome Mox did not retain the chosen second imprint: %v", got)
	}
	e2.priorityRound()
	if optionKinds(e2.Pending())["activate"] != 1 {
		t.Fatalf("Chrome Mox did not offer mana from its imprinted blue card: %+v", e2.Pending())
	}
	castFirst(t, e2, "activate")
	if e2.G.Players[0].Pool[state.MG] != 1 {
		t.Fatalf("Chrome Mox did not produce the chosen imprinted card's G: %+v", e2.G.Players[0].Pool)
	}

	// CR 607.2a: the linked card stops being "the exiled card" after it
	// leaves exile. The persistent imprint ID must not follow it into another
	// zone and continue granting mana colours.
	e2.emit(events.Event{Kind: events.MoveZone, Obj: green.ID, From: state.ZExile, To: state.ZGraveyard})
	e2.emit(events.Event{Kind: events.Untap, Obj: chrome})
	e2.priorityRound()
	if optionKinds(e2.Pending())["activate"] != 0 {
		t.Fatalf("Chrome Mox still offered mana after its imprinted card left exile: %+v", e2.Pending().Options)
	}
}

// TestManaFlareReflectsTheProducedManaType pins the Produced half of
// api:ManaReflected on Mana Flare's real SVar. Unlike Produce/Is, Defined$
// names the player receiving mana; the candidate type comes from the
// triggering mana event retained in TriggerContext.
// TestIncubationDruidReflectedManaAmount proves Amount$ is evaluated on a
// ManaReflected ability, rather than every reflected activation adding one.
func TestIncubationDruidReflectedManaAmount(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	druid := onBoard(t, e, 0, incubationDruidScript)
	e.G.Obj(druid).SummonSick = false
	_ = onBoard(t, e, 0, mountainScript())
	e.emit(events.Event{Kind: events.CounterChange, Obj: druid, Counter: "P1P1", Amount: 1})
	e.priorityRound()
	castFirst(t, e, "activate")
	if got := e.G.Players[0].Pool[state.MR]; got != 3 {
		t.Fatalf("Incubation Druid with a +1/+1 counter added %d R, want 3", got)
	}
}

// TestTazriReflectedManaGateAndRestriction uses Tazri's real ManaReflected
// SVar. The ability requires another activated ability, and its mana can pay
// an activated ability of a creature but neither a spell nor an artifact
// activation. The restriction is event-backed, so the successful payment also
// proves a cloned/replayed game retains its provenance.
func TestTazriReflectedManaGateAndRestriction(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	tazri := onBoard(t, e, 0, tazriStalwartSurvivorScript)
	ma := cards.ResolveSVar(e.G.Obj(tazri).Face().SVars, "Mana")
	if ma == nil || !e.manaReflectedPresentHolds(0, tazri, ma) {
		t.Fatal("Tazri's real ManaReflected SVar should see its other activated ability")
	}
	plain := onBoard(t, e, 0, "Name:Vanilla Creature\nManaCost:U\nTypes:Creature\nPT:1/1\nOracle:x\n")
	if e.manaReflectedPresentHolds(0, plain, ma) {
		t.Fatal("Tazri's ManaReflected SVar was live without another activated ability")
	}

	// Tazri's Continuous AddAbility$ resolves the REAL SVar from Tazri while
	// activating it from the affected creature. The vanilla creature is not
	// offered; Mana Adept is, and its sole blue candidate needs no colour ask.
	creature := onBoard(t, e, 0, "Name:Mana Adept\nManaCost:U\nTypes:Creature\nPT:1/1\nA:AB$ Draw | Cost$ U | NumCards$ 1\nOracle:x\n")
	e.G.Obj(creature).SummonSick = false
	e.priorityRound()
	d := e.Pending()
	adeptOption := -1
	for _, option := range d.Options {
		if option.Kind == "activate" && option.Obj == creature {
			adeptOption = option.Index
		}
		if option.Kind == "activate" && option.Obj == plain {
			t.Fatal("Tazri granted mana to a creature with no other activated ability")
		}
	}
	if adeptOption < 0 {
		t.Fatalf("Tazri did not grant Mana Adept its reflected mana ability: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{adeptOption}}); err != nil {
		t.Fatalf("activate Tazri-granted mana ability: %v", err)
	}
	if got := e.G.Players[0].Pool[state.MU]; got != 1 || len(e.G.Players[0].RestrictedMana) != 1 {
		t.Fatalf("Tazri mana did not retain its restriction: pool=%+v restrictions=%+v", e.G.Players[0].Pool, e.G.Players[0].RestrictedMana)
	}
	spell := e.G.AddObject(card(t, ancestralRecallScript), 0)
	spell.Zone = state.ZHand
	if e.costPayable(0, spell.ID, false, ParseCost("U")) {
		t.Fatal("Tazri mana incorrectly paid a spell")
	}
	artifact := onBoard(t, e, 0, "Name:Mana Rock\nTypes:Artifact\nA:AB$ Draw | Cost$ U | NumCards$ 1\nOracle:x\n")
	if e.costPayable(0, artifact, true, ParseCost("U")) {
		t.Fatal("Tazri mana incorrectly paid a noncreature activation")
	}
	if !e.costPayable(0, creature, true, ParseCost("U")) {
		t.Fatal("Tazri mana did not pay a creature activation")
	}
	clone := e.Clone()
	if !e.payManaConvFor(0, creature, true, ParseCost("U"), nil) || !clone.payManaConvFor(0, creature, true, ParseCost("U"), nil) {
		t.Fatal("Tazri mana could not pay the allowed activation")
	}
	if diff := diffGames(e.G, clone.G); diff != "" {
		t.Fatalf("restricted-mana replay diverged:\n%s", diff)
	}
	if got := e.G.Players[0].Pool[state.MU]; got != 0 || len(e.G.Players[0].RestrictedMana) != 0 {
		t.Fatalf("Tazri restricted mana remained after payment: pool=%+v restrictions=%+v", e.G.Players[0].Pool, e.G.Players[0].RestrictedMana)
	}
}

func TestManaFlareReflectsTheProducedManaType(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	flare := onBoard(t, e, 0, manaFlareScript)
	sa := cards.ResolveSVar(e.G.Obj(flare).Face().SVars, "TrigMana")
	if sa == nil || sa.Params["ReflectProperty"] != "Produced" {
		t.Fatalf("Mana Flare's real reflected-mana SVar changed: %+v", sa)
	}
	land := onBoard(t, e, 1, forestScript())
	tc := e.triggerReferents(e.G.Obj(flare).Face().Triggers[0], flare,
		events.Event{Kind: events.ManaAdd, Player: 1, Obj: land, Counter: "G", Amount: 1}, nil)
	ctx := &effects.Ctx{Source: flare, Controller: 0, TriggerContext: tc}
	effects.Resolve(e, ctx, sa)
	if got := e.G.Players[1].Pool[state.MG]; got != 1 {
		t.Fatalf("Mana Flare added %d green to the triggering player, want 1", got)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("Mana Flare gave its controller the triggering player's mana: %+v", e.G.Players[0].Pool)
	}
}

// TestChromaticOrreryManaConvert proves stat:ManaConvert on Chromatic
// Orrery's real script: with the Orrery on the battlefield, one red of
// floating mana may pay a {U} spell's pip; without it the cast is not
// offered. Also pins the scoping on Quicksilver Elemental's real script:
// Blue->AnyColor scoped by ValidSA$ Activated + ValidCard$ Card.Self never
// widens a SPELL cast, and its blue-only conversion does not make red mana
// pay its own {U} ability.
func TestChromaticOrreryManaConvert(t *testing.T) {
	e := handEngine(t, card(t, ancestralRecallScript))
	e.G.Players[0].Pool[state.MR] = 1
	e.priorityRound()
	if optionKinds(e.Pending())["cast"] != 0 {
		t.Fatal("Ancestral Recall castable from a red-only pool with no converter")
	}
	_ = onBoard(t, e, 0, chromaticOrreryScript)
	e.priorityRound()
	if optionKinds(e.Pending())["cast"] != 1 {
		t.Fatal("Ancestral Recall not castable with Chromatic Orrery's conversion live")
	}
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 8)
	if total := e.G.Players[0].Pool.Total(); total != 0 {
		t.Fatalf("the converted payment did not spend the pool: %+v", e.G.Players[0].Pool)
	}
	if len(e.G.Zone(state.ZHand, 0)) != 3 {
		t.Fatalf("Ancestral Recall did not draw 3: hand %d", len(e.G.Zone(state.ZHand, 0)))
	}

	// Quicksilver Elemental: the same red pool must NOT cast a green spell
	// (its conversion is scoped to its own abilities), and red must not pay
	// its own {U} ability either (only BLUE mana converts).
	e2 := handEngine(t, card(t, giantGrowthScript))
	_ = onBoard(t, e2, 0, quicksilverElementalScript)
	onBoard(t, e2, 1, "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e2.G.Players[0].Pool[state.MU] = 1
	e2.priorityRound()
	if optionKinds(e2.Pending())["cast"] != 0 {
		t.Fatal("Quicksilver's Blue->AnyColor conversion leaked onto a spell cast")
	}
	if optionKinds(e2.Pending())["ability"] != 1 {
		t.Fatal("Quicksilver's own {U} ability not offered from a blue pool")
	}
	e2.G.Players[0].Pool = state.Mana{}
	e2.G.Players[0].Pool[state.MR] = 1
	e2.priorityRound()
	if optionKinds(e2.Pending())["ability"] != 0 {
		t.Fatal("red mana paid Quicksilver's {U} ability although only BLUE converts")
	}
	// Positive Blue->AnyColor coverage needs a DIFFERENT pip; Quicksilver's
	// printed ability costs {U}, which would be vacuous. Add a gained-style
	// green activation to the same source face: the source-scoped static must
	// let blue pay {G}, so both its printed {U} and gained {G} abilities are
	// offered. Reverting ManaConvert leaves only the printed one.
	gained := &cards.SA{Kind: "AB", API: "GainLife", Params: map[string]string{
		"Cost": "G", "Defined": "You", "LifeAmount": "1", "SpellDescription": "gained green ability"}}
	e2.G.Obj(e2.G.Zone(state.ZBattlefield, 0)[0]).Face().Abilities = append(e2.G.Obj(e2.G.Zone(state.ZBattlefield, 0)[0]).Face().Abilities, gained)
	e2.G.Players[0].Pool = state.Mana{}
	e2.G.Players[0].Pool[state.MU] = 1
	e2.priorityRound()
	if got := optionKinds(e2.Pending())["ability"]; got != 2 {
		t.Fatalf("blue mana offered %d Quicksilver abilities, want printed {U} plus gained {G}", got)
	}
}

// TestDrumbellowerUntapsItsCreaturesForEachOtherPlayer pins the class shape
// of stat:UntapOtherPlayer beyond Endbringer's Card.Self: Drumbellower's
// ValidCard$ Creature.YouCtrl untaps EVERY creature its controller controls
// during each other player's untap step, and nothing else.
func TestFabledPassageUntapsOnlyAtFourLands(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	passage := onBoard(t, e, 0, "Name:Fabled Passage\nManaCost:no cost\nTypes:Land\n"+
		"A:AB$ ChangeZone | Cost$ T Sac<1/CARDNAME> | Origin$ Library | Destination$ Battlefield | Tapped$ True | ChangeType$ Land.Basic | RememberChanged$ True | SubAbility$ DBUntap\n"+
		"SVar:DBUntap:DB$ Untap | Defined$ Remembered | ConditionPresent$ Land.YouCtrl | ConditionCompare$ GE4\nOracle:x\n")
	fetched := onBoard(t, e, 0, mountainScript())
	e.emit(events.Event{Kind: events.Tap, Obj: fetched})
	untap := e.G.Obj(passage).Face().Abilities[0].Sub
	ctx := &effects.Ctx{Source: passage, Controller: 0, Remembered: []state.Target{{Obj: fetched}}}
	effects.Resolve(e, ctx, untap)
	if !e.G.Obj(fetched).Tapped {
		t.Fatal("Fabled Passage untapped fetched land below four lands")
	}
	_ = onBoard(t, e, 0, forestScript())
	_ = onBoard(t, e, 0, forestScript())
	effects.Resolve(e, ctx, untap)
	if e.G.Obj(fetched).Tapped {
		t.Fatal("Fabled Passage did not untap fetched land at four lands")
	}
}

func TestQuestForRenewalNeedsFourQuestCounters(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	quest := onBoard(t, e, 1, "Name:Quest for Renewal\nManaCost:1 G\nTypes:Enchantment\n"+
		"S:Mode$ UntapOtherPlayer | ValidCard$ Creature.YouCtrl | IsPresent$ Card.Self+counters_GE4_QUEST | Description$ As long as there are four or more quest counters on CARDNAME, untap all creatures you control during each other player's untap step.\nOracle:x\n")
	creature := onBoard(t, e, 1, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.emit(events.Event{Kind: events.Tap, Obj: creature})
	e.G.Turn = 2
	e.beginTurn(0)
	if !e.G.Obj(creature).Tapped {
		t.Fatal("Quest for Renewal untapped below four quest counters")
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: quest, Counter: "QUEST", Amount: 4})
	e.G.Turn = 3
	e.beginTurn(0)
	if e.G.Obj(creature).Tapped {
		t.Fatal("Quest for Renewal did not untap at four quest counters")
	}
}

func TestDrumbellowerUntapsItsCreaturesForEachOtherPlayer(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	drum := onBoard(t, e, 1, "Name:Drumbellower\nManaCost:2 W\nTypes:Creature Spirit\nPT:2/1\nK:Flying\n"+
		"S:Mode$ UntapOtherPlayer | ValidCard$ Creature.YouCtrl | Description$ Untap all creatures you control during each other player's untap step.\nOracle:x\n")
	creature := onBoard(t, e, 1, "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	land := onBoard(t, e, 1, mountainScript())
	e.emit(events.Event{Kind: events.Tap, Obj: drum})
	e.emit(events.Event{Kind: events.Tap, Obj: creature})
	e.emit(events.Event{Kind: events.Tap, Obj: land})
	e.G.Turn = 2
	e.beginTurn(0)
	if e.G.Obj(drum).Tapped || e.G.Obj(creature).Tapped {
		t.Fatal("Drumbellower's creatures did not untap during a foreign untap step")
	}
	if !e.G.Obj(land).Tapped {
		t.Fatal("a non-creature untapped under a Creature.YouCtrl static")
	}
}

// TestMysticRemoraCumulativeUpkeep proves the keyword is a real upkeep
// trigger. Its age counter is absent while the ability waits on the stack;
// only resolution places it, then opens the mana-only payment window.
func TestMysticRemoraCumulativeUpkeep(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	remora := onBoard(t, e, 0, mysticRemoraScript)
	_ = onBoard(t, e, 0, mountainScript())
	e.G.Turn = 2
	e.beginTurn(0)
	if got := e.G.Obj(remora).Counter("AGE"); got != 0 {
		t.Fatalf("age counter appeared before the upkeep trigger resolved: %d", got)
	}
	e.priorityRound()
	if len(e.G.Stack) != 1 || e.G.Obj(e.G.Stack[0]).Ability.API != "CumulativeUpkeep" {
		t.Fatalf("cumulative upkeep was not placed as a triggered ability: %v", e.G.Stack)
	}
	if got := e.G.Obj(remora).Counter("AGE"); got != 0 {
		t.Fatalf("age counter appeared while the trigger was still on stack: %d", got)
	}
	e.resolveTop()
	d := e.Pending()
	if d == nil || len(d.Options) != 2 || d.Options[0].Kind != "activate" || d.Options[1].Kind != "done" {
		t.Fatalf("expected cumulative mana window at resolution, got %+v", d)
	}
	if got := e.G.Obj(remora).Counter("AGE"); got != 1 {
		t.Fatalf("resolution placed %d age counters, want 1", got)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatalf("activate mana: %v", err)
	}
	d = e.Pending()
	if d == nil || len(d.Options) != 2 || d.Options[0].Kind != "cumulative_pay" {
		t.Fatalf("expected pay-or-sacrifice after mana, got %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatalf("pay upkeep: %v", err)
	}
	if e.G.Obj(remora).Zone != state.ZBattlefield || len(e.G.Stack) != 0 {
		t.Fatalf("paid Remora/stack = %s/%v", e.G.Obj(remora).Zone, e.G.Stack)
	}
}

// TestCumulativeUpkeepKeepsItsTriggerControllerAfterControlChanges proves a
// response that steals Mystic Remora does not steal the already-triggered
// upkeep's pay-or-sacrifice decision (CR 113.8).
func TestCumulativeUpkeepKeepsItsTriggerControllerAfterControlChanges(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	remora := onBoard(t, e, 0, mysticRemoraScript)
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("cumulative trigger stack = %v, want one", e.G.Stack)
	}
	// The source's live controller changes, while the stack object's
	// controller remains the controller that put the trigger on the stack.
	e.emit(events.Event{Kind: events.ControlChange, Obj: remora, Player: 1})
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.Player != 0 {
		t.Fatalf("cumulative payment decision = %+v, want original controller seat 0", d)
	}
}

// TestCumulativeUpkeepOrdersWithOrdinaryUpkeepTriggers pins CR 603.3b: both
// trigger from the same StepChange and the controller orders them. Resolving
// the ordinary trigger first still leaves AGE at zero; resolving cumulative
// upkeep second places it.
func TestCumulativeUpkeepOrdersWithOrdinaryUpkeepTriggers(t *testing.T) {
	t.Parallel()
	const watcher = "Name:Upkeep Watcher\nManaCost:1\nTypes:Artifact\n" +
		"T:Mode$ Phase | Phase$ Upkeep | ValidPlayer$ You | Execute$ TrigLife\n" +
		"SVar:TrigLife:DB$ GainLife | Defined$ You | LifeAmount$ 1\nOracle:x\n"
	e := handEngine(t)
	remora := onBoard(t, e, 0, mysticRemoraScript)
	_ = onBoard(t, e, 0, watcher)
	e.beginTurn(0)
	if !e.putTriggersOnStack() || e.Pending() == nil || e.Pending().Kind != decision.KTriggerOrder {
		t.Fatalf("simultaneous upkeep triggers did not ask for order: %+v", e.Pending())
	}
	// Cumulative is discovered before Watcher and choice[0] is pushed first,
	// so it sits below Watcher and resolves second.
	d := e.Pending()
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1}}); err != nil {
		t.Fatal(err)
	}
	if len(e.G.Stack) != 2 {
		t.Fatalf("stack = %v, want two upkeep triggers", e.G.Stack)
	}
	e.pending = nil
	e.resolveTop()
	if got := e.G.Obj(remora).Counter("AGE"); got != 0 {
		t.Fatalf("ordinary upkeep trigger observed premature age counter %d", got)
	}
	e.resolveTop()
	if got := e.G.Obj(remora).Counter("AGE"); got != 1 {
		t.Fatalf("cumulative trigger resolution left AGE=%d, want 1", got)
	}
}

func TestCumulativeUpkeepRecognizesEveryCorpusActionCost(t *testing.T) {
	t.Parallel()
	labels := []string{
		"AddCounter<1/M1M1>",
		"AddCounter<1/P1P1/Creature.OppCtrl/creature an opponent controls>",
		"AddMana<1/R>",
		"Discard<1/Card>",
		"Draw<1/You>",
		"ExileFromTop<1/Card>",
		"FlipCoin<1>",
		"GainControl<1/Land.YouDontCtrl/land you don't control>",
		"GainLife<1/Player.Opponent>",
		"PutCardToLibFromSameGrave<2/-1/Card>",
		"Sac<1/Creature>",
		"Sac<1/Land>",
	}
	for _, label := range labels {
		if action, ok := parseCumulativeAction(label); !ok || action == nil {
			t.Errorf("parseCumulativeAction(%q) = %+v, %v", label, action, ok)
		}
	}
	if action, ok := parseCumulativeAction("MakeCoffee<1>"); ok || action != nil {
		t.Fatalf("unknown cumulative action was accepted: %+v", action)
	}
}

// TestPhyrexianSoulgorgerPaysCumulativeUpkeepWithAChosenCreature
// exercises the real action-cost keyword shape. Paying Sac<1/Creature> opens
// a scaled object choice; choosing another creature keeps Soulgorger rather
// than treating the non-mana token as an unpriceable generic cost.
func TestPhyrexianSoulgorgerPaysCumulativeUpkeepWithAChosenCreature(t *testing.T) {
	t.Parallel()
	const soulgorgerScript = "Name:Phyrexian Soulgorger\nManaCost:3\nTypes:Snow Artifact Creature Phyrexian Construct\nPT:8/8\n" +
		"K:Cumulative upkeep:Sac<1/Creature>:Sacrifice a creature.\nOracle:x\n"
	e := handEngine(t)
	soulgorger := onBoard(t, e, 0, soulgorgerScript)
	bear := onBoard(t, e, 0, "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.beginTurn(0)
	e.putTriggersOnStack()
	e.resolveTop()
	d := e.Pending()
	if d == nil || len(d.Options) != 2 || d.Options[0].Kind != "cumulative_pay" {
		t.Fatalf("Soulgorger action upkeep did not offer payment: %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatal(err)
	}
	d = e.Pending()
	if d == nil || len(d.Options) != 2 || d.Options[0].Kind != "cumulative_action_sac" {
		t.Fatalf("Soulgorger did not ask which creature to sacrifice: %+v", d)
	}
	pick := 0
	if d.Options[pick].Obj == soulgorger {
		pick = 1
	}
	if d.Options[pick].Obj != bear {
		t.Fatalf("Soulgorger sacrifice options do not contain the other creature: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
		t.Fatal(err)
	}
	if e.G.Obj(soulgorger).Zone != state.ZBattlefield || e.G.Obj(bear).Zone != state.ZGraveyard {
		t.Fatalf("paid action upkeep zones: Soulgorger=%s Bear=%s", e.G.Obj(soulgorger).Zone, e.G.Obj(bear).Zone)
	}

	// The second age counter repeats the action twice. Two fresh creatures
	// are both required and chosen in one exact-size decision.
	bear2 := onBoard(t, e, 0, "Name:Bear Two\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	bear3 := onBoard(t, e, 0, "Name:Bear Three\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.putTriggersOnStack()
	e.resolveTop()
	d = e.Pending()
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatal(err)
	}
	d = e.Pending()
	if d == nil || d.Min != 2 || d.Max != 2 {
		t.Fatalf("second Soulgorger upkeep did not scale to two sacrifices: %+v", d)
	}
	var picks []int
	for _, option := range d.Options {
		if option.Obj == bear2 || option.Obj == bear3 {
			picks = append(picks, option.Index)
		}
	}
	if len(picks) != 2 {
		t.Fatalf("scaled sacrifice options lost fresh creatures: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: picks}); err != nil {
		t.Fatal(err)
	}
	if e.G.Obj(bear2).Zone != state.ZGraveyard || e.G.Obj(bear3).Zone != state.ZGraveyard || e.G.Obj(soulgorger).Counter("AGE") != 2 {
		t.Fatalf("scaled upkeep result: age=%d Bear Two=%s Bear Three=%s", e.G.Obj(soulgorger).Counter("AGE"), e.G.Obj(bear2).Zone, e.G.Obj(bear3).Zone)
	}
}

// TestTriggerBodyCostDeclineOnlyOnMandatoryTrigger (trigcost1) replaces the
// old TestUnrelatedTriggeredEffectCostIsNotIntercepted, which pinned the
// pre-ticket free-executor semantics: every trigger body whose API was not in
// the Untap/ImmediateTrigger/Draw allowlist ran its effect WITHOUT charging
// its Cost$. Forge's `Cost$ <cost>` on a trigger body is the "you may pay
// <cost>. If you do, ..." idiom, so the widened gate routes Keldon Raider's
// mandatory ETB body (`AB$ Draw | Cost$ Discard<1/Card>`) through the same
// window: the Discard component is unpriceable, so the window poses a
// DECLINE-ONLY ask (never a free execution, never a zero-amount payment),
// and the decline leaves the body unexecuted (no draw, no discard).
func TestTriggerBodyCostDeclineOnlyOnMandatoryTrigger(t *testing.T) {
	t.Parallel()
	const keldonRaiderScript = "Name:Keldon Raider\nManaCost:2 R R\nTypes:Creature Human Warrior\nPT:4/3\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigDiscard | TriggerDescription$ When CARDNAME enters, you may discard a card. If you do, draw a card.\n" +
		"SVar:TrigDiscard:AB$ Draw | Cost$ Discard<1/Card>\nOracle:x\n"
	e := handEngine(t)
	raider := e.G.AddObject(card(t, keldonRaiderScript), 0)
	raider.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{raider.ID})
	e.emit(events.Event{Kind: events.MoveZone, Obj: raider.ID, From: state.ZHand, To: state.ZBattlefield})
	e.putTriggersOnStack()
	e.resolveTop()
	d := e.Pending()
	if d == nil || len(d.Options) != 1 || d.Options[0].Kind != "trigger_cost_decline" {
		t.Fatalf("Keldon Raider's Cost$ body did not open a decline-only window: %+v", d)
	}
	mark := len(e.L.Events)
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatal(err)
	}
	passUntilStackEmpty(t, e, 20)
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Draw {
			t.Fatalf("a declined Cost$ body still drew: %+v", ev)
		}
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != 0 {
		t.Fatalf("hand holds %d cards after the declined body, want the untouched 0", got)
	}
}

// TestManaVaultTriggerChargesItsRealCost pins the pool-coverability gate at a
// plain-mana triggered cost: with {4} actually in the pool the window offers
// the pay election and the charge untaps the vault and drains the pool. (The
// old offer-then-fail shape -- pay offered over an empty pool and declined at
// the charge -- was closed by the pool-coverability gate
// triggeredCostManaHalfPayable, ticket agent-20260922T232740Z-cf0357bb: an
// unpayable window is decline-only now, and the unpayable direction is
// pinned by TestMonstrosityOfTheLakeUnpayableIsDeclinedAtSettle and
// TestAleshaHybridTriggerWillNotPayUnpayable.)
func TestManaVaultTriggerChargesItsRealCost(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	vault := onBoard(t, e, 0, "Name:Mana Vault\nManaCost:1\nTypes:Artifact\n"+
		"T:Mode$ Phase | Phase$ Upkeep | ValidPlayer$ You | OptionalDecider$ You | Execute$ TrigUntap\n"+
		"SVar:TrigUntap:AB$ Untap | Cost$ 4 | Defined$ Self\nOracle:x\n")
	e.emit(events.Event{Kind: events.Tap, Obj: vault})
	// The pool covers the announced {4} exactly: the precondition the pay
	// offer (and the charge below) depends on.
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 4})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.putTriggersOnStack()
	e.resolveTop()
	d := e.Pending()
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatal(err)
	}
	d = e.Pending()
	if d == nil || d.Options[0].Kind != "trigger_cost_pay" {
		t.Fatalf("Mana Vault did not ask for its {4}: %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatal(err)
	}
	if e.G.Obj(vault).Tapped {
		t.Fatal("Mana Vault stayed tapped although its {4} was paid from the pool")
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after the paid {4} = %d, want 0", got)
	}
}

func TestManaVaultTriggerCanActivateManaAndPay(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	vault := onBoard(t, e, 0, "Name:Mana Vault\nManaCost:1\nTypes:Artifact\n"+
		"T:Mode$ Phase | Phase$ Upkeep | ValidPlayer$ You | OptionalDecider$ You | Execute$ TrigUntap\n"+
		"SVar:TrigUntap:AB$ Untap | Cost$ 4 | Defined$ Self\nOracle:x\n")
	for i := 0; i < 4; i++ {
		_ = onBoard(t, e, 0, mountainScript())
	}
	e.emit(events.Event{Kind: events.Tap, Obj: vault})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.putTriggersOnStack()
	e.resolveTop()
	d := e.Pending()
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		d = e.Pending()
		if d == nil || d.Options[0].Kind != "activate" {
			t.Fatalf("mana payment window %d = %+v", i, d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
			t.Fatal(err)
		}
	}
	d = e.Pending()
	if d == nil || d.Options[0].Kind != "trigger_cost_pay" {
		t.Fatalf("paid trigger choice = %+v", d)
	}
	clone := e.Clone()
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}
	if err := e.Submit(in); err != nil {
		t.Fatal(err)
	}
	if err := clone.Submit(in); err != nil {
		t.Fatal(err)
	}
	if diff := diffGames(e.G, clone.G); diff != "" {
		t.Fatalf("trigger-cost replay diverged:\n%s", diff)
	}
	if e.G.Obj(vault).Tapped || e.G.Players[0].Pool.Total() != 0 || len(e.G.Stack) != 0 {
		t.Fatalf("paid Mana Vault result: tapped=%v pool=%v stack=%v", e.G.Obj(vault).Tapped, e.G.Players[0].Pool, e.G.Stack)
	}
}
