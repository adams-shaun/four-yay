package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestMindslaverControlPlayerPrintedAbility pins CR 720 through Mindslaver's
// printed {4}, tap, sacrifice-self activated ability, not a synthetic API.
func TestMindslaverControlPlayerPrintedAbility(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	card := lookup(t, reg, "Mindslaver")
	e, cfg := corpusEngineCfg(t, reg, []*cards.Card{card}, nil)
	id := moveByName(t, e, 0, "Mindslaver", state.ZBattlefield)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: Mindslaver on seat 0 battlefield, got %+v", o)
	}
	addMana(t, e, 0, "CCCC")
	e.Advance()

	opt := abilityOption(t, e, id, 0)
	submitChoices(t, e, opt.Index)
	// CR 601/activation flow chooses targets before completing costs. Pay the
	// printed sacrifice cost when its chooser follows the target answer.
	sawTarget, paidSacrifice := false, false
	for i := 0; i < 10 && (!sawTarget || !paidSacrifice); i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		switch d.Kind {
		case decision.KTarget:
			idx := indexOfPlayerOption(d, 1)
			if idx < 0 {
				t.Fatalf("Mindslaver target ask omitted seat 1: %+v", d.Options)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("target seat 1: %v", err)
			}
			sawTarget = true
		case decision.KChoose:
			if len(d.Options) != 1 || d.Options[0].Kind != "sacrifice" || d.Options[0].Obj != id {
				t.Fatalf("Mindslaver sacrifice-cost ask = %+v", d)
			}
			submitChoices(t, e, d.Options[0].Index)
			paidSacrifice = true
		case decision.KPriority:
			// With a single legal sacrifice, the cost walker pays it without
			// presenting a redundant choice; priority means activation is on-stack.
			paidSacrifice = e.G.Obj(id).Zone == state.ZGraveyard
		default:
			t.Fatalf("unexpected decision while activating Mindslaver: %+v", d)
		}
	}
	if !sawTarget || !paidSacrifice {
		t.Fatalf("activation did not complete both target and sacrifice cost: target=%v sacrifice=%v pending=%+v", sawTarget, paidSacrifice, e.Pending())
	}
	if e.G.Obj(id).Zone != state.ZGraveyard {
		t.Fatalf("Mindslaver sacrifice cost left it in %s", e.G.Obj(id).Zone)
	}
	sawTap := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Tap && ev.Obj == id {
			sawTap = true
			break
		}
	}
	if !sawTap {
		t.Fatal("Mindslaver's printed tap cost did not emit a Tap event")
	}
	passUntilStackEmpty(t, e, 20)

	if ctl, ok := e.G.ControlledBy[1]; !ok || ctl != 0 {
		t.Fatalf("after printed Mindslaver resolves ControlledBy = %+v, want seat 1 controlled by 0", e.G.ControlledBy)
	}
	driveToTurn(t, e, 2, 1)
	if e.G.Active != 1 {
		t.Fatalf("precondition: reached controlled seat 1's next turn, Active=%d", e.G.Active)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("seat 1's opening priority = %+v, want controller seat 0", d)
	}
	driveToTurn(t, e, 3, 0)
	if len(e.G.ControlledBy) != 0 {
		t.Fatalf("Mindslaver control outlived seat 1's next turn: %+v", e.G.ControlledBy)
	}
	replayCheck(t, e, cfg)
}
