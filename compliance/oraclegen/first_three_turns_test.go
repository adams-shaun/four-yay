package oraclegen

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

func TestFirstThreeTurnScenarioTurn(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name, mode, check, count string
		want                     bool
	}{
		{"Jace Reawakened", "CantBeCast", "Count$YourTurns", "Count$YourTurns", true},
		{"Serra Avenger", "CantBeCast", "X", "Count$YourTurns", true},
		{"Spider-Man 2099", "CantBeCast", "Z", "Count$YourTurns", true},
		{"Avatar of Hope", "ReduceCost", "NeedHope", "Count$YourLifeTotal", false},
		{"Reject Imperfection", "", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			card, ok := reg.Lookup(tc.name)
			if !ok || len(card.Faces) == 0 {
				t.Fatalf("missing compiled face for %s", tc.name)
			}
			f := card.Faces[0]
			// Pin the compiled clause being tested, not just the card's name.
			if tc.mode == "" {
				if len(f.Statics) != 0 || !strings.Contains(f.SVars["DBProliferate"], "ConditionSVarCompare$ LE3") {
					t.Fatal("expected an ability-only LE3 condition, with no statics")
				}
			} else {
				found := false
				for _, st := range f.Statics {
					if st.Mode == tc.mode && st.Params["SVarCompare"] == "LE3" && st.Params["CheckSVar"] == tc.check {
						v := st.Params["CheckSVar"]
						if body, ok := f.SVars[v]; ok {
							v = body
						}
						found = v == tc.count
					}
				}
				if !found {
					t.Fatal("expected compiled Mode/CheckSVar/LE3/count precondition is absent")
				}
			}
			// Renaming the face must not change the decision: future carriers
			// with this same compiled restriction need no name-list update.
			renamed := *f
			renamed.Name = "Unlisted fixture"
			assertFirstThreeTurnScenario(t, &renamed, tc.want)
		})
	}
	t.Run("nil face", func(t *testing.T) {
		assertFirstThreeTurnScenario(t, nil, false)
	})
	t.Run("different comparator", func(t *testing.T) {
		f := &cards.Face{Statics: []cards.Static{{Mode: "CantBeCast", Params: map[string]string{
			"CheckSVar": "Count$YourTurns", "SVarCompare": "LE2",
		}}}}
		assertFirstThreeTurnScenario(t, f, false)
	})
	t.Run("unresolved count", func(t *testing.T) {
		f := &cards.Face{Statics: []cards.Static{{Mode: "CantBeCast", Params: map[string]string{
			"CheckSVar": "Missing", "SVarCompare": "LE3",
		}}}}
		assertFirstThreeTurnScenario(t, f, false)
	})
	t.Run("other mode with same count", func(t *testing.T) {
		f := &cards.Face{Statics: []cards.Static{{Mode: "ReduceCost", Params: map[string]string{
			"CheckSVar": "Count$YourTurns", "SVarCompare": "LE3",
		}}}}
		assertFirstThreeTurnScenario(t, f, false)
	})
}

func assertFirstThreeTurnScenario(t *testing.T, f *cards.Face, want bool) {
	t.Helper()
	turn, ok := firstThreeTurnScenarioTurn(f)
	wantTurn := 0
	if want {
		wantTurn = 7
	}
	if turn != wantTurn || ok != want {
		t.Errorf("firstThreeTurnScenarioTurn = (%d, %t), want (%d, %t)", turn, ok, wantTurn, want)
	}
	// Non-carriers preserve an explicit turn. The input differs from both
	// helper outcomes, and the requested identity differs from the face name.
	it := NewItem(f, "Requested fixture", "cast-resolve", 1, Scenario{Turn: 9})
	wantItemTurn := 9
	if want {
		wantItemTurn = 7
	}
	if it.Turn != wantItemTurn || it.Card != "Requested fixture" || it.ID != "Requested fixture/cast-resolve/v1" {
		t.Errorf("NewItem did not preserve identity/derive turn: %+v", it)
	}
}
