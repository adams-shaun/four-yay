package templates

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// targetCarriers is generated from every front face in the pinned Forge
// corpus whose script contains target slots. Keeping identities, rather than
// only a count, makes a newly added carrier fail the ratchet by name.
func targetCarriers(t *testing.T, reg *cards.Registry) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "target-carriers.json"))
	if err != nil {
		t.Fatalf("read pinned target carrier census: %v", err)
	}
	var want []string
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("decode pinned target carrier census: %v", err)
	}
	var got []string
	for _, c := range reg.Cards {
		if len(c.Faces) != 0 && len(oraclegen.TargetSlots(c.Faces[0])) != 0 {
			got = append(got, c.Faces[0].Name)
		}
	}
	sort.Strings(got)
	got = uniqueSorted(got)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("target carrier census changed: got %d carriers, want %d; update the pinned identities only for an intentional corpus change\nfirst got: %v\nfirst want: %v", len(got), len(want), got[:min(10, len(got))], want[:min(10, len(want))])
	}
	return got
}

func uniqueSorted(names []string) []string {
	sort.Strings(names)
	out := names[:0]
	for _, name := range names {
		if len(out) == 0 || out[len(out)-1] != name {
			out = append(out, name)
		}
	}
	return out
}

// TestGeneratedTargetsAreGorgeChoices is the class ratchet: no scenario the
// generator emits may list a target gorge's own run does not consume. It
// walks every target-bearing card in the pinned Forge corpus, generates each
// supported scenario, and replays it through the runner's unused-target check.
//
// Without the rewrite this fails loudly: the fixture's over-offered targets
// (an "up to N" slot gorge declines, a token slot with no token, a wrong-type
// graveyard card) survive into the scenario and the runner reports
// OracleUnusedTargetMarker for each.
func TestGeneratedTargetsAreGorgeChoices(t *testing.T) {
	reg := loadGenRegistry(t)
	carriers := targetCarriers(t, reg)
	checked := 0
	for _, name := range carriers {
		it, skip := Generate(reg, name)
		if skip != nil {
			continue // Census still pins skipped carriers; no scenario was emitted.
		}
		checked++
		res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
		if err != nil {
			t.Errorf("%s: replay: %v", name, err)
			continue
		}
		for _, f := range res.Fails {
			if strings.Contains(f, rules.OracleUnusedTargetMarker) {
				t.Errorf("%s emitted a scenario with a target gorge did not choose: %s", name, f)
			}
		}
	}
	if len(carriers) == 0 || checked == 0 {
		t.Fatalf("precondition: corpus has %d target carriers, generator emitted %d scenarios", len(carriers), checked)
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
