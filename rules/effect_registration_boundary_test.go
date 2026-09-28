package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Each delivery route must use exactly the same affected-object convention,
// including the signed forms Num accepts. Other SVar names remain grantor-bound.
func TestEffectStaticAffectedXParity(t *testing.T) {
	t.Parallel()
	for _, expr := range []string{"AffectedX", "+AffectedX", "-AffectedX", " Other ", "Count$Valid Creature"} {
		want := strings.TrimLeft(strings.TrimSpace(expr), "+-") == "AffectedX"
		if got := effects.AffectedXStaticAmount(expr); got != want {
			t.Errorf("%q: affected-object flag %v, want %v", expr, got, want)
		}
	}
}

func TestEffectContinuousMixedUnreadFailsClosed(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	src := onBoard(t, e, 0, "Name:Grantor\nTypes:Creature\nPT:2/2\nA:AB$ Effect | StaticAbilities$ Gift\nSVar:Gift:Mode$ Continuous | Affected$ Creature.YouCtrl | AddKeyword$ Flying | AddHiddenKeyword$ Shroud\nOracle:x\n")
	other := onBoard(t, e, 0, "Name:Recipient\nTypes:Creature\nPT:2/2\nOracle:x\n")
	if e.G.Obj(src).Zone != state.ZBattlefield || e.G.Obj(other).Zone != state.ZBattlefield || e.HasKeyword(other, "Flying") {
		t.Fatal("precondition: source/recipient zones or keyword invalid")
	}
	face := e.G.Obj(src).Face()
	effects.Resolve(e, &effects.Ctx{Source: src, Controller: 0, SVars: face.SVars}, face.Abilities[0])
	if e.HasKeyword(other, "Flying") {
		t.Fatal("partial grant installed Flying despite unread AddHiddenKeyword")
	}
	if len(e.continuous) != 0 {
		t.Fatalf("partial grant registered: %+v", e.continuous)
	}
	if len(effectNotesContaining(e, "continuous effect Continuous unimplemented")) == 0 {
		t.Fatalf("missing loud rejection for unread parameter: %v", effectNoteTexts(e))
	}
}

func TestEffectTriggerExpiryAndUnsupportedLifetime(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, mode, duration string }{
		{"event expires", "SpellCast", ""},
		{"phase expires", "Phase", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := layerEngine(t)
			body := "Mode$ " + tc.mode + " | Execute$ Pain"
			if tc.mode == "Phase" {
				body += " | Phase$ End of Turn | ValidPlayer$ You"
			}
			src := onBoard(t, e, 0, "Name:Grantor\nTypes:Creature\nPT:2/2\nA:AB$ Effect | Triggers$ Hook | Duration$ "+tc.duration+"\nSVar:Hook:"+body+"\nSVar:Pain:DB$ LoseLife | Defined$ You | LifeAmount$ 2\nOracle:x\n")
			if o := e.G.Obj(src); o == nil || o.Zone != state.ZBattlefield {
				t.Fatalf("precondition: source not on battlefield: %+v", o)
			}
			face := e.G.Obj(src).Face()
			effects.Resolve(e, &effects.Ctx{Source: src, Controller: 0, SVars: face.SVars}, face.Abilities[0])
			if len(e.G.Delayed) != 1 {
				t.Fatalf("precondition: no delayed registration: %+v notes=%v", e.G.Delayed, effectNoteTexts(e))
			}
			dt := e.G.Delayed[0]
			if dt.MaxTurn != e.G.Turn || dt.MaxTurn <= 0 {
				t.Fatalf("expiry = %d, registering turn = %d", dt.MaxTurn, e.G.Turn)
			}
			if tc.mode == "Phase" && dt.ValidPlayer != "You" {
				t.Fatalf("phase player gate = %q, want You", dt.ValidPlayer)
			}
			before := e.G.Players[0].Life
			e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
			if tc.mode == "Phase" {
				e.emit(events.Event{Kind: events.StepChange, Step: state.StepEnd})
			} else {
				// The turn ceiling is checked before any SpellCast matching;
				// the registration's positive MaxTurn is asserted above.
				e.checkEventDelayedTriggers(events.Event{Kind: events.PutOnStack, Obj: src, Player: 0}, nil)
			}
			if len(e.pendingTriggers) != 0 || e.G.Players[0].Life != before {
				t.Fatalf("expired promise fired: pending=%+v life=%d (was %d)", e.pendingTriggers, e.G.Players[0].Life, before)
			}
		})
	}
}
