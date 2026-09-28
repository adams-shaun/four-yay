package rules

import (
	"fmt"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// selfTypeEffectsBoard is the Derived-read fixture for the layer-4 self-only
// early rejection: a token board (n vanilla goblins) plus a permanent carrying
// four registered `Affected$ Card.Self` layer-4 type effects, the shape of
// Clown Car's crew (four self type effects live while a Krenko doubling puts
// thousands of goblins on the battlefield). Every Derived read of a token
// walks the active layer-4 effect list; only the source can be reached by a
// Card.Self spec, so the read must reject every non-source id before the full
// match.
func selfTypeEffectsBoard(t testing.TB, n int) (*Engine, state.ObjID, state.ObjID) {
	t.Helper()
	e := layerEngine(t)
	var token state.ObjID
	for i := 0; i < n; i++ {
		token = onBoard(t, e, 0, fmt.Sprintf("Name:Goblin %d\nTypes:Creature Goblin\nPT:1/1\nOracle:x\n", i))
	}
	car := onBoard(t, e, 0, "Name:Car\nTypes:Artifact Vehicle\nPT:4/4\nOracle:x\n")
	for _, types := range [][]string{
		{"Artifact", "Creature"},
		{"Creature", "Goblin"},
		{"Vehicle"},
		{"Construct"},
	} {
		e.AddContinuous(ContinuousEffect{Source: car, Controller: 0, Affects: "Card.Self", Layer: LType,
			UntilEOT: true, AddTypes: types})
	}
	return e, car, token
}

// TestLayer4SelfTypeEffectRejectsNonSourceEarly proves the layer-4 self-only
// early rejection: a match against an `Affects$ Card.Self` effect whose id is
// not the effect's source is rejected before the full match, and the shortcut
// agrees with the full match (verify mode recomputes the full match and
// panics on any disagreement). The counter is asserted so the shortcut cannot
// be removed without a failure.
func TestLayer4SelfTypeEffectRejectsNonSourceEarly(t *testing.T) {
	t.Parallel()
	e, car, token := selfTypeEffectsBoard(t, 8)
	// Precondition: the source and the candidate are distinct, on the
	// battlefield, and the candidate does not carry the granted type. A
	// vacuous setup would silently pass.
	if car == token {
		t.Fatal("fixture precondition: the effect source and the candidate token are the same object")
	}
	src := e.G.Obj(car)
	cand := e.G.Obj(token)
	if src == nil || src.Zone != state.ZBattlefield {
		t.Fatalf("fixture precondition: effect source is not a battlefield permanent")
	}
	if cand == nil || cand.Zone != state.ZBattlefield {
		t.Fatalf("fixture precondition: candidate token is not a battlefield permanent")
	}
	if !containsFold(cand.Face().Types, "Goblin") || containsFold(cand.Face().Types, "Construct") {
		t.Fatalf("fixture precondition: token face types %v already carry/omit the granted type", cand.Face().Types)
	}

	// The four self effects are live and reachable through the layer walk.
	live := 0
	for i := range e.continuous {
		if e.continuous[i].Layer == LType && e.continuous[i].Affects == "Card.Self" {
			live++
		}
	}
	if live != 4 {
		t.Fatalf("fixture precondition: %d live Card.Self layer-4 effects, want 4", live)
	}

	// The source still receives its own type change.
	got := e.typeCharacteristics(car, 0)
	if !containsFold(got, "Creature") || !containsFold(got, "Goblin") {
		t.Fatalf("source derived types %v missing the self-granted Creature/Goblin", got)
	}

	// Run a Derived read on the non-source token: the early rejection fires
	// for each self effect whose source is not the token.
	before := selfRejectVerify.Load()
	_ = e.Derived(token)
	if after := selfRejectVerify.Load(); after <= before {
		t.Fatalf("the Card.Self early rejection did not fire for a non-source Derived read (%d -> %d)", before, after)
	}
	// The shortcut must not have leaked a type onto the non-source token.
	if tk := e.typeCharacteristics(token, 0); containsFold(tk, "Construct") || containsFold(tk, "Vehicle") {
		t.Fatalf("non-source token derived types %v carry a self-granted type", tk)
	}
}

func containsFold(list []string, want string) bool {
	for _, s := range list {
		if strings.EqualFold(s, want) {
			return true
		}
	}
	return false
}

// BenchmarkDerivedSelfTypeEffects measures a Derived read over a token board
// while four Card.Self layer-4 type effects are live. The token read must pay
// only the early id!=ce.Source rejection per self effect, not the full
// castProvenance/specCtx/MatchesSpecCtx match.
func BenchmarkDerivedSelfTypeEffects(b *testing.B) {
	for _, n := range []int{4000, 8000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			e, _, token := selfTypeEffectsBoard(b, n)
			benchWithoutVerify(b)
			// The last-added goblin is a token with no layer-4 effect naming
			// it: the read the benchmark measures.
			if token == 0 {
				b.Fatal("fixture token not found on the battlefield")
			}
			if got := e.Derived(token); got.Power != 1 || got.Toughness != 1 {
				b.Fatalf("token derived P/T = %d/%d, want 1/1", got.Power, got.Toughness)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				_ = e.Derived(token)
			}
		})
	}
}
