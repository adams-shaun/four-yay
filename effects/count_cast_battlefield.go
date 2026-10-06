package effects

import (
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// castBattlefieldHead is the Count$ head spelling whose carriers read the
// battlefield as it stood when the spell was cast.
const castBattlefieldHead = "LastStateBattlefieldWithFallback"

// SVarsReadCastBattlefield reports whether any SVar body counts the as-cast
// battlefield (Count$LastStateBattlefieldWithFallback), i.e. whether casting
// the spell owning these SVars must freeze the battlefield. The map is only
// scanned for membership, so its iteration order reaches nothing.
func SVarsReadCastBattlefield(svars map[string]string) bool {
	for _, body := range svars {
		if strings.Contains(body, castBattlefieldHead) {
			return true
		}
	}
	return false
}

// frozenHost answers the derived-P/T reads of a frozen battlefield from the
// values captured at cast (readers that go through Host.Power/Toughness, e.g.
// zoneCountFold.extremePropertyValue and P/T comparisons in the filter
// grammar), while every other Host read is promoted from the live host. The
// frozen game itself is scanned for membership, controller and printed
// characteristics, so the eight carriers' clauses (printed type/colour +
// controller + modified + $GreatestCardPower) all read frozen data. A
// layer-derived characteristic beyond P/T (a granted type/colour/keyword) still
// reads the live host and is out of this fix's scope.
type frozenHost struct {
	Host
	snap *state.CastBattlefield
}

func (f frozenHost) Power(id state.ObjID) int32 {
	if p, _, ok := f.snap.DerivedPT(id); ok {
		return p
	}
	return f.Host.Power(id)
}

func (f frozenHost) Toughness(id state.ObjID) int32 {
	if _, t, ok := f.snap.DerivedPT(id); ok {
		return t
	}
	return f.Host.Toughness(id)
}

// evalCastBattlefieldCount is Count$LastStateBattlefieldWithFallback: the Valid
// scan of arg over the battlefield as the source spell was cast, and over the
// current battlefield only when the source carries no frozen battlefield (a
// bare context, an uncast source). A frozen EMPTY battlefield is an
// authoritative zero, never "missing". The scan is evalCountBodyZone itself
// run against the frozen game, so the filter grammar, $Property folds and
// extreme reductions cannot drift from Count$Valid. An empty filter is not a
// body this build can count, so it fails the head's own verdict.
func evalCastBattlefieldCount(h Host, c *Ctx, g *state.Game, arg string, depth int) (int32, bool, bool) {
	if arg == "" {
		return 0, false, true
	}
	if c != nil {
		if src := g.Obj(c.Source); src != nil && src.Zone == state.ZStack && src.CastBattlefield != nil {
			snap := src.CastBattlefield
			return evalCountBodyZone(frozenHost{Host: h, snap: snap}, c, snap.Game, "Valid", arg, depth)
		}
	}
	return evalCountBodyZone(h, c, g, "Valid", arg, depth)
}
