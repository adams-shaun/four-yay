package levelb

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// becomesTargetAbilityServed reports whether a Mode$ BecomesTarget trigger
// whose ValidSource$ names an Ability is served by the targeted-ability cause
// (sub "trigger.becomes-target-ability"): every comma alternative carries the
// Ability base with controller-only YouCtrl qualifiers -- the cause is p0's
// own Prodigal Sorcerer ping -- and ValidTarget$ is empty or carries a bare
// "Player" alternative the cause's p1 target satisfies (Loki, God of
// Mischief's "Player,Permanent"). The engine's becomesTargetSourceMatches
// answers the Ability base and its YouCtrl/OppCtrl qualifier; the classifier
// narrows to the shapes the cause can actually fire: an OppCtrl ability would
// need an opponent-side activation (no cause), and the other qualifier
// families -- Backup's keyword provenance, a host-card filter
// (Ability.Land+namedX), a value comparison (Ability.numTargets EQ1) -- are
// engine-side fail-closed and stay gaps.
func becomesTargetAbilityServed(t *cards.Trigger) bool {
	vs := strings.TrimSpace(t.ParamStr(cards.PKValidSource))
	if vs == "" {
		return false
	}
	for alt := range strings.SplitSeq(vs, ",") {
		alt = strings.TrimSpace(alt)
		rest, isAbility := strings.CutPrefix(alt, "Ability")
		if !isAbility {
			return false
		}
		if rest == "" {
			continue
		}
		if !strings.HasPrefix(rest, ".") {
			return false
		}
		for q := range strings.SplitSeq(rest[1:], "+") {
			if q = strings.TrimSpace(q); q != "" && !strings.EqualFold(q, "YouCtrl") {
				return false
			}
		}
	}
	vt := strings.TrimSpace(t.ParamStr(cards.PKValidTarget))
	if vt == "" {
		return true
	}
	for alt := range strings.SplitSeq(vt, ",") {
		if strings.EqualFold(strings.TrimSpace(alt), "Player") {
			return true
		}
	}
	return false
}
