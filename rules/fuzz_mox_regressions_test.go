package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// Regressions for Mox Diamond's optional discard (real corpus cards).
//
// "If Mox Diamond would enter, you may discard a land card instead. If you
// do, put Mox Diamond onto the battlefield. If you don't, put it into its
// owner's graveyard." The replacement body is `DB$ Discard | DiscardValid$
// Land | Mode$ TgtChoose | Optional$ True`. With a land in hand the engine
// posed only a Min 0 card pick whose every option was "Discard <land>": the
// decline existed solely as an empty submission, so no option ever offered
// "don't discard" and the graveyard branch was unreachable from the options
// a player was shown. An Optional$ TgtChoose discard now poses a yes/no
// may-discard election first; "no" discards nothing, "yes" poses the pick.

// moxToElection casts Mox Diamond from hand and passes priority until its
// entry replacement poses the may-discard election, which it returns.
func moxToElection(t *testing.T, e *Engine) (mox state.ObjID, d *decision.Decision) {
	t.Helper()
	mox = searchMoveByName(t, e, "Mox Diamond", state.ZHand)
	submitChoices(t, e, castOptionFor(t, e, mox).Index)
	for i := 0; i < 6; i++ {
		d = e.Pending()
		if d == nil {
			t.Fatal("no decision pending")
		}
		if d.Kind != decision.KPriority {
			break
		}
		submitPass(t, e)
	}
	if d.Kind != decision.KChoose || d.ResumeKind != "discard_may" || len(d.Options) != 2 ||
		d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
		t.Fatalf("Mox Diamond's entry posed %v %q %+v, want the yes/no may-discard election", d.Kind, d.ResumeKind, d.Options)
	}
	return mox, d
}

func moxHandPlains(e *Engine, p state.PlayerID) []state.ObjID {
	var out []state.ObjID
	for _, id := range e.G.Zone(state.ZHand, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Plains" {
			out = append(out, id)
		}
	}
	return out
}

// TestMoxDiamondDeclineDiscardGoesToGraveyard: declining the optional
// discard keeps every land in hand and puts Mox Diamond into the graveyard.
func TestMoxDiamondDeclineDiscardGoesToGraveyard(t *testing.T) {
	t.Parallel()
	e, cfg := b5Engine(t, "Mox Diamond")
	lands := moxHandPlains(e, 0)
	if len(lands) == 0 {
		t.Fatal("precondition: no land in the opening hand")
	}
	mox, _ := moxToElection(t, e)
	submitChoices(t, e, 1) // No — don't discard
	if z := e.G.Obj(mox).Zone; z != state.ZGraveyard {
		t.Fatalf("declined Mox Diamond rests in %v, want graveyard", z)
	}
	for _, id := range lands {
		if z := e.G.Obj(id).Zone; z != state.ZHand {
			t.Fatalf("land %d moved to %v after declining the discard, want hand", id, z)
		}
	}
	if len(e.G.Stack) != 0 {
		t.Fatalf("stack not empty after the decline: %d", len(e.G.Stack))
	}
	replayCheck(t, e, cfg)
}

// TestMoxDiamondAcceptDiscardEntersBattlefield: electing to discard poses
// the land pick (no zero-card answer any more); the picked land goes to the
// graveyard and Mox Diamond enters the battlefield.
func TestMoxDiamondAcceptDiscardEntersBattlefield(t *testing.T) {
	t.Parallel()
	e, cfg := b5Engine(t, "Mox Diamond")
	lands := moxHandPlains(e, 0)
	if len(lands) < 2 {
		t.Fatalf("precondition: want at least two lands in hand for a real pick, have %d", len(lands))
	}
	mox, _ := moxToElection(t, e)
	submitChoices(t, e, 0) // Yes — discard
	d := e.Pending()
	if d == nil || d.ResumeKind != "discard" || d.Min != 1 || d.Max != 1 {
		t.Fatalf("after electing to discard got %+v, want a Min 1 / Max 1 land pick", d)
	}
	for _, o := range d.Options {
		if o.Kind != "discard" || o.Obj == 0 {
			t.Fatalf("land pick carries a non-card option %+v", o)
		}
	}
	pick := d.Options[1]
	submitChoices(t, e, pick.Index)
	if z := e.G.Obj(mox).Zone; z != state.ZBattlefield {
		t.Fatalf("Mox Diamond rests in %v after discarding a land, want battlefield", z)
	}
	if z := e.G.Obj(pick.Obj).Zone; z != state.ZGraveyard {
		t.Fatalf("picked land rests in %v, want graveyard", z)
	}
	if got := len(moxHandPlains(e, 0)); got != len(lands)-1 {
		t.Fatalf("hand holds %d lands, want %d (exactly the picked one discarded)", got, len(lands)-1)
	}
	replayCheck(t, e, cfg)
}
