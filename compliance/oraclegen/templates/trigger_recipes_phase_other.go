package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// phaseOtherCauses builds the candidate causes for the phase-trigger shapes
// levelb.PhaseOtherSub names. ok is false for every other sub-family so the
// default recipe chain falls through unchanged. Every cause is a pass_to
// checkpoint the XMage driver already serves; the fire probe decides which
// candidate actually puts the card's trigger on the stack.
func phaseOtherCauses(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger, sub string) ([]triggerCause, string, bool) {
	if sub != levelb.PhaseOtherSub {
		return nil, "", false
	}
	phase := t.ParamStr(cards.PKPhase)
	vp := t.ParamStr(cards.PKValidPlayer)
	switch {
	case strings.EqualFold(phase, "Main") && strings.EqualFold(t.ParamStr(cards.PKPhaseCount), "2"):
		return phaseOtherMain2Causes(reg, f, name, t), "", true
	case strings.EqualFold(phase, "End of Turn") && strings.EqualFold(vp, "You.descended"):
		return phaseOtherDescendCauses(reg, name, t), "", true
	case strings.EqualFold(phase, "Main1") && strings.EqualFold(vp, "Player"):
		return phaseOtherMain1PlayerCauses(reg, f, name, t), "", true
	case strings.EqualFold(phase, "EndCombat") && strings.EqualFold(vp, "You"):
		return phaseOtherEndCombatCauses(reg, f, name, t), "", true
	case strings.EqualFold(phase, "Upkeep") && strings.EqualFold(vp, "Player.EnchantedController"):
		return phaseOtherEnchantedUpkeepCauses(reg, f, name, t), "", true
	}
	return nil, "", false
}

// phaseOtherMain2Causes: "at the beginning of your second main phase". The
// base cause attacks with the source itself -- the attack taps it, satisfying
// the Survival PresentDefined$ Self / IsPresent$ Card.tapped gate -- and then
// passes to main2, where the beginning-of-main2 trigger is on the stack. A
// condition prelude (Scheming Silvertongue's life gain, Fireglass Mentor's
// opponent life loss) is prepended; a prelude that itself attacks is skipped,
// since the base already attacks and only one combat exists in the turn.
func phaseOtherMain2Causes(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger) []triggerCause {
	attack := oraclegen.Step{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:" + name}}
	base := triggerCause{steps: []oraclegen.Step{attack, {Op: "pass_to", Step: "main2"}}}
	return phaseOtherWithConditionPreludes(reg, f, t, base)
}

// phaseOtherDescendCauses: "at the beginning of your end step, if you
// descended this turn". The base waits at the end step on turn 1; the descend
// variant destroys a creature card into p0's graveyard first (Murder on
// Grizzly Bears), so the trigger matcher's descendedThisTurn gate holds.
func phaseOtherDescendCauses(reg *cards.Registry, name string, t *cards.Trigger) []triggerCause {
	base := triggerCause{steps: []oraclegen.Step{{Op: "pass_to", Step: "end"}}}
	out := []triggerCause{base}
	destroy, ok := castProbe(reg, "Murder", "p0:"+bearsProbe)
	if !ok {
		return out
	}
	prelude := conditionPrelude{
		hand:        []string{"Murder"},
		battlefield: []string{bearsProbe},
		steps:       []oraclegen.Step{destroy, {Op: "resolve"}},
	}
	return append(out, applyPrelude(base, prelude))
}

// phaseOtherMain1PlayerCauses: "at the beginning of each player's first main
// phase". The game starts in turn 1's main1, so the trigger would fire at once
// with the source in place; leave main1 with pass_to main2 first, then wait on
// p0's next main1 (turn 3) -- "each player" includes p0, and a pass_to to
// p1's main1 checkpoint has no PassToSteps entry.
func phaseOtherMain1PlayerCauses(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger) []triggerCause {
	base := triggerCause{steps: []oraclegen.Step{
		{Op: "pass_to", Step: "main2"},
		{Op: "pass_to", Step: "main1", Active: "p0"},
	}}
	return phaseOtherWithConditionPreludes(reg, f, t, base)
}

// phaseOtherEndCombatCauses: "at the end of combat on your turn", gated on
// combat damage this turn. Craw Wurm (6/4) attacks unblocked and deals six,
// satisfying MaxCombatDamageThisTurn GE6; passing to end-combat stops with the
// trigger on the stack. An attacking source would only add its own damage, so
// the Wurm is a separate attacker and the source stays on the battlefield.
func phaseOtherEndCombatCauses(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger) []triggerCause {
	base := triggerCause{
		battlefield: []string{"Craw Wurm"},
		steps: []oraclegen.Step{
			{Op: "attack", Seat: 0, Defender: "p1", Attackers: []string{"p0:Craw Wurm"}},
			{Op: "pass_to", Step: "end-combat"},
		},
	}
	return phaseOtherWithConditionPreludes(reg, f, t, base)
}

// phaseOtherEnchantedUpkeepCauses: "at the beginning of the upkeep of
// enchanted creature's controller". The Aura is cast onto p0's Grizzly Bears
// (an unattached Aura on the battlefield is a state-based action before the
// first step, so it cannot be placed unattached); an Equipment bearer is
// attached instead. Passing to p0's next upkeep (turn 3) stops with the
// trigger on the stack.
func phaseOtherEnchantedUpkeepCauses(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger) []triggerCause {
	base := triggerCause{steps: []oraclegen.Step{{Op: "pass_to", Step: "upkeep", Active: "p0"}}}
	var variant triggerCause
	if oraclegen.HasType(f, "Aura") {
		cast, ok := castProbe(reg, name, "p0:"+bearsProbe)
		if !ok {
			return []triggerCause{base}
		}
		variant = base
		variant.selfInHand = true
		variant.hand = []string{name}
		variant.battlefield = []string{bearsProbe}
		variant.prelude = []oraclegen.Step{cast, {Op: "resolve"}}
	} else {
		variant = base
		variant.prelude = []oraclegen.Step{{Op: "attach", Seat: 0, Card: "p0:" + name, AttachedTo: "p0:" + bearsProbe}}
	}
	// The attached-source variant is the one that satisfies the
	// Player.EnchantedController gate, so the condition preludes layer over it
	// (not the bare base, whose source is unattached and would be binned).
	out := phaseOtherWithConditionPreludes(reg, f, t, variant)
	return append([]triggerCause{base}, out...)
}

// phaseOtherWithConditionPreludes returns base followed by a variant of base
// with each non-attacking condition prelude prepended (the phase recipe's own
// pattern). A prelude that attacks is skipped only when the base already
// attacks: a second combat in the turn would not play through.
func phaseOtherWithConditionPreludes(reg *cards.Registry, f *cards.Face, t *cards.Trigger, base triggerCause) []triggerCause {
	out := []triggerCause{base}
	baseAttacks := false
	for _, st := range base.steps {
		baseAttacks = baseAttacks || st.Op == "attack"
	}
	for _, prelude := range conditionPreludes(reg, t.Params, f.SVars) {
		if baseAttacks && preludeAttacks(prelude) {
			continue
		}
		out = append(out, applyPrelude(base, prelude))
	}
	for _, prelude := range triggerConditionFixtures(reg, f, t) {
		if baseAttacks && preludeAttacks(prelude) {
			continue
		}
		out = append(out, applyPrelude(base, prelude))
	}
	return out
}
