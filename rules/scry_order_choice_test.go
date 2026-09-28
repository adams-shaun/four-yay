package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestScryReplacementOrderChangesDrawCount(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		first int
		drawn int
	}{
		{"Kenessos first", 0, 4}, {"Eligeth first", 1, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _, id := scryFixture(t, 9341)
			k := onBoardCard(t, e, 0, corpusCard(t, "Kenessos, Priest of Thassa"))
			x := onBoardCard(t, e, 0, corpusCard(t, "Eligeth, Crossroads Augur"))
			for _, source := range []state.ObjID{k, x} {
				o := e.G.Obj(source)
				if o == nil || o.Zone != state.ZBattlefield || len(o.Face().Repls) == 0 || o.Face().Repls[0].Event != "Scry" {
					t.Fatalf("precondition: replacement source %d not active: %+v", source, o)
				}
			}
			if len(e.G.Zone(state.ZLibrary, 0)) < 4 {
				t.Fatal("precondition: fewer than four library cards")
			}
			hand := len(e.G.Zone(state.ZHand, 0))
			d := castFixture(t, e, id, -1)
			if d == nil || d.Kind != decision.KReplacement || len(d.Options) != 2 || d.Options[0].Obj != k || d.Options[1].Obj != x {
				t.Fatalf("replacement order ask = %+v", d)
			}
			submitChoices(t, e, tc.first)
			passUntilStackEmpty(t, e, 20)
			if got := len(e.G.Zone(state.ZHand, 0)) - hand; got != tc.drawn-1 {
				t.Fatalf("net hand = %d, want %d (cast one, draw %d)", got, tc.drawn-1, tc.drawn)
			}
			for _, ev := range e.L.Events {
				if ev.Kind == events.Scry && ev.Obj == id {
					t.Fatal("draw substitution left a Scry trigger marker")
				}
			}
		})
	}
}

// A spell's Scry instruction must produce the same completed-action marker
// as a permanent's upkeep Scry, not a premature marker at the look.
func TestScrySpellTemporalAnchorBottomChoice(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		bottom bool
		pushes int
		amount int32
	}{
		{"bottom one", true, 1, 1},
		{"keep all on top", false, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _, id := scryFixture(t, 9342)
			source := onBoardCard(t, e, 0, corpusCard(t, "The Temporal Anchor"))
			o := e.G.Obj(source)
			if o == nil || o.Zone != state.ZBattlefield || len(o.Face().Triggers) < 2 || o.Face().Triggers[1].Params["ToBottom"] != "True" {
				t.Fatalf("precondition: ToBottom trigger source not on battlefield: %+v", o)
			}
			d := scryDecision(t, e, id)
			if len(d.Options) != 3 || d.Max != 3 || len(e.G.Zone(state.ZLibrary, 0)) < 4 || d.Options[0].Obj == d.Options[1].Obj {
				t.Fatalf("precondition: need distinct top/bottom cards and a nonempty remainder: %+v", d)
			}
			if marks := scryMarkers(e); len(marks) != 0 {
				t.Fatalf("scry recorded before arrangement: %+v", marks)
			}
			if tc.bottom {
				submitChoices(t, e, d.Options[0].Index, d.Options[1].Index)
			} else {
				submitChoices(t, e, d.Options[0].Index, d.Options[1].Index, d.Options[2].Index)
			}
			marks := scryMarkers(e)
			if len(marks) != 1 || marks[0].Amount != tc.amount || marks[0].Player != 0 || marks[0].Obj != id {
				t.Fatalf("completed scry record = %+v, want one marker bottoming %d", marks, tc.amount)
			}
			passUntilStackEmpty(t, e, 40)
			if n := anchorTriggerPushes(e, source); n != tc.pushes {
				t.Fatalf("Temporal Anchor pushed %d triggers, want %d", n, tc.pushes)
			}
			if n := len(libraryExiled(e, 0)); n != tc.pushes {
				t.Fatalf("Temporal Anchor exiled %d cards, want %d", n, tc.pushes)
			}
		})
	}
}
