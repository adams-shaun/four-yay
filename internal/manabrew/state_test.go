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
	steps := []string{"untap", "upkeep", "draw", "main1", "begin-combat", "declare-attackers", "declare-blockers", "combat-damage", "end-combat", "main2", "end", "cleanup"}
	want := []mb.StepKind{mb.StepUntap, mb.StepUpkeep, mb.StepDraw, mb.StepMain1, mb.StepCombatBegin, mb.StepCombatDeclareAttackers, mb.StepCombatDeclareBlockers, mb.StepCombatDamage, mb.StepCombatEnd, mb.StepMain2, mb.StepEndOfTurn, mb.StepCleanup}
	for i, s := range steps {
		if got := stepKind(s); got != want[i] {
			t.Errorf("stepKind(%q)=%q, want %q", s, got, want[i])
		}
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
