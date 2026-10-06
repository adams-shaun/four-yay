package searchprobe

import "slices"

// The typed frame equality sameFrame uses. Each function answers exactly what
// reflect.DeepEqual answers for its type -- including DeepEqual's nil-versus-
// empty slice distinction, which slices.Equal alone does not draw -- without
// reflection: the replay compares every frame of every attempt, and the
// reflective walk was ~2% of sampler CPU. TestFrameEqualityMatchesDeepEqual
// holds them to reflect.DeepEqual and fails when a field is added to one of
// these types without a comparison here.

// sameSliceShape is DeepEqual's slice prelude: both nil or both non-nil, and
// the same length.
func sameSliceShape[T any](a, b []T) bool {
	return (a == nil) == (b == nil) && len(a) == len(b)
}

// equalComparableSlices is DeepEqual over a slice of a comparable,
// pointer-free element type.
func equalComparableSlices[T comparable](a, b []T) bool {
	return sameSliceShape(a, b) && slices.Equal(a, b)
}

func identitiesEqual(a, b []Identity) bool { return equalComparableSlices(a, b) }

func observedEventEqual(a, b *ObservedEvent) bool {
	return a.Kind == b.Kind && a.Player == b.Player && a.Obj == b.Obj &&
		a.From == b.From && a.To == b.To && a.Amount == b.Amount && a.Step == b.Step &&
		a.Counter == b.Counter && a.Text == b.Text && a.Secret == b.Secret &&
		equalComparableSlices(a.IDs, b.IDs) && equalComparableSlices(a.Pairs, b.Pairs)
}

func observedEventsEqual(a, b []ObservedEvent) bool {
	if !sameSliceShape(a, b) {
		return false
	}
	for i := range a {
		if !observedEventEqual(&a[i], &b[i]) {
			return false
		}
	}
	return true
}

func observedDecisionEqual(a, b *ObservedDecision) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Player == b.Player && a.Kind == b.Kind && a.Min == b.Min && a.Max == b.Max &&
		a.Source == b.Source && a.EffectAPI == b.EffectAPI && a.DamageKnown == b.DamageKnown &&
		a.Damage == b.Damage && equalComparableSlices(a.Options, b.Options)
}
