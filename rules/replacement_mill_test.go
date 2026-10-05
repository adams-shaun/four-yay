package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func millEventCount(e *Engine) int {
	n := 0
	for _, ev := range e.L.Events {
		if events.IsMill(ev) {
			n++
		}
	}
	return n
}

func TestWaterCrystalAddsFourToOpponentMill(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := millTriggerEngine(t)
	id := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "The Water Crystal"))
	if obj := e.G.Obj(id); obj == nil || obj.Zone != state.ZBattlefield {
		t.Fatalf("Water Crystal id %d is not on the battlefield", id)
	}
	if len(e.G.Zone(state.ZLibrary, 1)) < 6 || len(e.G.Zone(state.ZLibrary, 0)) < 2 {
		t.Fatal("precondition: mill libraries are too short")
	}
	before := millEventCount(e)
	resolveMill(t, e, 0, 2)
	if got := millEventCount(e) - before; got != 2 {
		t.Fatalf("own mill moves = %d, want 2 (opponent-only restriction)", got)
	}
	before = millEventCount(e)
	resolveMill(t, e, 1, 2)
	if got := millEventCount(e) - before; got != 6 {
		t.Fatalf("opponent mill moves = %d, want 6", got)
	}
}

func TestBruvacDoublesOpponentMill(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := millTriggerEngine(t)
	id := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Bruvac the Grandiloquent"))
	if obj := e.G.Obj(id); obj == nil || obj.Zone != state.ZBattlefield {
		t.Fatalf("Bruvac id %d is not on the battlefield", id)
	}
	if len(e.G.Zone(state.ZLibrary, 1)) < 4 {
		t.Fatal("precondition: opponent library has fewer than four cards")
	}
	before := millEventCount(e)
	resolveMill(t, e, 1, 2)
	if got := millEventCount(e) - before; got != 4 {
		t.Fatalf("opponent mill moves = %d, want 4", got)
	}
}

func TestMillReplacementCarrierCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	got := make(map[string]bool)
	for _, c := range reg.Cards {
		if c == nil || len(c.Faces) == 0 {
			continue
		}
		for _, face := range c.Faces {
			for _, r := range face.Repls {
				if r.EventKind().String() == "Mill" {
					got[face.Name] = true
				}
			}
		}
	}
	want := map[string]bool{"The Water Crystal": true, "Bruvac the Grandiloquent": true}
	if len(got) != len(want) {
		t.Fatalf("R:Event$ Mill carriers = %v, want exactly %v", got, want)
	}
	for name := range want {
		if !got[name] {
			t.Errorf("missing R:Event$ Mill carrier %q", name)
		}
	}
}

func TestMillReplacementPrimitiveIsRegistered(t *testing.T) {
	if !effects.Supported()["repl:Mill"] {
		t.Fatal(`effects.Supported() is missing "repl:Mill"`)
	}
}
