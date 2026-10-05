package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestObstinateFamiliarOptionalDraw(t *testing.T) {
	t.Parallel()
	for _, answer := range []struct {
		name       string
		optionKind string
		wantDraw   int
	}{
		{name: "accept", optionKind: "apply", wantDraw: 0},
		{name: "decline", optionKind: "decline", wantDraw: 1},
	} {
		t.Run(answer.name, func(t *testing.T) {
			t.Parallel()
			reg := testutil.CorpusRegistry(t)
			e := drawReplEngine(t, 743)
			familiar := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Obstinate Familiar"))
			if o := e.G.Obj(familiar); o == nil || o.Zone != state.ZBattlefield {
				t.Fatal("precondition: Obstinate Familiar is not on the battlefield")
			}
			setupDrawLibrary(t, e, 0, mustCorpusCard(t, reg, "Mountain"))
			if got := len(e.G.Zone(state.ZLibrary, 0)); got == 0 {
				t.Fatal("precondition: seat 0 library is empty")
			}
			handBefore, drawsBefore := len(e.G.Zone(state.ZHand, 0)), countDraw(e)
			// The helper starts with an unrelated priority offer pending; this
			// raw event stands in for the draw instruction that interrupts it.
			e.pending = nil
			emitDraw(t, e, 0)
			d := e.Pending()
			if d == nil || d.Kind != decision.KReplacement || d.Player != 0 {
				t.Fatalf("pending decision = %+v, want seat 0 KReplacement", d)
			}
			if len(d.Options) != 2 || d.Options[0].Kind != "apply" || d.Options[1].Kind != "decline" {
				t.Fatalf("replacement options = %+v, want apply/decline", d.Options)
			}
			pick := -1
			for _, option := range d.Options {
				if option.Kind == answer.optionKind {
					pick = option.Index
				}
			}
			if pick < 0 {
				t.Fatalf("no %q option in %+v", answer.optionKind, d.Options)
			}
			submitChoices(t, e, pick)
			if got := countDraw(e) - drawsBefore; got != answer.wantDraw {
				t.Errorf("draw events = %d, want %d", got, answer.wantDraw)
			}
			wantHand := handBefore + answer.wantDraw
			if got := len(e.G.Zone(state.ZHand, 0)); got != wantHand {
				t.Errorf("hand size = %d, want %d", got, wantHand)
			}
		})
	}
}
