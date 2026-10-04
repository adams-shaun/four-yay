package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestMoltenEchoesCopiesEnteringCreatureExilesAtNextEndStep pins the
// TriggeredCardLKICopy source and the AtEOT$ Exile delayed registration: a
// nontoken Bear entering under a Molten Echoes whose chosen type is Bear
// creates a hasted token copy of it, which the next end step's delayed
// trigger exiles.
func TestMoltenEchoesCopiesEnteringCreatureExilesAtNextEndStep(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := searchEngine(t, reg, "Molten Echoes")
	// Molten Echoes enters for real under the kernel: its as-enters
	// ChooseType (every creature type, CR 205.3m) is posed and answered Bear.
	var molten state.ObjID
	var from state.Zone
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, 0) {
			if o := e.G.Obj(id); molten == 0 && o != nil && o.Face() != nil && o.Face().Name == "Molten Echoes" {
				molten, from = id, z
			}
		}
	}
	if molten == 0 {
		t.Fatal("Molten Echoes absent from hand/library")
	}
	e.pending = nil
	e.probe(func() {
		e.emit(events.Event{Kind: events.MoveZone, Obj: molten, From: from, To: state.ZBattlefield})
	})
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("Molten Echoes' entry posed no ChooseType ask: %+v", d)
	}
	pick := -1
	for _, o := range d.Options {
		if o.Label == "Bear" {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("the ChooseType ask offered no Bear: %+v", d.Options)
	}
	submitChoices(t, e, pick)
	if got := e.G.Obj(molten).ChosenType; got != "Bear" {
		t.Fatalf("Molten Echoes chosen type = %q, want Bear", got)
	}
	kr5Settle(e)

	bear := searchMoveByName(t, e, "Grizzly Bears", state.ZBattlefield)
	e.putTriggersOnStack()
	passUntilStackEmpty(t, e, 20)

	bearCard := e.G.Obj(bear).Card
	cid := findTokenCopyOf(t, e, bearCard, bear)
	o := e.G.Obj(cid)
	if o.Controller != 0 || o.Zone != state.ZBattlefield || o.Tapped || o.IsAttacking {
		t.Fatalf("Molten Echoes copy: controller=%d zone=%s tapped=%v attacking=%v",
			o.Controller, o.Zone, o.Tapped, o.IsAttacking)
	}
	if !e.HasKeyword(cid, "Haste") {
		t.Fatal("the PumpKeywords$ Haste rider did not reach the copy")
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && strings.Contains(ev.Text, "PumpKeywords$") {
			t.Fatalf("implemented PumpKeywords$ rider still named by a skip note: %q", ev.Text)
		}
	}
	noUnimplementedCopyPermanent(t, e)

	driveToStepAll(t, e, e.G.Turn, e.G.Active, state.StepEnd)
	passUntilStackEmpty(t, e, 20)
	exiledTo(t, e, cid)
	if got := e.G.Obj(bear).Zone; got != state.ZBattlefield {
		t.Fatalf("the copied bear itself moved: %s", got)
	}
	noUnimplementedCopyPermanent(t, e)
	replayCheck(t, e, cfg)
}
