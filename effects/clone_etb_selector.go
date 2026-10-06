package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// Only this ETB selector has a supported numeric RHS: Y is the total mana
// captured at payment, not the entering permanent's printed mana value.
const mockingbirdETBSelector = "Creature.Other+cmcLEY"
const mockingbirdSpendSVar = "Count$CastTotalManaSpent"
const mockingbirdSpendName = "Y"

// CloneETBSpendSelector reports whether the body's SVar table can supply the
// one resolver-dependent ETB Clone selector this build implements.
func CloneETBSpendSelector(spec string, svars map[string]string) bool {
	return spec == mockingbirdETBSelector && strings.TrimSpace(svars["Y"]) == mockingbirdSpendSVar
}

// CloneETBSelectorMatches is the common announcement/replacement matcher.
// Other selectors keep their original resolver-free context; Mockingbird's
// Y is available only while the entering object retains its cast provenance.
// In particular a non-cast entry cannot mistake an absent spend for zero.
func CloneETBSelectorMatches(g *state.Game, spec string, id state.ObjID, sc SpecContext) bool {
	if spec == "" {
		spec = "Creature.Other"
	}
	if !strings.Contains(spec, ".") && !strings.HasPrefix(spec, "Card") {
		spec = "Card." + spec
	}
	if spec == mockingbirdETBSelector {
		source := g.Obj(sc.Source)
		if source == nil || source.Face() == nil || !CloneETBSpendSelector(spec, source.Face().SVars) ||
			source.CastFlags&state.FlagManaSpent == 0 {
			return false
		}
		spend := source.ManaSpent
		sc.Resolve = func(name string) (int32, bool) {
			return spend, name == mockingbirdSpendName
		}
	}
	return MatchesSpecCtx(g, spec, id, sc)
}
