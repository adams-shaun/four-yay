package rules

// Kernel-era restorations of effects/scry_optional_test.go and
// effects/scry_surveil_test.go's multi-player surveil pin (deleted with the
// W3 legacy removal).

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestOptionalScryAsksBeforeLookingAndDeclineSkips: an Optional$ Scry asks
// its scry_optional election before looking at anything; declining scries
// nothing (no arrange is posed, the library order never changes).
func TestOptionalScryAsksBeforeLookingAndDeclineSkips(t *testing.T) {
	t.Parallel()
	spell := "Name:Maybe Scry\nManaCost:U\nTypes:Sorcery\nA:SP$ Scry | Defined$ You | ScryNum$ 1 | Optional$ True\nOracle:x\n"
	for i, accept := range []bool{false, true} {
		name := "decline"
		if accept {
			name = "accept"
		}
		t.Run(name, func(t *testing.T) {
			e, cfg := kr3Game(t, uint64(121+i), kr3Cards(t, spell), nil)
			kr3Move(t, e, 0, "Maybe Scry", state.ZHand)
			lib := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
			mark := len(e.L.Events)
			d := kr3Cast(t, e, "Maybe Scry", "U", -1)
			if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "scry_optional" {
				t.Fatalf("decision = %+v, want the optional scry election", d)
			}
			if n := kr3Count(e, mark, events.LibraryOrder) + kr3Count(e, mark, events.Note); n != 0 {
				t.Fatalf("the scry looked before the election (%d events)", n)
			}
			if !accept {
				if d := kr3Answer(t, e, kr3OptionKind(t, d, "no")); d != nil {
					t.Fatalf("a declined scry posed %+v", d)
				}
				if kr3Count(e, mark, events.LibraryOrder) != 0 || !sameObjIDs(lib, e.G.Zone(state.ZLibrary, 0)) {
					t.Fatal("a declined scry changed the library")
				}
			} else {
				d = kr3Answer(t, e, kr3OptionKind(t, d, "yes"))
				if d == nil || d.Kind != decision.KArrange || len(d.Options) != 1 || d.Options[0].Obj != lib[0] {
					t.Fatalf("an accepted scry posed %+v, want the arrange over the top card", d)
				}
				if d := kr3Answer(t, e); d != nil {
					t.Fatalf("unexpected follow-up %+v", d)
				}
				if got := e.G.Zone(state.ZLibrary, 0); got[len(got)-1] != lib[0] {
					t.Fatalf("the top card %d was not scried to the bottom", lib[0])
				}
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestSurveilMultiPlayerContinuesAndMarksEachLibrary: a `Defined$ Player`
// Surveil poses one KArrange per library, in seat order, and records its
// events.Surveil marker only when that library is reached -- exactly one per
// library, with no third ask after the last answer.
func TestSurveilMultiPlayerContinuesAndMarksEachLibrary(t *testing.T) {
	t.Parallel()
	spell := "Name:Everyone Surveils\nManaCost:U\nTypes:Sorcery\nA:SP$ Surveil | Defined$ Player | Amount$ 1\nOracle:x\n"
	e, cfg := kr3Game(t, 131, kr3Cards(t, spell), nil)
	kr3Move(t, e, 0, "Everyone Surveils", state.ZHand)
	mark := len(e.L.Events)
	d := kr3Cast(t, e, "Everyone Surveils", "U", -1)
	if d == nil || d.Kind != decision.KArrange || d.Player != 0 {
		t.Fatalf("first arrange = %+v, want seat 0's", d)
	}
	if n := kr3Count(e, mark, events.Surveil); n != 1 {
		t.Fatalf("after seat 0's ask, Surveil markers = %d, want 1", n)
	}
	top1 := e.G.Zone(state.ZLibrary, 1)[0]
	d = kr3Answer(t, e) // seat 0 bins its card
	if d == nil || d.Kind != decision.KArrange || d.Player != 1 || d.Options[0].Obj != top1 {
		t.Fatalf("second arrange = %+v, want seat 1's over its own top card", d)
	}
	if n := kr3Count(e, mark, events.Surveil); n != 2 {
		t.Fatalf("after seat 1's ask, Surveil markers = %d, want one per reached library", n)
	}
	seen := [2]bool{}
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Surveil && int(ev.Player) < len(seen) {
			seen[ev.Player] = true
		}
	}
	if !seen[0] || !seen[1] {
		t.Fatal("Surveil markers did not name both library owners")
	}
	if d := kr3Answer(t, e, 0); d != nil {
		t.Fatalf("a third ask after the last library: %+v", d)
	}
	if n := kr3Count(e, mark, events.Surveil); n != 2 {
		t.Fatalf("after both answers, Surveil markers = %d, want exactly one per library", n)
	}
	if kr3Zone(e, top1) != state.ZLibrary {
		t.Fatal("seat 1 kept its card on top, but it left the library")
	}
	replayCheck(t, e, cfg)
}
