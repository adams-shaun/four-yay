package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// abilityOption finds the "ability" option for (obj, index) in the pending
// priority decision, fatal if it is absent.
func abilityOption(t *testing.T, e *Engine, id state.ObjID, idx int) decision.Option {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatalf("no decision pending while searching for ability %d of %d", idx, id)
	}
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == id && o.Ability == idx {
			return o
		}
	}
	t.Fatalf("no ability option for %d [%d]: %+v", id, idx, d.Options)
	return decision.Option{}
}

// findAbilityOption is abilityOption's non-fatal form: the "ability" option
// for (obj, index), or ok=false when the pending decision does not offer it.
func findAbilityOption(e *Engine, id state.ObjID, idx int) (decision.Option, bool) {
	d := e.Pending()
	if d == nil {
		return decision.Option{}, false
	}
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == id && o.Ability == idx {
			return o, true
		}
	}
	return decision.Option{}, false
}

// hasActivateOption reports whether the pending priority decision offers the
// existing "activate" (tap-for-mana) option for obj.
func hasActivateOption(e *Engine, obj state.ObjID) bool {
	d := e.Pending()
	if d == nil {
		return false
	}
	for _, o := range d.Options {
		if o.Kind == "activate" && o.Obj == obj {
			return true
		}
	}
	return false
}

// indexOfPlayerOption returns the option index (not position) that targets
// the given player, or -1 if the pending decision offers no such option --
// the KTarget shape askTarget produces for a player-targeting subject.
func indexOfPlayerOption(d *decision.Decision, p state.PlayerID) int {
	for _, o := range d.Options {
		if o.Player == p {
			return o.Index
		}
	}
	return -1
}

// TestManaCostAbilityGoesOnTheStackAndResolves: an activated ability whose
// cost is mana-only is offered as an "ability" option, pays into the pool,
// pushes a real ability stack object, and its effect (a Draw) resolves.
func TestManaCostAbilityGoesOnTheStackAndResolves(t *testing.T) {
	t.Parallel()
	src := "Name:Sailor\nManaCost:U\nTypes:Creature Spirit\nPT:1/1\nA:AB$ Draw | Cost$ 3 U | NumCards$ 1 | Defined$ You | SpellDescription$ Draw a card.\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 34, src)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	addMana(t, e, 0, "UUUU")
	e.Advance()
	opt := abilityOption(t, e, id, 0) // helper: the "ability" option for (obj, index), fatal if absent
	if opt.Label != "Sailor: Draw a card." {
		t.Fatalf("label %q", opt.Label)
	}
	hand := len(e.G.Zone(state.ZHand, 0))
	submitChoices(t, e, opt.Index)
	if len(e.G.Stack) != 1 || e.G.Obj(e.G.Stack[0]).Ability == nil || e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("stack %v pool %d", e.G.Stack, e.G.Players[0].Pool.Total())
	}
	passUntilStackEmpty(t, e, 20)
	if len(e.G.Zone(state.ZHand, 0)) != hand+1 {
		t.Fatal("the ability did not draw")
	}
	replayCheck(t, e, cfg)
}

// TestRemoveCounterCostAndTargetedAbility: a SubCounter cost is required
// before the ability is offered, removes the counter at activation, and the
// ability targets and deals damage.
func TestScaldingTarnPayLifeActivation(t *testing.T) {
	t.Parallel()
	const tarn = "Name:Scalding Tarn\nManaCost:no cost\nTypes:Land\n" +
		"A:AB$ ChangeZone | Cost$ T PayLife<1> Sac<1/CARDNAME> | Origin$ Library | Destination$ Battlefield | ChangeType$ Island,Mountain | SpellDescription$ Search your library for a Island or Mountain card, put it onto the battlefield, then shuffle.\n" +
		"Oracle:{T}, Pay 1 life, Sacrifice Scalding Tarn: Search your library for an Island or Mountain card, put it onto the battlefield, then shuffle.\n"

	for _, tc := range []struct {
		name string
		mana string
	}{
		{name: "empty_pool", mana: ""},
		{name: "pool_is_not_payment", mana: "G"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, cfg, id := newFixtureDeck(t, 41, tarn)
			moveSeeded(t, e, 0, tarn, state.ZBattlefield)
			if tc.mana != "" {
				addMana(t, e, 0, tc.mana)
			} else {
				e.Advance()
			}
			beforeLife := e.G.Players[0].Life
			beforePool := e.G.Players[0].Pool
			opt := abilityOption(t, e, id, 0)
			submitChoices(t, e, opt.Index)
			if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
				t.Fatalf("Scalding Tarn wrongly posed a singleton sacrifice choice: %+v", d)
			}
			if got := e.G.Players[0].Life; got != beforeLife-1 {
				t.Fatalf("life after activation = %d, want %d", got, beforeLife-1)
			}
			if got := e.G.Players[0].Pool; got != beforePool {
				t.Fatalf("pool after activation = %v, want %v", got, beforePool)
			}
			if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
				t.Fatalf("Scalding Tarn zone = %s, want Graveyard", got)
			}
			// Let both seats pass so the newly pushed fetch ability resolves and
			// poses its library-search decision.
			submitChoices(t, e, 0)
			submitChoices(t, e, 0)
			d := e.Pending()
			if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
				t.Fatalf("search decision = %+v", d)
			}
			submitChoices(t, e, d.Options[0].Index)
			passUntilStackEmpty(t, e, 20)
			replayCheck(t, e, cfg)
		})
	}
}

func TestRemoveCounterCostAndTargetedAbility(t *testing.T) {
	t.Parallel()
	src := "Name:Ballista\nManaCost:X X\nTypes:Artifact Creature Construct\nPT:0/0\n" +
		"A:AB$ PutCounter | Cost$ 4 | CounterType$ P1P1 | CounterNum$ 1 | SpellDescription$ Put a +1/+1 counter on CARDNAME.\n" +
		"A:AB$ DealDamage | Cost$ SubCounter<1/P1P1> | ValidTgts$ Any | NumDmg$ 1 | SpellDescription$ It deals 1 damage to any target.\nOracle:x\n"
	e, cfg, _ := newFixtureDeck(t, 36, src)
	id := putCreature(t, e, 0, src) // drives to Main1 and leaves e.pending nil, so the Advance below re-asks a fresh priority decision
	e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "P1P1", Amount: 2})
	e.Advance()
	if _, ok := findAbilityOption(e, id, 0); ok {
		t.Fatal("{4} ability offered with an empty pool")
	}
	opt := abilityOption(t, e, id, 1)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no target decision: %+v", d)
	}
	life := e.G.Players[1].Life
	submitChoices(t, e, indexOfPlayerOption(d, 1))
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(id).Counter("P1P1") != 1 || e.G.Players[1].Life != life-1 {
		t.Fatalf("counters %d life %d", e.G.Obj(id).Counter("P1P1"), e.G.Players[1].Life)
	}
	replayCheck(t, e, cfg)
}

// TestGraveyardActivationWithSacrificeCost: an ActivationZone$ Graveyard
// ability whose cost sacrifices a land is activatable from the graveyard,
// asks the sacrifice as a cost, and returns the card to hand.
func TestGraveyardActivationWithSacrificeCost(t *testing.T) {
	t.Parallel()
	src := "Name:Breaker\nManaCost:6 G\nTypes:Creature Eldrazi\nPT:5/7\nK:Devoid\n" +
		"A:AB$ ChangeZone | Cost$ 2 C Sac<1/Land> | Origin$ Graveyard | Destination$ Hand | ActivationZone$ Graveyard | SpellDescription$ Return CARDNAME from your graveyard to your hand.\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 39, src)
	// The sacrificial land is a real deck Mountain moved with a logged
	// MoveZone, not a putLands fixture: replayCheck reconstructs a deck card
	// from the config, but putLands bypasses events (direct AddObject), so
	// such an object has no replay trace and would shift every ID after it.
	land := moveSeeded(t, e, 0, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n", state.ZBattlefield)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZGraveyard})
	addMana(t, e, 0, "CGG")
	e.Advance()
	opt := abilityOption(t, e, id, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "sacrifice" || d.Options[0].Obj != land {
		t.Fatalf("sacrifice choice %+v", d)
	}
	submitChoices(t, e, 0)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(land).Zone != state.ZGraveyard || e.G.Obj(id).Zone != state.ZHand || e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("land %s breaker %s pool %d", e.G.Obj(land).Zone, e.G.Obj(id).Zone, e.G.Players[0].Pool.Total())
	}
	replayCheck(t, e, cfg)
}

// TestSorcerySpeedAndSummoningSicknessGates: a SorcerySpeed$ True ability is
// only offered free on a sorcery-speed window (empty stack), and a creature's
// {T} cost needs no summoning sickness unless the creature has Haste (CR
// 302.6) -- here it does not, so a summoning-sick Tapper offers nothing.
func TestSorcerySpeedAndSummoningSicknessGates(t *testing.T) {
	t.Parallel()
	src := "Name:Gear\nManaCost:1\nTypes:Artifact\n" +
		"A:AB$ GainLife | Cost$ 1 | Defined$ You | LifeAmount$ 1 | SorcerySpeed$ True | SpellDescription$ x\nOracle:x\n"
	boltSrc := "Name:Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n"
	e, _, id := newFixtureDeck(t, 34, src, boltSrc)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	addMana(t, e, 0, "G")
	e.Advance()
	abilityOption(t, e, id, 0) // seat 0's own main phase, empty stack: offered
	bolt := addToHand(t, e, 0, boltSrc)
	addMana(t, e, 0, "R")
	// Cast Bolt but do NOT drain the stack (castObj would): the check below
	// needs a spell still on the stack, where a sorcery-speed ability must
	// not be offered even to the active player who owns both.
	idx := -1
	for _, o := range e.Pending().Options {
		if o.Kind == "cast" && o.Obj == bolt {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Bolt: %+v", e.Pending().Options)
	}
	submitChoices(t, e, idx)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget && len(d.Options) > 0 {
		submitChoices(t, e, d.Options[0].Index)
	}
	if len(e.G.Stack) == 0 {
		t.Fatal("Bolt did not reach the stack")
	}
	if _, ok := findAbilityOption(e, id, 0); ok {
		t.Fatal("sorcery-speed ability offered with a spell on the stack")
	}
	tapper := "Name:Tapper\nManaCost:G\nTypes:Creature Elf\nPT:1/1\nA:AB$ GainLife | Cost$ T | Defined$ You | LifeAmount$ 1 | SpellDescription$ x\nOracle:x\n"
	e2, _, elf := newFixtureDeck(t, 35, tapper)
	e2.emit(events.Event{Kind: events.MoveZone, Obj: elf, From: state.ZHand, To: state.ZBattlefield})
	e2.Advance()
	if _, ok := findAbilityOption(e2, elf, 0); ok {
		t.Fatal("a summoning-sick creature's {T} ability was offered")
	}
}

// TestRealCorpusSacCostWithAlternationAndDescriptionPays drives a REAL
// corpus activated-ability line end to end -- the return-to-hand template
// shared by the graveyard-recursion creatures -- exercising exactly the
// token shapes Task 20's ParseCost fix (Ruling FL-54) targets: the trailing
// "/artifact or creature" description inside the Sac<...> group (which a
// naive whitespace split would tear apart before MatchesSpec could see the
// spec) and the ";" OR alternation folded into MatchesSpec's "," (so a
// Bear, being a Creature, satisfies Sac<1/Artifact;Creature>). A Sac cost
// that cannot be paid never yields an "ability" option at all; once offered,
// the sacrifice is asked as a cost and the card still returns to hand.
func TestRealCorpusSacCostWithAlternationAndDescriptionPays(t *testing.T) {
	t.Parallel()
	src := "Name:Trawler\nManaCost:1 U\nTypes:Creature Whale\nPT:3/3\n" +
		"A:AB$ ChangeZone | Cost$ 2 Sac<1/Artifact;Creature/artifact or creature> | Origin$ Graveyard | Destination$ Hand | ActivationZone$ Graveyard | SpellDescription$ Return CARDNAME from your graveyard to your hand.\nOracle:x\n"
	bearSrc := "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e, cfg, id := newFixtureDeck(t, 38, src, bearSrc)
	bear := putCreature(t, e, 0, bearSrc)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZGraveyard})
	addMana(t, e, 0, "GG")
	e.Advance()
	opt := abilityOption(t, e, id, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "sacrifice" || d.Options[0].Obj != bear {
		t.Fatalf("sacrifice choice %+v", d)
	}
	submitChoices(t, e, 0)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(bear).Zone != state.ZGraveyard || e.G.Obj(id).Zone != state.ZHand || e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("bear %s trawler %s pool %d", e.G.Obj(bear).Zone, e.G.Obj(id).Zone, e.G.Players[0].Pool.Total())
	}
	replayCheck(t, e, cfg)
}

// TestCantBeActivatedValidSASparesManaAbilities: a CantBeActivated static
// with ValidCard$ Card.NamedCard and ValidSA$ Activated.!ManaAbility blocks a
// named card's activated (non-mana) abilities but expressly spares its mana
// abilities; once the source is renamed away the ability is activatable again.
func TestCantBeActivatedValidSASparesManaAbilities(t *testing.T) {
	t.Parallel()
	needle := "Name:Needle\nManaCost:1\nTypes:Artifact\nS:Mode$ CantBeActivated | ValidCard$ Card.NamedCard | ValidSA$ Activated.!ManaAbility | Description$ x\nOracle:x\n"
	ballista := "Name:Ballista\nManaCost:X X\nTypes:Artifact Creature Construct\nPT:0/0\nA:AB$ DealDamage | Cost$ SubCounter<1/P1P1> | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n"
	e, _, n := newFixtureDeck(t, 36, needle, ballista)
	e.emit(events.Event{Kind: events.MoveZone, Obj: n, From: state.ZHand, To: state.ZBattlefield})
	e.emit(events.Event{Kind: events.Choose, Obj: n, Counter: "name", Text: "Ballista"})
	b := putCreature(t, e, 0, ballista)
	e.emit(events.Event{Kind: events.CounterChange, Obj: b, Counter: "P1P1", Amount: 1})
	land := putLands(t, e, 0, 1)[0]
	e.emit(events.Event{Kind: events.Choose, Obj: n, Counter: "name", Text: "Mountain"})
	e.Advance()
	if _, ok := findAbilityOption(e, b, 0); !ok {
		t.Fatal("Ballista (not the named card any more) should be activatable")
	}
	if !hasActivateOption(e, land) {
		t.Fatal("the named land's mana ability must still be offered: ValidSA spares mana abilities")
	}
}

// TestDoubleSacCostRequiresDistinctCandidates closes fix round 1's reviewer
// Important 1: a cost with TWO Sac parts of the same spec cannot be paid by
// the same permanent twice, so it must not be offered with only one legal
// candidate. castable checks each part independently against the same board,
// so a naive gate would let a single creature satisfy an offered
// `Sac<1/Creature> Sac<1/Creature>`, then sacAsk would pay only the first
// part (the creature is gone) and skip the second, activating with half the
// cost unpaid. The fixed castable reserves each part's candidates (distinct),
// so one creature makes the whole cost unpayable and the option is never
// offered; two creatures make it offered, and BOTH are asked and sacrificed
// before the ability resolves.
func TestDoubleSacCostRequiresDistinctCandidates(t *testing.T) {
	t.Parallel()
	src := "Name:Feeder\nManaCost:2\nTypes:Artifact\n" +
		"A:AB$ GainLife | Cost$ Sac<1/Creature> Sac<1/Creature> | Defined$ You | LifeAmount$ 2 | SpellDescription$ Sacrifice two creatures: you gain 2 life.\nOracle:x\n"
	creatureSrc := "Name:Thrull\nManaCost:1\nTypes:Creature Thrull\nPT:1/1\nOracle:x\n"

	// Exactly one legal candidate: the two-Sac-part cost is unpayable (one
	// permanent cannot pay both parts), so the ability must not be offered.
	e, _, id := newFixtureDeck(t, 38, src, creatureSrc)
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	putCreature(t, e, 0, creatureSrc)
	e.Advance()
	if _, ok := findAbilityOption(e, id, 0); ok {
		t.Fatal("a Sac<1/Creature> Sac<1/Creature> cost offered with exactly one creature: one permanent cannot pay both parts")
	}

	// Two legal candidates: offered, and both sacrifices are asked and paid.
	e2, cfg2, id2 := newFixtureDeck(t, 39, src, creatureSrc, creatureSrc)
	e2.emit(events.Event{Kind: events.MoveZone, Obj: id2, From: state.ZHand, To: state.ZBattlefield})
	cA := putCreature(t, e2, 0, creatureSrc)
	cB := putCreature(t, e2, 0, creatureSrc)
	e2.Advance()
	opt := abilityOption(t, e2, id2, 0)
	submitChoices(t, e2, opt.Index)
	// The two Sac parts are paid as TWO sequential decisions (sacAsk walks
	// pc.cost.Sac one part at a time), each Min=Max=1. The first part's
	// decision has both creatures; the second part's decision must have only
	// ONE candidate -- the creature already chosen for the first part is
	// excluded (pc.sacs), so the two parts cannot both pick the same one.
	d := e2.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "sacrifice" || len(d.Options) != 2 {
		t.Fatalf("first sacrifice choice %+v", d)
	}
	submitChoices(t, e2, 0) // part 1: sacrifice the first creature
	d = e2.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "sacrifice" || len(d.Options) != 1 {
		t.Fatalf("second sacrifice choice must offer only the un-sacrificed creature, got %+v", d)
	}
	submitChoices(t, e2, 0) // part 2: sacrifice the remaining creature
	passUntilStackEmpty(t, e2, 20)
	if e2.G.Obj(cA).Zone != state.ZGraveyard || e2.G.Obj(cB).Zone != state.ZGraveyard {
		t.Fatalf("a %s b %s: both creatures must be sacrificed", e2.G.Obj(cA).Zone, e2.G.Obj(cB).Zone)
	}
	if e2.G.Obj(id2).Zone != state.ZBattlefield {
		t.Fatalf("the activated permanent itself left the battlefield: %s", e2.G.Obj(id2).Zone)
	}
	replayCheck(t, e2, cfg2)
}

// TestActivateSkipsRestrictedManaAbility closes fix round 1's reviewer
// Important 2: the tap-for-mana "activate" option must resolve exactly the
// mana abilities whose restriction the offer gate checked. Mint has two
// mana abilities -- the first produces {B}, the second {U} -- and a
// CantBeActivated scoped to ManaAbility<Produce:B> singles out only the
// {B} one (test-only ValidSA grammar; the corpus only ever uses empty /
// "Activated" / "Activated.!ManaAbility", so adding this subset is what
// makes the disagreement reachable). The gate offers the option because the
// {U} ability is unrestricted, and activation must produce only {U}: gate
// and activation agreeing is the fix.
func TestActivateSkipsRestrictedManaAbility(t *testing.T) {
	t.Parallel()
	mint := "Name:Mint\nManaCost:C\nTypes:Artifact\n" +
		"A:AB$ Mana | Cost$ T | Produced$ B | SpellDescription$ Add {B}.\n" +
		"A:AB$ Mana | Cost$ T | Produced$ U | SpellDescription$ Add {U}.\nOracle:x\n"
	needle := "Name:NeedleB\nManaCost:1\nTypes:Artifact\n" +
		"S:Mode$ CantBeActivated | ValidCard$ Card.NamedCard | ValidSA$ Activated.ManaAbility<Produce:B> | Description$ Mint's {B} mana ability can't be activated.\nOracle:x\n"
	e, _, m := newFixtureDeck(t, 41, mint, needle)
	e.emit(events.Event{Kind: events.MoveZone, Obj: m, From: state.ZHand, To: state.ZBattlefield})
	n := moveSeeded(t, e, 0, needle, state.ZBattlefield)
	e.emit(events.Event{Kind: events.Choose, Obj: n, Counter: "name", Text: "Mint"})
	e.Advance()
	if !hasActivateOption(e, m) {
		t.Fatal("Mint's option must be offered: the {U} mana ability is unrestricted, only the {B} one is restricted")
	}
	d := e.Pending()
	actIdx := -1
	for _, o := range d.Options {
		if o.Kind == "activate" && o.Obj == m {
			actIdx = o.Index
		}
	}
	if actIdx < 0 {
		t.Fatalf("no activate option for Mint: %+v", d.Options)
	}
	submitChoices(t, e, actIdx)
	pool := e.G.Players[0].Pool
	if pool.Total() != 1 || pool[state.MU] != 1 || pool[state.MB] != 0 {
		t.Fatalf("activation must resolve only the unrestricted {U} ability (gate and activation agree), pool %+v", pool)
	}
	// A bare ManaAbility restriction splashes both: the whole tap-for-mana
	// option must disappear (gate rejects -- no unrestricted member left).
	needleAll := "Name:NeedleAll\nManaCost:1\nTypes:Artifact\n" +
		"S:Mode$ CantBeActivated | ValidCard$ Card.NamedCard | ValidSA$ Activated.ManaAbility | Description$ x\nOracle:x\n"
	e2, _, m2 := newFixtureDeck(t, 41, mint, needleAll)
	e2.emit(events.Event{Kind: events.MoveZone, Obj: m2, From: state.ZHand, To: state.ZBattlefield})
	n2 := moveSeeded(t, e2, 0, needleAll, state.ZBattlefield)
	e2.emit(events.Event{Kind: events.Choose, Obj: n2, Counter: "name", Text: "Mint"})
	e2.Advance()
	if hasActivateOption(e2, m2) {
		t.Fatal("Mint must offer no tap option when both mana abilities are restricted")
	}
}
