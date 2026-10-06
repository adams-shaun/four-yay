package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
)

// knownCostChangedPlanGaps names the corpus cost-modifier permanents under
// which the census below still finds a planned cast whose executor falls
// back with cost_changed. A shrink-only ratchet: a card that newly does so
// fails by name.
var knownCostChangedPlanGaps = map[string]bool{}

// The census's probe spells: blue instants (the board's mana is Islands)
// that target a creature, any target, and a player -- the announcements a
// target-reading cost static prices.
var costStaticPlanProbes = []string{
	"Name:Census Bounce\nManaCost:2 U\nTypes:Instant\nA:SP$ ChangeZone | ValidTgts$ Creature | Origin$ Battlefield | Destination$ Hand | SpellDescription$ x\nOracle:x\n",
	"Name:Census Ping\nManaCost:2 U\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1 | SpellDescription$ x\nOracle:x\n",
	"Name:Census Draw\nManaCost:2 U\nTypes:Instant\nA:SP$ Draw | ValidTgts$ Player | NumCards$ 1 | SpellDescription$ x\nOracle:x\n",
}

const costStaticCensusBear = "Name:Census Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// costStaticCensusSubject reports whether a corpus card's front face is a
// permanent carrying a RaiseCost/ReduceCost/SetCost static.
func costStaticCensusSubject(c *cards.Card) bool {
	if c == nil || len(c.Faces) == 0 || c.Faces[0] == nil {
		return false
	}
	f := c.Faces[0]
	if f.SpellAbility() != nil && f.SpellAbility().API != "PermanentNoncreature" && f.SpellAbility().API != "PermanentCreature" {
		// Instants and sorceries carry their cost statics for themselves;
		// the census places its subject on the battlefield.
		return false
	}
	for _, st := range f.Statics {
		switch st.Mode {
		case "RaiseCost", "ReduceCost", "SetCost":
			return true
		}
	}
	return false
}

// TestCostStaticPlannedCastsNeverCostChange<k> is the class census behind
// cardfuzz fuzz-1003's planfb cost_changed (Battlefield Thaumaturge under
// Call to Heel, Press the Enemy, Symbol of Unsummoning): every corpus
// permanent with a cost-modifier static is put on both battlefields beside a
// creature each and six Islands, and each probe spell's offered payment plan
// is executed with the first legal target. The planner witnesses the price
// before CR 601.2c; a static that reprices the cast per announcement must
// make it decline (shape:target_dependent_cost), never witness a price the
// executor then finds changed.
//
// The census is sharded into costStaticPlanChunks independent tests (the
// operator's per-test budget, 2026-10-05: 2 GB, 2 vCPU, 1 min): chunk k
// covers its contiguous slice of the sorted subject list, every subject is
// in exactly one chunk, and each cast keeps the seed it had unsharded.
func TestCostStaticPlannedCastsNeverCostChange0(t *testing.T) { costStaticPlanCensusChunk(t, 0) }
func TestCostStaticPlannedCastsNeverCostChange1(t *testing.T) { costStaticPlanCensusChunk(t, 1) }
func TestCostStaticPlannedCastsNeverCostChange2(t *testing.T) { costStaticPlanCensusChunk(t, 2) }
func TestCostStaticPlannedCastsNeverCostChange3(t *testing.T) { costStaticPlanCensusChunk(t, 3) }

// costStaticPlanChunks is the number of TestCostStaticPlannedCastsNeverCostChange<k>
// tests above; TestCensusChunksCoverEverySubject holds the two in step.
const costStaticPlanChunks = 4

// costStaticPlanSubjects is the census's sorted subject list, computed once
// per process for every chunk.
var costStaticPlanSubjects = censusSubjects(costStaticCensusSubject)

func costStaticPlanCensusChunk(t *testing.T, k int) {
	t.Parallel()
	subjects := costStaticPlanSubjects(t)
	if len(subjects) < 300 {
		t.Fatalf("census found %d cost-static permanents, want the corpus's several hundred", len(subjects))
	}
	lo, hi := censusChunkRange(len(subjects), k, costStaticPlanChunks)
	probeNames := make([]string, len(costStaticPlanProbes))
	for pi, probe := range costStaticPlanProbes {
		probeNames[pi] = card(t, probe).Faces[0].Name
	}
	var gaps []string
	planned, fallbacks := 0, map[string]int{}
	var sp Spare
	for si := lo; si < hi; si++ {
		s := subjects[si]
		name := s.Faces[0].Name
		for pi, probe := range costStaticPlanProbes {
			reason, ok := costStaticCensusCast(t, s, probe, uint64(80000+si*len(costStaticPlanProbes)+pi), &sp)
			if !ok {
				continue
			}
			planned++
			if reason != "" {
				fallbacks[reason]++
			}
			if reason == paymentFallbackCostChanged && !knownCostChangedPlanGaps[name] {
				gaps = append(gaps, name+" / "+probeNames[pi])
			}
		}
	}
	t.Logf("cost-static plan census chunk %d/%d: permanents [%d,%d) of %d, %d planned casts executed, fallbacks %v",
		k, costStaticPlanChunks, lo, hi, len(subjects), planned, fallbacks)
	if len(gaps) != 0 {
		t.Fatalf("%d planned casts fell back with cost_changed:\n%s", len(gaps), strings.Join(gaps, "\n"))
	}
}

// costStaticCensusCast builds one census board and, when the probe's cast is
// offered a payment plan, executes it. ok reports whether a plan was
// executed; reason is the first PaymentFallback reason the cast posed.
//
// The engine is built on sp's recycled storage and Released back into it on
// return, so the census allocates one engine's log and arena, not one per
// board.
func costStaticCensusCast(t *testing.T, s *cards.Card, probe string, seed uint64, sp *Spare) (reason string, ok bool) {
	t.Helper()
	e, _, spell := newFixtureDeckSpare(t, sp, seed, probe)
	defer func() { *sp = e.Release() }()
	onBoardCard(t, e, 0, s)
	onBoardCard(t, e, 1, s)
	onBoard(t, e, 0, costStaticCensusBear)
	onBoard(t, e, 1, costStaticCensusBear)
	for i := 0; i < 6; i++ {
		onBoard(t, e, 0, ppIsland)
	}
	e.pending = nil
	e.askPriority(0)
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		return "", false
	}
	e.EnsurePaymentActions()
	var action *decision.PaymentAction
	for i := range d.PaymentActions {
		if a := &d.PaymentActions[i]; a.Cast.Object == spell && len(a.Plans) > 0 {
			action = a
			break
		}
	}
	if action == nil {
		return "", false
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player,
		Payment: &decision.PaymentSelection{ActionID: action.ID, Plan: action.Plans[0]}}); err != nil {
		t.Fatalf("%s: submit planned cast: %v", s.Faces[0].Name, err)
	}
	for step := 0; step < 20; step++ {
		d = e.Pending()
		if d == nil {
			break
		}
		if d.PaymentFallback != nil && reason == "" {
			reason = d.PaymentFallback.Reason
		}
		if d.Kind == decision.KPriority || d.Player != 0 || !chainCensusAnswer(e, d) {
			break
		}
	}
	return reason, true
}
