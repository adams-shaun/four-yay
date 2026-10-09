package levelb

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// The gated combat-legality statics: a face static that forbids its own host
// an attack, a block or an unblocked attack EXCEPT under a condition, or a
// MinMaxBlocker Max$ cap, or an unconditional MustAttack on the host. Each
// recognized shape names the v1 sub-family that observes it through the gate
// being set (restriction active) and unset (control, the action offered) --
// templates/static_legality_gated.go builds those paired runs. A shape whose
// parameters are not all modelled here stays a visible gap, per the census
// convention: an unmodelled condition must never be served as unconditional.
//
// This file classifies; it resolves no gate value. A recognized requirement
// whose gate the template cannot set up is a named template skip, not this
// gap.

// gatedGateKeys are the conditional parameter keys a gated static may carry
// and the template's gate resolver reads: the CheckSVar$/SVarCompare$
// intervening-if family, the Condition$ ability-word family and the
// IsPresent$/IsPresent2$/PresentZone$/PresentCompare$ count family -- the
// same keys the engine's shared static gate (rules/layers.go
// continuousGateHolds) evaluates, so a recognized line is one the engine
// enforces.
var gatedGateKeys = []cards.ParamKey{
	cards.PKCheckSVar, cards.PKSVarCompare, cards.PKCondition,
	cards.PKIsPresent, cards.PKIsPresent2, cards.PKPresentZone, cards.PKPresentCompare,
}

// GatedGateKeys is gatedGateKeys for the template side's own param census.
func GatedGateKeys() []cards.ParamKey {
	return gatedGateKeys
}

// selfFilterTokensOK reports whether every "+" qualifier after a Self-scoped
// base ("Card.Self", "Creature.Self") is one the gate resolver can read: the
// blocker-decision true "attacking", the counter gate "HasCounters", and the
// printed-power compare "power<op><int>" (op LT LE GT GE). Any other
// qualifier is unmodelled.
func selfFilterTokensOK(filter string) bool {
	parts := strings.Split(filter, "+")
	base := strings.TrimSpace(parts[0])
	if !strings.EqualFold(base, "Card.Self") && !strings.EqualFold(base, "Creature.Self") {
		return false
	}
	for _, tok := range parts[1:] {
		tok = strings.TrimSpace(tok)
		switch strings.ToLower(tok) {
		case "attacking", "hascounters":
			continue
		}
		if _, _, ok := PowerFilterBound(tok); !ok {
			return false
		}
	}
	return true
}

// PowerFilterBound parses a "power<op><int>" qualifier into (op, n).
func PowerFilterBound(tok string) (string, int, bool) {
	low := strings.ToLower(strings.TrimSpace(tok))
	rest, ok := strings.CutPrefix(low, "power")
	if !ok {
		return "", 0, false
	}
	for _, op := range []string{"lt", "le", "gt", "ge"} {
		if v, ok2 := strings.CutPrefix(rest, op); ok2 {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil {
				return "", 0, false
			}
			return op, n, true
		}
	}
	return "", 0, false
}

// gateParamsOnly reports whether st carries exactly the scope key, the bound
// keys, the gate keys, the display parameters and (when allowed) Secondary --
// nothing else.
func gateParamsOnly(st *cards.Static, scope cards.ParamKey, allowSecondary bool, extra ...string) bool {
	for k := range st.Params {
		if k == scope.String() || isDisplayKey(k, allowSecondary) || gatedGateKey(k) {
			continue
		}
		allowed := false
		for _, e := range extra {
			allowed = allowed || k == e
		}
		if allowed {
			continue
		}
		return false
	}
	return true
}

func isDisplayKey(k string, allowSecondary bool) bool {
	switch k {
	case "Mode", "Description":
		return true
	case "Secondary":
		return allowSecondary
	}
	return false
}

func gatedGateKey(k string) bool {
	for _, g := range gatedGateKeys {
		if g.String() == k {
			return true
		}
	}
	return false
}

// displayOnlyParams is gateParamsOnly with no gate keys allowed.
func displayOnlyParams(st *cards.Static, scope cards.ParamKey) bool {
	for k := range st.Params {
		if k == scope.String() || isDisplayKey(k, true) {
			continue
		}
		return false
	}
	return true
}

// MaxBlockerCap is the Max$ of a MinMaxBlocker static when it is a plain
// literal 1..6 (a cap the bound-publishing observation can field: the
// scenario fields exactly two probe blockers, so a larger cap would leave
// the pair under-declared rather than over), else 0.
func MaxBlockerCap(st *cards.Static) int {
	n, err := strconv.Atoi(strings.TrimSpace(st.ParamStr(cards.PKMax)))
	if err != nil || n < 1 || n > 6 {
		return 0
	}
	return n
}

// gatedLegalityStatic names the sub-family serving st's gated combat-legality
// shape, or ok=false.
func gatedLegalityStatic(f *cards.Face, st *cards.Static) (string, bool) {
	if !f.IsCreature() {
		return "", false
	}
	switch strings.ToLower(st.Mode) {
	case "cantattack", "cantblock":
		if !selfFilterTokensOK(st.ParamStr(cards.PKValidCard)) || !gateParamsOnly(st, cards.PKValidCard, true) {
			return "", false
		}
		if strings.EqualFold(st.Mode, "CantAttack") {
			return "static.cant-attack-gated", true
		}
		return "static.cant-block-gated", true
	case "cantblockby":
		if !selfFilterTokensOK(st.ParamStr(cards.PKValidAttacker)) || st.HasParam(cards.PKValidBlocker) ||
			!gateParamsOnly(st, cards.PKValidAttacker, true) {
			return "", false
		}
		return "static.cant-block-by-gated", true
	case "canattackdefender":
		if !selfFilterTokensOK(st.ParamStr(cards.PKValidCard)) || !gateParamsOnly(st, cards.PKValidCard, true) {
			return "", false
		}
		return "static.can-attack-defender-gated", true
	case "mustattack":
		if !strings.EqualFold(st.ParamStr(cards.PKValidCreature), "Card.Self") || !displayOnlyParams(st, cards.PKValidCreature) {
			return "", false
		}
		return "static.must-attack-self", true
	case "minmaxblocker":
		if !strings.EqualFold(st.ParamStr(cards.PKValidCard), "Card.Self") || MaxBlockerCap(st) == 0 ||
			!gateParamsOnly(st, cards.PKValidCard, true, "Max") {
			return "", false
		}
		return "static.max-blockers", true
	}
	return "", false
}
