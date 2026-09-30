package manabrew

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	mb "github.com/adams-shaun/gorge/protocol/manabrew"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

func projectionFixture() view.View {
	return view.View{Viewer: 0, Turn: 3, Step: "main1", Active: 0, Priority: 1, Players: []view.PlayerView{
		{ID: 0, Name: "Alice", Life: 20, Hand: []view.CardView{{ID: 1, Name: "Known in own hand", Printing: view.Printing{Name: "Island"}, Types: "Basic Land — Island", Owner: 0, Controller: 0}}, HandSize: 1, LibrarySize: 30,
			Battlefield: []view.CardView{{ID: 2, Name: "Face-down secret", Printing: view.Printing{Name: "Face-down secret"}, FaceDown: true, Types: "Creature — Human", Owner: 0, Controller: 0}, {ID: 3, Printing: view.Printing{Name: "Bear"}, Types: "Creature — Bear", Owner: 0, Controller: 0, BlockedBy: []state.ObjID{4}}},
			Exile:       []view.CardView{{ID: 5, Name: "Exiled secret", Printing: view.Printing{Name: "Exiled secret"}, FaceDown: true, Owner: 0, Controller: 0}},
			LibraryTop:  &view.CardView{ID: 6, Printing: view.Printing{Name: "Revealed top"}, Types: "Sorcery", Owner: 0, Controller: 0}, Pool: map[string]int32{"W": 1}},
		{ID: 1, Name: "Bob", Life: 18, Hand: nil, HandSize: 4, LibrarySize: 28, Battlefield: []view.CardView{{ID: 4, Printing: view.Printing{Name: "Elf"}, Types: "Creature — Elf", Owner: 0, Controller: 1}}, Pool: map[string]int32{}}},
	}
}

func TestStateProjectionGolden(t *testing.T) {
	fixture := projectionFixture()
	if len(fixture.Players) != 2 || len(fixture.Players[0].Battlefield) != 2 || len(fixture.Players[1].Battlefield) != 1 {
		t.Fatal("fixture must contain both battlefield controller buckets")
	}
	if fixture.Players[1].Battlefield[0].Owner == fixture.Players[1].Battlefield[0].Controller {
		t.Fatal("fixture must distinguish battlefield controller from owner")
	}
	// The engine keys every battlefield membership list by CONTROLLER, not
	// owner: events/apply.go's zoneOwner returns o.Controller for ZBattlefield,
	// move (events/apply_state_helpers.go's move) files a permanent with
	// zoneOwner(o, to), and
	// changeControl (events/apply_helpers.go) moves a stolen permanent from its
	// old controller's list to its new one. So view.PlayerView.Battlefield
	// holds exactly the permanents that player controls, and the projection
	// below must bucket by controller while preserving the owner id.
	got, err := json.Marshal(New("table", 2, nil).gameView(fixture))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("testdata", "state", "basic.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(got, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got)+"\n" != string(want) {
		t.Fatalf("state projection differs from %s\n got: %s\nwant: %s", path, got, want)
	}
	var dto mb.GameViewDto
	if err := json.Unmarshal(got, &dto); err != nil {
		t.Fatal(err)
	}
	var controllerBucket *mb.ZoneDto
	for i := range dto.Zones {
		z := &dto.Zones[i]
		if z.Zone == mb.ZoneBattlefield && z.OwnerID == "player-1" {
			controllerBucket = z
			break
		}
	}
	if controllerBucket == nil || len(controllerBucket.Cards) != 1 {
		t.Fatalf("controller bucket missing card: %#v", controllerBucket)
	}
	card, ok := controllerBucket.Cards[0].Value.(mb.VisibleCard)
	if !ok || card.ID != "o4" || card.OwnerID != "player-0" || card.ControllerID != "player-1" {
		t.Fatalf("battlefield was not bucketed by controller while retaining owner: %#v", controllerBucket.Cards[0])
	}
	// The owner's bucket must NOT carry it: a permanent appears under exactly
	// one controller, so an owner-grouped projection (the one this asserts
	// against) would have to move o4 into player-0's bucket and empty
	// player-1's -- both of which this test would catch.
	var ownerBucket *mb.ZoneDto
	for i := range dto.Zones {
		z := &dto.Zones[i]
		if z.Zone == mb.ZoneBattlefield && z.OwnerID == "player-0" {
			ownerBucket = z
			break
		}
	}
	if ownerBucket == nil {
		t.Fatal("player-0 battlefield bucket missing")
	}
	for _, entry := range ownerBucket.Cards {
		if vc, ok := entry.Value.(mb.VisibleCard); ok && vc.ID == "o4" {
			t.Fatalf("controlled-by-other permanent o4 leaked into its owner's battlefield bucket: %#v", vc)
		}
	}
	var faceDown bool
	for _, z := range dto.Zones {
		for _, entry := range z.Cards {
			if visible, ok := entry.Value.(mb.VisibleCard); ok && visible.ID == "o2" {
				faceDown = visible.IsFaceDown && visible.Identity.Name == ""
			}
		}
	}
	if !faceDown {
		t.Fatal("face-down permanent must retain public state but redact identity")
	}
}

func TestHiddenZonesNeverVisible(t *testing.T) {
	got, err := json.Marshal(New("table", 2, nil).gameView(projectionFixture()))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"Face-down secret", "Exiled secret"} {
		if strings.Contains(string(got), secret) {
			t.Errorf("hidden card identity leaked: %s", secret)
		}
	}
	if !strings.Contains(string(got), `"visibility":"hidden","id":"h-exile-0-0"`) {
		t.Fatalf("face-down exile not represented as positional hidden entry: %s", got)
	}
	var dto mb.GameViewDto
	if err := json.Unmarshal(got, &dto); err != nil {
		t.Fatal(err)
	}
	var hidden bool
	for _, z := range dto.Zones {
		for _, c := range z.Cards {
			if h, ok := c.Value.(mb.HiddenCard); ok && h.ID == "h-exile-0-0" {
				hidden = true
			}
		}
	}
	if !hidden {
		t.Fatal("hidden exile entry missing after decode")
	}
}

func TestStepMapTotal(t *testing.T) {
	// Derived from the engine's OWN step set rather than a hand-copied list:
	// every valid state.Step must map to a non-empty StepKind. A step the
	// engine adds (a new state.Step constant) makes this fail until stepKind
	// names it, so the map cannot silently fall through.
	seen := 0
	for step := state.Step(0); step.Valid(); step++ {
		name := step.String()
		if got := stepKind(name); got == "" {
			t.Errorf("stepKind(%q) = empty; every engine step must map to a StepKind", name)
		}
		seen++
	}
	if seen != 12 {
		t.Fatalf("engine exposes %d valid steps, expected 12", seen)
	}
	// An unknown (or malformed) name maps to the empty kind, not a pass-through.
	if got := stepKind("not-a-step"); got != "" {
		t.Errorf("stepKind(unknown) = %q, want empty", got)
	}
}

func TestIDsDeterministic(t *testing.T) {
	if got, want := []string{playerID(7), cardID(42), stackID(42), hiddenCardID("exile", 3, 2), actionID(4), payActionID("abc")}, []string{"player-7", "o42", "s42", "h-exile-3-2", "opt-4", "pay-abc"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ids=%v want %v", got, want)
	}
	if got := promptID(&decision.Decision{Seq: 9}); got != 9 {
		t.Fatalf("promptID=%d want 9", got)
	}
}
