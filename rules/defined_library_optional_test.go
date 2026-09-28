package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestDefinedLibraryOptionalDeclineResumesTheRealEffect drives Kenessos's
// compiled DBBottom through Engine.Ask and handleChoose, rather than merely
// attaching the answer to an effects Ctx. The declined direct fetch leaves
// its remembered card in place and does not shuffle it.
func TestDefinedLibraryOptionalDeclineResumesTheRealEffect(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Kenessos, Priest of Thassa")
	source := searchMoveByName(t, e, "Kenessos, Priest of Thassa", state.ZStack)
	kenessos := e.G.Obj(source)
	if kenessos == nil || kenessos.Face() == nil {
		t.Fatal("Kenessos source has no face")
	}
	bottom := cards.ResolveSVar(kenessos.Face().SVars, "DBBottom")
	if bottom == nil {
		t.Fatal("Kenessos has no compiled DBBottom continuation")
	}
	lib := e.G.Zone(state.ZLibrary, 0)
	if len(lib) == 0 {
		t.Fatal("fixture library is empty")
	}
	remembered := lib[0]
	start := len(e.L.Events)
	effects.Resolve(e, &effects.Ctx{Source: source, Controller: 0,
		Remembered: []state.Target{{Obj: remembered}}}, bottom)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "defined_library_optional" {
		t.Fatalf("pending = %+v, want the optional direct-fetch choose", d)
	}
	if len(d.Options) != 2 || d.Options[1].Kind != "no" {
		t.Fatalf("options = %+v, want yes/no", d.Options)
	}
	submitChoices(t, e, d.Options[1].Index)
	if o := e.G.Obj(remembered); o == nil || o.Zone != state.ZLibrary {
		t.Fatalf("declined DBBottom left card %+v, want library", o)
	}
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.Shuffle || (ev.Kind == events.MoveZone && ev.Obj == remembered) {
			t.Fatalf("declined DBBottom emitted %+v", ev)
		}
	}
	replayCheck(t, e, cfg)
}
