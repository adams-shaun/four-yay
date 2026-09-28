package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestLlanowarRebornGraftEntersWithCounterAndMayMoveIt(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, answer := range []int{0, 1} {
		answer := answer
		t.Run(map[int]string{0: "yes", 1: "no"}[answer], func(t *testing.T) {
			e, cfg := proliferateEngine(t, reg, "Llanowar Reborn", "Grizzly Bears")
			land := putNamedOnBattlefield(t, e, "Llanowar Reborn")
			if o := e.G.Obj(land); o == nil || o.Zone != state.ZBattlefield {
				t.Fatalf("Llanowar Reborn = %+v, want battlefield", o)
			}
			if got := e.G.Obj(land).Counter("P1P1"); got != 1 {
				t.Fatalf("Llanowar Reborn P1P1 = %d, want 1 (Graft 1)", got)
			}

			creature := putNamedOnBattlefield(t, e, "Grizzly Bears")
			if o := e.G.Obj(creature); o == nil || o.Zone != state.ZBattlefield {
				t.Fatalf("Grizzly Bears = %+v, want battlefield", o)
			}
			d := passUntilNonPriority(t, e, 60)
			if d == nil || d.Kind != decision.KTriggerOptional {
				t.Fatalf("decision = %+v, want optional Graft trigger", d)
			}
			if d.Source != land {
				t.Fatalf("optional trigger source = %d, want Llanowar Reborn %d", d.Source, land)
			}
			submitChoices(t, e, answer)
			passUntilStackEmpty(t, e, 60)

			wantLand, wantCreature := int32(1), int32(0)
			if answer == 0 {
				wantLand, wantCreature = 0, 1
			}
			if got := e.G.Obj(land).Counter("P1P1"); got != wantLand {
				t.Fatalf("Llanowar Reborn P1P1 = %d, want %d after answer %d", got, wantLand, answer)
			}
			if got := e.G.Obj(creature).Counter("P1P1"); got != wantCreature {
				t.Fatalf("Grizzly Bears P1P1 = %d, want %d after answer %d", got, wantCreature, answer)
			}
			replayCheck(t, e, cfg)
		})
	}
}
