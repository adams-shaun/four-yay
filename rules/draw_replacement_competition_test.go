package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func drawCompetitionEngine(t *testing.T) *Engine {
	t.Helper()
	e := drawReplEngine(t, 743)
	for _, name := range []string{"first", "second"} {
		c := card(t, "Name:"+name+"\nTypes:Creature\nPT:1/1\nR:Event$ Draw | Optional$ True\nOracle:skip optionally\n")
		onBoardCard(t, e, 0, c)
	}
	setupDrawLibrary(t, e, 0, mountainDeck(t, 1)[0])
	if len(e.G.Zone(state.ZLibrary, 0)) == 0 {
		t.Fatal("precondition: seat 0 library is empty")
	}
	return e
}

func TestDrawReplacementCompetitionCanDeclineOptionalBodylessDrawReplacement(t *testing.T) {
	t.Parallel()
	for _, answer := range []string{"decline", "apply"} {
		t.Run(answer, func(t *testing.T) {
			e := drawCompetitionEngine(t)
			drawsBefore := countDraw(e)
			e.pending = nil // the helper's unrelated priority decision
			emitDraw(t, e, 0)
			d := e.Pending()
			if d == nil || d.Kind != decision.KReplacement || len(d.Options) != 3 {
				t.Fatalf("precondition: pending replacement choice = %+v, want two candidates and decline option", d)
			}
			if d.Options[0].Kind != "replacement" || d.Options[1].Kind != "replacement" || d.Options[2].Kind != "skip_replacement" {
				t.Fatalf("replacement options = %+v, want two candidates and skip_replacement", d.Options)
			}
			if answer == "apply" {
				submitChoices(t, e, d.Options[0].Index)
				if got := countDraw(e) - drawsBefore; got != 0 {
					t.Fatalf("draw events after applying optional replacement = %d, want 0", got)
				}
				return
			}
			submitChoices(t, e, d.Options[2].Index)
			d = e.Pending()
			if d == nil || d.Kind != decision.KReplacement || len(d.Options) != 2 || d.Options[0].Kind != "apply" || d.Options[1].Kind != "decline" {
				t.Fatalf("remaining replacement did not receive its optional choice: %+v", d)
			}
			if len(e.G.Zone(state.ZLibrary, 0)) == 0 {
				t.Fatal("precondition: library became empty before the remaining replacement choice")
			}
			submitChoices(t, e, d.Options[1].Index)
			if got := countDraw(e) - drawsBefore; got != 1 {
				t.Fatalf("draw events after declining both replacements = %d, want 1", got)
			}
		})
	}
}
