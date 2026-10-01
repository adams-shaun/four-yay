package events

import (
	"sort"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// ResolvedAbilityKey identifies "this ability" for Forge's
// Count$ResolvedThisTurn -- the per-ability resolution tally that gates
// Sephiroth, Fabled SOLDIER's "if this is the fourth time this ability has
// resolved this turn, transform" (and Prowl's, Victor's, Nissa's, ...).
//
// The key is (source permanent ObjID, the resolving root Ability$ body's
// CONTENT), never a *cards.SA pointer: cards.Link resolves an Execute$ SVar
// body by PARSING THE TEXT FRESH, so one card's two T: lines that share an
// Execute$ (Victor, Valgavoth's Seneschal's ChangesZone and FullyUnlock lines
// both Execute$ TrigSurveil) carry two structurally-equal but pointer-distinct
// SAs -- and a recorded log re-parses the body a third time. Content therefore
// is the only identity that merges the abilities the oracle calls one, and it
// is stable across a live game and its replay because the script text is
// static. The walk is a fixed pre-order with sorted parameter names, so it
// never depends on map iteration order.
//
// source is part of the key because two permanents can carry the same body
// (two Sephiroths, or a Sephiroth and a copied one) and each has its own tally.
func ResolvedAbilityKey(source state.ObjID, sa *cards.SA) string {
	return strconv.FormatUint(uint64(source), 10) + "|" + saKeyContent(sa)
}

func writeSAKey(b *strings.Builder, sa *cards.SA, depth int) {
	if sa == nil || depth > 32 {
		return
	}
	b.WriteString(sa.Kind)
	b.WriteByte(':')
	b.WriteString(sa.API)
	keys := make([]string, 0, len(sa.Params))
	for k := range sa.Params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteByte(';')
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(sa.Params[k])
	}
	if sa.Sub != nil {
		b.WriteByte('>')
		writeSAKey(b, sa.Sub, depth+1)
	}
}
