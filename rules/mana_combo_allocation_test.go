package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestComboAnyAllocatesEachManaUnit proves the activation path records a real
// distribution for "any combination" rather than selecting one colour then
// multiplying Amount$. The two selected colours deliberately differ.
func TestComboAnyAllocatesEachManaUnit(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	source := onBoard(t, e, 0, "Name:Archive Key\nTypes:Artifact\nA:AB$ Mana | Cost$ T | Produced$ Combo Any | Amount$ 2\nOracle:x\n")
	if o := e.G.Obj(source); o == nil || o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("Combo Any source precondition failed: %+v", o)
	}
	e.askPriority(0)
	submitChoices(t, e, activateOption(t, e, source))
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 2 || d.Max != 2 || len(d.Options) != 10 {
		t.Fatalf("Combo Any allocation ask = %+v, want two choices from ten unit-colour options", d)
	}
	blue, red := -1, -1
	for _, option := range d.Options {
		switch option.Label {
		case "Add U":
			if blue < 0 {
				blue = option.Index
			}
		case "Add R":
			if red < 0 {
				red = option.Index
			}
		}
	}
	if blue < 0 || red < 0 || blue == red {
		t.Fatalf("Combo Any options lack distinct U/R units: %+v", d.Options)
	}
	submitChoices(t, e, blue, red)
	got := e.G.Players[0].Pool
	want := state.Mana{state.MU: 1, state.MR: 1}
	if got != want {
		t.Fatalf("Combo Any pool = %v, want split allocation %v", got, want)
	}
}

// TestAvailableManaChosenColorFailsClosedUntilRecorded tests the source-aware
// projection: a source cannot advertise ChosenColor's hypothetical WUBRG set
// before its as-enters event exists, but does report its recorded colour.
// TestComboRestrictedColoursAllocateEachManaUnit is the non-Any Combo form:
// only its named colours are offerable, while their allocation may still split.
func TestComboRestrictedColoursAllocateEachManaUnit(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	source := onBoard(t, e, 0, "Name:Burnt Key\nTypes:Artifact\nA:AB$ Mana | Cost$ T | Produced$ Combo U R | Amount$ 2\nOracle:x\n")
	if o := e.G.Obj(source); o == nil || o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("restricted Combo source precondition failed: %+v", o)
	}
	e.askPriority(0)
	submitChoices(t, e, activateOption(t, e, source))
	d := e.Pending()
	if d == nil || d.Min != 2 || d.Max != 2 || len(d.Options) != 4 {
		t.Fatalf("restricted Combo allocation ask = %+v, want two choices from U/R units", d)
	}
	blue, red := -1, -1
	for _, option := range d.Options {
		if option.Label == "Add U" && blue < 0 {
			blue = option.Index
		}
		if option.Label == "Add R" && red < 0 {
			red = option.Index
		}
	}
	if blue < 0 || red < 0 || blue == red {
		t.Fatalf("restricted Combo options lack U/R units: %+v", d.Options)
	}
	submitChoices(t, e, blue, red)
	if got, want := e.G.Players[0].Pool, (state.Mana{state.MU: 1, state.MR: 1}); got != want {
		t.Fatalf("restricted Combo pool = %v, want %v", got, want)
	}
}

func TestAvailableManaChosenColorFailsClosedUntilRecorded(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	source := onBoard(t, e, 0, "Name:Chosen Source\nTypes:Land\nA:AB$ Mana | Cost$ T | Produced$ ChosenColor\nOracle:x\n")
	if o := e.G.Obj(source); o == nil || o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("ChosenColor source precondition failed: %+v", o)
	}
	if got := e.AvailableMana(0); got.Total() != 0 {
		t.Fatalf("unrecorded ChosenColor available = %v, want fail-closed zero", got)
	}
	e.emit(events.Event{Kind: events.Choose, Obj: source, Counter: "color", Text: "G"})
	got := e.AvailableMana(0)
	want := state.Mana{state.MG: 1}
	if got != want {
		t.Fatalf("recorded G ChosenColor available = %v, want %v", got, want)
	}
}
