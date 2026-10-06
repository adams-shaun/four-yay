package oraclegen_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen/templates"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

func TestGeneratedFirstThreeTurnClausesOnFourthControllerTurn(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, name := range []string{"Jace Reawakened", "Spider-Man 2099", "Serra Avenger"} {
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

			// Precondition for the assertion above: the card's first-three-turn
			// clause is real. Re-run the SAME generated scenario forced to turn 1
			// and require the cast be refused; if the engine stopped enforcing the
			// clause, the turn-7 success would still pass but this would not, so
			// the fixture cannot silently become vacuous.
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(item.Raw(), &raw); err != nil {
				t.Fatal(err)
			}
			raw["turn"] = json.RawMessage("1")
			turn1, err := json.Marshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			turn1Res, err := rules.RunOracleScenarioJSON(reg, turn1)
			if err != nil {
				t.Fatal(err)
			}
			refused := false
			for _, f := range turn1Res.Fails {
				if strings.Contains(f, "not offered") {
					refused = true
				}
			}
			if !refused {
				t.Fatalf("%s was castable on turn 1 (fails = %v): the first-three-turn clause is not enforced, so the turn-7 fixture proves nothing", name, turn1Res.Fails)
			}
		})
	}
}
