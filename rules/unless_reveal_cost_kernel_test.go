package rules

import (
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Priest of the Wakening Sun's SWITCHED unless reveal: paying causes the
// body (2 life, one public reveal), declining runs nothing.

func kr9DrivePriestTrigger(t *testing.T, dinos int) (*Engine, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e := stealEngine(t, 743)
	priest := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Priest of the Wakening Sun"))
	sa := cards.ResolveSVar(e.G.Obj(priest).Face().SVars, "ABGainLife")
	if sa == nil || sa.Params["UnlessCost"] != "Reveal<1/Creature.Dinosaur>" || sa.Params["UnlessPayer"] != "You" || sa.Params["UnlessSwitched"] != "True" {
		t.Fatalf("Priest ABGainLife = %+v, want the switched reveal shape", sa)
	}
	// Seed the reveal candidates BEFORE the ask is posed: the option list is
	// built at pose time from the hand, so a card injected afterwards would
	// not (and should not) change what was offered.
	dinoNames := []string{"Rexy", "Brutus", "Cera"}
	for i := 0; i < dinos; i++ {
		name := dinoNames[i%len(dinoNames)]
		dino := card(t, fmt.Sprintf("Name:%s\nTypes:Creature Dinosaur\nPT:3/3\nOracle:x\n", name))
		o := e.G.AddObject(dino, 0)
		o.Zone = state.ZHand
		e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), o.ID))
	}
	e.emit(events.Event{Kind: events.TriggerPush, Obj: priest, Player: 0, Amount: 0})
	kr9ResolveTop(e)
	// OptionalDecider$ You makes the trigger itself an optional election;
	// accept it so the unless-pay gate on the body is reached.
	if d := e.Pending(); d != nil && d.Kind == decision.KTriggerOptional && d.ResumeKind == "optional" {
		submitChoices(t, e, d.Options[0].Index) // yes
	}
	if d := e.Pending(); d == nil || d.ResumeKind != "unless_pay" || d.Player != 0 {
		t.Fatalf("pending = %+v, want the unless-pay ask for seat 0", d)
	}
	return e, priest
}

func TestUnlessRevealPriestSwitchedPayGainsLifeKernel(t *testing.T) {
	t.Parallel()
	e, priest := kr9DrivePriestTrigger(t, 2)
	before := e.G.Players[0].Life
	answerUnlessPay(t, e, true)

	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "unless_cost" {
		t.Fatalf("pending = %+v, want the unless_cost reveal KChoose", d)
	}
	for _, o := range d.Options {
		if o.Kind != "revealcost" {
			t.Fatalf("option kind = %q, want revealcost", o.Kind)
		}
	}
	if len(d.Options) != 2 {
		t.Fatalf("reveal options = %+v, want both Dinosaurs", d.Options)
	}
	chosenID := d.Options[1].Obj
	submitChoices(t, e, d.Options[1].Index)
	if got := e.G.Players[0].Life; got != before+2 {
		t.Fatalf("life = %d, want %d (the switched body ran on pay)", got, before+2)
	}
	notes := revealNotes(e)
	if len(notes) != 1 || len(notes[0].IDs) != 1 || notes[0].IDs[0] != chosenID {
		t.Fatalf("reveal Notes = %+v, want exactly one with the chosen Dinosaur's id %d", notes, chosenID)
	}
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(id).Face().Name == "Rexy" || e.G.Obj(id).Face().Name == "Brutus" {
			if got := e.G.Obj(id).Zone; got != state.ZHand {
				t.Fatalf("revealed card %s left the hand: zone = %s", e.G.Obj(id).Face().Name, got)
			}
		}
	}
	if o := e.G.Obj(priest); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Priest zone = %+v, want kept", o)
	}
}

func TestUnlessRevealPriestSwitchedDeclineRunsNoBodyKernel(t *testing.T) {
	t.Parallel()
	e, _ := kr9DrivePriestTrigger(t, 1)
	before := e.G.Players[0].Life
	answerUnlessPay(t, e, false)
	if got := e.G.Players[0].Life; got != before {
		t.Fatalf("life = %d, want unchanged (%d): the decline must not pay", got, before)
	}
	if notes := revealNotes(e); len(notes) != 0 {
		t.Fatalf("reveal Notes = %+v, want none on decline", notes)
	}
}
