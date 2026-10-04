package rules

// Restores effects/optional_changezone_confirm_test.go on the kernel: an
// explicit Optional$ hand fetch (hand_move_confirm) and an Optional$ Hidden$
// public-origin pick (hidden_pick_confirm) ask "proceed?" BEFORE any card is
// picked; a decline poses no pick and moves nothing, an accept enters the
// Min-0 pick (which may still take nothing), an empty eligible pool still
// confirms, and ChoiceOptional$ / Mandatory$ / a markerless text-may stay
// confirmation-free. Yuna's Decision is the real corpus carrier. (The no-host
// R-9 leaves stay in effects/optional_changezone_confirm_kernel_test.go.)

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

const (
	kr2HandOptional   = "A:SP$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ Land | ChangeNum$ 1 | Optional$ True"
	kr2HiddenOptional = "A:SP$ ChangeZone | Hidden$ True | Origin$ Graveyard | Destination$ Battlefield | ChangeType$ Creature | ChangeNum$ 1 | Optional$ True"
	kr2IsleSrc        = "Name:Isle\nTypes:Basic Land Island\nOracle:x\n"
	kr2BearSrc        = "Name:Grizzly Bears\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
)

// kr2HandFetch seats Bear, Isle, Isle in seat 0's hand and casts line.
func kr2HandFetch(t *testing.T, line string) (*Engine, []state.ObjID, *decision.Decision) {
	t.Helper()
	e := kr2Engine(t, 2)
	ids := []state.ObjID{
		kr2Put(t, e, 0, kr2Src(t, kr2BearSrc), state.ZHand, false),
		kr2Put(t, e, 0, kr2Src(t, kr2IsleSrc), state.ZHand, false),
		kr2Put(t, e, 0, kr2Src(t, kr2IsleSrc), state.ZHand, false),
	}
	spell := kr2Put(t, e, 0, kr2Sorcery(t, "Fetcher", line), state.ZHand, false)
	return e, ids, kr2Cast(t, e, 0, spell)
}

func kr2RequireConfirm(t *testing.T, d *decision.Decision, resume string) {
	t.Helper()
	kr2Want(t, d, resume)
	if d.Player != 0 || d.Kind != decision.KChoose || d.Min != 1 || d.Max != 1 ||
		len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
		t.Fatalf("confirm = %+v, want a 1-of yes/no %s gate for seat 0", d, resume)
	}
}

func kr2RequireZones(t *testing.T, e *Engine, ids []state.ObjID, z state.Zone, what string) {
	t.Helper()
	for i, id := range ids {
		if got := e.G.Obj(id).Zone; got != z {
			t.Fatalf("%s: ids[%d] is on %s, want %s", what, i, got, z)
		}
	}
}

func TestOptionalChangeZoneHandConfirm(t *testing.T) {
	t.Parallel()
	t.Run("gate then decline", func(t *testing.T) {
		t.Parallel()
		e, ids, d := kr2HandFetch(t, kr2HandOptional)
		kr2RequireConfirm(t, d, "hand_move_confirm")
		kr2RequireZones(t, e, ids, state.ZHand, "the confirmation itself")
		if d = kr2Answer(t, e, d, kr2Kind(t, d, "no")); d != nil {
			t.Fatalf("a declined confirmation posed %+v", d)
		}
		kr2RequireZones(t, e, ids, state.ZHand, "decline")
	})
	t.Run("accept then pick", func(t *testing.T) {
		t.Parallel()
		e, ids, d := kr2HandFetch(t, kr2HandOptional)
		d = kr2Want(t, kr2Answer(t, e, d, kr2Kind(t, d, "yes")), "hand_move")
		if d.Min != 0 || d.Max != 1 || len(d.Options) != 2 || d.Options[0].Obj != ids[1] || d.Options[1].Obj != ids[2] {
			t.Fatalf("pick = %+v, want a Min 0 hand_move over the two Isles", d)
		}
		if d = kr2Answer(t, e, d, kr2ObjIdx(t, d, ids[2])); d != nil {
			t.Fatalf("unexpected ask: %+v", d)
		}
		kr2RequireZones(t, e, ids[2:], state.ZBattlefield, "answered isle")
		kr2RequireZones(t, e, ids[:2], state.ZHand, "unchosen cards")
	})
	t.Run("accept then pick none", func(t *testing.T) {
		t.Parallel()
		e, ids, d := kr2HandFetch(t, kr2HandOptional)
		d = kr2Want(t, kr2Answer(t, e, d, kr2Kind(t, d, "yes")), "hand_move")
		if d = kr2Answer(t, e, d); d != nil {
			t.Fatalf("unexpected ask: %+v", d)
		}
		kr2RequireZones(t, e, ids, state.ZHand, "accept-then-none")
	})
	t.Run("markerless may stays confirmation-free", func(t *testing.T) {
		t.Parallel()
		_, _, d := kr2HandFetch(t, "A:SP$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ Land | ChangeNum$ 1 | SpellDescription$ You may put a land card from your hand onto the battlefield.")
		if kr2Want(t, d, "hand_move"); d.Min != 0 || d.Max != 1 {
			t.Fatalf("markerless first ask = %+v, want the Min 0 hand_move pick", d)
		}
	})
}

func TestYunasDecisionFetchConfirmsThenOffersEachMember(t *testing.T) {
	t.Parallel()
	e := kr2Engine(t, 2)
	fodder := kr2Put(t, e, 0, kr2Src(t, "Name:Fodder\n"+kr2CreatureSrc), state.ZBattlefield, false)
	bear := kr2Put(t, e, 0, kr2Src(t, kr2BearSrc), state.ZHand, false)
	isle := kr2Put(t, e, 0, kr2Src(t, kr2IsleSrc), state.ZHand, false)
	yuna := kr2Put(t, e, 0, kr2Corpus(t, "Yuna's Decision"), state.ZHand, false)
	d := kr2Cast(t, e, 0, yuna)
	for i := 0; d != nil && d.ResumeKind != "hand_move_confirm" && i < 6; i++ {
		switch {
		case d.Kind == decision.KModes:
			d = kr2Answer(t, e, d, d.Options[0].Index) // Continue the Pilgrimage
		case d.ResumeKind == "sacrifice" || d.Kind == decision.KChoose:
			d = kr2Answer(t, e, d, kr2ObjIdx(t, d, fodder))
		default:
			t.Fatalf("unexpected decision %+v", d)
		}
	}
	kr2RequireConfirm(t, d, "hand_move_confirm")
	if z := e.G.Obj(fodder).Zone; z != state.ZGraveyard {
		t.Fatalf("the sacrificed creature is on %s, want graveyard", z)
	}
	d = kr2Want(t, kr2Answer(t, e, d, kr2Kind(t, d, "yes")), "hand_move")
	kr2ObjIdx(t, d, bear)
	kr2ObjIdx(t, d, isle)
}

// kr2HiddenFetch seats Bear and Isle in seat 0's graveyard (just Isle when
// noCreature) and casts line.
func kr2HiddenFetch(t *testing.T, line string, noCreature bool) (*Engine, state.ObjID, state.ObjID, *decision.Decision) {
	t.Helper()
	e := kr2Engine(t, 2)
	var bear state.ObjID
	if !noCreature {
		bear = kr2Put(t, e, 0, kr2Src(t, kr2BearSrc), state.ZGraveyard, false)
	}
	isle := kr2Put(t, e, 0, kr2Src(t, kr2IsleSrc), state.ZGraveyard, false)
	spell := kr2Put(t, e, 0, kr2Sorcery(t, "Reanimator", line), state.ZHand, false)
	return e, bear, isle, kr2Cast(t, e, 0, spell)
}

func TestOptionalChangeZoneHiddenPickConfirm(t *testing.T) {
	t.Parallel()
	t.Run("gate then decline", func(t *testing.T) {
		t.Parallel()
		e, bear, _, d := kr2HiddenFetch(t, kr2HiddenOptional, false)
		kr2RequireConfirm(t, d, "hidden_pick_confirm")
		kr2RequireZones(t, e, []state.ObjID{bear}, state.ZGraveyard, "the confirmation itself")
		if d = kr2Answer(t, e, d, kr2Kind(t, d, "no")); d != nil {
			t.Fatalf("a declined confirmation posed %+v", d)
		}
		kr2RequireZones(t, e, []state.ObjID{bear}, state.ZGraveyard, "decline")
	})
	t.Run("accept picks the eligible card", func(t *testing.T) {
		t.Parallel()
		e, bear, isle, d := kr2HiddenFetch(t, kr2HiddenOptional, false)
		d = kr2Want(t, kr2Answer(t, e, d, kr2Kind(t, d, "yes")), "hidden_pick")
		if d.Min != 0 || d.Max != 1 || len(d.Options) != 1 || d.Options[0].Obj != bear {
			t.Fatalf("pick = %+v, want a Min 0 hidden_pick whose only option is the bear", d)
		}
		if d = kr2Answer(t, e, d, 0); d != nil {
			t.Fatalf("unexpected ask: %+v", d)
		}
		kr2RequireZones(t, e, []state.ObjID{bear}, state.ZBattlefield, "answered creature")
		kr2RequireZones(t, e, []state.ObjID{isle}, state.ZGraveyard, "the land")
	})
	t.Run("accept then pick none", func(t *testing.T) {
		t.Parallel()
		e, bear, _, d := kr2HiddenFetch(t, kr2HiddenOptional, false)
		d = kr2Want(t, kr2Answer(t, e, d, kr2Kind(t, d, "yes")), "hidden_pick")
		if d = kr2Answer(t, e, d); d != nil {
			t.Fatalf("unexpected ask: %+v", d)
		}
		kr2RequireZones(t, e, []state.ObjID{bear}, state.ZGraveyard, "accept-then-none")
	})
	for _, accept := range []bool{false, true} {
		name := "empty pool still confirms, decline"
		if accept {
			name = "empty pool still confirms, accept"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e, _, isle, d := kr2HiddenFetch(t, kr2HiddenOptional, true)
			kr2RequireConfirm(t, d, "hidden_pick_confirm")
			choice := kr2Kind(t, d, "no")
			if accept {
				choice = kr2Kind(t, d, "yes")
			}
			if d = kr2Answer(t, e, d, choice); d != nil {
				t.Fatalf("an empty-pool fetch posed %+v after the confirmation", d)
			}
			kr2RequireZones(t, e, []state.ObjID{isle}, state.ZGraveyard, "the land")
		})
	}
	t.Run("ChoiceOptional$ does not confirm", func(t *testing.T) {
		t.Parallel()
		_, _, _, d := kr2HiddenFetch(t, "A:SP$ ChangeZone | Hidden$ True | Origin$ Graveyard | Destination$ Battlefield | ChangeType$ Creature | ChangeNum$ 1 | ChoiceOptional$ True", false)
		if kr2Want(t, d, "hidden_pick"); d.Min != 0 || d.Max != 1 {
			t.Fatalf("ChoiceOptional$ first ask = %+v, want the Min 0 hidden_pick", d)
		}
	})
	t.Run("Mandatory$ does not confirm", func(t *testing.T) {
		t.Parallel()
		_, _, _, d := kr2HiddenFetch(t, "A:SP$ ChangeZone | Hidden$ True | Origin$ Graveyard | Destination$ Battlefield | ChangeType$ Creature | ChangeNum$ 1 | Mandatory$ True", false)
		if kr2Want(t, d, "hidden_pick"); d.Min != 1 || d.Max != 1 {
			t.Fatalf("Mandatory$ first ask = %+v, want the Min 1 hidden_pick", d)
		}
	})
}
