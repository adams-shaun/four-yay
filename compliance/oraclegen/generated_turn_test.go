package oraclegen_test

import (
	"encoding/json"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

func TestGeneratedFirstThreeTurnClausesOnFourthControllerTurn(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, name := range []string{"Jace Reawakened", "Spider-Man 2099"} {
		t.Run(name, func(t *testing.T) {
			item, skip := templates.Generate(reg, name)
			if skip != nil {
				t.Fatalf("generator skipped fixture: %s", skip.Reason)
			}
			if item.Turn != 7 {
				t.Fatalf("generated scenario turn = %d, want global turn 7 (the card controller's fourth turn)", item.Turn)
			}
			var sc struct {
				Turn int `json:"turn"`
			}
			if err := json.Unmarshal(item.Raw(), &sc); err != nil {
				t.Fatal(err)
			}
			if sc.Turn != 7 {
				t.Fatalf("emitted scenario turn = %d, want 7", sc.Turn)
			}
			res, err := rules.RunOracleScenarioJSON(reg, item.Raw())
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Fails) != 0 {
				t.Fatalf("generated scenario failed: %v", res.Fails)
			}
			if len(res.Snapshots) != len(item.Steps)+1 || res.Snapshots[0].Turn != 7 {
				t.Fatalf("snapshots do not start at turn 7: %+v", res.Snapshots)
			}
			found := false
			for _, p := range res.Snapshots[len(res.Snapshots)-1].Permanents {
				if cards.NormalizeName(p.Name) == cards.NormalizeName(name) {
					found = true
				}
			}
			if !found {
				t.Fatalf("scenario did not resolve %s onto battlefield", name)
			}
		})
	}
}
