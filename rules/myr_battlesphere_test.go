package rules

// cost:tapXType -- the dynamic tap-permanent cost (rules/mana.go's
// dynTapCost heads). ParseCost's literal choiceCost regex only read
// tapXType<N/Spec>; the dynamic forms degraded the whole token to one
// phantom generic mana and were silently bought for {1}, with the X count
// never bound. The carriers:
//
//   - Myr Battlesphere (the Rebellion Rising import): an optional attack
//     trigger whose Execute AB carries Cost$ tapXType<X/Myr>, NumAtt$ +X and
//     a chained DB$ DealDamage | NumDmg$ X reading SVar:X:Count$xPaid.
//   - Mossbridge Troll: an activated AB with tapXType<Any/
//     Creature.Other+withTotalPowerGE10> -- the Any form binds no X, and the
//     withTotalPowerGE10 group predicate is a SET-level floor the tap-cost
//     machinery enforces (Decision.MinSum over the tapped set's total
//     power; crew_test.go pins the end-to-end shape), so the ability is
//     offered only when the board's other untapped creatures can reach
//     total power 10 (pinned below as the no-free-pump guard; pre-fix the
//     token degraded to one generic and pumped for a floating mana, and
//     before the floor was modelled the unknown predicate failed closed).
//   - Explosive Singularity: a RaiseCost static with tapXType<Tapped/
//     Creature>, whose count rides an announced SVar variable -- different
//     machinery (the RaiseCost Cost$ grammar), out of scope here.
//
// Also exercised: the activated-ability cast flow's X announce paths (the
// tap election announcing X by itself, and the printed-{X} composition
// deferring to xAsk), and the "Mandatory" cost marker (yotia_declares_war's
// Cost$ Mandatory tapXType<X/Artifact> -- the marker is stripped at the
// parser; the chapter-body wrapper's own unpaid-cost gap is ledgered
// separately).

import (
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func battlesphereFixture(t *testing.T) (*Engine, state.ObjID, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	card, ok := reg.Lookup("Myr Battlesphere")
	if !ok {
		t.Fatal("corpus has no Myr Battlesphere")
	}
	e := combatEngine(t)
	sphere := onBoardCard(t, e, 0, card)
	myr1 := onBoardReady(t, e, 0, "Name:Myr Servant\nManaCost:3\nTypes:Artifact Creature Myr\nPT:1/1\nOracle:x\n")
	myr2 := onBoardReady(t, e, 0, "Name:Myr Galvanizer\nManaCost:3\nTypes:Artifact Creature Myr\nPT:2/1\nOracle:x\n")
	tappedMyr := onBoardReady(t, e, 0, "Name:Myr Moonvessel\nManaCost:2\nTypes:Artifact Creature Myr\nPT:1/1\nOracle:x\n")
	e.emit(events.Event{Kind: events.Tap, Obj: tappedMyr})
	// A real attacker is tapped by the declaration; the synthetic event does
	// not tap, so tap the sphere explicitly to keep it out of the election.
	e.emit(events.Event{Kind: events.Tap, Obj: sphere})
	return e, sphere, myr1, myr2, tappedMyr
}

// battlesphereTrigger drives the attack declaration through the trigger
// queue and answers the optional-apply ask "yes" -- the state where the
// tap-election decision is pending.
func battlesphereTrigger(t *testing.T, e *Engine, sphere state.ObjID) {
	t.Helper()
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{sphere}})
	e.putTriggersOnStack()
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTriggerOptional {
		t.Fatalf("optional-apply ask missing: %+v", d)
	}
	submitChoices(t, e, 0) // yes
}

func TestMyrBattlesphereAttackTriggerAsksAndPaysTheTapXCost(t *testing.T) {
	t.Parallel()
	e, sphere, myr1, myr2, tappedMyr := battlesphereFixture(t)
	battlesphereTrigger(t, e, sphere)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 0 || d.Max != 2 {
		t.Fatalf("tap election %+v, want KChoose Min 0 Max 2", d)
	}
	if len(d.Options) != 2 {
		t.Fatalf("election options %+v, want only the two untapped Myr (the tapped Myr and the attacking Battlesphere are not offered)", d.Options)
	}
	for _, o := range d.Options {
		if o.Kind != "trigger_cost_tap" || (o.Obj != myr1 && o.Obj != myr2) {
			t.Fatalf("election option %+v, want trigger_cost_tap on myr1/myr2", o)
		}
	}
	// Tap both Myr: X = 2. The answer replays byte-identically in a clone
	// taken at the decision boundary (the window's resume + ctx.X binding are
	// engine state a replay re-derives).
	clone := e.Clone()
	in := decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1}}
	if err := e.Submit(in); err != nil {
		t.Fatal(err)
	}
	if err := clone.Submit(in); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(e.L.Events, clone.L.Events) {
		t.Fatal("clone diverged across the tap-X window's payment and resolution")
	}
	if !e.G.Obj(myr1).Tapped || !e.G.Obj(myr2).Tapped {
		t.Fatal("the elected Myr were not tapped as the cost")
	}
	if !e.G.Obj(tappedMyr).Tapped {
		t.Fatal("the already-tapped Myr untapped")
	}
	if got := e.Power(sphere); got != 6 {
		t.Fatalf("Battlesphere power = %d, want 6 (4 base + X=2 NumAtt$ +X)", got)
	}
	if got := e.G.Players[1].Life; got != 18 {
		t.Fatalf("defender life = %d, want 18 (20 - X=2 NumDmg$ X)", got)
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack not empty after the trigger resolved: %d objects", len(e.G.Stack))
	}
}

func TestMyrBattlesphereTapXEmptyElectionDeclines(t *testing.T) {
	t.Parallel()
	e, sphere, myr1, myr2, _ := battlesphereFixture(t)
	battlesphereTrigger(t, e, sphere)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 0 {
		t.Fatalf("tap election %+v, want KChoose Min 0", d)
	}
	// An empty election is the decline: the body never runs, so no pump, no
	// damage, no taps -- "you may tap X untapped Myr. If you do, ..." with
	// nothing tapped does nothing, and the no-op body's events are not
	// emitted either.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: nil}); err != nil {
		t.Fatalf("empty election rejected: %v", err)
	}
	if got := e.Power(sphere); got != 4 {
		t.Fatalf("Battlesphere power = %d, want 4 (decline: no pump)", got)
	}
	if e.G.Obj(myr1).Tapped || e.G.Obj(myr2).Tapped {
		t.Fatal("the decline tapped the Myr")
	}
	if got := e.G.Players[1].Life; got != 20 {
		t.Fatalf("defender life = %d, want 20 (decline: no damage)", got)
	}
}

// TestMossbridgeTrollFloorWithholdsWhenPowerFallsShort pins the Any form's
// floor guard: Mossbridge's tap spec (Creature.Other+withTotalPowerGE10)
// carries a set-level "total power 10 or greater" floor the board's other
// untapped creatures cannot reach (2 power at best), so the ability is not
// offered -- never an election whose every answer Decision.Validate rejects,
// and never a zero-tap payment of an effect (+20/+20) that does not scale
// with the taps. The offered-and-paid direction is pinned in
// crew_test.go's TestMossbridgeTrollPaysWhenTotalPowerReachesTheFloor.
func TestMossbridgeTrollFloorWithholdsWhenPowerFallsShort(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	troll := onBoard(t, e, 0, "Name:Mossbridge Troll\nManaCost:5 G G\nTypes:Creature Troll\nPT:5/5\nOracle:x\n")
	other := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.priorityRound()
	if _, offered := findAbilityOption(e, troll, 0); offered {
		t.Fatal("Mossbridge Troll's tap-any ability was offered although its tap spec admits no candidate (withTotalPowerGE10 fails closed)")
	}
	if e.G.Obj(other).Tapped {
		t.Fatal("the other creature was tapped")
	}
}

// TestTapXTypeCastFlowElectionAnnouncesX pins the activated-ability path
// where the tap election IS the {X} announce (no printed {X} in the cost):
// the paid count binds Count$xPaid through the pay-time CastInfo, so
// NumAtt$ +X reads the tapped count.
func TestTapXTypeCastFlowElectionAnnouncesX(t *testing.T) {
	t.Parallel()
	src := "Name:Forge Adept\nManaCost:2\nTypes:Artifact Creature Myr\nPT:1/1\n" +
		"A:AB$ Pump | Cost$ T tapXType<X/Artifact> | Defined$ Self | NumAtt$ +X | SorcerySpeed$ True\nSVar:X:Count$xPaid\nOracle:x\n"
	e := handEngine(t)
	adept := onBoard(t, e, 0, src)
	e.G.Obj(adept).SummonSick = false
	art := onBoard(t, e, 0, "Name:Anvil\nManaCost:1\nTypes:Artifact\nOracle:x\n")
	addMana(t, e, 0, "")
	opt := abilityOption(t, e, adept, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 0 || d.Max != 1 {
		t.Fatalf("tap election %+v, want KChoose Min 0 Max 1", d)
	}
	submitChoices(t, e, 0)
	passUntilStackEmpty(t, e, 20)
	if !e.G.Obj(art).Tapped {
		t.Fatal("the artifact was not tapped as the cost")
	}
	if got := e.Power(adept); got != 2 {
		t.Fatalf("power = %d, want 2 (1 base + X=1)", got)
	}
}

// TestTapXTypeCastFlowDefersToThePrintedX pins the composed path (Necron
// Overlord's "X, tap X untapped artifacts"): the printed {X} announces
// first (xAsk, bounded by the artifact count), the tap settle asks exactly
// that many, and the same X drives the body.
func TestTapXTypeCastFlowDefersToThePrintedX(t *testing.T) {
	t.Parallel()
	src := "Name:Overlord Adept\nManaCost:4\nTypes:Artifact Creature\nPT:2/2\n" +
		"A:AB$ LoseLife | Cost$ X T tapXType<X/Artifact> | ValidTgts$ Opponent | LifeAmount$ X | TgtPrompt$ Select target opponent\nSVar:X:Count$xPaid\nOracle:x\n"
	e := handEngine(t)
	adept := onBoard(t, e, 0, src)
	e.G.Obj(adept).SummonSick = false
	art1 := onBoard(t, e, 0, "Name:Anvil\nManaCost:1\nTypes:Artifact\nOracle:x\n")
	art2 := onBoard(t, e, 0, "Name:Gear\nManaCost:1\nTypes:Artifact\nOracle:x\n")
	addMana(t, e, 0, "GGG")
	opt := abilityOption(t, e, adept, 0)
	submitChoices(t, e, opt.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) == 0 || d.Options[0].Kind != "x" {
		t.Fatalf("X announce ask %+v", d)
	}
	maxX := -1
	for _, o := range d.Options {
		if o.Amount > maxX {
			maxX = o.Amount
		}
	}
	if maxX != 2 {
		t.Fatalf("X options max=%d, want 2 (the two artifacts cap the pool bound)", maxX)
	}
	submitChoices(t, e, 1) // X = 1
	dt := e.Pending()
	if dt == nil || dt.Kind != decision.KChoose || dt.Min != 1 || dt.Max != 1 {
		t.Fatalf("exact tap settle %+v, want Min/Max 1", dt)
	}
	if len(dt.Options) != 2 || dt.Options[0].Obj != art1 || dt.Options[1].Obj != art2 {
		t.Fatalf("tap options %+v, want the two artifacts", dt.Options)
	}
	submitChoices(t, e, 1) // tap art2
	if d := e.Pending(); d == nil || d.Kind != decision.KTarget {
		t.Fatalf("target ask %+v, want KTarget on the opponent", d)
	}
	tp := indexOfPlayerOption(e.Pending(), 1)
	if tp < 0 {
		t.Fatal("no opponent option in the target ask")
	}
	submitChoices(t, e, tp)
	passUntilStackEmpty(t, e, 20)
	if !e.G.Obj(art2).Tapped || e.G.Obj(art1).Tapped {
		t.Fatal("the elected artifact was not the one tapped")
	}
	if got := e.G.Players[1].Life; got != 19 {
		t.Fatalf("opponent life = %d, want 19 (X=1 LifeAmount$ X)", got)
	}
}

// TestParseCostModelsDynamicTapXType pins the parser half: the dynamic heads
// are TapPermanent parts with the Dyn token, no Unknown label, and the
// Mandatory marker is not a payment.
func TestParseCostModelsDynamicTapXType(t *testing.T) {
	t.Parallel()
	c := ParseCost("tapXType<X/Myr>")
	if len(c.TapPermanent) != 1 || c.TapPermanent[0].Dyn != "X" || c.TapPermanent[0].Spec != "Myr" {
		t.Fatalf("X form parsed %+v", c.TapPermanent)
	}
	if len(c.Unknown) != 0 {
		t.Fatalf("X form reported Unknown %v", c.Unknown)
	}
	c = ParseCost("tapXType<Any/Creature.Other+withTotalPowerGE10>")
	// The Any form's withTotalPowerGE<N> group predicate is a SET-level floor:
	// it moves into CostPart.MinPower and out of the spec, so the per-object
	// filter sees only the per-candidate terms (crew_test.go pins the
	// end-to-end floor; the X form keeps the predicate in the spec and fails
	// closed as before).
	if len(c.TapPermanent) != 1 || c.TapPermanent[0].Dyn != "Any" || c.TapPermanent[0].Spec != "Creature.Other" || c.TapPermanent[0].MinPower != 10 {
		t.Fatalf("Any form parsed %+v", c.TapPermanent)
	}
	if len(c.Unknown) != 0 {
		t.Fatalf("Any form reported Unknown %v", c.Unknown)
	}
	c = ParseCost("Mandatory tapXType<X/Artifact>")
	if len(c.TapPermanent) != 1 || c.Generic != 0 || len(c.Unknown) != 0 {
		t.Fatalf("Mandatory form parsed generic=%d parts=%+v unknown=%v", c.Generic, c.TapPermanent, c.Unknown)
	}
	c = ParseCost("2 R R R tapXType<X/Creature>")
	if c.Generic != 2 || c.Colored[state.MR] != 3 || len(c.TapPermanent) != 1 || c.TapPermanent[0].Dyn != "X" {
		t.Fatalf("composed form parsed parts=%+v generic=%d R=%d", c.TapPermanent, c.Generic, c.Colored[state.MR])
	}
	if len(c.Unknown) != 0 {
		t.Fatalf("composed form reported Unknown %v", c.Unknown)
	}
	// The literal form is untouched; a literal spec's group predicate also
	// moves into MinPower (exactly N are tapped, so the floor is enforced
	// against the N largest candidates).
	c = ParseCost("tapXType<2/Artifact>")
	if len(c.TapPermanent) != 1 || c.TapPermanent[0].Dyn != "" || c.TapPermanent[0].N != 2 {
		t.Fatalf("literal form parsed %+v", c.TapPermanent)
	}
	c = ParseCost("tapXType<2/Creature+withTotalPowerGE5>")
	if len(c.TapPermanent) != 1 || c.TapPermanent[0].N != 2 || c.TapPermanent[0].Spec != "Creature" || c.TapPermanent[0].MinPower != 5 {
		t.Fatalf("literal floor form parsed %+v", c.TapPermanent)
	}
	if len(c.Unknown) != 0 {
		t.Fatalf("literal floor form reported Unknown %v", c.Unknown)
	}
	// formatCost round-trips the dynamic token (the Dyn token, not N=0).
	if got := formatCost(ParseCost("tapXType<Any/Creature>")); !strings.Contains(got, "tapXType<Any/Creature>") {
		t.Fatalf("formatCost = %q, want it to carry the Any token", got)
	}
	// A floored Any form round-trips with the stripped spec; formatCost is a
	// display/wire renderer (decision.Option.Cost), the floor itself lives in
	// MinSum/Validate at payment, so the predicate is not re-emitted here.
	if got := formatCost(ParseCost("tapXType<Any/Creature.Other+withTotalPowerGE10>")); !strings.Contains(got, "tapXType<Any/Creature.Other>") {
		t.Fatalf("formatCost = %q, want it to carry the Any token", got)
	}
}

// TestTapXTypeXFormZeroCandidatesActivationResolvesXZero pins the
// zero-candidate X-form election on the ACTIVATED-ABILITY path against a
// real corpus card. Secluded Starforge's second ability is
// "{2}, {T}, Tap X untapped artifacts you control: Target creature gets
// +X/+0" -- with a creature to target but no artifacts on the board the
// cost is payable at X=0 (CR 601.2b), so the ability is offered and the
// election has no options. Posting a Min 0/Max 0 choose with zero options
// panics rules/engine.go's ask, so the X=0 announcement must resolve
// silently (mirroring rules/cumulative.go's triggeredTapAsk decline).
func TestTapXTypeXFormZeroCandidatesActivationResolvesXZero(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	forge := onBoardCard(t, e, 0, corpusAlternativeCard(t, "Secluded Starforge"))
	e.G.Obj(forge).SummonSick = false
	// A creature to target; deliberately NO artifacts, so the X-form tap
	// spec has zero candidates.
	target := onBoard(t, e, 0, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	addMana(t, e, 0, "CC")
	// Ability index 1 is the {2},{T} pump (index 0 is the mana ability).
	opt := abilityOption(t, e, forge, 1)
	submitChoices(t, e, opt.Index)
	// No tap election may be posted (nobody could answer it differently).
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		t.Fatalf("zero-candidate X-form posted an election: %+v", d)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("target ask %+v, want KTarget after the X=0 announce", d)
	}
	targetOpt := -1
	for _, o := range d.Options {
		if o.Obj == target {
			targetOpt = o.Index
		}
	}
	if targetOpt < 0 {
		t.Fatalf("target ask offers no option for the creature: %+v", d.Options)
	}
	submitChoices(t, e, targetOpt)
	passUntilStackEmpty(t, e, 20)
	// X=0: no artifacts were tapped and the creature gets no pump.
	if e.G.Obj(target).Tapped {
		t.Fatal("X=0 tapped the target creature")
	}
	if got := e.Power(target); got != 2 {
		t.Fatalf("target power = %d, want 2 (X=0 pump)", got)
	}
}

// TestTapXTypeXFormZeroCandidatesCastResolvesXZero pins the zero-candidate
// X-form election on the SPELL path against a real corpus card. Burn at the
// Stake's cost is "{2}{R}{R}{R}, tap any number of untapped creatures you
// control" -- with no creatures on board the spell is castable at X=0 (the
// announcement), and selecting the cast must not panic.
func TestTapXTypeXFormZeroCandidatesCastResolvesXZero(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Burn at the Stake"))
	spell := e.G.Zone(state.ZHand, 0)[0]
	addMana(t, e, 0, "RRRRR")
	submitChoices(t, e, castOption(t, e, spell))
	// No tap election: zero creatures, so the X=0 announce is silent.
	for i := 0; i < 30; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		if d.Kind == decision.KChoose {
			t.Fatalf("zero-candidate X-form posted an election: %+v", d)
		}
		if d.Kind == decision.KTarget {
			submitChoices(t, e, 0)
			continue
		}
		break
	}
	passUntilStackEmpty(t, e, 20)
	// X=0: the spell resolves and deals 0 damage to the target.
	if e.G.Obj(spell).Zone != state.ZGraveyard {
		t.Fatalf("spell zone=%s, want graveyard after resolving", e.G.Obj(spell).Zone)
	}
}
