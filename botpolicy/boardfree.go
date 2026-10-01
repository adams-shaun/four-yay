package botpolicy

import "github.com/adams-shaun/gorge/decision"

// DecideBoardFree is Decide for the decisions whose answer provably reads no
// Board fact, so a caller can skip the board build for them: ok is false for
// every other decision, which the caller answers with Decide as usual.
//
// They are the priority decisions where Decide falls through to its explicit
// pass (policy.go's decide):
//
//   - every option is "pass" or "concede": the tap gate (chooseTap) answers
//     only with an offered "activate", the land drop (chooseLand) only with
//     a "play_land", the cast (chooseCast) only with a "cast" and the
//     ability ranking (chooseAbility) only with an "ability", so with none
//     offered each returns -1 whatever the Board holds;
//   - outside a main phase (isMain false: Board.IsMain, g.Step.IsMain()),
//     "activate" options as well: the tap gate and the ability ranking run
//     only in a main phase, and nothing else answers with an "activate".
//
// The answer is the first offered "pass", clamped -- exactly Decide's -- and
// like Decide on these decisions it consumes no rng. (ExploreDecide is not
// covered: its explore branches read the rng before the pass.) The
// whole-game equivalence test (board_inc_test.go) holds it to Decide on a
// filled Board at every decision it answers.
func DecideBoardFree(d *decision.Decision, isMain bool) (decision.Intent, bool) {
	if d.Kind != decision.KPriority {
		return decision.Intent{}, false
	}
	pass := -1
	for i := range d.Options {
		switch o := &d.Options[i]; o.Kind {
		case "pass":
			if pass < 0 {
				pass = o.Index
			}
		case "concede":
		case "activate":
			if isMain {
				return decision.Intent{}, false
			}
		default:
			return decision.Intent{}, false
		}
	}
	if pass < 0 {
		return decision.Intent{}, false
	}
	return Clamp(d, decision.Intent{Seq: d.Seq, Player: d.Player, Choices: passChoice(pass)}), true
}

// passChoices backs passChoice: element v is v.
var passChoices = func() (a [256]int) {
	for i := range a {
		a[i] = i
	}
	return a
}()

// passChoice is []int{v} as a capped read-only window of passChoices (a
// fresh slice past its range): the free answer is posed per priority window
// of every simulation, and nothing writes an intent's Choices in place (an
// append reallocates, the window being capped).
func passChoice(v int) []int {
	if v >= 0 && v < len(passChoices) {
		return passChoices[v : v+1 : v+1]
	}
	return []int{v}
}
