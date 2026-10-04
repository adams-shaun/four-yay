package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Kuroki, Thief of Talents' targeted unless-draw (Draw<4/Player.targetedBy>)
// and Tresserhorn's Lord's compound unless cost, driven under the kernel.

func kr9DriveKurokiTrig(t *testing.T) (*Engine, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 741)
	kuroki := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Kuroki, Thief of Talents"))
	sa := cards.ResolveSVar(e.G.Obj(kuroki).Face().SVars, "TrigReveal")
	if sa == nil || sa.Params["UnlessPayer"] != "Player.targetedBy" || sa.Params["UnlessCost"] != "Draw<4/Player.targetedBy>" {
		t.Fatalf("Kuroki TrigReveal = %+v, want the targeted unless-draw shape", sa)
	}
	e.emit(events.Event{Kind: events.TriggerPush, Obj: kuroki, Player: 0, Amount: 0})
	ability := e.G.Stack[len(e.G.Stack)-1]
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: ability, Player: 1, Amount: 1})
	kr9ResolveTop(e)
	if d := e.Pending(); d == nil || d.ResumeKind != "unless_pay" || d.Player != 1 {
		t.Fatalf("pending = %+v, want the unless-pay ask for seat 1", d)
	}
	return e, kuroki
}

func TestUnlessDrawCostKurokiPayKernel(t *testing.T) {
	t.Parallel()
	e, kuroki := kr9DriveKurokiTrig(t)
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "unless_pay" {
		t.Fatalf("pending = %+v, want the unless-pay ask", d)
	}
	if d.Player != 1 {
		t.Fatalf("payer = seat %d, want the targeted opponent seat 1", d.Player)
	}
	before := countDraw(e)
	answerUnlessPay(t, e, true)
	if got := countDraw(e) - before; got != 4 {
		t.Fatalf("pay drew %d cards for the opponent, want 4", got)
	}
	if o := e.G.Obj(kuroki); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Kuroki zone = %v, want kept", o)
	}
}

func TestUnlessDrawCostKurokiDeclineKernel(t *testing.T) {
	t.Parallel()
	e, _ := kr9DriveKurokiTrig(t)
	before := countDraw(e)
	answerUnlessPay(t, e, false)
	if got := countDraw(e) - before; got != 0 {
		t.Fatalf("decline drew %d cards, want 0", got)
	}
}

func TestUnlessCostTresserhornPaysSacLifeAndDrawKernel(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 742)
	lord := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Tresserhorn's Lord, Returned"))
	creatures := []state.ObjID{
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears")),
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Goblin Piledriver")),
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears")),
	}
	life := e.G.Players[0].Life

	// Forge spells this carrier as ValidTarget$ rather than ValidTgts$, so
	// target offering is a separate gap. Seed the ordinary stack target event
	// to exercise the real corpus SA with the target binding it requires.
	e.emit(events.Event{Kind: events.TriggerPush, Obj: lord, Player: 0, Amount: 0})
	ability := e.G.Stack[len(e.G.Stack)-1]
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: ability, Player: 1, Amount: 1})
	kr9ResolveTop(e)
	answerUnlessPay(t, e, true)

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "unless_cost" || d.Min != 3 || d.Max != 3 {
		t.Fatalf("pending = %+v, want exact three-creature cost choice", d)
	}
	choices := make([]int, 0, len(creatures))
	for _, want := range creatures {
		found := -1
		for _, o := range d.Options {
			if o.Obj == want {
				found = o.Index
			}
		}
		if found < 0 {
			t.Fatalf("creature %d absent from sacrifice-cost options: %+v", want, d.Options)
		}
		choices = append(choices, found)
	}
	before := countDraw(e)
	submitChoices(t, e, choices...)

	for _, id := range creatures {
		if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
			t.Fatalf("cost creature %d zone = %v, want graveyard", id, got)
		}
	}
	if got := e.G.Players[0].Life; got != life-3 {
		t.Fatalf("payer life = %d, want %d", got, life-3)
	}
	if got := countDraw(e) - before; got != 3 {
		t.Fatalf("targeted opponent drew %d cards, want 3", got)
	}
	// Forge's handleUnlessCost resolves the main body when paid ==
	// UnlessSwitched, so this script's switched Sacrifice body still puts the
	// Lord in the graveyard after the compound cost is paid.
	if got := e.G.Obj(lord).Zone; got != state.ZGraveyard {
		t.Fatalf("Lord zone = %v, want graveyard after the switched paid cost", got)
	}
}
