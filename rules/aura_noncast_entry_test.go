package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// CR 303.4g: "If an Aura is entering the battlefield and there is no legal
// object or player for it to enchant, the Aura remains in its current zone,
// unless that zone is the stack." CR 303.4f: an Aura put onto the battlefield
// without being cast has its controller CHOOSE what it enchants as it enters
// -- that is not targeting, so hexproof/shroud do not stop it.
//
// The fuzz-1003 defect: Restoration Seminar (a Paradigm Lesson) returning an
// Aura with no creature on the battlefield put the Aura onto the battlefield
// (firing its ETB trigger) and the CR 704.5m SBA then swept it to the
// graveyard.

const auraHexproofFoeSrc = "Name:Hexproof Foe\nManaCost:1 G\nTypes:Creature Elf\nPT:2/2\nK:Hexproof\nOracle:x\n"

func auraSeminarCast(t *testing.T, e *Engine, auraName string) (aura, seminar state.ObjID) {
	t.Helper()
	toMain1(t, e)
	for _, z := range []state.Zone{state.ZLibrary, state.ZHand} {
		for _, id := range e.G.Zone(z, 0) {
			if o := e.G.Obj(id); aura == 0 && o != nil && o.Face() != nil && o.Face().Name == auraName {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: state.ZGraveyard})
				e.pending = nil
				aura = id
			}
		}
	}
	if aura == 0 {
		t.Fatalf("%s not in seat 0's library or hand", auraName)
	}
	e.priorityRound()
	seminar = findAndMoveToHand(t, e, 0, "Restoration Seminar")
	addMana(t, e, 0, "WWWWWWW")
	submitChoices(t, e, castOptionFor(t, e, seminar).Index)
	answerTargetAsk(t, e, []state.ObjID{aura})
	return aura, seminar
}

// TestRestorationSeminarAuraWithNothingToEnchantStaysInGraveyard is the
// fuzz-1003 carrier with the real cards: nothing on the battlefield is a
// creature, so Kenrith's Transformation (Enchant creature, "When it enters,
// draw a card") must remain in the graveyard -- no battlefield entry, no ETB
// draw, no SBA trip.
func TestRestorationSeminarAuraWithNothingToEnchantStaysInGraveyard(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 1003, []string{"Restoration Seminar", "Kenrith's Transformation"}, nil, nil)
	aura, _ := auraSeminarCast(t, e, "Kenrith's Transformation")
	mark := len(e.L.Events)
	libBefore := len(e.G.Zone(state.ZLibrary, 0))
	passUntilStackEmpty(t, e, 20)

	if o := e.G.Obj(aura); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("aura zone = %v, want graveyard (CR 303.4g: it remains in its current zone)", zoneOf(o))
	}
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.MoveZone && ev.Obj == aura {
			t.Errorf("aura moved (%v -> %v, %q); CR 303.4g says it never leaves its zone", ev.From, ev.To, ev.Text)
		}
	}
	if got := len(e.G.Zone(state.ZLibrary, 0)); got != libBefore {
		t.Errorf("library %d -> %d: the Aura's ETB draw fired though it never entered", libBefore, got)
	}
	replayCheck(t, e, cfg)
}

// TestRestorationSeminarAuraEnchantsHexproofOpponentCreature pins CR 303.4f's
// non-targeting half: the only creature is an opponent's hexproof creature,
// which the Aura can still be put onto because choosing what it enchants as
// it enters is not targeting.
func TestRestorationSeminarAuraEnchantsHexproofOpponentCreature(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 1004, []string{"Restoration Seminar", "Pacifism"}, nil, []string{auraHexproofFoeSrc})
	foe := moveSeeded(t, e, 1, auraHexproofFoeSrc, state.ZBattlefield)
	aura, _ := auraSeminarCast(t, e, "Pacifism")
	passUntilStackEmpty(t, e, 20)

	o := e.G.Obj(aura)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("aura zone = %v, want battlefield (hexproof does not stop a non-target attach)", zoneOf(o))
	}
	if o.AttachedTo != foe {
		t.Fatalf("aura attached to %d, want the hexproof creature %d", o.AttachedTo, foe)
	}
	replayCheck(t, e, cfg)
}

// TestRestorationSeminarAuraChoiceIsAskedOfController pins CR 303.4f's
// chooser: with two legal creatures the Aura's controller is asked which one
// it enchants, and the ask is a choice, never a KTarget ask.
func TestRestorationSeminarAuraChoiceIsAskedOfController(t *testing.T) {
	t.Parallel()
	e, cfg, _ := altCostEngine(t, 1005, []string{"Restoration Seminar", "Pacifism"}, nil,
		[]string{auraHexproofFoeSrc, altDragonSrc})
	foe := moveSeeded(t, e, 1, auraHexproofFoeSrc, state.ZBattlefield)
	dragon := moveSeeded(t, e, 1, altDragonSrc, state.ZBattlefield)
	aura, _ := auraSeminarCast(t, e, "Pacifism")

	asked := false
	for i := 0; i < 40; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		if d.Kind == decision.KPriority {
			if len(e.G.Stack) == 0 {
				break
			}
			submitChoices(t, e, passIndex(t, d))
			continue
		}
		if d.Kind == decision.KTarget {
			t.Fatalf("the Aura's entry posed a KTarget ask (CR 303.4f: not targeting): %+v", d)
		}
		if d.Player != 0 {
			t.Fatalf("entry choice asked of seat %d, want the Aura's controller 0: %+v", d.Player, d)
		}
		idx := -1
		for _, opt := range d.Options {
			if opt.Obj == foe {
				idx = opt.Index
			}
		}
		if idx < 0 {
			t.Fatalf("hexproof creature %d not offered as a bearer: %+v", foe, d.Options)
		}
		hasDragon := false
		for _, opt := range d.Options {
			hasDragon = hasDragon || opt.Obj == dragon
		}
		if !hasDragon {
			t.Fatalf("second creature %d not offered as a bearer: %+v", dragon, d.Options)
		}
		asked = true
		submitChoices(t, e, idx)
	}
	if !asked {
		t.Fatal("no bearer choice was asked of the Aura's controller with two legal creatures (CR 303.4f)")
	}
	if o := e.G.Obj(aura); o == nil || o.Zone != state.ZBattlefield || o.AttachedTo != foe {
		t.Fatalf("aura = %+v, want on the battlefield attached to %d", o, foe)
	}
	replayCheck(t, e, cfg)
}
