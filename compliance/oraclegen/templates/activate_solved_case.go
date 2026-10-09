// Solved-Case activation preludes. An MKM Case's "Solved —" activated ability
// is offered only once the Case is solved (Activation$ Solved), and Case of
// the Burning Masks gates the same state through IsPresent$ Card.Self+IsSolved.
// A Case is solved by its own end-step "To solve" trigger (CR 719.3a), which
// fires only when the Case is unsolved and its solve condition holds; no setup
// the activate template already builds reaches that state, so the row was a
// named restriction gap.
//
// This file builds the missing setup: satisfy the solve condition, wait for
// the end step so the "To solve" trigger fires, resolve it, then return to a
// main phase so a sorcery-speed "Solved —" ability can be activated. The
// condition candidates are the shared trigger-condition helpers applied to the
// solve trigger's own params, plus the bare candidate for a solve condition
// that already holds at setup (Case of the Stashed Skeleton's "no suspected
// Skeletons": setup places the Case without firing its ETB, so no token
// exists).
package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// caseSolveTrigger is the source's "To solve" trigger (CR 719.3a): an
// end-step trigger gated on the Case being unsolved (IsPresent$ Card.Self+!IsSolved)
// whose body grants the Solved attribute. nil when the card carries none.
func caseSolveTrigger(f *cards.Face) *cards.Trigger {
	for i := range f.Triggers {
		t := &f.Triggers[i]
		if !strings.EqualFold(strings.TrimSpace(t.ParamStr(cards.PKPhase)), "End of Turn") {
			continue
		}
		present := strings.ToLower(t.ParamStr(cards.PKIsPresent) + " " + t.ParamStr(cards.PKIsPresent2))
		if !strings.Contains(present, "!issolved") {
			continue
		}
		body := strings.ToLower(f.SVars[t.ParamStr(cards.PKExecute)])
		if strings.Contains(body, "alterattribute") && strings.Contains(body, "solved") {
			return t
		}
	}
	return nil
}

// solvedCaseSteps appends the steps that solve the source Case: the solve
// condition's own steps (cond), the wait for the beginning of the end step so
// the "To solve" trigger fires, its resolve, and the return to p0's first main
// phase (turn 3) so a sorcery-speed ability can be activated with the Case
// already solved.
func solvedCaseSteps(cond []oraclegen.Step) []oraclegen.Step {
	out := append([]oraclegen.Step(nil), cond...)
	return append(out,
		oraclegen.Step{Op: "pass_to", Step: "end"},
		oraclegen.Step{Op: "resolve"},
		oraclegen.Step{Op: "pass_to", Step: "main1", Active: "p0"},
	)
}

// solvedCasePreludes is the activation setup for an ability offered only while
// its source Case is solved (Activation$ Solved, or IsPresent$ Card.Self+IsSolved).
// Each candidate satisfies the Case's own solve condition and then runs the
// "To solve" trigger at the end step; the leading bare candidate covers a
// solve condition that already holds. ok is false when the card carries no
// "To solve" trigger, so the caller names the gap as before.
func solvedCasePreludes(reg *cards.Registry, f *cards.Face, name string) ([]conditionPrelude, bool) {
	st := caseSolveTrigger(f)
	if st == nil {
		return nil, false
	}
	conds := []conditionPrelude{{}}
	for _, c := range append(conditionPreludes(reg, st.Params, f.SVars), triggerConditionFixtures(reg, f, st)...) {
		// A prelude that attacks would take the turn through combat before the
		// solve trigger's end step; no Case solve condition asks for it.
		if preludeAttacks(c) {
			continue
		}
		conds = append(conds, c)
	}
	out := make([]conditionPrelude, 0, len(conds))
	for _, c := range conds {
		c.steps = solvedCaseSteps(c.steps)
		out = append(out, c)
	}
	return out, true
}

// solvedSelfSpec reports an IsPresent$ filter that needs the source Case
// solved: "Card.Self+IsSolved". "Card.Self+!IsSolved" is the unsolved Case
// that solves itself and is not this gate.
func solvedSelfSpec(spec string) bool {
	return strings.Contains(strings.ReplaceAll(strings.ToLower(spec), "!issolved", ""), "issolved")
}
