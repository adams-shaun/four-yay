package searchprobe

import "slices"

// Typed equalities for the observation types the redeal check and
// internal/searchbench compare, replacing reflect.DeepEqual. Each answers
// exactly what DeepEqual answers for its type: a nil and an empty non-nil
// slice DIFFER (DeepEqual's rule, which slices.Equal alone does not draw),
// two nil decisions are equal and a nil one equals no non-nil one. Every
// element type here is comparable and pointer-free, so == on an element is
// DeepEqual on it. TestEqualMatchesDeepEqual holds them to DeepEqual and
// fails when one of these types grows a field.

// IdentitiesEqual is reflect.DeepEqual over two identity lists.
func IdentitiesEqual(a, b []Identity) bool {
	return (a == nil) == (b == nil) && slices.Equal(a, b)
}

// ObservedDecisionEqual is reflect.DeepEqual over two observed decisions.
func ObservedDecisionEqual(a, b *ObservedDecision) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Player == b.Player && a.Kind == b.Kind && a.Min == b.Min && a.Max == b.Max &&
		a.Source == b.Source && a.EffectAPI == b.EffectAPI && a.DamageKnown == b.DamageKnown &&
		a.Damage == b.Damage && (a.Options == nil) == (b.Options == nil) && slices.Equal(a.Options, b.Options)
}
