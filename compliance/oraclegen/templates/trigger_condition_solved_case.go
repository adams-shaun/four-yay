// Solved-Case preludes for triggers (Level B). A Case's "Solved —" trigger is
// offered only while the source Case is solved (IsPresent$ Card.Self+IsSolved),
// and a Case is solved only by its own end-step "To solve" trigger (CR 719.3a).
// The setup that solves it is the solve trigger's own condition candidates
// (the shared prelude and fixture helpers), then the end-step pass that lets
// the solve trigger fire and its resolve. The row trigger's own cause then
// runs against a solved Case.
//
// The candidates carry no phase positioning of their own: the phase recipe
// emits the pass_to that waits for the row trigger's phase with the row
// trigger's You gate in mind (a begin-combat or end-step row trigger must not
// stop at p1's first matching phase), and the spell-cast recipe's preludes end
// at p0's next main phase, where a sorcery-speed cast is legal.
package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// triggerSolvedCasePreludes solves the source Case when the trigger's own
// IsPresent gate needs it solved. It reports nothing when the trigger has no
// such gate or the card carries no "To solve" trigger; the row then keeps its
// named "needs a solved Case" gap.
func triggerSolvedCasePreludes(reg *cards.Registry, f *cards.Face, t *cards.Trigger) []conditionPrelude {
	needed := false
	for _, key := range []cards.ParamKey{cards.PKIsPresent, cards.PKIsPresent2} {
		if solvedSelfSpec(t.ParamStr(key)) {
			needed = true
		}
	}
	if !needed {
		return nil
	}
	solve := caseSolveTrigger(f)
	if solve == nil {
		return nil
	}
	var conds []conditionPrelude
	conds = append(conds, conditionPreludes(reg, solve.Params, f.SVars)...)
	conds = append(conds, triggerConditionFixtures(reg, f, solve)...)
	if len(conds) == 0 {
		conds = []conditionPrelude{{}}
	}
	out := make([]conditionPrelude, 0, len(conds))
	for _, c := range conds {
		// A prelude that attacks takes the turn through combat before the
		// solve trigger's end step; no Case solve condition asks for it.
		if preludeAttacks(c) {
			continue
		}
		c.solvedCase = true
		c.steps = append(append([]oraclegen.Step(nil), c.steps...),
			oraclegen.Step{Op: "pass_to", Step: "end"},
			oraclegen.Step{Op: "resolve"},
			// The cause runs from p0's next main phase (turn 3): an attack
			// cause there reaches its own declare-attackers ask, and the
			// phase recipe's pass_to then reaches the row trigger's phase.
			oraclegen.Step{Op: "pass_to", Step: "main1", Active: "p0"})
		out = append(out, c)
	}
	return out
}

// colorsCountPreludes is the board a CheckSVar$ Count$...$Colors gate needs:
// one permanent of each colour, so the distinct-colour count reaches the
// gate's floor. The probes are plain permanents setup can place, one per
// colour, in fixed order.
func colorsCountPreludes(reg *cards.Registry, body, compare string) []conditionPrelude {
	if !strings.Contains(strings.ToLower(body), "$colors") {
		return nil
	}
	n := staticCountFrom(compare)
	// The five colours; a floor above five colours is unreachable.
	if n < 1 || n > 5 {
		return nil
	}
	board := existingCards(reg, []string{
		"Savannah Lions", "Cloud Sprite", "Bog Rats", "Dragon Hatchling", "Grizzly Bears",
	})
	if len(board) < n {
		return nil
	}
	return []conditionPrelude{{battlefield: board[:n]}}
}
