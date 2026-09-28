package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// The DamageSource$ spec families the rv2b-damagesource ticket closes:
// specs newDamageRider's resolver could not model kept the RESOLVING source
// instead of redirecting (CR 609.7). Real corpus cards where the corpus
// reaches the shape (Halana's Spawner> chain, Crush Underfoot's ChosenCard,
// Enchanter's Bane's Imprinted count), inline fixtures where no corpus card
// isolates the mechanism (the multi-source Valid arm, the RelativeTarget$
// pairing, the raw imprint read), per the licensing rule.

// drainDamageSourceAsks answers the decisions a DamageSource$ test's flow
// poses until the stack is empty: the immediate-trigger cost-pay ask (option
// 0 = pay), a target ask naming wantObj, a KChoose offering wantObj's card,
// and priority (passed). Anything else is a test failure.
func drainDamageSourceAsks(t *testing.T, e *Engine, wantObj state.ObjID, limit int) {
	t.Helper()
	for i := 0; i < limit; i++ {
		d := e.Pending()
		if d == nil {
			return
		}
		if d.Kind == decision.KPriority && len(e.G.Stack) == 0 {
			return
		}
		switch d.Kind {
		case decision.KPriority:
			passFirst(t, e)
		case decision.KTarget:
			targetObject(t, e, wantObj)
		case decision.KChoose:
			if len(d.Options) > 0 && d.Options[0].Kind == "trigger_cost_pay" {
				submitChoices(t, e, 0)
				continue
			}
			if d.ResumeKind == "sacrifice" {
				// The optional sacrifice ask (Enchanter's Bane's "unless that
				// player sacrifices it"): decline with the empty answer Min 0
				// makes legal, which is what gates the damage leg.
				submitChoices(t, e)
				continue
			}
			for _, o := range d.Options {
				if o.Obj == wantObj {
					submitChoices(t, e, o.Index)
					break
				}
			}
			// A mid-resolution ChooseCard ask (Crush Underfoot's Giant pick):
			// its options are the eligible CARDS, none of them wantObj -- the
			// first offered entry is the deterministic answer (the fixture
			// controls exactly one eligible card in every flow this drain
			// serves).
			submitChoices(t, e, d.Options[0].Index)
		case decision.KTriggerOptional:
			submitChoices(t, e, 0)
		default:
			t.Fatalf("unexpected decision %q while draining: %+v", d.Kind, d.Options)
		}
	}
	t.Fatal("drainDamageSourceAsks never emptied the stack")
}

// countPlayerDamage counts Damage events dealt to player p for exactly
// amount, the observable the multi-source tests pin: one emission per source
// means N events, not one summed event.
func countPlayerDamageAmount(t *testing.T, e *Engine, p state.PlayerID, amount int32) int {
	t.Helper()
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Damage && ev.Player == p && ev.Amount == amount {
			n++
		}
	}
	return n
}

// TestValidDamageSourceDealsFromEachSource pins the multi-source DamageSource$
// arm on an inline probe: a DamageSource$ spec that resolves to SEVERAL
// objects makes EACH of them a separate damager (Missy's "each artifact
// creature you control deals 1 damage to that opponent" is the corpus shape).
// The Valid Creature+YouCtrl walk names the conduit itself plus both bears, so
// the opponent takes three separate 1-point hits. Pre-fix the rider took the
// first resolved source only: one hit, the opponent two life richer.
func TestValidDamageSourceDealsFromEachSource(t *testing.T) {
	t.Parallel()
	conduit := "Name:Sweep Conduit\nManaCost:3\nTypes:Creature Elemental\nPT:1/1\n" +
		"A:AB$ DealDamage | Cost$ T | DamageSource$ Valid Creature.YouCtrl | Defined$ Opponent | " +
		"NumDmg$ 1 | SpellDescription$ Each creature you control deals 1 damage to each opponent.\nOracle:x\n"
	bearA := "Name:Probe Bear A\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	bearB := "Name:Probe Bear B\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e, cfg, _ := newFixtureDeck(t, 91, conduit, bearA, bearB)
	conduitID := putCreature(t, e, 0, conduit)
	aID := putCreature(t, e, 0, bearA)
	bID := putCreature(t, e, 0, bearB)
	// Preconditions: the three damagers stand on the battlefield unsick, and
	// the opponent is alive at 20 -- the compared values differ from the
	// expected result.
	for _, id := range []state.ObjID{conduitID, aID, bID} {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("damager %d not on the battlefield: %+v", id, o)
		}
	}
	if e.G.Players[1].Lost || e.G.Players[1].Life != 20 {
		t.Fatalf("opponent precondition: life %d lost %v", e.G.Players[1].Life, e.G.Players[1].Lost)
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	e.priorityRound()
	opt := abilityOption(t, e, conduitID, 0)
	submitChoices(t, e, opt.Index)
	passUntilStackEmpty(t, e, 20)
	if got := countPlayerDamageAmount(t, e, 1, 1); got != 3 {
		t.Fatalf("logged %d Damage(player 1, 1) events, want 3: every creature the Valid walk names is its own damager", got)
	}
	if got := e.G.Players[1].Life; got != 17 {
		t.Fatalf("seat 1 life = %d, want 17", got)
	}
	replayCheck(t, e, cfg)
}

// TestRelativeTargetDamagePairsEachSource pins RelativeTarget$ True (aura
// barbs' shape): the per-source recipients re-resolve through Defined$
// anchored on EACH damage source, so each enchantment damages its own
// controller -- and Defined$ CardController names that anchoring card's
// controller. Pre-fix CardController did not resolve and the Valid walk's
// first match was the only damager, so the opponent took nothing.
func TestRelativeTargetDamagePairsEachSource(t *testing.T) {
	t.Parallel()
	spell := "Name:Pairing Bolt\nManaCost:2 R\nTypes:Instant\n" +
		"A:SP$ DealDamage | NumDmg$ 2 | DamageSource$ Valid Enchantment | Defined$ CardController | " +
		"RelativeTarget$ True | SpellDescription$ Each enchantment deals 2 damage to its controller.\nOracle:x\n"
	aura := "Name:Probe Sigil\nManaCost:1 W\nTypes:Enchantment\nOracle:x\n"
	e, cfg, spellID := newFixtureDeckWithOpponentCard(t, 93, spell, aura, aura)
	myAura := moveSeeded(t, e, 0, aura, state.ZBattlefield)
	oppAura := moveSeeded(t, e, 1, aura, state.ZBattlefield)
	// Preconditions: one enchantment per battlefield, the spell in hand, both
	// seats alive at 20.
	if o := e.G.Obj(myAura); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("my aura not on the battlefield: %+v", o)
	}
	if o := e.G.Obj(oppAura); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("opponent aura not on the battlefield: %+v", o)
	}
	if e.G.Obj(spellID).Zone != state.ZHand {
		t.Fatalf("spell not in hand: %s", e.G.Obj(spellID).Zone)
	}
	if e.G.Players[0].Life != 20 || e.G.Players[1].Life != 20 {
		t.Fatalf("life precondition: %d/%d, want 20/20", e.G.Players[0].Life, e.G.Players[1].Life)
	}
	addMana(t, e, 0, "CCR")
	castFirst(t, e, "cast")
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[0].Life; got != 18 {
		t.Fatalf("seat 0 life = %d, want 18 (own enchantment dealt to its controller)", got)
	}
	if got := e.G.Players[1].Life; got != 18 {
		t.Fatalf("seat 1 life = %d, want 18: the opponent's enchantment is its own damager, paired with its own controller by RelativeTarget$", got)
	}
	replayCheck(t, e, cfg)
}

// TestImprintedDamageSourceReadsTheRawAssociation pins the ungated imprint
// read the DamageSource$ Imprinted arm takes: Forge's getImprintedCards has
// no zone gate, and the corpus line (Enchanter's Bane) imprints a BATTLEFIELD
// permanent, so the CR 607.2a exile-gated Defined$ Imprinted read must not
// drop the source. The probe imprints a wither creature and then deals 2
// damage with DamageSource$ Imprinted at that same creature: the WITHER
// source deals the damage, so it lands as two -1/-1 counters and no marked
// damage. Pre-fix the resolving source (the probe, no wither) dealt the
// damage: two marked, no counters.
func TestImprintedDamageSourceReadsTheRawAssociation(t *testing.T) {
	t.Parallel()
	probe := "Name:Imprint Probe\nManaCost:2 R\nTypes:Creature Goblin Shaman\nPT:1/3\n" +
		"A:AB$ Effect | Cost$ T | ValidTgts$ Creature | RememberObjects$ Targeted | ImprintCards$ Targeted | " +
		"SpellDescription$ Imprint target creature.\n" +
		"A:AB$ DealDamage | Cost$ T | ValidTgts$ Creature | DamageSource$ Imprinted | NumDmg$ X | " +
		"SpellDescription$ The imprinted creature deals damage equal to its power to target creature.\n" +
		"SVar:X:Imprinted$CardPower\nOracle:x\n"
	victim := "Name:Withering Ox\nManaCost:1 G\nTypes:Creature Ox\nPT:2/4\nK:Wither\nOracle:x\n"
	e, cfg, _ := newFixtureDeck(t, 97, probe, victim)
	probeID := putCreature(t, e, 0, probe)
	oxID := putCreature(t, e, 0, victim)
	// Preconditions: the ox is on the battlefield and is the ONLY creature
	// with wither -- the compared keyword states actually differ.
	if o := e.G.Obj(oxID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("ox not on the battlefield: %+v", o)
	}
	if hasKeyword(e, oxID, "Wither") != true {
		t.Fatal("precondition: the ox must carry wither")
	}
	if hasKeyword(e, probeID, "Wither") == true {
		t.Fatal("precondition: the probe must not carry wither")
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	e.priorityRound()

	// Leg 1: imprint the ox. The association is the probe's persistent
	// Imprinted list (a battlefield card, which the CR 607.2a exile gate
	// drops) -- assert it before leg 2 leans on it.
	opt := abilityOption(t, e, probeID, 0)
	submitChoices(t, e, opt.Index)
	targetObject(t, e, oxID)
	passUntilStackEmpty(t, e, 20)
	imprinted := e.G.Obj(probeID).Imprinted
	if len(imprinted) != 1 || imprinted[0] != oxID {
		t.Fatalf("probe imprinted %v, want [%d]: the raw read has nothing to serve leg 2", imprinted, oxID)
	}

	// Leg 2: the imprinted creature (the ox, wither) is the damage source, so
	// its 2 damage lands as -1/-1 counters on itself, never as marked damage.
	e.emit(events.Event{Kind: events.Untap, Obj: probeID})
	e.priorityRound()
	opt = abilityOption(t, e, probeID, 1)
	submitChoices(t, e, opt.Index)
	targetObject(t, e, oxID)
	passUntilStackEmpty(t, e, 20)
	ox := e.G.Obj(oxID)
	if n := ox.Counter("M1M1"); n != 2 {
		t.Fatalf("ox carries %d -1/-1 counters, want 2: the WITHER imprinted source (power 2, the Imprinted$CardPower count) must have dealt the damage", n)
	}
	if ox.Damage != 0 {
		t.Fatalf("ox carries %d marked damage, want 0: wither damage from the imprinted source converts to counters", ox.Damage)
	}
	replayCheck(t, e, cfg)
}

// hasKeyword is the engine-facing keyword read the tests use for preconditions.
func hasKeyword(e *Engine, id state.ObjID, kw string) bool {
	return e.HasKeyword(id, kw)
}

// TestSpawnerDamageSourceDealsFromTheEnteringCreature pins the Spawner>
// re-anchor on the real card: Halana's immediate-trigger chain resolves its
// DealDamage with DamageSource$ Spawner>TriggeredCardLKICopy and
// X:Spawner>TriggeredCard$CardPower, and BOTH must read the triggering
// capture (the creature that entered), not Halana and not zero. The entering
// Kinscaer Sentry has lifelink, so the source provenance pays its controller:
// the victim takes 2 marked and seat 0 gains 2. Pre-fix the Spawner> spec was
// unresolvable (loud fail-closed now) and X evaluated to zero, so nothing
// happened at all.
func TestSpawnerDamageSourceDealsFromTheEnteringCreature(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := dsBoard(t, reg, "Halana, Kessig Ranger", "Kinscaer Sentry", "Bear Cub")
	sentry := ids["Kinscaer Sentry"]
	cub := ids["Bear Cub"]
	// Preconditions: the entering creature carries lifelink (the provenance
	// observable) and stands ready to enter; the victim is on the battlefield.
	if !hasKeyword(e, sentry, "Lifelink") {
		t.Fatal("precondition: Kinscaer Sentry must carry lifelink for the provenance read")
	}
	if o := e.G.Obj(cub); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Bear Cub not on the battlefield: %+v", o)
	}
	moveToHand(t, e, sentry)
	addMana(t, e, 0, "CC")
	before := e.G.Players[0].Life
	e.emit(events.Event{Kind: events.MoveZone, Obj: sentry, From: state.ZHand, To: state.ZBattlefield})
	e.pending = nil
	e.Advance()
	drainDamageSourceAsks(t, e, cub, 20)
	if z := e.G.Obj(cub).Zone; z != state.ZGraveyard {
		t.Fatalf("Bear Cub alive in %s: the entered creature (power 2) must have dealt its lethal 2 to the 2/2 cub", z)
	}
	sawLethal := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == cub && strings.Contains(ev.Text, "lethal damage") {
			sawLethal = true
		}
	}
	if !sawLethal {
		t.Fatal("no lethal-damage departure for the cub")
	}
	if got := e.G.Players[0].Life; got != before+2 {
		t.Fatalf("seat 0 life = %d, want %d: the LIFELINK entered creature dealt the damage", got, before+2)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unresolvable DamageSource$") {
			t.Fatalf("Spawner> spec failed closed loudly instead of resolving: %q", ev.Text)
		}
	}
	replayCheck(t, e, cfg)
}

// TestChosenCardDamageSourceDealsFromTheChosenCard pins DamageSource$
// ChosenCard and its count ref on the real card: Crush Underfoot chooses a
// Giant, and the CHOSEN Giant deals damage equal to its power
// (X:ChosenCard$CardPower) to the target. The chosen Giant is Kroxa and
// Kunoros (6/6 with lifelink) and the target is that same Giant: the damage
// comes from the LIFELINK Giant, so its controller gains 6 and the Giant dies
// to its own lethality. Pre-fix the ChosenCard count ref did not resolve
// (zero damage): the Giant survived and nobody gained life.
func TestChosenCardDamageSourceDealsFromTheChosenCard(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := dsBoard(t, reg, "Crush Underfoot", "Kroxa and Kunoros")
	crush := ids["Crush Underfoot"]
	giant := ids["Kroxa and Kunoros"]
	// Preconditions: the chosen Giant stands on the battlefield carrying
	// lifelink (the provenance observable) at power 6 -- the damage amount
	// and the life gain the pin asserts.
	g := e.G.Obj(giant)
	if g == nil || g.Zone != state.ZBattlefield {
		t.Fatalf("giant not on the battlefield: %+v", g)
	}
	if f := g.Face(); f == nil || f.Power() != 6 {
		t.Fatalf("giant power = %+v, want 6", f)
	}
	if !hasKeyword(e, giant, "Lifelink") {
		t.Fatal("precondition: Kroxa and Kunoros must carry lifelink")
	}
	moveToHand(t, e, crush)
	addMana(t, e, 0, "CR")
	castFirst(t, e, "cast")
	before := e.G.Players[0].Life
	drainDamageSourceAsks(t, e, giant, 20)
	if z := e.G.Obj(giant).Zone; z != state.ZGraveyard {
		t.Fatalf("the chosen Giant survived in %s: it must have dealt its own 6 damage (ChosenCard$CardPower) to itself", z)
	}
	if n := countLifeChanges(t, e, 0, 6); n != 1 {
		t.Fatalf("logged %d LifeChange(0, +6) events, want 1: the LIFELINK chosen Giant must be the damage source", n)
	}
	if got := e.G.Players[0].Life; got != before+6 {
		t.Fatalf("seat 0 life = %d, want %d", got, before+6)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "unresolvable DamageSource$") {
			t.Fatalf("ChosenCard spec failed closed instead of resolving: %q", ev.Text)
		}
	}
	replayCheck(t, e, cfg)
}
