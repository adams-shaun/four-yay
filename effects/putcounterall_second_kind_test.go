package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestPutCounterAllSecondKindDefaultsFilter(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		card, second string
	}{
		{"Sagu Pummeler", "Reach"},
		{"Kheru Goldkeeper", "Flying"},
	} {
		t.Run(tc.card, func(t *testing.T) {
			card, ok := reg.Lookup(tc.card)
			if !ok {
				t.Fatalf("corpus has no %s", tc.card)
			}
			var body *cards.SA
			for _, face := range card.Faces {
				for name := range face.SVars {
					sa := cards.ResolveSVar(face.SVars, name)
					if sa != nil && sa.API == "PutCounterAll" {
						body = sa
						break
					}
				}
			}
			if body == nil {
				t.Fatalf("precondition: %s has no compiled PutCounterAll body", tc.card)
			}
			if body.Params["ValidCards2"] != "" || body.Params["CounterType2"] == "" {
				t.Fatalf("precondition: unexpected second-kind parameters: %v", body.Params)
			}

			h := &fakeHost{}
			h.g = state.NewGame(names(2))
			bear := mkCard(t, "Name:Grizzly Bears\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
			obj := h.g.AddObject(bear, 0)
			obj.Zone = state.ZBattlefield
			h.g.SetZone(state.ZBattlefield, 0, append(h.g.Zone(state.ZBattlefield, 0), obj.ID))
			if obj.Zone != state.ZBattlefield || obj.Counter("P1P1") != 0 || obj.Counter(tc.second) != 0 {
				t.Fatalf("precondition: target zone/counters = %s/%d/%d", obj.Zone, obj.Counter("P1P1"), obj.Counter(tc.second))
			}
			ctx := &Ctx{Source: obj.ID, Controller: 0, Targets: []state.Target{{Obj: obj.ID}}}
			Resolve(h, ctx, body)

			if got := obj.Counter("P1P1"); got != 2 {
				t.Errorf("P1P1 counters = %d, want 2", got)
			}
			if got := obj.Counter(tc.second); got != 1 {
				t.Errorf("%s counters = %d, want 1", tc.second, got)
			}
			var first, second bool
			for _, ev := range h.log {
				if ev.Kind == events.Note {
					t.Errorf("unexpected Note; PutCounterAll handler did not complete: %q", ev.Text)
				}
				if ev.Kind == events.CounterChange && ev.Obj == obj.ID {
					first = first || ev.Counter == "P1P1" && ev.Amount == 2
					second = second || ev.Counter == tc.second && ev.Amount == 1
				}
			}
			if !first || !second {
				t.Fatalf("counter events missing: first=%v second=%v log=%+v", first, second, h.log)
			}
		})
	}
}
