package effects

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestGreenSunsTwilightRestBottomRandomizesOnlyImprintedRemainder(t *testing.T) {
	_, ability, vars := corpusRiderSA(t, "Green Sun's Twilight", "")
	if ability.API != "DigMultiple" || ability.Params["ImprintRest"] != "True" || ability.Params["ChangeLater"] != "True" {
		t.Fatalf("precondition: Green Sun's Twilight DigMultiple params = %+v", ability.Params)
	}
	rest := cards.ResolveSVar(vars, "RestBottom")
	if rest == nil || rest.API != "ChangeZone" || rest.Params["Defined"] != "Imprinted" ||
		rest.Params["Origin"] != "Library" || rest.Params["Destination"] != "Library" ||
		rest.Params["LibraryPosition"] != "-1" || rest.Params["RandomOrder"] != "True" || rest.Params["NoShuffle"] != "True" {
		t.Fatalf("precondition: Green Sun rest-bottom chain = %+v", rest)
	}
	if cz := ChangeZoneOf(rest); !cz.RandomOrder || !cz.NoShuffle || cz.Defined != "Imprinted" {
		t.Fatalf("precondition: compiled RestBottom params = %+v", cz)
	}
	chosen := cards.ResolveSVar(vars, "DBChangeZone")
	if chosen == nil || chosen.Params["Defined"] != "Remembered" || chosen.Params["Destination"] != "Hand" ||
		chosen.Params["DestinationAlternative"] != "Battlefield" || chosen.Params["DestAltSVarCompare"] != "GE5" {
		t.Fatalf("precondition: chosen-card ChangeZone chain = %+v", chosen)
	}

	h, ids := digMultipleBoard(t)
	tailA := h.g.AddObject(mkCard(t, "Name:TailA\nTypes:Sorcery\nOracle:x\n"), 0).ID
	tailB := h.g.AddObject(mkCard(t, "Name:TailB\nTypes:Sorcery\nOracle:x\n"), 0).ID
	h.g.SetZone(state.ZLibrary, 0, append(ids, tailA, tailB))
	if tailA == tailB || tailA == ids[0] || tailB == ids[0] {
		t.Fatal("precondition: distinct untouched tail IDs with an order that can be checked")
	}
	source := h.g.AddObject(mkCard(t, "Name:Source\nTypes:Sorcery\nOracle:x\n"), 0).ID
	if !reflect.DeepEqual(h.g.Zone(state.ZLibrary, 0), []state.ObjID{ids[0], ids[1], ids[2], ids[3], tailA, tailB}) {
		t.Fatalf("precondition: library order before resolution = %v", h.g.Zone(state.ZLibrary, 0))
	}
	h.choices = []int{1, 2}
	runtimeVars := make(map[string]string, len(vars)+1)
	for key, value := range vars {
		runtimeVars[key] = value
	}
	runtimeVars["X"] = "Number$3"
	ctx := &Ctx{Controller: 0, Source: source, SVars: runtimeVars}
	effDigMultiple(h, ctx, ability)
	if !reflect.DeepEqual(h.g.Obj(source).Imprinted, []state.ObjID{ids[0], ids[3]}) {
		t.Fatalf("precondition: DigMultiple imprinted = %v", h.g.Obj(source).Imprinted)
	}
	if len(ctx.Remembered) != 2 || ctx.Remembered[0].Obj != ids[1] || ctx.Remembered[1].Obj != ids[2] {
		t.Fatalf("precondition: DigMultiple remembered = %v", ctx.Remembered)
	}
	Resolve(h, ctx, cards.ResolveSVar(vars, "DBChangeZone"))

	if !reflect.DeepEqual(h.g.Zone(state.ZHand, 0), []state.ObjID{ids[1], ids[2]}) {
		t.Fatalf("precondition/result: chosen cards in hand = %v", h.g.Zone(state.ZHand, 0))
	}
	var imprinted []state.ObjID
	var shuffles, placements []events.Event
	for _, ev := range h.log {
		if ev.Kind == events.Imprint && ev.Obj == source {
			imprinted = append(imprinted, ev.IDs...)
		}
		if ev.Kind == events.Shuffle && ev.Player == 0 {
			shuffles = append(shuffles, ev)
		}
		if ev.Kind == events.LibraryOrder && ev.Player == 0 {
			placements = append(placements, ev)
		}
	}
	if !reflect.DeepEqual(imprinted, []state.ObjID{ids[0], ids[3]}) || ids[0] == ids[3] {
		t.Fatalf("precondition: Imprint events = %v, want distinct remainder IDs %v", imprinted, []state.ObjID{ids[0], ids[3]})
	}
	wantRandom := []state.ObjID{ids[3], ids[0]} // fakeHost's deterministic zero draws reverse this two-card pile.
	if len(shuffles) != 0 {
		t.Fatalf("Shuffle events = %+v, want none (a subset shuffle must not replace the whole library)", shuffles)
	}
	if len(placements) != 1 {
		t.Fatalf("LibraryOrder events = %d, want one bottom placement", len(placements))
	}
	wantLibrary := append([]state.ObjID{tailA, tailB}, wantRandom...)
	if !reflect.DeepEqual(placements[0].IDs, wantLibrary) || !reflect.DeepEqual(h.g.Zone(state.ZLibrary, 0), wantLibrary) {
		t.Fatalf("bottom placement = %v / library = %v, want untouched tail then randomized remainder %v; log=%+v", placements[0].IDs, h.g.Zone(state.ZLibrary, 0), wantLibrary, h.log)
	}
}

func TestDefinedLibraryFetchKeepsDefaultShuffleOutsideGreenSunContinuation(t *testing.T) {
	h := newHost(t, 2)
	source := h.g.AddObject(mkCard(t, "Name:Ordinary Fetch\nTypes:Sorcery\nOracle:x\n"), 0)
	first := h.g.AddObject(mkCard(t, "Name:First\nTypes:Sorcery\nOracle:x\n"), 0)
	second := h.g.AddObject(mkCard(t, "Name:Second\nTypes:Sorcery\nOracle:x\n"), 0)
	h.g.SetZone(state.ZLibrary, 0, []state.ObjID{first.ID, second.ID})
	if h.g.Obj(first.ID).Zone != state.ZLibrary || h.g.Obj(second.ID).Zone != state.ZLibrary || first.ID == second.ID {
		t.Fatal("precondition: two distinct fetch targets are in the library")
	}
	sa := &cards.SA{API: "ChangeZone", Params: map[string]string{
		"Defined": "Remembered", "Origin": "Library", "Destination": "Hand",
	}}
	ctx := &Ctx{Source: source.ID, Controller: 0, Remembered: []state.Target{{Obj: first.ID}, {Obj: second.ID}}}
	effChangeZone(h, ctx, sa)
	var shuffles int
	for _, ev := range h.log {
		if ev.Kind == events.Shuffle && ev.Player == 0 {
			shuffles++
		}
	}
	if shuffles != 1 {
		t.Fatalf("ordinary resolved Defined fetch Shuffle events = %d, want established default 1; log=%+v", shuffles, h.log)
	}
}
