package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// fillActivateXAbility fills every served activate step's empty XAbility slot
// with the XMage rule-text prefix that selects the step's ability (agent
// 20261009T041408Z: cluster C5). XAbility is parallel to Scenario.Steps, so
// the slice is grown first; a step that is not an activate, or one whose slot
// the serving template already filled, is left alone. The step's Card ref
// names the ability's source (a prelude step can activate a probe card, not
// the row's own card), so the prefix is derived from that card's front face;
// a card outside the corpus, or one whose XMage ability text is ambiguous,
// keeps its empty slot -- the same served shape a generator that never filled
// the slot produced, and one XMage's replay driver reports loudly rather than
// answering silently.
//
// It runs centrally in GenerateB's finalisation so every serving site that
// emits an activate step is covered by one helper instead of one fill per
// builder (measured holes: the Surveillance CanAttackDefender branch, the
// UntapOtherPlayer mana-ability tap, the gated CantBlockBy prelude's
// Chromatic Star sacrifice, the ability-stack probe prelude).
func fillActivateXAbility(reg *cards.Registry, it *oraclegen.Item) {
	if it == nil || len(it.Steps) == 0 {
		return
	}
	var grown bool
	if len(it.XAbility) < len(it.Steps) {
		it.XAbility = append(it.XAbility, make([]string, len(it.Steps)-len(it.XAbility))...)
		grown = true
	}
	// A row can activate several cards (its own card, a probe, a sacrifice
	// fixture); cache the per-face prefix maps the lookup produces.
	prefixesByCard := map[string]map[int]string{}
	filled := 0
	for i, st := range it.Steps {
		if st.Op != "activate" || it.XAbility[i] != "" || st.AbilityIndex == nil {
			continue
		}
		_, name, ok := strings.Cut(st.Card, ":")
		if !ok || name == "" {
			continue
		}
		if prefixes, seen := prefixesByCard[name]; seen {
			if it.XAbility[i] = prefixes[*st.AbilityIndex]; it.XAbility[i] != "" {
				filled++
			}
			continue
		}
		f := lookupFrontFace(reg, name)
		if f == nil {
			prefixesByCard[name] = nil
			continue
		}
		prefixes, why := oraclegen.XMageAbility(f)
		if why != "" {
			prefixesByCard[name] = nil
			continue
		}
		prefixesByCard[name] = prefixes
		if it.XAbility[i] = prefixes[*st.AbilityIndex]; it.XAbility[i] != "" {
			filled++
		}
	}
	// A row with no filled slot must not grow the item's bytes: an
	// all-empty xmage_ability array on every served row would stale
	// hundreds of verdicts that carry no activate step at all. Only a row
	// the fill actually changed carries the field.
	if filled == 0 && grown {
		it.XAbility = nil
	}
}

// lookupFrontFace returns the front face of the named corpus card, or nil
// when the name is not in it.
func lookupFrontFace(reg *cards.Registry, name string) *cards.Face {
	c, ok := reg.Lookup(name)
	if !ok || len(c.Faces) == 0 {
		return nil
	}
	return c.Faces[0]
}
