// Solved-Case fixtures for continuous statics (Level B). A static whose
// IsPresent$ gate needs its own source Case solved (MKM's "Solved —" lines:
// Case of the Gateway Express, Case of the Gorgon's Kiss) is live only after
// the Case's end-step "To solve" trigger has fired and resolved (CR 719.3a),
// so the fixture runs the solve condition's own events in the turn the Case
// is cast, then waits for the end step.
//
// The activation-side machinery (activate_solved_case.go) cannot be reused:
// its candidates solve the Case BEFORE the card enters play, which is right
// for a setup-placed activation source and useless here -- the solve trigger
// is a battlefield trigger, so the Case must be on the battlefield when the
// end step begins. These fixtures instead put the solve condition's steps in
// the prelude (the count is per-turn, so pre-cast events count), let
// staticBase cast the Case, and append the end-step solve sequence after it
// (staticFixture.afterSteps).
package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// staticSolvedCaseFixtures offers one fixture per candidate condition for a
// static gated on its own source Case being solved. ok is false when the
// static has no such gate or the face carries no "To solve" trigger.
func staticSolvedCaseFixtures(reg *cards.Registry, f *cards.Face, st cards.Static) []staticFixture {
	needed := false
	for _, key := range []cards.ParamKey{cards.PKIsPresent, cards.PKIsPresent2} {
		if solvedSelfSpec(st.ParamStr(key)) {
			needed = true
		}
	}
	// A static whose own Affected$ filter is the solved gate (the
	// AffectedDefined$ Self form the compiler normalises to
	// "Card.Self+IsSolved": Case of the Gorgon's Kiss) needs its source
	// solved just as much.
	if !needed {
		affected := st.ParamStr(cards.PKAffected)
		if strings.Contains(strings.ToLower(affected), "card.self") && solvedSelfSpec(affected) {
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
	cands := []conditionPrelude{{}}
	for _, c := range append(conditionPreludes(reg, solve.Params, f.SVars), triggerConditionFixtures(reg, f, solve)...) {
		// A shared candidate that attacks carries its own pass_to main2, so
		// the attack count it serves would be consumed by an earlier combat;
		// the measured attack condition below builds its own prelude instead.
		if preludeAttacks(c) {
			continue
		}
		cands = append(cands, c)
	}
	// The one measured solve condition the shared candidates do not cover: a
	// count of this turn's attackers (Gateway Express's
	// Count$AttackersDeclared; the graveyard-entry count IS covered by the
	// shared kill-spell candidates). The count reads the turn's own events,
	// so the fixture's prelude makes it true before the Case is cast.
	bodies := staticSVarBodies(f, cards.Static{})
	if solveSVar := strings.TrimSpace(solve.ParamStr(cards.PKCheckSVar)); solveSVar != "" {
		if !strings.HasPrefix(solveSVar, "Count$") {
			if body, ok := f.SVars[solveSVar]; ok {
				bodies = append(bodies, body)
			}
		} else {
			bodies = append(bodies, solveSVar)
		}
	}
	joined := strings.ToLower(strings.Join(bodies, " "))
	if strings.Contains(joined, "attackersdeclared") {
		cands = append(cands, staticCaseAttackPrelude())
	}
	var out []staticFixture
	for _, c := range cands {
		fx := staticFixture{conditionPrelude: c}
		fx.afterSteps = []oraclegen.Step{
			{Op: "pass_to", Step: "end"},
			{Op: "resolve"},
			{Op: "pass_to", Step: "main1", Active: "p0"},
		}
		out = append(out, fx)
	}
	return out
}

// staticCaseAttackPrelude declares three attackers so a
// Count$AttackersDeclared solve condition reaches its floor: the compared
// probe plus two fixture copies, all setup-placed and so not summoning sick.
func staticCaseAttackPrelude() conditionPrelude {
	return conditionPrelude{
		battlefield: []string{staticProbe, staticProbe},
		steps: []oraclegen.Step{
			{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{
				"p0:" + staticProbe, "p0:" + staticProbe + "#2", "p0:" + staticProbe + "#3",
			}},
			{Op: "pass_to", Step: "main2"},
		},
	}
}
