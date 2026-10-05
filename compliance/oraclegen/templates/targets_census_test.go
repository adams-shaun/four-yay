package templates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// targetSets are the sets the target-rewrite ticket names: every card here
// whose scenario the generator emits must carry exactly the targets gorge's
// own run consumes. The sweep below is the class ratchet -- a card whose
// fixture still over-offers (or whose resolve-step target is dropped) fails
// and is named, so a new carrier cannot slip in silently.
var targetSets = []string{"SOS", "DFT", "BLB", "OTJ", "MKM", "LCI", "TDM", "WOE", "ECL", "TMT"}

// targetSetCards loads a set manifest's card names.
func targetSetCards(t *testing.T, set string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "compliance", "manifests", set+".json"))
	if err != nil {
		t.Fatalf("manifest %s: %v", set, err)
	}
	var m struct {
		Cards []struct {
			Name string `json:"name"`
		} `json:"cards"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("manifest %s: %v", set, err)
	}
	out := make([]string, 0, len(m.Cards))
	for _, c := range m.Cards {
		if n := strings.TrimSpace(c.Name); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// TestGeneratedTargetsAreGorgeChoices is the class ratchet: no scenario the
// generator emits may list a target gorge's own run does not consume. It
// walks every card in the ticket's sets, generates its scenario, and replays
// that scenario through the runner's unused-target self-check.
//
// Without the rewrite this fails loudly: the fixture's over-offered targets
// (an "up to N" slot gorge declines, a token slot with no token, a wrong-type
// graveyard card) survive into the scenario and the runner reports
// OracleUnusedTargetMarker for each.
func TestGeneratedTargetsAreGorgeChoices(t *testing.T) {
	reg := loadGenRegistry(t)
	checked := 0
	for _, set := range targetSets {
		for _, name := range targetSetCards(t, set) {
			it, skip := Generate(reg, name)
			if skip != nil {
				continue // no scenario emitted; nothing to check
			}
			checked++
			res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
			if err != nil {
				t.Errorf("%s/%s: replay: %v", set, name, err)
				continue
			}
			for _, f := range res.Fails {
				if strings.Contains(f, rules.OracleUnusedTargetMarker) {
					t.Errorf("%s/%s emitted a scenario with a target gorge did not choose: %s", set, name, f)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("precondition: the sweep generated no scenario, so the ratchet is vacuous")
	}
}

// TestDivergentEquationTargetsAreEmpty pins the ticket's headline card: an
// optional "up to X target instant/sorcery card in your graveyard" whose
// legal pool the fixture cannot fill (the fixture seeds Grizzly Bears, not an
// instant/sorcery). gorge poses no target decision, so the scenario must
// carry none -- the fixture's surplus Grizzly Bears target is exactly what
// made XMage fail with "Can't find ability to activate command".
func TestDivergentEquationTargetsAreEmpty(t *testing.T) {
	reg := loadGenRegistry(t)
	it, skip := Generate(reg, "Divergent Equation")
	if skip != nil {
		t.Fatalf("Divergent Equation: %s", skip.Reason)
	}
	cast := 0
	for _, st := range it.Scenario.Steps {
		if st.Op != "cast" {
			continue
		}
		cast++
		if len(st.Targets) != 0 {
			t.Fatalf("cast step carries targets %v, want none (gorge chose none)", st.Targets)
		}
	}
	if cast == 0 {
		t.Fatal("precondition: scenario has no cast step")
	}
}

// TestConductElectricityDropsTheTokenSlot: the ticket's model card -- one
// mandatory creature target and one optional "up to one target creature
// token". With no token on the board gorge poses only the first target, so
// the scenario must list one, not the fixture's two same-named Grizzlies.
func TestGeneratedReversedCastsAreRejected(t *testing.T) {
	reg := loadGenRegistry(t)
	for _, name := range []string{"Long River's Pull", "Urgent Necropsy", "Doppelgang"} {
		it, skip := Generate(reg, name)
		if skip != nil {
			continue
		}
		res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
		if err != nil || len(res.Fails) != 0 {
			t.Errorf("%s generated scenario does not replay cleanly: err=%v fails=%v", name, err, res.Fails)
			continue
		}
		for i, st := range it.Scenario.Steps {
			if st.Op != "cast" || st.Card != "p0:"+name || i+1 >= len(res.Snapshots) {
				continue
			}
			before, after := 0, 0
			for _, card := range res.Snapshots[i].Players[0].Hand {
				if card == name {
					before++
				}
			}
			for _, card := range res.Snapshots[i+1].Players[0].Hand {
				if card == name {
					after++
				}
			}
			if before == 0 || after >= before {
				t.Errorf("%s cast was reversed to hand: before=%d after=%d", name, before, after)
			}
		}
	}
}

func TestRepulsiveMutationHasCreatureAndStackTargets(t *testing.T) {
	reg := loadGenRegistry(t)
	it, skip := Generate(reg, "Repulsive Mutation")
	if skip != nil {
		t.Fatalf("Repulsive Mutation: %s", skip.Reason)
	}
	for _, st := range it.Scenario.Steps {
		if st.Op == "cast" && st.Card == "p0:Repulsive Mutation" {
			if len(st.Targets) != 2 {
				t.Fatalf("Repulsive Mutation targets = %v, want creature + stack spell", st.Targets)
			}
			return
		}
	}
	t.Fatal("precondition: generated scenario has no Repulsive Mutation cast")
}

func TestDiscoverFixtureSeedsNonlandLibraryCard(t *testing.T) {
	reg := loadGenRegistry(t)
	it, skip := Generate(reg, "Hit the Mother Lode")
	if skip != nil {
		t.Fatalf("Hit the Mother Lode: %s", skip.Reason)
	}
	library := it.Setup["p0"].LibraryTop
	if len(library) == 0 {
		t.Fatal("precondition: discover fixture has an empty library top")
	}
	for _, name := range library {
		if name == "Wastes" || name == "Forest" || name == "Plains" {
			continue
		}
		return
	}
	t.Fatalf("discover fixture has no nonland candidate: %v", library)
}

func TestSoulImmolationBlightXIsPositive(t *testing.T) {
	reg := loadGenRegistry(t)
	it, skip := Generate(reg, "Soul Immolation")
	if skip != nil {
		t.Fatalf("Soul Immolation: %s", skip.Reason)
	}
	foundPositive := false
	for _, st := range it.Scenario.Steps {
		if st.Op != "cast" {
			continue
		}
		for _, answer := range st.Answers {
			if answer.Kind == "choose" && len(answer.Pick) > 0 && strings.HasPrefix(answer.Pick[0], "X = ") {
				if answer.Pick[0] == "X = 0" {
					t.Fatal("blight X answer is zero; XMage rejects BlightCost amounts below one")
				}
				foundPositive = true
			}
		}
	}
	if !foundPositive {
		t.Fatal("precondition: generated scenario did not answer blight X")
	}
}

func TestDistinctTargetSlotsDoNotReuseSameNamedObject(t *testing.T) {
	reg := loadGenRegistry(t)
	checked, multiTarget := 0, 0
	for _, name := range []string{"Dissection Practice", "Vibrant Outburst", "Badlands Revival", "Cut In", "Piercing Exhale"} {
		it, skip := Generate(reg, name)
		if skip != nil {
			continue
		}
		checked++
		for _, st := range it.Scenario.Steps {
			if st.Op == "cast" && len(st.Targets) > 1 {
				multiTarget++
			}
			seen := map[string]bool{}
			for _, ref := range st.Targets {
				if !strings.Contains(ref, ":") {
					continue // a player reference is not a fixture object name
				}
				base := strings.SplitN(ref, "#", 2)[0]
				if seen[base] {
					t.Errorf("%s targets the same named fixture object twice: %v", name, st.Targets)
				}
				seen[base] = true
			}
		}
	}
	if checked == 0 || multiTarget == 0 {
		t.Fatalf("precondition: generated %d named-card fixtures with %d multi-target casts", checked, multiTarget)
	}
}

func TestConductElectricityDropsTheTokenSlot(t *testing.T) {
	reg := loadGenRegistry(t)
	it, skip := Generate(reg, "Conduct Electricity")
	if skip != nil {
		t.Fatalf("Conduct Electricity: %s", skip.Reason)
	}
	var castTargets []string
	for _, st := range it.Scenario.Steps {
		if st.Op == "cast" {
			castTargets = st.Targets
			break
		}
	}
	if len(castTargets) != 1 {
		t.Fatalf("Conduct Electricity targets = %v, want exactly the mandatory creature target", castTargets)
	}
	if castTargets[0] == "" {
		t.Fatalf("Conduct Electricity target %q is empty", castTargets[0])
	}
}
