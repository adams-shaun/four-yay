package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestEachExplicitZeroChangeNumSelectsNothing guards against treating an
// explicit per-type ChangeNum$ 0 as the default one on any structured path.
func TestEachExplicitZeroChangeNumSelectsNothing(t *testing.T) {
	t.Run("option_builder", func(t *testing.T) {
		h := newHost(t, 2)
		crea := h.g.AddObject(mkCard(t, "Name:ZeroCreature\nTypes:Creature Bear\nOracle:x\n"), 0)
		land := h.g.AddObject(mkCard(t, "Name:ZeroLand\nTypes:Land Forest\nOracle:x\n"), 0)
		groups := [][]state.ObjID{{crea.ID}, {land.ID}}
		d := &decision.Decision{}
		if got := eachStructuredOptions(h.g, d, groups, 0, false, 0, "search"); got != 0 {
			t.Fatalf("zero per-type ceiling = %d, want 0", got)
		}
		if len(d.Options) != 0 {
			t.Fatalf("explicit ChangeNum 0 produced options: %+v", d.Options)
		}
	})
	for _, tc := range []struct {
		name   string
		origin string
		params map[string]string
		zone   state.Zone
		to     state.Zone
	}{
		{name: "hand", origin: "Hand", params: map[string]string{"Destination": "Battlefield", "Optional": "True"}, zone: state.ZHand, to: state.ZBattlefield},
		{name: "library_search", origin: "Library", params: map[string]string{"Destination": "Hand"}, zone: state.ZLibrary, to: state.ZHand},
		{name: "hidden_pick", origin: "Graveyard", params: map[string]string{"Destination": "Hand", "Hidden": "True", "Optional": "True"}, zone: state.ZGraveyard, to: state.ZHand},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost(t, 2)
			src := h.g.AddObject(mkCard(t, "Name:ZeroProbe\nTypes:Sorcery\nOracle:x\n"), 0)
			crea := h.g.AddObject(mkCard(t, "Name:ZeroCreature\nTypes:Creature Bear\nOracle:x\n"), 0)
			land := h.g.AddObject(mkCard(t, "Name:ZeroLand\nTypes:Land Forest\nOracle:x\n"), 0)
			h.g.SetZone(tc.zone, 0, []state.ObjID{crea.ID, land.ID})
			eachSetZone(h.g, 0, tc.zone, crea.ID, land.ID)
			for _, id := range []state.ObjID{crea.ID, land.ID} {
				if o := h.g.Obj(id); o == nil || o.Zone != tc.zone {
					t.Fatalf("precondition failed: candidate %d is not in %s", id, tc.zone)
				}
			}
			specCtx := (&Ctx{Source: src.ID, Controller: 0}).SpecContext(0)
			if !MatchesSpecCtx(h.g, "Creature", crea.ID, specCtx) || !MatchesSpecCtx(h.g, "Land", land.ID, specCtx) {
				t.Fatal("precondition failed: the test candidates do not match their respective EACH sub-specs")
			}
			params := map[string]string{"Origin": tc.origin, "ChangeType": "EACH Creature & Land", "ChangeNum": "0"}
			for k, v := range tc.params {
				params[k] = v
			}
			sh := &suspendHost{fakeHost: *h}
			Resolve(sh, &Ctx{Source: src.ID, Controller: 0}, eachSyntheticChangeZone(params))
			if tc.name != "library_search" {
				// The Optional$ marker on these two subcases poses the
				// confirm-before-pick gate even over an empty ChangeNum$ 0
				// pick; answer yes, and the entered fetch completes with no
				// further ask and no movement (Forge's gate runs before the
				// fetch list is consulted).
				if sh.asked == nil || sh.asked.ResumeKind != "hand_move_confirm" && sh.asked.ResumeKind != "hidden_pick_confirm" {
					t.Fatalf("zero-count EACH first ask = %+v, want the Optional$ confirmation gate", sh.asked)
				}
				sh.suspended = false
				sh.asked = nil
				Resolve(sh, &Ctx{Source: src.ID, Controller: 0,
					HandMoveConfirmDone: tc.name == "hand", HandMoveConfirm: "yes", HandMoveConfirmTarget: 0,
					HiddenPickConfirmDone: tc.name == "hidden_pick", HiddenPickConfirm: "yes", HiddenPickConfirmTarget: 0},
					eachSyntheticChangeZone(params))
			}
			if sh.asked != nil {
				t.Fatalf("zero-count EACH unexpectedly posed a choice: min=%d max=%d options=%+v", sh.asked.Min, sh.asked.Max, sh.asked.Options)
			}
			for _, ev := range sh.log {
				if ev.Kind == events.MoveZone && ev.Obj != src.ID && ev.To == tc.to {
					t.Fatalf("explicit ChangeNum 0 moved candidate %d to %s", ev.Obj, tc.to)
				}
				if ev.Kind == events.Note && strings.Contains(ev.Text, "unimplemented API ChangeZone") {
					t.Fatalf("ChangeZone handler did not run: %s", ev.Text)
				}
			}
			for _, id := range []state.ObjID{crea.ID, land.ID} {
				if got := h.g.Obj(id).Zone; got != tc.zone {
					t.Fatalf("candidate %d ended in %s, want it to remain in %s", id, got, tc.zone)
				}
			}
		})
	}
}
