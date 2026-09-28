package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestDuplicantImprintReplacement resolves the real compiled trigger body twice
// against the same host, without bouncing the host (which would itself clean up).
func TestDuplicantImprintReplacement(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	duplicant := lookup(t, reg, "Duplicant")
	e := layerEngine(t)
	src := onBoardCard(t, e, 0, duplicant)
	first := onBoard(t, e, 1, "Name:First Victim\nTypes:Creature Elf\nPT:3/5\nOracle:x\n")
	second := onBoard(t, e, 1, "Name:Second Victim\nTypes:Creature Goblin\nPT:6/7\nOracle:x\n")
	exile := resolveSourceFaceSA(t, e, src, "TrigExile")
	cleanup := resolveSourceFaceSA(t, e, src, "DBCleanup")
	for key := range map[string]string{"Unimprint": "True", "Imprint": "True", "ImprintLast": "True"} {
		if exile.Params[key] != "True" {
			t.Fatalf("real Duplicant missing %s: %+v", key, exile.Params)
		}
	}
	if cleanup.Params["ClearImprinted"] != "True" {
		t.Fatalf("real cleanup: %+v", cleanup.Params)
	}
	if e.G.Obj(src).Zone != state.ZBattlefield || e.G.Obj(first).Zone != state.ZBattlefield || e.G.Obj(second).Zone != state.ZBattlefield {
		t.Fatal("precondition: host and both targets must be on battlefield")
	}
	if e.G.Obj(first).Face().Power() == e.G.Obj(second).Face().Power() || e.G.Obj(first).Face().Toughness() == e.G.Obj(second).Face().Toughness() {
		t.Fatal("precondition: victim powers and toughnesses must differ")
	}
	for _, tc := range []struct {
		id               state.ObjID
		power, toughness int32
		subtype          string
		absent           string
	}{{first, 3, 5, "Elf", "Goblin"}, {second, 6, 7, "Goblin", "Elf"}} {
		effects.Resolve(e, &effects.Ctx{Source: src, Controller: 0, Targets: []state.Target{{Obj: tc.id}}, TargetsOffered: true}, exile)
		if e.G.Obj(tc.id).Zone != state.ZExile {
			t.Fatalf("victim %d did not reach exile: %s", tc.id, e.G.Obj(tc.id).Zone)
		}
		if got := e.G.Obj(src).Imprinted; !slices.Equal(got, []state.ObjID{tc.id}) {
			t.Fatalf("imprint after victim %d = %v, want only %d", tc.id, got, tc.id)
		}
		d := e.Derived(src)
		if d.Power != tc.power || d.Toughness != tc.toughness || !slices.Contains(d.Types, tc.subtype) || slices.Contains(d.Types, tc.absent) {
			t.Fatalf("after victim %d: Duplicant %d/%d types %v, want %d/%d %s without %s", tc.id, d.Power, d.Toughness, d.Types, tc.power, tc.toughness, tc.subtype, tc.absent)
		}
	}
	effects.Resolve(e, &effects.Ctx{Source: src, Controller: 0}, cleanup)
	if got := e.G.Obj(src).Imprinted; len(got) != 0 {
		t.Fatalf("cleanup left imprint %v", got)
	}
	if d := e.Derived(src); d.Power != 2 || d.Toughness != 4 {
		t.Fatalf("cleanup left P/T %d/%d, want printed 2/4", d.Power, d.Toughness)
	}
	if countEvents(e, func(ev events.Event) bool { return ev.Kind == events.Imprint && ev.Obj == src && ev.Text == "clear" }) < 2 {
		t.Fatal("replacement and cleanup must emit replayable clear events")
	}
}

// Last-only also applies to multi-object moves, independently of Unimprint.
func TestChangeZoneImprintLastMultiObject(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	src := onBoard(t, e, 0, "Name:Imprint Host\nTypes:Artifact\nOracle:x\n")
	a := onBoard(t, e, 1, "Name:A\nTypes:Creature Elf\nPT:1/1\nOracle:x\n")
	b := onBoard(t, e, 1, "Name:B\nTypes:Creature Goblin\nPT:2/2\nOracle:x\n")
	if e.G.Obj(a).Zone != state.ZBattlefield || e.G.Obj(b).Zone != state.ZBattlefield {
		t.Fatal("precondition: candidates are not on battlefield")
	}
	e.emit(events.Event{Kind: events.Imprint, Obj: src, IDs: []state.ObjID{a}})
	sa := &cards.SA{API: "ChangeZone", Params: map[string]string{"Defined": "Targeted", "Origin": "Battlefield", "Destination": "Exile", "Imprint": "True", "ImprintLast": "True"}}
	effects.Resolve(e, &effects.Ctx{Source: src, Controller: 0, Targets: []state.Target{{Obj: a}, {Obj: b}}, TargetsOffered: true}, sa)
	if e.G.Obj(a).Zone != state.ZExile || e.G.Obj(b).Zone != state.ZExile {
		t.Fatal("precondition: both candidates must actually move")
	}
	if got := e.G.Obj(src).Imprinted; !slices.Equal(got, []state.ObjID{b}) {
		t.Fatalf("last moved imprint = %v, want [%d]", got, b)
	}
}

// Unimprint clears before the move even if the operation finds no candidate.
func TestChangeZoneUnimprintWithoutMovedCard(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	src := onBoard(t, e, 0, "Name:Imprint Host\nTypes:Artifact\nOracle:x\n")
	old := onBoard(t, e, 1, "Name:Old Card\nTypes:Creature Elf\nPT:1/1\nOracle:x\n")
	e.emit(events.Event{Kind: events.Imprint, Obj: src, IDs: []state.ObjID{old}})
	if got := e.G.Obj(src).Imprinted; !slices.Equal(got, []state.ObjID{old}) {
		t.Fatalf("precondition: old imprint = %v, want [%d]", got, old)
	}
	sa := &cards.SA{API: "ChangeZone", Params: map[string]string{"Defined": "Targeted", "Origin": "Graveyard", "Destination": "Exile", "Unimprint": "True"}}
	effects.Resolve(e, &effects.Ctx{Source: src, Controller: 0, Targets: []state.Target{{Obj: old}}, TargetsOffered: true}, sa)
	if e.G.Obj(old).Zone != state.ZBattlefield {
		t.Fatal("precondition: origin mismatch must leave the candidate on the battlefield")
	}
	if got := e.G.Obj(src).Imprinted; len(got) != 0 {
		t.Fatalf("Unimprint without a successful move left association %v", got)
	}
}
