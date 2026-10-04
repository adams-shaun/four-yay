package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Meathook Massacre II's opponent-death return (UnlessCost$ PayLife<3>,
// UnlessPayer$ TriggeredCardController) under the kernel. These also cover
// unless_sacrifice_test.go's TestMeathookUnlessPayPaysLife /
// TestMeathookUnlessPayDeclined (same carrier, same assertions).

func TestUnlessPayChangeZoneMeathookPayKeepsTheCardInTheGraveyardKernel(t *testing.T) {
	t.Parallel()
	e := stealEngine(t, 751)
	onBoardCard(t, e, 0, choiceCorpusCard(t, "Meathook Massacre II"))
	bear := onBoard(t, e, 1, "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZBattlefield, To: state.ZGraveyard})
	e.putTriggersOnStack()
	kr9ResolveTop(e)

	ask := e.Pending()
	if ask == nil || ask.Kind != decision.KModes || ask.ResumeKind != "unless_pay" {
		t.Fatalf("no unless-pay ask after the opponent creature died: %+v", ask)
	}
	if ask.Player != 1 {
		t.Fatalf("pay ask player = seat %d, want the dead creature's controller", ask.Player)
	}
	if ask.ResumeSA == nil || ask.ResumeSA.API != "ChangeZone" {
		t.Fatalf("ask SA = %+v, want the DB$ ChangeZone body", ask.ResumeSA)
	}
	// Pay: 3 life, the return is prevented.
	submitChoices(t, e, ask.Options[0].Index)
	if life := e.G.Players[1].Life; life != 17 {
		t.Fatalf("seat 1 life = %d, want 17 (20 - 3)", life)
	}
	if !containsObj(e.G.Zone(state.ZGraveyard, 1), bear) {
		t.Fatalf("paid Meathook bear zone = %v, want seat 1's graveyard (the return was paid off)", e.G.Obj(bear))
	}
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if id == bear {
			t.Fatal("paid Meathook return still put the bear on seat 0's battlefield")
		}
	}
}

func TestUnlessPayChangeZoneMeathookDeclineReturnsItWithFinalityKernel(t *testing.T) {
	t.Parallel()
	e := stealEngine(t, 752)
	onBoardCard(t, e, 0, choiceCorpusCard(t, "Meathook Massacre II"))
	bear := onBoard(t, e, 1, "Name:Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZBattlefield, To: state.ZGraveyard})
	e.putTriggersOnStack()
	kr9ResolveTop(e)

	ask := e.Pending()
	if ask == nil || ask.Kind != decision.KModes || ask.ResumeKind != "unless_pay" {
		t.Fatalf("no unless-pay ask after the opponent creature died: %+v", ask)
	}
	submitChoices(t, e, ask.Options[1].Index)
	if life := e.G.Players[1].Life; life != 20 {
		t.Fatalf("seat 1 life = %d, want 20 (nothing paid)", life)
	}
	o := e.G.Obj(bear)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("declined Meathook bear = %+v, want back on the battlefield", o)
	}
	if o.Controller != 0 {
		t.Fatalf("returned bear controller = %d, want Meathook's controller (GainControl$ True)", o.Controller)
	}
	if o.Counter("FINALITY") != 1 {
		t.Fatalf("returned bear FINALITY counters = %d, want 1", o.Counter("FINALITY"))
	}
}
