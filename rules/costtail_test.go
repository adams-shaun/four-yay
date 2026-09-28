package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The cost-static census' remaining classes (TestCostStaticCensus), each
// driven through the real offer gate and cast/activation flow: Relative$
// raises, the Amount$/CheckSVar$ count heads, the ability's own mana-cost
// ReduceCost$, and the gate parameters the chain used to ignore.

// costtailPermCard puts c onto p's battlefield through a real MoveZone.
func costtailPermCard(t *testing.T, e *Engine, p state.PlayerID, c *cards.Card) state.ObjID {
	t.Helper()
	o := e.G.AddObject(c, p)
	e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	return o.ID
}

// costtailDrive answers every non-priority decision of the flow in progress
// with pick's choice (option 0 when pick is nil or returns nil) until the
// flow settles (priority, or nothing pending in a handEngine fixture).
func costtailDrive(t *testing.T, e *Engine, pick func(d *decision.Decision) []int) {
	t.Helper()
	for i := 0; i < 16; i++ {
		d := e.Pending()
		if d == nil || d.Kind == decision.KPriority {
			return
		}
		choice := []int{0}
		if pick != nil {
			if c := pick(d); c != nil {
				choice = c
			}
		}
		submitChoices(t, e, choice...)
	}
	t.Fatalf("flow did not return to priority; pending %+v", e.Pending())
}

// pickObj answers a target ask with the option naming obj.
func pickObj(obj state.ObjID) func(d *decision.Decision) []int {
	return func(d *decision.Decision) []int {
		for _, opt := range d.Options {
			if opt.Obj == obj {
				return []int{opt.Index}
			}
		}
		return nil
	}
}

const costtailBolt = "Name:Test Zap\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Creature | NumDmg$ 1\nOracle:x\n"

// --- class 1: Relative$ True on RaiseCost -----------------------------------

// Hum of the Radix: "Each artifact spell costs {1} more to cast for each
// artifact its controller controls." Relative$: the count is the CASTER's
// artifacts, not the Hum controller's.
func TestCosttailHumOfTheRadixRaisesByCastersArtifacts(t *testing.T) {
	t.Parallel()
	relic := card(t, "Name:Test Relic\nManaCost:1\nTypes:Artifact\nOracle:x\n")
	e := handEngine(t, relic)
	costtailPermCard(t, e, 1, corpusAlternativeCard(t, "Hum of the Radix"))
	for i := 0; i < 2; i++ {
		costtailPermCard(t, e, 0, card(t, "Name:Test Trinket\nTypes:Artifact\nOracle:x\n"))
	}
	spell := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MC] = 2
	if hasCastOption(e.legalActions(0), spell) {
		t.Fatalf("a {1} artifact is offered on {C}{C} under Hum with two artifacts (want {3})")
	}
	e.G.Players[0].Pool[state.MC] = 3
	if !hasCastOption(e.legalActions(0), spell) {
		t.Fatalf("a {1} artifact is not offered on {C}{C}{C} under Hum with two artifacts")
	}
	castMode(t, e, spell, "")
	costtailDrive(t, e, nil)
	if z := e.G.Obj(spell).Zone; z != state.ZStack {
		t.Fatalf("relic in %s after cast, want stack", z)
	}
	if left := e.G.Players[0].Pool.Total(); left != 0 {
		t.Fatalf("pool after cast = %d, want 0 (charged {3})", left)
	}
}

// Hinata, Dawn-Crowned: "Spells your opponents cast cost {1} more to cast
// for each target they have." A one-target {R} spell costs {1}{R}: the full
// price is charged at the CR 601.2c reprice, and with only {R} the cast
// unwinds cleanly (CR 733.1) instead of completing underpaid.
func TestCosttailHinataRaisesPerTarget(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T, pool int32) (*Engine, state.ObjID, state.ObjID) {
		e := handEngine(t, card(t, costtailBolt))
		costtailPermCard(t, e, 1, corpusAlternativeCard(t, "Hinata, Dawn-Crowned"))
		bear := costredPerm(t, e, 1, costredBear)
		e.G.Players[0].Pool[state.MR] = 1
		e.G.Players[0].Pool[state.MC] = pool
		return e, e.G.Zone(state.ZHand, 0)[0], bear
	}
	t.Run("paid", func(t *testing.T) {
		e, spell, bear := setup(t, 1)
		castMode(t, e, spell, "")
		costtailDrive(t, e, pickObj(bear))
		if z := e.G.Obj(spell).Zone; z != state.ZStack {
			t.Fatalf("zap in %s after cast, want stack", z)
		}
		if left := e.G.Players[0].Pool.Total(); left != 0 {
			t.Fatalf("pool after cast = %d, want 0 (charged {1}{R})", left)
		}
	})
	t.Run("unaffordable unwinds", func(t *testing.T) {
		e, spell, bear := setup(t, 0)
		castMode(t, e, spell, "")
		costtailDrive(t, e, pickObj(bear))
		if z := e.G.Obj(spell).Zone; z != state.ZHand {
			t.Fatalf("zap in %s, want back in hand (the {1}{R} cast cannot be paid)", z)
		}
		if left := e.G.Players[0].Pool.Total(); left != 1 {
			t.Fatalf("pool after the reversed cast = %d, want the {R} untouched", left)
		}
	})
}

// Officious Interrogation: "This spell costs {W}{U} more to cast for each
// target beyond the first" -- Relative$ AND Cost$ W U paired with Amount$:
// two targets add {W}{U}, never {2}. {W}{U}{C}{C} would cover a generic
// {2}; only {W}{W}{U}{U} covers the real raise.
func TestCosttailOfficiousInterrogationChargesWUPerExtraTarget(t *testing.T) {
	t.Parallel()
	bothPlayers := func(d *decision.Decision) []int {
		var both []int
		for _, opt := range d.Options {
			if opt.Kind == "player" {
				both = append(both, opt.Index)
			}
		}
		if len(both) == 2 {
			return both
		}
		return nil
	}
	cast := func(t *testing.T, w, u, c int32) (*Engine, state.ObjID) {
		e := handEngine(t, corpusAlternativeCard(t, "Officious Interrogation"))
		spell := e.G.Zone(state.ZHand, 0)[0]
		pool := &e.G.Players[0].Pool
		pool[state.MW], pool[state.MU], pool[state.MC] = w, u, c
		castMode(t, e, spell, "")
		costtailDrive(t, e, bothPlayers)
		return e, spell
	}
	t.Run("generic does not pay it", func(t *testing.T) {
		e, spell := cast(t, 1, 1, 2)
		if z := e.G.Obj(spell).Zone; z != state.ZHand {
			t.Fatalf("interrogation in %s after a two-target cast on {W}{U}{C}{C}, want reversed to hand", z)
		}
	})
	t.Run("W U pays it", func(t *testing.T) {
		e, spell := cast(t, 2, 2, 0)
		if z := e.G.Obj(spell).Zone; z != state.ZStack {
			t.Fatalf("interrogation in %s after a two-target cast on {W}{W}{U}{U}, want stack", z)
		}
		if got := len(e.G.Obj(spell).Targets); got != 2 {
			t.Fatalf("interrogation has %d targets, want 2", got)
		}
		if left := e.G.Players[0].Pool.Total(); left != 0 {
			t.Fatalf("pool after cast = %d, want 0", left)
		}
	})
}

// --- class 2: Amount$ count heads ---------------------------------------------

// Chandra's Incinerator: "costs {X} less, where X is the total amount of
// noncombat damage dealt to your opponents this turn."
func TestCosttailChandrasIncineratorReducesByNoncombatDamage(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Chandra's Incinerator"))
	spell := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MR] = 1
	e.G.Players[0].Pool[state.MC] = 2
	if hasCastOption(e.legalActions(0), spell) {
		t.Fatalf("Incinerator offered on 3 mana with no damage dealt")
	}
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 3})
	if !hasCastOption(e.legalActions(0), spell) {
		t.Fatalf("Incinerator not offered on {2}{R} after 3 noncombat damage to the opponent")
	}
	castMode(t, e, spell, "")
	costtailDrive(t, e, nil)
	if left := e.G.Players[0].Pool.Total(); left != 0 || e.G.Obj(spell).Zone != state.ZStack {
		t.Fatalf("Incinerator zone %s pool %d, want stack and an empty pool", e.G.Obj(spell).Zone, left)
	}
}

// Cemetery Prowler: "Spells you cast cost {1} less to cast for each card
// type they share with cards exiled with Cemetery Prowler" -- the AffectedX
// amount reads the spell being priced.
func TestCosttailCemeteryProwlerReducesBySharedTypes(t *testing.T) {
	t.Parallel()
	e := handEngine(t, card(t, costredBear))
	prowler := costtailPermCard(t, e, 0, corpusAlternativeCard(t, "Cemetery Prowler"))
	spell := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MG] = 1
	if hasCastOption(e.legalActions(0), spell) {
		t.Fatalf("a {1}{G} creature is offered on {G} with nothing exiled")
	}
	exiled := e.G.AddObject(card(t, "Name:Test Corpse\nManaCost:2\nTypes:Creature Zombie\nPT:2/2\nOracle:x\n"), 1)
	e.emit(events.Event{Kind: events.MoveZone, Obj: exiled.ID, From: state.ZLibrary, To: state.ZExile, IDs: []state.ObjID{prowler}})
	if !hasCastOption(e.legalActions(0), spell) {
		t.Fatalf("a {1}{G} creature is not offered on {G} with a creature exiled with the Prowler")
	}
	castMode(t, e, spell, "")
	costtailDrive(t, e, nil)
	if e.G.Obj(spell).Zone != state.ZStack || e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("bear zone %s pool %d, want stack and an empty pool", e.G.Obj(spell).Zone, e.G.Players[0].Pool.Total())
	}
}

// The remaining Amount$ heads, evaluated on a real engine: each reads its
// per-turn fact and answers a resolved verdict.
func TestCosttailCountHeadsResolve(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	src := costredPerm(t, e, 0, costredBear)
	ctx := &effects.Ctx{Source: src, Controller: 0}
	eval := func(body string) int32 {
		t.Helper()
		n, ok := effects.EvalCountOK(e, ctx, body)
		if !ok {
			t.Fatalf("%s: unresolved", body)
		}
		return n
	}
	// Heliod: opponents' draws this turn.
	for _, id := range e.G.Zone(state.ZLibrary, 1)[:2] {
		e.emit(events.Event{Kind: events.Draw, Player: 1, Obj: id, From: state.ZLibrary, To: state.ZHand, Secret: true})
	}
	if n := eval("PlayerCountOpponents$CardsDrawn"); n != 2 {
		t.Errorf("PlayerCountOpponents$CardsDrawn = %d, want 2", n)
	}
	// Samut: speed.
	e.emit(events.Event{Kind: events.SpeedChange, Player: 0, Amount: 3})
	if n := eval("Count$YourSpeed"); n != 3 {
		t.Errorf("Count$YourSpeed = %d, want 3", n)
	}
	// Fast Forward: distinct opponents attacked this turn (only a player
	// attack counts; a permanent attack does not).
	if n := eval("PlayerCountPropertyYou$OpponentsAttackedThisTurn"); n != 0 {
		t.Errorf("OpponentsAttackedThisTurn before any attack = %d, want 0", n)
	}
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{src}})
	if n := eval("PlayerCountPropertyYou$OpponentsAttackedThisTurn"); n != 1 {
		t.Errorf("OpponentsAttackedThisTurn = %d, want 1", n)
	}
	// Korvold: distinct card types among permanents sacrificed this turn.
	art := costredPerm(t, e, 0, "Name:Test Beast Relic\nTypes:Artifact Creature Beast\nPT:1/1\nOracle:x\n")
	land := costredPerm(t, e, 0, costredMountain)
	for _, id := range []state.ObjID{art, land} {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZGraveyard, Text: "sacrificed"})
	}
	if !e.G.Entered[len(e.G.Entered)-1].Sacrificed {
		t.Fatalf("fixture: the sacrifice move was not stamped Sacrificed")
	}
	if n := eval("PlayerCountPropertyYou$SacrificedPermanentTypesThisTurn"); n != 3 {
		t.Errorf("SacrificedPermanentTypesThisTurn = %d, want 3 (Artifact, Creature, Land)", n)
	}
	// Glamdring: an unattached Equipment reads "its power" as 0, resolved.
	if n := eval("Equipped$CardPower"); n != 0 {
		t.Errorf("Equipped$CardPower unattached = %d, want 0", n)
	}
	// Synchronized Eviction: the largest group sharing a creature type.
	if n := eval("Count$MostProminentCreatureType Creature.YouCtrl"); n != 1 {
		t.Errorf("MostProminentCreatureType = %d, want 1 (one Bear)", n)
	}
	costredPerm(t, e, 0, costredBear)
	if n := eval("Count$MostProminentCreatureType Creature.YouCtrl"); n != 2 {
		t.Errorf("MostProminentCreatureType = %d, want 2 (two Bears)", n)
	}
}

// Glamdring, Foe-hammer: "Instant and sorcery spells you cast cost {X} less
// to cast, where X is equipped creature's power."
func TestCosttailGlamdringReducesByEquippedPower(t *testing.T) {
	t.Parallel()
	e := handEngine(t, card(t, "Name:Test Bolt\nManaCost:2 R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n"))
	glam := costtailPermCard(t, e, 0, corpusAlternativeCard(t, "Glamdring, Foe-hammer"))
	bear := costredPerm(t, e, 0, costredBear)
	spell := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MR] = 1
	if hasCastOption(e.legalActions(0), spell) {
		t.Fatalf("a {2}{R} instant is offered on {R} with Glamdring unattached")
	}
	e.emit(events.Event{Kind: events.Attach, Obj: glam, IDs: []state.ObjID{bear}})
	if e.G.Obj(glam).AttachedTo != bear {
		t.Fatalf("fixture: Glamdring not attached")
	}
	if !hasCastOption(e.legalActions(0), spell) {
		t.Fatalf("a {2}{R} instant is not offered on {R} with Glamdring on a 2-power creature")
	}
}

// --- class 3: CheckSVar$ gates --------------------------------------------------

// Synchronized Eviction: "costs {2} less if you control at least two
// creatures that share a creature type."
func TestCosttailSynchronizedEvictionSharedType(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Synchronized Eviction"))
	target := costredPerm(t, e, 1, costredBear)
	costredPerm(t, e, 0, costredBear)
	spell := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MU] = 1
	e.G.Players[0].Pool[state.MC] = 2
	if hasCastOption(e.legalActions(0), spell) {
		t.Fatalf("Eviction offered on 3 mana with one Bear")
	}
	costredPerm(t, e, 0, costredBear)
	if !hasCastOption(e.legalActions(0), spell) {
		t.Fatalf("Eviction not offered on {2}{U} with two Bears")
	}
	castMode(t, e, spell, "")
	costtailDrive(t, e, pickObj(target))
	if e.G.Obj(spell).Zone != state.ZStack || e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("Eviction zone %s pool %d, want stack and an empty pool", e.G.Obj(spell).Zone, e.G.Players[0].Pool.Total())
	}
}

// Seize the Secrets: "costs {1} less to cast if you've committed a crime
// this turn." Targeting an opponent's creature is a crime.
func TestCosttailSeizeTheSecretsAfterACrime(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Seize the Secrets"), card(t, costtailBolt))
	opp := costredPerm(t, e, 1, costredBear)
	seize, zap := e.G.Zone(state.ZHand, 0)[0], e.G.Zone(state.ZHand, 0)[1]
	e.G.Players[0].Pool[state.MU] = 1
	e.G.Players[0].Pool[state.MC] = 1
	if hasCastOption(e.legalActions(0), seize) {
		t.Fatalf("Seize offered on 2 mana before any crime")
	}
	e.G.Players[0].Pool[state.MR] = 1
	castMode(t, e, zap, "")
	costtailDrive(t, e, pickObj(opp))
	if e.G.Obj(zap).Zone != state.ZStack {
		t.Fatalf("zap in %s, want stack", e.G.Obj(zap).Zone)
	}
	if !e.CommittedCrimeThisTurn(0) {
		t.Fatalf("targeting an opponent's creature did not record a crime")
	}
	e.resolveTop() // Seize is a sorcery: the stack must be empty
	if !hasCastOption(e.legalActions(0), seize) {
		t.Fatalf("Seize not offered on {1}{U} after a crime")
	}
}

// Tezzeret, Betrayer of Flesh: "The first activated ability of an artifact
// you activate each turn costs {2} less to activate."
func TestCosttailTezzeretFirstArtifactActivation(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	costtailPermCard(t, e, 0, corpusAlternativeCard(t, "Tezzeret, Betrayer of Flesh"))
	engine := costredPerm(t, e, 0, "Name:Test Engine\nTypes:Artifact\nA:AB$ GainLife | Cost$ 2 | LifeAmount$ 1 | SpellDescription$ gain 1\nOracle:x\n")
	if !hasAbilityOptionFor(e.legalActions(0), engine) {
		t.Fatalf("the first artifact activation is not offered for {0}")
	}
	opt := costtailAbilityOpt(t, e, engine)
	e.beginActivation(0, opt)
	costtailDrive(t, e, nil)
	e.resolveTop()
	if hasAbilityOptionFor(e.legalActions(0), engine) {
		t.Fatalf("the second artifact activation this turn is offered for {0}")
	}
	e.G.Players[0].Pool[state.MC] = 2
	if !hasAbilityOptionFor(e.legalActions(0), engine) {
		t.Fatalf("the second artifact activation is not offered at its full {2}")
	}
}

func costtailAbilityOpt(t *testing.T, e *Engine, id state.ObjID) decision.Option {
	t.Helper()
	for _, opt := range e.legalActions(0) {
		if opt.Kind == "ability" && opt.Obj == id {
			return opt
		}
	}
	t.Fatalf("no ability option for %d", id)
	return decision.Option{}
}

// --- class 5: an ability's own mana-cost ReduceCost$ ---------------------------

// Kami of Jealous Thirst: "This ability costs {4}{B} less to activate if
// you've drawn three or more cards this turn" (ReduceCost$ 4 B |
// ReduceAmount$ X).
func TestCosttailKamiOfJealousThirstOwnManaReduction(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	kami := costtailPermCard(t, e, 0, corpusAlternativeCard(t, "Kami of Jealous Thirst"))
	draw := func() {
		id := e.G.Zone(state.ZLibrary, 0)[0]
		e.emit(events.Event{Kind: events.Draw, Player: 0, Obj: id, From: state.ZLibrary, To: state.ZHand, Secret: true})
	}
	draw()
	draw()
	if hasAbilityOptionFor(e.legalActions(0), kami) {
		t.Fatalf("Kami's ability is offered for free after two draws")
	}
	draw()
	if !hasAbilityOptionFor(e.legalActions(0), kami) {
		t.Fatalf("Kami's ability is not offered for free after three draws")
	}
	life := e.G.Players[1].Life
	e.beginActivation(0, costtailAbilityOpt(t, e, kami))
	costtailDrive(t, e, nil)
	e.resolveTop()
	if e.G.Players[1].Life != life-2 {
		t.Fatalf("opponent life %d, want %d: the free activation did not resolve", e.G.Players[1].Life, life-2)
	}
}

// --- class 6: gate parameters the chain misread ---------------------------------

// AffectedZone$ gates the zone a SPELL is cast from: a Graveyard-scoped
// raise leaves a hand cast alone and charges a flashback cast.
func TestCosttailAffectedZoneGatesTheCastFromZone(t *testing.T) {
	t.Parallel()
	flash := card(t, "Name:Test Flicker\nManaCost:R\nTypes:Instant\nK:Flashback:R\nA:SP$ GainLife | LifeAmount$ 1\nOracle:x\n")
	tax := card(t, "Name:Test Gravetax\nTypes:Enchantment\nS:Mode$ RaiseCost | ValidCard$ Card | Type$ Spell | AffectedZone$ Graveyard | Amount$ 2 | Description$ Spells cast from graveyards cost {2} more.\nOracle:x\n")
	e := handEngine(t, flash)
	costtailPermCard(t, e, 1, tax)
	spell := e.G.Zone(state.ZHand, 0)[0]
	e.G.Players[0].Pool[state.MR] = 1
	if !hasCastOption(e.legalActions(0), spell) {
		t.Fatalf("an {R} spell is not offered from hand on {R}: a graveyard-scoped raise was applied to a hand cast")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: spell, From: state.ZHand, To: state.ZGraveyard})
	if hasCastOption(e.legalActions(0), spell) {
		t.Fatalf("the flashback cast is offered on {R} under a {2} graveyard raise")
	}
	e.G.Players[0].Pool[state.MC] = 2
	if !hasCastOption(e.legalActions(0), spell) {
		t.Fatalf("the flashback cast is not offered on {2}{R}")
	}
	castMode(t, e, spell, "flashback")
	costtailDrive(t, e, nil)
	if e.G.Obj(spell).Zone != state.ZStack || e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("flashback zone %s pool %d, want stack and {2}{R} charged", e.G.Obj(spell).Zone, e.G.Players[0].Pool.Total())
	}
}

// Closing Statement: "This spell costs {2} less to cast during your end
// step" (Phases$ End of Turn | PlayerTurn$ You).
func TestCosttailClosingStatementEndStepOnly(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Closing Statement"))
	costredPerm(t, e, 1, costredBear)
	spell := e.G.Zone(state.ZHand, 0)[0]
	pool := &e.G.Players[0].Pool
	pool[state.MW], pool[state.MB], pool[state.MC] = 1, 1, 1
	if hasCastOption(e.legalActions(0), spell) {
		t.Fatalf("Closing Statement offered on 3 mana in the main phase")
	}
	e.G.Step = state.StepEnd
	if !hasCastOption(e.legalActions(0), spell) {
		t.Fatalf("Closing Statement not offered on {1}{W}{B} in its controller's end step")
	}
	e.G.Active = 1
	if hasCastOption(e.legalActions(0), spell) {
		t.Fatalf("Closing Statement offered on 3 mana in the OPPONENT's end step")
	}
}

// UnlessValidTarget$ True inverts ValidTarget$: "if that spell doesn't
// target a creature you control, it costs {2} more."
func TestCosttailUnlessValidTargetInverts(t *testing.T) {
	t.Parallel()
	tax := card(t, "Name:Test Ward Tax\nTypes:Enchantment\nS:Mode$ RaiseCost | ValidCard$ Card | ValidTarget$ Creature.YouCtrl+inZoneBattlefield | UnlessValidTarget$ True | Activator$ You | Type$ Spell | Amount$ 2 | Description$ x\nOracle:x\n")
	setup := func(t *testing.T) (*Engine, state.ObjID, state.ObjID, state.ObjID) {
		e := handEngine(t, card(t, costtailBolt))
		costtailPermCard(t, e, 0, tax)
		mine := costredPerm(t, e, 0, costredBear)
		theirs := costredPerm(t, e, 1, costredBear)
		e.G.Players[0].Pool[state.MR] = 1
		return e, e.G.Zone(state.ZHand, 0)[0], mine, theirs
	}
	t.Run("own creature is free of the raise", func(t *testing.T) {
		e, spell, mine, _ := setup(t)
		castMode(t, e, spell, "")
		costtailDrive(t, e, pickObj(mine))
		if tg := e.G.Obj(spell).Targets; len(tg) != 1 || tg[0].Obj != mine {
			t.Fatalf("zap targets %+v, want my creature (was it dropped from the target menu as unaffordable?)", tg)
		}
		if e.G.Obj(spell).Zone != state.ZStack || e.G.Players[0].Pool.Total() != 0 {
			t.Fatalf("zap at my creature: zone %s pool %d, want stack and {R} charged", e.G.Obj(spell).Zone, e.G.Players[0].Pool.Total())
		}
	})
	t.Run("opponent's creature pays the raise", func(t *testing.T) {
		e, spell, _, theirs := setup(t)
		e.G.Players[0].Pool[state.MC] = 2
		castMode(t, e, spell, "")
		costtailDrive(t, e, pickObj(theirs))
		if e.G.Obj(spell).Zone != state.ZStack || e.G.Players[0].Pool.Total() != 0 {
			t.Fatalf("zap at their creature: zone %s pool %d, want stack and {2}{R} charged", e.G.Obj(spell).Zone, e.G.Players[0].Pool.Total())
		}
	})
}
