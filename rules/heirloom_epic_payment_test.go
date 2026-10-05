package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// The real activation must offer and settle four optional creature payments.
// The Oracle scenario chooses all four; a missing ask or missing ability offer
// fails before it can assert the tapped objects and drawn card.
func TestHeirloomEpicCreatureManaPayment(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	files := loadOracleFiles(t)
	f, ok := files["testdata/oracle/activated-ability/heirloom-epic.json"]
	if !ok || len(f.Scenarios) != 1 {
		t.Fatal("precondition: missing Heirloom Epic scenario")
	}
	fails, transcript, run := runOracleScenario(reg, f.Scenarios[0])
	if run.e == nil {
		t.Fatalf("precondition: engine not built: %v", transcript)
	}
	if len(fails) != 0 {
		t.Fatalf("Heirloom Epic activation: %s\n%s", strings.Join(fails, "; "), strings.Join(transcript, "\n"))
	}
}

// The corpus has exactly one activation with this payment permission. A new
// carrier or a removed one must be audited rather than silently changing the
// cost planner's coverage.
// A partial election uses the remaining pool mana rather than converting
// the activation into a fixed four-creature tap cost. The source also pays
// its own {T} independently of the two elected creatures.
func TestHeirloomEpicMixedCreatureAndManaPayment(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	f := loadOracleFiles(t)["testdata/oracle/activated-ability/heirloom-epic.json"]
	if len(f.Scenarios) != 1 {
		t.Fatal("precondition: missing real-card activation")
	}
	sc := f.Scenarios[0]
	seat := sc.Setup["p0"]
	seat.Battlefield = []string{"Heirloom Epic", "Grizzly Bears", "Hill Giant"}
	sc.Setup = map[string]oracleSeat{"p0": seat}
	sc.Steps = []oracleStep{{Op: "activate", Seat: 0, Card: "p0:Heirloom Epic", Ability: "Draw", Mana: "CC",
		Answers: []oracleAnswer{{Kind: "choose", Pick: []string{"p0:Grizzly Bears", "p0:Hill Giant"}}}}, {Op: "resolve"}}
	fTapped := true
	sc.Expect = []oracleExpect{
		{Card: "p0:Heirloom Epic", Tapped: &fTapped},
		{Card: "p0:Grizzly Bears", Tapped: &fTapped},
		{Card: "p0:Hill Giant", Tapped: &fTapped},
		{Pool: map[string]string{"p0": ""}},
		{HandSize: map[string]int{"p0": 1}},
	}
	fails, transcript, run := runOracleScenario(reg, sc)
	if run.e == nil {
		t.Fatalf("precondition: engine not built: %v", transcript)
	}
	if len(fails) != 0 {
		t.Fatalf("mixed creature/mana activation: %s\n%s", strings.Join(fails, "; "), strings.Join(transcript, "\n"))
	}
}

func TestTapCreaturesForManaCorpusCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	var names []string
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			for _, a := range f.Abilities {
				if a.Params["TapCreaturesForMana"] != "" {
					names = append(names, f.Name)
				}
			}
		}
	}
	if len(names) != 1 || names[0] != "Heirloom Epic" {
		t.Fatalf("TapCreaturesForMana carriers = %v; want [Heirloom Epic]", names)
	}
}
