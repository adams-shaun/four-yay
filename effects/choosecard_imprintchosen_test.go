package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestChooseCardImprintChosenNoHostRecordsAndAccumulates(t *testing.T) {
	h := newHost(t, 2)
	source := h.g.AddObject(mkCard(t, "Name:Imprint Source\nTypes:Artifact\nOracle:x\n"), 0)
	first := h.g.AddObject(mkCard(t, "Name:First Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0)
	second := h.g.AddObject(mkCard(t, "Name:Second Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"), 1)
	for _, o := range []*state.Object{source, first, second} {
		h.Emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	}
	sa := &cards.SA{API: "ChooseCard", Params: map[string]string{"Choices": "Creature", "Amount": "1", "Mandatory": "True", "ImprintChosen": "True"}}
	ctx := NewCtx(source.ID, 0, CtxInit{})
	Resolve(h, &ctx, sa)
	if got := h.g.Obj(source.ID).SeekFound; len(got) != 1 || got[0] != first.ID {
		t.Fatalf("first completed pick association = %v, want [%d]; chosen=%+v", got, first.ID, ctx.Chosen)
	}
	if h.g.Obj(first.ID).Zone != state.ZBattlefield || h.g.Obj(second.ID).Zone != state.ZBattlefield {
		t.Fatalf("fixture creatures zones = %s/%s, want both battlefield", h.g.Obj(first.ID).Zone, h.g.Obj(second.ID).Zone)
	}
	chooseCardRecord(h, NewCtxPtr(source.ID, 0, CtxInit{}), sa, []state.Target{{Obj: second.ID}})
	if got := h.g.Obj(source.ID).SeekFound; len(got) != 2 || got[0] != first.ID || got[1] != second.ID {
		t.Fatalf("cumulative association = %v, want [%d %d]", got, first.ID, second.ID)
	}
	for _, id := range []state.ObjID{first.ID, second.ID} {
		if !imprintAssociationContainsCandidate(h.g, h.g.Obj(source.ID), h.g.Obj(id)) {
			t.Fatalf("IsImprinted membership did not read chosen object %d", id)
		}
	}
	if got := imprintPileTargets(h.g, NewCtxPtr(source.ID, 0, CtxInit{})); len(got) != 2 || got[0].Obj != first.ID || got[1].Obj != second.ID {
		t.Fatalf("Defined Imprinted targets = %v, want both chosen cards", got)
	}
	h.Emit(events.Event{Kind: events.Imprint, Obj: source.ID, Text: "clear"})
	if got := h.g.Obj(source.ID).SeekFound; len(got) != 0 {
		t.Fatalf("ClearImprinted did not clear the rider association: %v", got)
	}
}

func TestChooseCardImprintChosenRiderAndEmptyControls(t *testing.T) {
	h := newHost(t, 2)
	source := h.g.AddObject(mkCard(t, "Name:Control Source\nTypes:Artifact\nOracle:x\n"), 0)
	chosen := h.g.AddObject(mkCard(t, "Name:Control Bear\nTypes:Creature\nPT:2/2\nOracle:x\n"), 0)
	for _, o := range []*state.Object{source, chosen} {
		h.Emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	}
	for _, tc := range []struct {
		name   string
		params map[string]string
		picked []state.Target
		source state.ObjID
		want   int
	}{
		{name: "absent", params: map[string]string{}, picked: []state.Target{{Obj: chosen.ID}}},
		{name: "false", params: map[string]string{"ImprintChosen": "False"}, picked: []state.Target{{Obj: chosen.ID}}},
		{name: "empty", params: map[string]string{"ImprintChosen": "True"}},
		{name: "player only", params: map[string]string{"ImprintChosen": "True"}, picked: []state.Target{{Player: 1, IsPlayer: true}}},
		{name: "no source", params: map[string]string{"ImprintChosen": "True"}, picked: []state.Target{{Obj: chosen.ID}}, source: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sa := &cards.SA{API: "ChooseCard", Params: tc.params}
			chooseCardRecord(h, NewCtxPtr(tc.source, 0, CtxInit{}), sa, tc.picked)
			if got := len(h.g.Obj(source.ID).SeekFound); got != tc.want {
				t.Fatalf("SeekFound count = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestChooseCardImprintChosenRandomRecordsActualPick(t *testing.T) {
	_, original := corpusSA(t, "Last One Standing", "")
	if original.API != "ChooseCard" || original.Params["AtRandom"] != "True" {
		t.Fatalf("random ChooseCard fixture changed: %+v", original)
	}
	sa := *original
	sa.Params = make(map[string]string, len(original.Params)+1)
	for key, value := range original.Params {
		sa.Params[key] = value
	}
	sa.Params["ImprintChosen"] = "True"
	h := newHost(t, 2)
	source := h.g.AddObject(mkCard(t, "Name:Random Imprint Source\nTypes:Artifact\nOracle:x\n"), 0)
	creature := h.g.AddObject(mkCard(t, "Name:Random Candidate\nTypes:Creature\nPT:1/1\nOracle:x\n"), 0)
	for _, object := range []*state.Object{source, creature} {
		h.Emit(events.Event{Kind: events.MoveZone, Obj: object.ID, From: state.ZLibrary, To: state.ZBattlefield})
	}
	ctx := NewCtx(source.ID, 0, CtxInit{})
	Resolve(h, &ctx, &sa)
	if len(ctx.Chosen) != 1 || ctx.Chosen[0].Obj != creature.ID {
		t.Fatalf("random actual pick = %+v, want the only candidate %d", ctx.Chosen, creature.ID)
	}
	if got := h.g.Obj(source.ID).SeekFound; len(got) != 1 || got[0] != creature.ID {
		t.Fatalf("random rider association = %v, want [%d]", got, creature.ID)
	}
}
