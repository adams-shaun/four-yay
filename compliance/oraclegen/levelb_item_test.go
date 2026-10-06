package oraclegen

import (
	"encoding/json"
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

func itemJSON(t *testing.T, it Item) string {
	t.Helper()
	b, err := json.Marshal(it)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestNewItemLevelABytesArePinned holds NewItem's output byte for byte: the
// verdict's ScenarioSHA is the sha of the whole item, so any change here
// stales every committed level-A row.
func TestNewItemLevelABytesArePinned(t *testing.T) {
	sc := Scenario{
		Setup: map[string]Seat{"p0": {Hand: []string{"Shock"}}},
		Steps: []Step{{Op: "cast", Seat: 0, Card: "p0:Shock"}},
	}
	const want = `{"id":"Shock/cast-resolve/v1","card":"Shock","template":"cast-resolve","name":"gen1-cast-resolve","cr":["601.2"],"why":"generated level-A scenario","setup":{"p0":{"hand":["Shock"]}},"steps":[{"op":"cast","seat":0,"card":"p0:Shock"}]}`
	if got := itemJSON(t, NewItem(&cards.Face{Name: "Shock"}, "Shock", "cast-resolve", 1, sc)); got != want {
		t.Errorf("NewItem output changed:\n got %s\nwant %s", got, want)
	}
}

// TestNewLevelBItemIdentity: a level-B item is named by its requirement key
// and carries the family's CR, versioned in its id and name like level A.
func TestNewLevelBItemIdentity(t *testing.T) {
	sc := Scenario{
		Setup: map[string]Seat{"p0": {Battlefield: []string{"Prodigal Sorcerer"}}},
		Steps: []Step{{Op: "activate", Seat: 0, Card: "p0:Prodigal Sorcerer"}},
	}
	it := NewLevelBItem("Prodigal Sorcerer", "activate#0.0", 3, []string{"602.2"}, sc)
	if it.ID != "Prodigal Sorcerer/activate#0.0/v3" {
		t.Errorf("ID = %q", it.ID)
	}
	if it.Card != "Prodigal Sorcerer" {
		t.Errorf("Card = %q", it.Card)
	}
	if it.Template != "activate#0.0" {
		t.Errorf("Template = %q", it.Template)
	}
	if it.Name != "gen3-activate#0.0" {
		t.Errorf("Name = %q", it.Name)
	}
	if it.Why != "generated level-B scenario" {
		t.Errorf("Why = %q", it.Why)
	}
	if len(it.CR) != 1 || it.CR[0] != "602.2" {
		t.Errorf("CR = %v", it.CR)
	}
	const want = `{"id":"Prodigal Sorcerer/activate#0.0/v3","card":"Prodigal Sorcerer","template":"activate#0.0","name":"gen3-activate#0.0","cr":["602.2"],"why":"generated level-B scenario","setup":{"p0":{"battlefield":["Prodigal Sorcerer"]}},"steps":[{"op":"activate","seat":0,"card":"p0:Prodigal Sorcerer"}]}`
	if got := itemJSON(t, it); got != want {
		t.Errorf("NewLevelBItem output changed:\n got %s\nwant %s", got, want)
	}
}
