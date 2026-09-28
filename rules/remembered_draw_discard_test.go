package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestRememberedDrawThenDiscardDiscards(t *testing.T) {
	t.Parallel()
	e, _, spell := newFixtureDeck(t, 9360, "Name:Draw Discard Test\nManaCost:U\nTypes:Sorcery\nA:SP$ Draw | NumCards$ 3 | RememberDrawn$ True | SubAbility$ DBDiscard | SpellDescription$ Draw three, then discard two.\nSVar:DBDiscard:DB$ Discard | Defined$ You | Mode$ TgtChoose | NumCards$ 2 | ConditionDefined$ Remembered | ConditionPresent$ Card | SubAbility$ DBCleanup\nSVar:DBCleanup:DB$ Cleanup | ClearRemembered$ True\nOracle:x\n")
	toMain1(t, e)
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "U", Amount: 1})
	e.pending = nil
	e.askPriority(0)
	d := e.Pending()
	cast := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == spell {
			cast = o.Index
		}
	}
	if cast < 0 {
		t.Fatalf("no cast option for spell %d in %#v", spell, d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{cast}}); err != nil {
		t.Fatal(err)
	}
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZStack {
		t.Fatalf("precondition: cast spell %d zone = %v, want stack", spell, o)
	}
	handBefore := len(e.G.Zone(state.ZHand, 0))
	graveBefore := len(e.G.Zone(state.ZGraveyard, 0))
	sawDiscardChoice := false
	for i := 0; i < 20 && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		if d.Kind == decision.KPriority {
			if e.G.Obj(spell).Zone != state.ZStack {
				break
			}
			pass := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					pass = o.Index
				}
			}
			if pass < 0 {
				t.Fatalf("no pass option in %#v", d.Options)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pass}}); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if d.Kind == decision.KModes && d.ResumeKind == "discard" {
			sawDiscardChoice = true
			if len(d.Options) < 3 || d.Min != 2 || d.Max != 2 {
				t.Fatalf("precondition: discard ask has %d options, bounds %d..%d; want 3 options and exactly 2 picks", len(d.Options), d.Min, d.Max)
			}
		}
		var ch []int
		for k := 0; k < d.Min; k++ {
			ch = append(ch, d.Options[k].Index)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: ch}); err != nil {
			t.Fatalf("answer %s %q: %v", d.Kind, d.Prompt, err)
		}
	}
	hand := len(e.G.Zone(state.ZHand, 0))
	discarded := len(e.G.Zone(state.ZGraveyard, 0)) - graveBefore - 1 // minus the resolved sorcery
	if !sawDiscardChoice {
		t.Error("discard handler never posed its expected mid-resolution choice")
	}
	if hand != handBefore+1 || discarded != 2 {
		t.Errorf("after draw 3 / discard 2: hand %d -> %d, discarded %d; want hand +1 and 2 discards", handBefore, hand, discarded)
	}
	if e.discardBatchOpen || e.discardBatchDepth != 0 {
		t.Errorf("discard batch left open at rest: open=%v depth=%d", e.discardBatchOpen, e.discardBatchDepth)
	}
}
