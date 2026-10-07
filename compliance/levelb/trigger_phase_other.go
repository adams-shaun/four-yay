package levelb

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// PhaseOtherSub is the level-B sub-family of the phase-trigger shapes that
// the original TriggerPhase switch in classifyTrigger does not serve and that
// a pass_to checkpoint plus a short prelude can cause:
//
//   - "your second main phase, if this is tapped" (DSK Survival:
//     Phase$ Main | PhaseCount$ 2 | ValidPlayer$ You), caused by attacking
//     with the source (which taps it) and passing to main2;
//   - "your end step, if you descended" (Phase$ End of Turn |
//     ValidPlayer$ You.descended), caused by destroying a creature card into
//     p0's graveyard and passing to the end step;
//   - "each player's first main phase" (Phase$ Main1 | ValidPlayer$ Player),
//     caused by leaving main1 and waiting for p0's next main1;
//   - "your end of combat" (Phase$ EndCombat | ValidPlayer$ You), caused by
//     attacking and passing to end-combat;
//   - "the upkeep of enchanted creature's controller" (Phase$ Upkeep |
//     ValidPlayer$ Player.EnchantedController), caused by attaching the Aura
//     to p0's creature and passing to p0's next upkeep.
//
// The recipe in compliance/oraclegen/templates is keyed off the same params;
// the fire probe (abilityOnStack) still decides whether any candidate fires.
// Static$ True bookkeeping triggers never reach the stack and stay gaps.
const PhaseOtherSub = "trigger.phase-other"

// classifyPhaseOther reports the level-B sub-family of a Phase$ trigger whose
// phase/player pair the original TriggerPhase case does not serve but whose
// cause the phase-other recipe can build. ok is false for Static$ True
// triggers, for every non-Phase mode, and for every pair outside the five
// shapes above (which stay mode gaps).
func classifyPhaseOther(t *cards.Trigger) (sub string, ok bool) {
	if strings.EqualFold(t.ParamStr(cards.PKStatic), "True") {
		return "", false
	}
	if t.ModeKind() != cards.TriggerPhase {
		return "", false
	}
	phase := t.ParamStr(cards.PKPhase)
	vp := t.ParamStr(cards.PKValidPlayer)
	switch {
	case strings.EqualFold(phase, "Main") && strings.EqualFold(t.ParamStr(cards.PKPhaseCount), "2") && strings.EqualFold(vp, "You"):
		return PhaseOtherSub, true
	case strings.EqualFold(phase, "End of Turn") && strings.EqualFold(vp, "You.descended"):
		return PhaseOtherSub, true
	case strings.EqualFold(phase, "Main1") && strings.EqualFold(vp, "Player"):
		return PhaseOtherSub, true
	case strings.EqualFold(phase, "EndCombat") && strings.EqualFold(vp, "You"):
		return PhaseOtherSub, true
	case strings.EqualFold(phase, "Upkeep") && strings.EqualFold(vp, "Player.EnchantedController"):
		return PhaseOtherSub, true
	}
	return "", false
}
