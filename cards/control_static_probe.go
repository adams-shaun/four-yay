package cards

import "strings"

// MayCarryControlStatic reports whether a game object built from this card
// could ever carry a Mode$ Continuous static with a GainControl$ value
// (Mind Control's "You control enchanted creature"): one printed on a face,
// one an AddStaticAbility$ grant installs from an SVar body, or -- through a
// CopyFromChosenName$ clone, which copies a face out of the name universe
// rather than the match's pool -- any card at all. It is a card-data
// question the rules engine asks ONCE, at genesis, over the match's pool: a
// pool with no such card can never realize a static control transfer, so
// the engine skips the per-event reconcile scan (rules/control_static.go).
//
// A SUPERSET probe like ChangesTypes and SetsName: a false positive costs
// only the reconcile scan the engine ran before.
func (c *Card) MayCarryControlStatic() bool {
	if c == nil {
		return false
	}
	for _, f := range c.Faces {
		if f == nil {
			continue
		}
		for _, st := range f.Statics {
			if st.HasParam(PKGainControl) {
				return true
			}
		}
		for _, body := range f.SVars {
			if strings.Contains(body, "GainControl$") || strings.Contains(body, "CopyFromChosenName") {
				return true
			}
		}
		for _, a := range f.Abilities {
			if saCopiesChosenName(a) {
				return true
			}
		}
		for _, t := range f.Triggers {
			if saCopiesChosenName(t.Effect) {
				return true
			}
		}
		for _, r := range f.Repls {
			if saCopiesChosenName(r.With) {
				return true
			}
		}
	}
	return false
}

// saCopiesChosenName reports whether an ability (or any of its SubAbility
// chain) carries CopyFromChosenName$.
func saCopiesChosenName(a *SA) bool {
	for ; a != nil; a = a.Sub {
		if _, ok := a.Params["CopyFromChosenName"]; ok {
			return true
		}
	}
	return false
}
