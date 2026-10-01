package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestCharacteristicDefiningCountSeesGrantedTypes pins CR 604.3/613.1f for a
// characteristic-defining P/T: Ashaya, Soul of the Wild's layer-4 grant makes
// every nontoken creature its controller controls a Forest land, so its own
// CDA P/T (Count$Valid Land.YouCtrl) must count the granted types, Ashaya
// itself included. The CDA evaluation Ctx must carry the published layer-4
// type table; without it the count falls through to the printed face and
// reads only the printed Forest (Ashaya read 1/1 on the three-permanent
// board, 2/2-class defect as filed).
func TestCharacteristicDefiningCountSeesGrantedTypes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		extra []string // battlefield permanents besides Ashaya
		want  int32    // lands the CDA must count = Ashaya + the granted bear(s) + the printed Forest
	}{
		{name: "forest+bear+ashaya", extra: []string{"Forest", "Grizzly Bears"}, want: 3},
		{name: "bear+ashaya", extra: []string{"Grizzly Bears"}, want: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reg := testutil.CorpusRegistry(t)
			extras := make([]*cards.Card, 0, len(tc.extra)+1)
			extras = append(extras, lookup(t, reg, "Ashaya, Soul of the Wild"))
			for _, n := range tc.extra {
				extras = append(extras, lookup(t, reg, n))
			}
			e := corpusEngine(t, reg, extras, nil)
			ashaya := cdaMoveTo(t, e, 0, "Ashaya, Soul of the Wild", state.ZBattlefield)
			if e.G.Obj(ashaya) == nil || e.G.Obj(ashaya).Zone != state.ZBattlefield {
				t.Fatalf("precondition: Ashaya not on the battlefield")
			}
			// Precondition the real assertion depends on: the layer-4 grant is
			// live. Ashaya is a nontoken creature its controller controls, so
			// the SAME static grants IT Forest+Land too, and every nontoken
			// creature put onto the battlefield after it. Without the grant
			// the P/T below could pass on a board where the grant never fired.
			for _, g := range []string{"Forest", "Land"} {
				if !slices.Contains(e.typeCharacteristics(ashaya, 0), g) {
					t.Fatalf("precondition: Ashaya's derived types %v lack granted %q", e.typeCharacteristics(ashaya, 0), g)
				}
			}
			for _, n := range tc.extra {
				id := cdaMoveTo(t, e, 0, n, state.ZBattlefield)
				o := e.G.Obj(id)
				if o == nil || o.Zone != state.ZBattlefield {
					t.Fatalf("precondition: %s not on the battlefield", n)
				}
				if n == "Forest" {
					continue // printed Forest: counted by its printed type, not a grant carrier
				}
				for _, g := range []string{"Forest", "Land"} {
					if !slices.Contains(e.typeCharacteristics(id, 0), g) {
						t.Fatalf("precondition: %s's derived types %v lack granted %q", n, e.typeCharacteristics(id, 0), g)
					}
				}
			}
			if got := e.Power(ashaya); got != tc.want {
				t.Fatalf("Ashaya power = %d, want %d (CDA must count granted land types)", got, tc.want)
			}
			if got := e.Toughness(ashaya); got != tc.want {
				t.Fatalf("Ashaya toughness = %d, want %d (CDA must count granted land types)", got, tc.want)
			}
		})
	}
}
