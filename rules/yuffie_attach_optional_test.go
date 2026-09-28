package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestYuffieMayDeclineHerETBAttach exercises the real ETB trigger and its
// Choices$ Equipment.YouCtrl may-attach election. Declining must preserve the
// equipment's unattached state after the preceding gain-control rider resolves.
func TestYuffieMayDeclineHerETBAttach(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	lookup := func(name string) *cards.Card {
		t.Helper()
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("%s not found in compiled corpus registry", name)
		}
		return c
	}
	yuffie, equipment, artifact := lookup("Yuffie, Materia Hunter"), lookup("Bonesplitter"), lookup("Sol Ring")
	cfg := Config{Seed: 904, Names: []string{"a", "b"}, Decks: [][]*cards.Card{
		append([]*cards.Card{yuffie, equipment}, mountainDeck(t, 38)...),
		append([]*cards.Card{artifact}, mountainDeck(t, 39)...),
	}, Tokens: reg.Tokens}
	e := New(seatZeroStart(cfg))
	e.Advance()
	yuffieID := findAndMoveToHand(t, e, 0, "Yuffie, Materia Hunter")
	equipmentID := findAndMoveToHand(t, e, 0, "Bonesplitter")
	artifactID := findAndMoveToHand(t, e, 1, "Sol Ring")
	moveToBattlefield(t, e, equipmentID)
	moveToBattlefield(t, e, artifactID)
	moveToBattlefield(t, e, yuffieID)
	if e.G.Obj(yuffieID).Zone != state.ZBattlefield || e.G.Obj(equipmentID).Zone != state.ZBattlefield || e.G.Obj(artifactID).Zone != state.ZBattlefield {
		t.Fatal("precondition: Yuffie, Bonesplitter and Sol Ring must all be on the battlefield")
	}

	var attachAsk *decision.Decision
	for n := 0; n < 80; n++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no pending decision while resolving Yuffie's ETB trigger")
		}
		switch {
		case d.Kind == decision.KChoose && d.ResumeKind == "attach_choice":
			attachAsk = d
		case d.Kind == decision.KChoose:
			if len(d.Options) < d.Min {
				t.Fatalf("cannot answer unrelated choice: %+v", d)
			}
			choices := make([]int, d.Min)
			for i := range choices {
				choices[i] = d.Options[i].Index
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
				t.Fatalf("answer unrelated choice: %v", err)
			}
		case d.Kind == decision.KTarget:
			answerKTarget(t, e, artifactID)
		case d.Kind == decision.KAttackers:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player}); err != nil {
				t.Fatalf("declare no attackers: %v", err)
			}
		case d.Kind == decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
					break
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision has no pass option: %+v", d.Options)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("pass priority: %v", err)
			}
		default:
			t.Fatalf("unexpected decision resolving Yuffie's ETB: %+v", d)
		}
		if attachAsk != nil {
			break
		}
	}
	if attachAsk == nil {
		t.Fatalf("Yuffie's ETB never posed its Optional$ attach choice; events=%+v", e.L.Events)
	}
	if attachAsk.Player != 0 || attachAsk.Min != 0 || attachAsk.Max != 1 || len(attachAsk.Options) != 1 || attachAsk.Options[0].Obj != equipmentID {
		t.Fatalf("attach ask = %+v, want controller's optional Bonesplitter choice", attachAsk)
	}
	if e.G.Obj(artifactID).Controller != 0 {
		t.Fatalf("Yuffie's gain-control rider did not resolve: Sol Ring controller = %d", e.G.Obj(artifactID).Controller)
	}

	if err := e.Submit(decision.Intent{Seq: attachAsk.Seq, Player: attachAsk.Player, Choices: nil}); err != nil {
		t.Fatalf("decline Yuffie's attach: %v", err)
	}
	passUntilStackEmpty(t, e, 80)
	if e.G.Obj(equipmentID).AttachedTo != 0 {
		t.Fatalf("declined Bonesplitter is attached to %d", e.G.Obj(equipmentID).AttachedTo)
	}
	attaches := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Attach && ev.Obj == equipmentID {
			attaches++
		}
	}
	if attaches != 0 {
		t.Fatalf("declined Yuffie attach emitted %d Attach events", attaches)
	}
	replayCheck(t, e, cfg)
}
