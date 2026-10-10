package oraclegen

import "github.com/adams-shaun/gorge/rules"

// IsSetupTargetDecline reports whether a decision is an OPTIONAL target ask
// posed while the runner drove from genesis to the setup checkpoint (Step < 0)
// that gorge's runner declined: a front-face Saga placed by addCard resolves
// its chapter I, and the next chapter's phase-begin trigger is put on the
// stack, before the checkpoint (Summon: Bahamut's "Destroy up to one target
// nonland permanent"). Gorge answers such an ask with the empty pick; XMage's
// base TestPlayer, with no scripted target, auto-picks a legal one (the
// opposing Grizzly Bears), so the two engines' setup boards diverge on an
// answer neither scenario scripted. Scripting the decline on both sides makes
// the scenario say what happens.
func IsSetupTargetDecline(d rules.OracleDecision) bool {
	return d.Step < 0 && d.Kind == "target" && d.Min == 0 && d.Options > 0 && len(d.Picks) == 0
}

// SetupTargetDeclines is the gorge-side half: one explicit empty target answer
// per setup-posed declined optional target ask, in decision order, for the
// scenario's setup_answers. The runner consumes them instead of falling back
// (the same empty pick, now scripted, so the transcript shows an answer rather
// than a "[setup fallback]"). Nil when setup posed no such ask.
func SetupTargetDeclines(ds []rules.OracleDecision) []Answer {
	var out []Answer
	for _, d := range ds {
		if IsSetupTargetDecline(d) {
			out = append(out, Answer{Kind: "target", Pick: []string{}})
		}
	}
	return out
}
