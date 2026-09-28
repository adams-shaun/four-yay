package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// putcounter_dist_test.go (task inbox-paramcensus-putcounter-etb) pins the
// DividedAsYouChoose$ PutCounter distribution shape. The brief's other
// carriers (the etbCounter X-cost family: Chalice of the Void, Endless One,
// Walking Ballista, Hangarback Walker) were already read and pinned on main
// before this work (effects/counters.go's ETB$ read,
// rules/etb_test.go's TestEtbCounterUsesTheChosenX), so this file covers what
// remained: the Vastwood Hydra death trigger's Choices$ recipient pick
// (ChoiceAmount$/MinChoiceAmount$/DividedAsYouChoose$) and the target-based
// split the same parameter rides in the wider corpus.

// vastwoodSrc is Vastwood Hydra's own script (the corpus card, copied inline:
// fixtures are written inline, the GPL corpus is never committed). The
// Counters.Y SVar is exactly the corpus line.
const vastwoodSrc = "Name:Vastwood Hydra\nManaCost:X G G\nTypes:Creature Hydra\nPT:0/0\n" +
	"K:etbCounter:P1P1:X\nSVar:X:Count$xPaid\n" +
	"T:Mode$ ChangesZone | ValidCard$ Card.Self | Origin$ Battlefield | Destination$ Graveyard | Execute$ TrigCounterDist | OptionalDecider$ TriggeredCardController | TriggerDescription$ x\n" +
	"SVar:TrigCounterDist:DB$ PutCounter | Choices$ Creature.YouCtrl | ChoiceTitle$ Choose any number of creatures you control to distribute counters to | CounterType$ P1P1 | CounterNum$ Y | ChoiceAmount$ Y | MinChoiceAmount$ 1 | DividedAsYouChoose$ Y\n" +
	"SVar:Y:TriggeredCard$CardCounters.P1P1\n" +
	"Oracle:Vastwood Hydra enters with X +1/+1 counters on it.\n"

const distBearSrc = "Name:Grizzly\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
const distBoltSrc = "Name:Bolt\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 3\nOracle:x\n"

// castHydra casts a Vastwood Hydra fixture for X and returns its id.
func castHydra(t *testing.T, e *Engine, id state.ObjID, x int) {
	t.Helper()
	submitChoices(t, e, castOptionFor(t, e, id).Index)
	submitChoices(t, e, x) // the {X} paid
	passUntilStackEmpty(t, e, 20)
}

// TestVastwoodHydraDistributesCountersAsChosenOnDeath pins the whole
// distribution: with two eligible creatures the death trigger poses a real
// KChoose (ResumeKind "counter_dist", Min 1 from MinChoiceAmount$, Max the
// ChoiceAmount$ Y clamped to the eligible count), and the answered pick --
// NOT the deterministic first-Max zone-order stand-in -- is where the
// CounterNum$ Y counters land, divided one at a time round-robin in answer
// order (the earlier-chosen recipient takes the extras).
func TestVastwoodHydraDistributesCountersAsChosenOnDeath(t *testing.T) {
	t.Parallel()
	e, cfg, find := etbConfig(t, 71, []string{vastwoodSrc, distBearSrc, distBearSrc}, []string{distBoltSrc})
	b1 := putCreature(t, e, 0, distBearSrc)
	b2 := putCreature(t, e, 0, distBearSrc)
	hydra := find("Vastwood Hydra", 0)
	addMana(t, e, 0, "GGGGG")
	castHydra(t, e, hydra, 3)
	if o := e.G.Obj(hydra); o.Zone != state.ZBattlefield || o.Counter("P1P1") != 3 {
		t.Fatalf("hydra %s counters %d, want battlefield/3", o.Zone, o.Counter("P1P1"))
	}

	// Kill it: seat 1's bolt deals exactly lethal to the 3/3.
	bolt := addToHand(t, e, 1, distBoltSrc)
	passToPlayerOne(t, e)
	addMana(t, e, 1, "R")
	submitChoices(t, e, castOptionFor(t, e, bolt).Index)
	td := e.Pending()
	if td == nil || td.Kind != decision.KTarget {
		t.Fatalf("after casting the bolt: %+v, want the target decision", td)
	}
	tgtIdx := -1
	for _, o := range td.Options {
		if o.Obj == hydra {
			tgtIdx = o.Index
		}
	}
	if tgtIdx < 0 {
		t.Fatalf("no bolt option for the hydra: %+v", td.Options)
	}
	submitChoices(t, e, tgtIdx)

	// The death trigger is optional (OptionalDecider$): accept it.
	d := passUntilNonPriority(t, e, 20)
	if d.Kind != decision.KTriggerOptional {
		t.Fatalf("after the bolt: %+v, want the death trigger's optional ask", d)
	}
	submitChoices(t, e, 0) // yes

	// The distribution ask: bounded by MinChoiceAmount$ 1 / ChoiceAmount$ Y=3,
	// clamped to the two eligible creatures, posed to the hydra's controller.
	d = passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "counter_dist" {
		t.Fatalf("after the yes: %+v, want a counter_dist KChoose", d)
	}
	if d.Player != 0 {
		t.Fatalf("distribution ask player = %d, want the hydra's controller (0)", d.Player)
	}
	if d.Min != 1 || d.Max != 2 {
		t.Fatalf("distribution ask range = %d..%d, want 1..2 (MinChoiceAmount$ 1, ChoiceAmount$ 3 clamped to eligible)", d.Min, d.Max)
	}
	if len(d.Options) != 2 || d.Options[0].Obj != b1 || d.Options[1].Obj != b2 {
		t.Fatalf("distribution options = %+v, want one per eligible creature in zone order (%d, %d)", d.Options, b1, b2)
	}

	// Answer with the SECOND creature first, so the answer order -- not zone
	// order -- drives the split: 3 counters round-robin over (b2, b1) gives
	// b2 two and b1 one.
	submitChoices(t, e, d.Options[1].Index, d.Options[0].Index)
	if d = e.Pending(); d == nil || d.Kind != decision.KPriority {
		t.Fatalf("after the distribution answer pending = %+v, want priority", d)
	}
	if got := e.G.Obj(b2).Counter("P1P1"); got != 2 {
		t.Fatalf("b2 took %d counters, want 2 (first in answer order)", got)
	}
	if got := e.G.Obj(b1).Counter("P1P1"); got != 1 {
		t.Fatalf("b1 took %d counters, want 1", got)
	}
	replayCheck(t, e, cfg)
}

// TestVastwoodHydraDistributionSkipsTheAskWhenOneCreatureIsEligible pins the
// strict-supersets gate: with a single eligible creature the only legal
// recipient set is that creature, so no decision is posed and the whole
// CounterNum$ total lands on it silently.
func TestVastwoodHydraDistributionSkipsTheAskWhenOneCreatureIsEligible(t *testing.T) {
	t.Parallel()
	e, cfg, find := etbConfig(t, 72, []string{vastwoodSrc, distBearSrc}, []string{distBoltSrc})
	b1 := putCreature(t, e, 0, distBearSrc)
	hydra := find("Vastwood Hydra", 0)
	addMana(t, e, 0, "GGGGG")
	castHydra(t, e, hydra, 3)

	bolt := addToHand(t, e, 1, distBoltSrc)
	passToPlayerOne(t, e)
	addMana(t, e, 1, "R")
	submitChoices(t, e, castOptionFor(t, e, bolt).Index)
	td := e.Pending()
	if td == nil || td.Kind != decision.KTarget {
		t.Fatalf("after casting the bolt: %+v, want the target decision", td)
	}
	tgtIdx := -1
	for _, o := range td.Options {
		if o.Obj == hydra {
			tgtIdx = o.Index
		}
	}
	if tgtIdx < 0 {
		t.Fatalf("no bolt option for the hydra: %+v", td.Options)
	}
	submitChoices(t, e, tgtIdx)

	// The death trigger is optional (OptionalDecider$): accept it. With one
	// eligible creature the trigger's own resolution asks nothing after it.
	d := passUntilNonPriority(t, e, 20)
	if d.Kind != decision.KTriggerOptional {
		t.Fatalf("after the bolt: %+v, want the death trigger's optional ask", d)
	}
	submitChoices(t, e, 0) // yes
	d = e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("after the yes pending = %+v, want priority (no distribution ask over one eligible creature)", d)
	}
	if got := e.G.Obj(b1).Counter("P1P1"); got != 3 {
		t.Fatalf("b1 took %d counters, want all 3", got)
	}
	replayCheck(t, e, cfg)
}

// TestPutCounterDividedAmongTargetsSplitsTheTotal pins the target-based
// carrier (Choices$ absent, 53 further raw corpus lines): DividedAsYouChoose$
// makes the CounterNum$ total a split over the chosen targets, NOT the
// pre-fix n-per-target misread -- 4 counters over two targets are 2 and 2.
func TestPutCounterDividedAmongTargetsSplitsTheTotal(t *testing.T) {
	t.Parallel()
	src := "Name:Distributor\nManaCost:2 G\nTypes:Creature Elf Druid\nPT:1/1\n" +
		"A:SP$ PutCounter | Cost$ 2 G | ValidTgts$ Creature.YouCtrl | TargetMax$ 2 | CounterType$ P1P1 | CounterNum$ 4 | DividedAsYouChoose$ 4 | SpellDescription$ Distribute four +1/+1 counters among up to two target creatures you control.\n" +
		"Oracle:x\n"
	bear := "Name:Grizzly\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e, cfg, find := etbConfig(t, 73, []string{src, bear, bear}, nil)
	b1 := putCreature(t, e, 0, bear)
	b2 := putCreature(t, e, 0, bear)
	dist := find("Distributor", 0)
	addMana(t, e, 0, "GGG")
	submitChoices(t, e, castOptionFor(t, e, dist).Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("after casting: %+v, want the target decision", d)
	}
	if d.Max != 2 {
		t.Fatalf("target ask max = %d, want 2 (TargetMax$ 2)", d.Max)
	}
	tgt := map[state.ObjID]int{}
	for _, o := range d.Options {
		tgt[o.Obj] = o.Index
	}
	// Answer b2 first so the split follows answer order: 4 round-robin over
	// (b2, b1) is 2 and 2 -- indistinguishable from zone order here by size,
	// but the per-recipient CounterChange events carry it.
	submitChoices(t, e, tgt[b2], tgt[b1])
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(b2).Counter("P1P1"); got != 2 {
		t.Fatalf("b2 took %d counters, want 2 (a split, not 4-per-target)", got)
	}
	if got := e.G.Obj(b1).Counter("P1P1"); got != 2 {
		t.Fatalf("b1 took %d counters, want 2", got)
	}
	replayCheck(t, e, cfg)
}
