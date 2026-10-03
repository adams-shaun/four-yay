package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// The modal family's MODE-BODY reads. A Charm's Choices$ names ability bodies
// of their own APIs; their parameters are not the Charm's (compileCharm in
// charm_params.go reads only those), so the few facts the modal machinery
// needs about a mode body -- whether it targets, its spec, TargetUnique$,
// its bounds, its display label and its UnlessCost$ -- are read here, one
// reader each, by the charm.go and rules paths that walk the chosen modes.

// ModeTargetSpec is a mode body's ValidTgts$ spec, trimmed: "" when the mode
// declares no targets.
func ModeTargetSpec(sub *cards.SA) string {
	return strings.TrimSpace(sub.ParamStr(cards.PKValidTgts))
}

// modeTargetUnique reports a mode body's TargetUnique$ True.
func modeTargetUnique(sub *cards.SA) bool {
	return strings.EqualFold(sub.ParamStr(cards.PKTargetUnique), "True")
}

// modeUnlessCost is a mode body's UnlessCost$, trimmed.
func modeUnlessCost(sub *cards.SA) string {
	return strings.TrimSpace(sub.ParamStr(cards.PKUnlessCost))
}

// charmUniqueBounds mirrors rules' targetBounds for the single-target check:
// absent TargetMin$/TargetMax$ mean 1..1 (the M1 single-target contract).
// Anything a caller set explicitly beyond 1..1 keeps the mode out of the
// combined ask.
func charmUniqueBounds(sa *cards.SA) (min, max int) {
	min, max = 1, 1
	if v, ok := sa.Param(cards.PKTargetMin); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			min = n
		}
	}
	if v, ok := sa.Param(cards.PKTargetMax); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			max = n
		}
	}
	if min < 1 {
		min = 1
	}
	if max < min {
		max = min
	}
	return min, max
}

// CharmModeLabel is the printed display label of one Charm mode whose
// resolved body is sub: the mode body's own SpellDescription$ when it
// carries one, else the first SpellDescription$ found walking the body's
// SubAbility$ chain, else fallback -- the raw SVar name. In the corpus the
// chain-only shape is exactly the printed bullet text of the card's Oracle
// line: What Must Be Done's Release Juno mode carries its description one
// hop down (on DBChangeZone), and Varchild's War-Riders' two upkeep modes
// carry theirs on SurvivorDistribution and Sacrifice, so a mode whose
// SpellDescription$ rides a sub is still labelled by the card's printed
// words, not its SVar name. Body-first precedence keeps every mode that
// already labelled by its own SpellDescription$ byte-identical. The chain
// is linked by cards' resolver (ResolveSVar/link, depth-capped at
// maxSVarDepth), so the walk is a plain pointer walk: no re-resolution and
// no new cycle risk. A nil sub returns the fallback.
func CharmModeLabel(sub *cards.SA, fallback string) string {
	if sub == nil {
		return fallback
	}
	if d := strings.TrimSpace(sub.ParamStr(cards.PKSpellDescription)); d != "" {
		return d
	}
	for s := sub.Sub; s != nil; s = s.Sub {
		if d := strings.TrimSpace(s.ParamStr(cards.PKSpellDescription)); d != "" {
			return d
		}
	}
	return fallback
}
