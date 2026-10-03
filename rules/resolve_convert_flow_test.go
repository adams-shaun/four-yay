package rules

// W3 step 2 (group "flow"): dual-run tests for the card-flow ask sites
// converted onto the resolution kernel -- the look ack, the reveal pick and
// may-reveal, the discard family, Dig, DigUntil, Surveil's look election,
// Hideaway, NameCard, Draw's optional/up-to asks, Dredge and Recruit. Each
// case is cast and resolved on legacy and on the tape kernel by the same
// deterministic driver (tapeDual) and must stay identical, its asks served
// from the tape with no legacy switch, and its named resume kind posed.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
)

// tapeFlowCase is one converted-site scenario: the sorcery's ability lines,
// the resume kinds the run must pose (on both paths) and the minimum
// number of tape-served answers.
type tapeFlowCase struct {
	name, src string
	kinds     []string
	served    int64
	seats     int
	extra     []string // further fixture cards, moved to seat 0's hand
	setup     func(t *testing.T, e *Engine)
	// checkpointAll: the site's ask is board-dependent (a Dredge ask over
	// an otherwise ask-free Draw), which the ask-free predicate cannot see,
	// so the case checkpoints every resolution (tapeCheckpointAll).
	checkpointAll bool
}

// tapeCastAndResolveKinds is tapeCastAndResolve recording every posed
// non-priority decision's resume kind.
func tapeCastAndResolveKinds(t *testing.T, e *Engine, name, mana string, kinds map[string]int) {
	t.Helper()
	addMana(t, e, 0, mana)
	id := fixtureInHand(t, e, name)
	submitChoices(t, e, castOptionFor(t, e, id).Index)
	for i := 0; i < 400; i++ {
		d := e.Pending()
		if d == nil || e.G.Over {
			return
		}
		if d.Kind == decision.KPriority {
			if len(e.G.Stack) == 0 {
				return
			}
			submitChoices(t, e, tapePassIndex(d))
			continue
		}
		kinds[d.ResumeKind]++
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: tapePick(d)}); err != nil {
			t.Fatalf("submit %s/%s %v: %v", d.Kind, d.ResumeKind, tapePick(d), err)
		}
	}
	t.Fatal("stack never drained")
}

const tapeEnhancedSrc = "Name:Tape Enhanced\nManaCost:1 U\nTypes:Enchantment\n" +
	"S:Mode$ SurveilNum | Num$ 2 | ValidPlayer$ You | Optional$ True | Description$ x\nOracle:x\n"

const tapeDredgerSrc = "Name:Tape Dredger\nManaCost:G\nTypes:Sorcery\nK:Dredge:2\nA:SP$ GainLife | LifeAmount$ 1\nOracle:x\n"

const (
	tapeAuraSrc  = "Name:Tape Aura\nManaCost:W\nTypes:Enchantment Aura\nK:Enchant creature\nA:SP$ Attach | Cost$ W | ValidTgts$ Creature | AILogic$ Pump\nOracle:x\n"
	tapeBearASrc = "Name:Tape Bear A\nManaCost:G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	tapeBearBSrc = "Name:Tape Bear B\nManaCost:G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
)
