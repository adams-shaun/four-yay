package botpolicy

import (
	stdmaps "maps"
	"slices"
)

// The typed value equalities boardsDiffer's verify path compares Board
// tables with, replacing reflect.DeepEqual at exactly its strictness: a nil
// and an empty non-nil slice or map DIFFER. TestBoardEqualMatchesDeepEqual
// holds them to DeepEqual and to the types' field counts.

// comparableEqual is DeepEqual for a pointer-free comparable value (Card,
// a life total): a field that stops being comparable fails to compile here.
func comparableEqual[V comparable](a, b *V) bool { return *a == *b }

func creatureEqual(a, b *Creature) bool {
	return a.Power == b.Power && a.Toughness == b.Toughness && a.Damage == b.Damage &&
		a.Tapped == b.Tapped && a.Controller == b.Controller &&
		(a.Keywords == nil) == (b.Keywords == nil) && slices.Equal(a.Keywords, b.Keywords)
}

func commanderEqual(a, b *Commander) bool {
	return a.Casts == b.Casts && a.InCommandZone == b.InCommandZone &&
		(a.Damage == nil) == (b.Damage == nil) && stdmaps.Equal(a.Damage, b.Damage)
}
