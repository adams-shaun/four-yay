package rules

// Restores effects/library_continuations_test.go on the kernel: a
// library-walking effect over `Defined$ Player` (search, rearrange, scry)
// must move on to the NEXT player's library once the first library's ask is
// answered, posing that library's own ask over that library's cards.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func TestLibraryWalkContinuesWithTheNextLibraryAfterAnAnswer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, line string
		kind       decision.Kind
		resume     string
	}{
		{"search", "A:SP$ ChangeZone | Defined$ Player | Origin$ Library | Destination$ Hand | ChangeType$ Creature", decision.KChoose, "search"},
		{"rearrange", "A:SP$ RearrangeTopOfLibrary | Defined$ Player | NumCards$ 1", decision.KArrange, ""},
		{"scry", "A:SP$ Scry | Defined$ Player | ScryNum$ 1", decision.KArrange, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := kr2Engine(t, 2)
			var tops [2]state.ObjID
			for p := state.PlayerID(0); p < 2; p++ {
				tops[p] = kr2Put(t, e, p, kr2Src(t, "Name:Lib Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), state.ZLibrary, true)
			}
			spell := kr2Put(t, e, 0, kr2Sorcery(t, "Walker", tc.line), state.ZHand, false)
			d := kr2Cast(t, e, 0, spell)
			if d == nil || d.Kind != tc.kind || (tc.resume != "" && d.ResumeKind != tc.resume) {
				t.Fatalf("first decision = %+v, want a %s %q ask over seat 0's library", d, tc.kind, tc.resume)
			}
			if d.Options[0].Obj != tops[0] {
				t.Fatalf("first ask options = %+v, want seat 0's top card %d first", d.Options, tops[0])
			}
			d = kr2Answer(t, e, d, d.Options[0].Index)
			if d == nil || d.Kind != tc.kind || (tc.resume != "" && d.ResumeKind != tc.resume) {
				t.Fatalf("after answering seat 0's library, decision = %+v, want the next library's %s ask", d, tc.kind)
			}
			if d.Options[0].Obj != tops[1] {
				t.Fatalf("second ask options = %+v, want seat 1's top card %d", d.Options, tops[1])
			}
			for _, o := range d.Options {
				if e.G.Obj(o.Obj).Owner != 1 {
					t.Fatalf("second ask offered %d owned by seat %d, want only seat 1's library", o.Obj, e.G.Obj(o.Obj).Owner)
				}
			}
			if d = kr2Answer(t, e, d, d.Options[0].Index); d != nil {
				t.Fatalf("a third ask followed the two libraries: %+v", d)
			}
			if tc.name == "search" {
				for p := 0; p < 2; p++ {
					if z := e.G.Obj(tops[p]).Zone; z != state.ZHand {
						t.Fatalf("seat %d's searched creature is on %s, want hand", p, z)
					}
				}
			}
		})
	}
}
