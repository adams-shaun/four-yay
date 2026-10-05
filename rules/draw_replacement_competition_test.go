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
			if d == nil || d.Kind != decision.KReplacement || len(d.Options) != 2 {
				t.Fatalf("precondition: pending replacement order = %+v, want two candidates", d)
			}
			if d.Options[0].Kind != "replacement" || d.Options[1].Kind != "replacement" {
				t.Fatalf("replacement order options = %+v, want candidate ordering without a may-decision", d.Options)
			}
			submitChoices(t, e, d.Options[0].Index)
			d = e.Pending()
			if d == nil || d.Kind != decision.KReplacement || d.Player != 0 || len(d.Options) != 2 ||
				d.Options[0].Kind != "apply" || d.Options[1].Kind != "decline" {
				t.Fatalf("optional candidate did not receive its may-choice: %+v", d)
			}
			if answer == "apply" {
				submitChoices(t, e, d.Options[0].Index)
				if got := countDraw(e) - drawsBefore; got != 0 {
					t.Fatalf("draw events after applying optional replacement = %d, want 0", got)
				}
				return
			}
			submitChoices(t, e, d.Options[1].Index)
			d = e.Pending()
			if d == nil || d.Kind != decision.KReplacement || len(d.Options) != 2 ||
				d.Options[0].Kind != "apply" || d.Options[1].Kind != "decline" {
				t.Fatalf("remaining replacement did not receive its optional choice: %+v", d)
			}
			if len(e.G.Zone(state.ZLibrary, 0)) == 0 {
				t.Fatal("precondition: library became empty before remaining replacement choice")
			}
			submitChoices(t, e, d.Options[1].Index)
			if got := countDraw(e) - drawsBefore; got != 1 {
				t.Fatalf("draw events after declining both replacements = %d, want 1", got)
			}
		})
	}
}

func TestDrawReplacementCompetitionOptionalMayUsesItsDecider(t *testing.T) {
	t.Parallel()
	e := drawReplEngine(t, 744)
	optional := card(t, "Name:OtherSeatOptional\nTypes:Creature\nPT:1/1\nR:Event$ Draw | Optional$ True | OptionalDecider$ You\nOracle:skip optionally\n")
	optionalID := onBoardCard(t, e, 1, optional)
	other := card(t, "Name:OtherReplacement\nTypes:Creature\nPT:1/1\nR:Event$ Draw | Optional$ True\nOracle:skip optionally\n")
	onBoardCard(t, e, 0, other)
	setupDrawLibrary(t, e, 0, mountainDeck(t, 1)[0])
	if len(e.G.Zone(state.ZLibrary, 0)) == 0 || e.controllerOf(optionalID) != 1 || e.G.Obj(optionalID).Zone != state.ZBattlefield {
		t.Fatal("precondition: affected player's library and seat 1's battlefield replacement must exist")
	}
	drawsBefore := countDraw(e)
	e.pending = nil
	emitDraw(t, e, 0)
	d := e.Pending()
	if d == nil || d.Kind != decision.KReplacement || d.Player != 0 || len(d.Options) != 2 {
		t.Fatalf("CR 616.1 order decision = %+v, want affected player 0 ordering two candidates", d)
	}
	optionalIndex := -1
	for _, opt := range d.Options {
		if opt.Kind == "replacement" && opt.Obj == optionalID {
			optionalIndex = opt.Index
		}
	}
	if optionalIndex < 0 {
		t.Fatalf("seat 1 optional Draw candidate missing from order choice: %+v", d.Options)
	}
	submitChoices(t, e, optionalIndex)
	d = e.Pending()
	if d == nil || d.Kind != decision.KReplacement || d.Player != 1 || len(d.Options) != 2 ||
		d.Options[0].Kind != "apply" || d.Options[1].Kind != "decline" {
		t.Fatalf("optional may-decision = %+v, want OptionalDecider You seat 1", d)
	}
	submitChoices(t, e, d.Options[1].Index)
	d = e.Pending()
	if d == nil || d.Kind != decision.KReplacement || d.Player != 0 || len(d.Options) != 2 {
		t.Fatalf("remaining candidate was not re-evaluated for affected player: %+v", d)
	}
	submitChoices(t, e, d.Options[1].Index)
	if got := countDraw(e) - drawsBefore; got != 1 {
		t.Fatalf("draws after declining both candidates = %d, want 1", got)
	}
}
