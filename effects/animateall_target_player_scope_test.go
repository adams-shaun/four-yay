package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// AnimateAll's object sweep filters on ValidCards$ only.  When the printed
// scope ("each creature target opponent controls") is carried by a player-kind
// ValidTgts$, the sweep must read it or the resolving controller's own
// creatures are animated too.  Curious Colossus is the carrier:
// `AnimateAll | ValidTgts$ Opponent | ValidCards$ Creature | Power$ 1 |
// Toughness$ 1 | Types$ Coward` (std3 ECL: gorge turned its OWN Colossus into
// a 1/1 Coward, xmage left it 7/7 and shrank the opponent's creature).  These
// leaves pin the scope, mirroring effects/damageall_target_player_scope_test.go.
//
// board(t) gives the two-controller board this needs: myBear/myFlier on seat
// 0, theirBig on seat 1 (effects/filter_test.go:10).

// TestAnimateAllOpponentScopeRestrictsSweep is the Curious Colossus shape:
// ValidTgts$ Opponent | ValidCards$ Creature.  The target opponent's creature
// must be animated; seat 0's creatures must take no grant at all.
func TestAnimateAllOpponentScopeRestrictsSweep(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	// Preconditions: the objects under assertion are on the battlefield the
	// sweep reads, and the two seats' creatures are different objects with
	// different controllers -- otherwise the assertion below is vacuous.
	for _, name := range []string{"myBear", "myFlier", "theirBig"} {
		if o := g.Obj(ids[name]); o == nil || o.Zone != state.ZBattlefield {
			t.Fatalf("precondition: %s is not on the battlefield", name)
		}
	}
	if g.Obj(ids["myBear"]).Controller == g.Obj(ids["theirBig"]).Controller {
		t.Fatal("precondition: myBear and theirBig share a controller")
	}
	Resolve(h, playerCtx(1),
		sa(t, "DB$ AnimateAll | ValidTgts$ Opponent | ValidCards$ Creature | Power$ 1 | Toughness$ 1 | Types$ Coward"))
	var ptOnTarget, typeOnTarget int
	onTarget := map[state.ObjID]bool{ids["theirBig"]: true}
	for _, ce := range h.continuous {
		if !onTarget[ce.Source] && (ce.Layer == state.LPT || (ce.Layer == state.LType && containsAny(ce.AddTypes, "Coward"))) {
			t.Fatalf("grant landed on a non-target creature (source %d): %+v", ce.Source, ce)
		}
		switch {
		case ce.Source == ids["theirBig"] && ce.Layer == state.LPT:
			ptOnTarget++
			if ce.Sub != state.SubSet || ce.SetPower != 1 || ce.SetToughness != 1 {
				t.Fatalf("target's P/T effect = %+v", ce)
			}
		case ce.Source == ids["theirBig"] && ce.Layer == state.LType:
			typeOnTarget++
		}
	}
	if ptOnTarget != 1 || typeOnTarget != 1 {
		t.Fatalf("target opponent's creature grants: P/T=%d type=%d, want 1/1", ptOnTarget, typeOnTarget)
	}
}

// TestAnimateAllNonPlayerTargetsSweepsAllControllers is the regression leaf:
// a non-player ValidTgts$ must NOT scope the sweep.  Every matching creature
// is animated.
func TestAnimateAllNonPlayerTargetsSweepsAllControllers(t *testing.T) {
	g, ids := board(t)
	h := &fakeHost{g: g}
	Resolve(h, &Ctx{Controller: 0, TargetsOffered: true,
		Targets: []state.Target{{Obj: ids["theirBig"]}}},
		sa(t, "DB$ AnimateAll | ValidTgts$ Creature | ValidCards$ Creature | Types$ Golem"))
	sources := map[state.ObjID]bool{}
	for _, ce := range h.continuous {
		if ce.Layer == state.LType {
			sources[ce.Source] = true
		}
	}
	for _, want := range []state.ObjID{ids["myBear"], ids["myFlier"], ids["theirBig"]} {
		if !sources[want] {
			t.Fatalf("creature id %d not animated (Creature ValidTgts$ must not scope the sweep)", want)
		}
	}
}

// containsAny reports whether want is one of the strings in vals.
func containsAny(vals []string, want string) bool {
	for _, v := range vals {
		if v == want {
			return true
		}
	}
	return false
}
