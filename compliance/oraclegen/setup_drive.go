package oraclegen

import (
	"fmt"

	"github.com/adams-shaun/gorge/rules"
)

// setupDriveAnswers maps a decision the runner answered while it drove from
// genesis to turn N's first main phase (Step < 0) to the XMage answers that
// answer the same dialogs, or nil when XMage poses no ask this decision
// matches. Gorge's fallback answers such an ask in place, but the generator's
// transcriber used to drop every Step<0 decision that was not an as-enters
// colour/type choice (IsSetupChoice), a declined optional target
// (IsSetupTargetDecline) or a setup-placed trigger order
// (triggerOrderSetupPosed). XMage then answered the dropped ask with its own
// AI, so the two engines diverged before the card's real behaviour was ever
// judged (Zuko, Conflicted's turn-1 charm; Gathering Stone's mill/reveal
// pair).
//
// This covers the two classes those predicates leave: a modal pick
// (GorgeKind "modes") and a bare two-way boolean (yesNo) -- the optional
// mill/reveal booleans Gathering Stone's triggers pose. Each answer is the
// fallback's own pick, mapped to XMage's dialog vocabulary the same way the
// step-time transcriber maps the identical decision (the numeric mode queue,
// the boolean choice, the labelled choice queue), so scripting it cannot
// change gorge's side: xmage_answers are invisible to the runner.
//
// The kinds deliberately left unscripted, all of which XMage answers without
// the setup drive's help or whose ask this generator cannot yet place: an
// "opening_*" pregame ask (a Leyline's "begin with this?" -- XMage's own
// start-of-game handling) and an as-enters "choose a number" (neither is a
// bare yesNo), and a "mode" whose label resolves to no charm position (a
// plain GenericChoice on XMage's CHOICE dialog, or an unless-pay XMage does
// not pose at all).
func setupDriveAnswers(d rules.OracleDecision, modes map[string]int) []XAnswer {
	if d.GorgeKind == "modes" {
		return setupModeAnswers(d, modes)
	}
	if v, ok := yesNo(d); ok {
		return []XAnswer{{d.Seat, "setup_choice", v}}
	}
	return nil
}

// setupModeAnswers is the setup-drive form of the step-time "mode" arm for a
// MODAL pick whose label resolves to a known charm position (or a
// GenericChoice queue sentinel): the same numeric/choice mapping, but the
// answers ride the pre-build setup queue (kind "setup_mode" for a numeric
// mode, "setup_choice" for a GenericChoice label or a yes/no boolean) so the
// driver queues them before XMage places the seeded permanents. An unmapped
// label returns nil: a plain GenericChoice's label reaches XMage's CHOICE
// dialog, not chooseMode, so scripting the step-time arm's positional
// fallback would answer the wrong queue. The unless-pay and play "mode"
// decisions are likewise left to XMage, which poses their chooseUse itself.
func setupModeAnswers(d rules.OracleDecision, modes map[string]int) []XAnswer {
	var out []XAnswer
	for k := range d.PickIdx {
		m, ok := modeNumberFor(d, k, modes)
		if !ok {
			return nil
		}
		switch m {
		case ModeChoiceQueue:
			// A DB$ GenericChoice | SetChosenMode$ True body: XMage reads the
			// pick through its CHOICE queue, showing the option LABEL.
			out = append(out, XAnswer{d.Seat, "setup_choice", d.Picks[k]})
		case ModeYesQueue, ModeNoQueue:
			// A per-player "may shuffle" GenericChoice: XMage asks chooseUse.
			out = append(out, XAnswer{d.Seat, "setup_choice", map[bool]string{true: "yes", false: "no"}[m == ModeYesQueue]})
		default:
			out = append(out, XAnswer{d.Seat, "setup_mode", fmt.Sprint(m)})
		}
	}
	if len(out) > 0 && d.Max > len(d.Picks) {
		// XMage keeps choosing modes up to Max; stop it with the mode queue's
		// skip token.
		out = append(out, XAnswer{d.Seat, "setup_mode", "[mode_skip]"})
	}
	return out
}
