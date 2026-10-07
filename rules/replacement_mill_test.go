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

func TestWaterCrystalZeroMillDoesNotBecomeFour(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := millTriggerEngine(t)
	id := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "The Water Crystal"))
	if obj := e.G.Obj(id); obj == nil || obj.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Water Crystal id %d is not on the battlefield", id)
	}
	if len(e.G.Zone(state.ZLibrary, 1)) < 6 {
		t.Fatal("precondition: opponent library cannot supply six cards")
	}
	before := millEventCount(e)
	resolveMill(t, e, 1, 0)
	if got := millEventCount(e) - before; got != 0 {
		t.Fatalf("zero-card mill moved %d cards, want 0", got)
	}
	before = millEventCount(e)
	resolveMill(t, e, 1, 2)
	if got := millEventCount(e) - before; got != 6 {
		t.Fatalf("precondition: nonzero opponent mill moved %d cards, want 6 (not 2)", got)
	}
}

func TestBruvacZeroMillDoesNotReportUnsupportedReplacement(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := millTriggerEngine(t)
	id := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Bruvac the Grandiloquent"))
	if obj := e.G.Obj(id); obj == nil || obj.Zone != state.ZBattlefield {
		t.Fatalf("Bruvac id %d is not on the battlefield", id)
	}
	proposal := events.Event{Kind: events.MillProposal, Player: 1, Amount: 0}
	matched := false
	obj := e.G.Obj(id)
	for i := range obj.Face().Repls {
		repl := &obj.Face().Repls[i]
		m := replMatch{id: id, face: obj.Face(), repl: repl}
		if e.replacementMatches(*repl, id, proposal) && millCountBodySupported(e.replCtx(m, proposal), repl.With) {
			matched = true
			break
		}
	}
	if !matched {
		t.Fatal("precondition: Bruvac's supported replacement matches a zero-card opponent mill")
	}
	before := len(e.L.Events)
	got := e.CountReplacementProposed(proposal)
	if got.Amount != 0 {
		t.Fatalf("zero-card mill rewritten to %d", got.Amount)
	}
	for _, ev := range e.L.Events[before:] {
		if ev.Kind == events.Note && ev.Text == "unimplemented Mill replacement" && ev.Obj == id {
			t.Fatal("supported Twice replacement emitted an unimplemented Note for a zero-card mill")
		}
	}
}

func TestMillReplacementCarrierCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	got := make(map[string]bool)
	for _, c := range reg.AllCards() {
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
