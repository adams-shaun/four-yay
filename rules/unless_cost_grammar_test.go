package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestUnlessCostResolvedUsesTheAnnouncedX pins the CR 601.2b channel used by
// an UnlessCost$ after the resolving spell's X choice has already happened.
// The raw spell parameters remain X/XX; the suspended resolution must price
// the same announced value rather than asking the payer to pay an unbound
// token. Power Sink and Thassa's Intervention are the real corpus spell
// shapes, and the direct resolution contexts model their recorded X.
func TestUnlessCostResolvedUsesTheAnnouncedX(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, mustCorpusCard(t, reg, "Power Sink"), mustCorpusCard(t, reg, "Thassa's Intervention"))
	for _, tc := range []struct {
		name string
		cost string
		want string
	}{
		{"Power Sink", "X", "{3}"},
		{"Thassa's Intervention", "XX", "{6}"},
	} {
		c := mustCorpusCard(t, reg, tc.name)
		var sa *cards.SA
		for _, face := range c.Faces {
			walkAllSAs(face, func(candidate *cards.SA) {
				if sa == nil && candidate.Params["UnlessCost"] == tc.cost {
					sa = candidate
				}
			})
		}
		if sa == nil {
			t.Fatalf("%s has no UnlessCost$ %s SA", tc.name, tc.cost)
		}
		if sa.Params["UnlessCost"] != tc.cost {
			t.Fatalf("%s precondition lost: raw unless cost = %q", tc.name, sa.Params["UnlessCost"])
		}
		got := effects.UnlessCostResolved(e, &effects.Ctx{Controller: 0, X: 3}, sa)
		if got != tc.want {
			t.Fatalf("%s resolved unless cost = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestUnlessCostResolvedFoldsDynamicSVarForEveryAPI covers the shared gate's
// non-Counter path with Fettergeist's real Sacrifice UnlessCost$ Y. Two other
// creatures are deliberately put on the controller's battlefield, so the
// count is a proved nonzero value rather than a vacuous zero.
func TestUnlessCostResolvedFoldsDynamicSVarForEveryAPI(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	fetter := mustCorpusCard(t, reg, "Fettergeist")
	e := handEngine(t, fetter)
	source := onBoardCard(t, e, 0, fetter)
	onBoard(t, e, 0, "Name:Bear One\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	onBoard(t, e, 0, "Name:Bear Two\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	var sa *cards.SA
	walkAllSAs(fetter.Faces[0], func(candidate *cards.SA) {
		if sa == nil && candidate.Params["UnlessCost"] == "Y" {
			sa = candidate
		}
	})
	if sa == nil {
		t.Fatal("Fettergeist has no real UnlessCost$ Y ability")
	}
	if len(e.G.Zone(state.ZBattlefield, 0)) != 3 {
		t.Fatalf("fixture battlefield = %d objects, want Fettergeist plus two creatures", len(e.G.Zone(state.ZBattlefield, 0)))
	}
	ctx := &effects.Ctx{Source: source, Controller: 0, SVars: fetter.Faces[0].SVars}
	got := effects.UnlessCostResolved(e, ctx, sa)
	if got != "{2}" {
		t.Fatalf("Fettergeist dynamic unless cost = %q, want {2}", got)
	}
}
