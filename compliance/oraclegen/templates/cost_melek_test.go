package templates

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

const melekName = "Melek, Reforged Researcher"

func melekRequirement(t *testing.T, reg *cards.Registry) levelb.Requirement {
	t.Helper()
	return costRequirement(t, reg, melekName, "static#0.1")
}

func TestCostStaticMelekLiveFixture(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	it, skip := GenerateB(reg, melekName, melekRequirement(t, reg))
	if skip != nil {
		t.Fatalf("GenerateB: %s", skip.Reason)
	}
	probe := probeStep(t, it)
	if probe.Op != "cast" || probe.Mana != "CCCC" {
		t.Fatalf("probe = %+v, want All Is Dust at exactly {4}", probe)
	}
	if probe.Card != "p0:All Is Dust" {
		t.Fatalf("probe spell = %q, want p0:All Is Dust", probe.Card)
	}
	grave := it.Scenario.Setup["p0"].Graveyard
	if !slices.Contains(grave, "Opt") {
		t.Fatalf("precondition: Melek survival spell absent from graveyard: %v", grave)
	}
	opt, ok := reg.Lookup("Opt")
	if !ok || !opt.Faces[0].IsInstant() && !opt.Faces[0].IsSorcery() {
		t.Fatal("precondition: Opt is not an instant or sorcery in the corpus")
	}
	allDust, ok := reg.Lookup("All Is Dust")
	if !ok {
		t.Fatal("precondition: All Is Dust missing from corpus")
	}
	printed, why := oraclegen.PoolFor(allDust.Faces[0].ManaCost)
	if why != "" || printed != "CCCCCCC" || len(printed)-len(probe.Mana) != 3 {
		t.Fatalf("precondition: All Is Dust printed pool %q (%s), probe pool %q; want exact three-generic reduction", printed, why, probe.Mana)
	}
	res := runSteps(t, reg, it.Scenario, it.Steps)
	if len(res.Fails) != 0 {
		t.Fatalf("generated reduced-price probe fails: %v", res.Fails)
	}
	castIndex := -1
	for i, step := range it.Steps {
		if step.Op == "cast" && step.Card == probe.Card {
			castIndex = i
		}
	}
	if castIndex < 0 || len(res.Snapshots) <= castIndex+1 {
		t.Fatalf("precondition: missing setup/cast checkpoints: %d snapshots for cast %d", len(res.Snapshots), castIndex)
	}
	for _, checkpoint := range []struct {
		label string
		snap  rules.OracleSnapshot
	}{{"setup", res.Snapshots[castIndex]}, {"cast", res.Snapshots[castIndex+1]}} {
		label, snap := checkpoint.label, checkpoint.snap
		found := false
		for _, perm := range snap.Permanents {
			if perm.Name == melekName && perm.Controller == 0 {
				found = true
				if !strings.HasSuffix(perm.PT, "/2") {
					t.Errorf("Melek has non-positive toughness at %s checkpoint: %+v", label, perm)
				}
			}
		}
		if !found {
			t.Errorf("Melek absent from battlefield at %s checkpoint", label)
		}
		if label == "cast" {
			spellOnStack := false
			for _, stack := range snap.Stack {
				spellOnStack = spellOnStack || stack.Kind == "spell" && stack.Source == probe.Card
			}
			if !spellOnStack {
				t.Errorf("All Is Dust not on stack at cast checkpoint: %+v", snap.Stack)
			}
			if len(snap.Players) == 0 || snap.Players[0].Pool != "" {
				t.Errorf("pool after exactly-priced cast = %+v, want empty", snap.Players)
			}
		}
	}
}

func TestMelekValidSpellReductionAlreadyWorks(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	generated, skip := GenerateB(reg, melekName, melekRequirement(t, reg))
	if skip != nil {
		t.Fatalf("GenerateB: %s", skip.Reason)
	}
	for _, tc := range []struct {
		spell, printed, mana string
		targets              []string
	}{
		{"All Is Dust", "CCCCCCC", "CCCC", nil},
		{"Divination", "CCU", "U", nil},
		{"Lightning Strike", "CR", "R", []string{"p1"}},
	} {
		t.Run(tc.spell, func(t *testing.T) {
			card, ok := reg.Lookup(tc.spell)
			if !ok {
				t.Fatalf("precondition: %s absent from corpus", tc.spell)
			}
			printed, why := oraclegen.PoolFor(card.Faces[0].ManaCost)
			if why != "" || printed != tc.printed || len(printed)-len(tc.mana) != min(3, len(printed)-1) {
				t.Fatalf("precondition: %s printed pool %q (%s), probe %q does not reduce up to three generic mana", tc.spell, printed, why, tc.mana)
			}
			scenario := generated.Scenario
			scenario.Setup = map[string]oraclegen.Seat{"p0": generated.Scenario.Setup["p0"], "p1": generated.Scenario.Setup["p1"]}
			seat := scenario.Setup["p0"]
			seat.Hand = []string{tc.spell}
			scenario.Setup["p0"] = seat
			steps := append([]oraclegen.Step(nil), generated.Steps...)
			for i := range steps {
				if steps[i].Op == "cast" {
					steps[i].Card, steps[i].Mana, steps[i].Targets = "p0:"+tc.spell, tc.mana, tc.targets
				}
			}
			res := runSteps(t, reg, scenario, steps)
			if len(res.Fails) != 0 {
				t.Fatalf("live Melek refused %s at %s: %v", tc.spell, tc.mana, res.Fails)
			}
			if len(res.Snapshots) < 2 {
				t.Fatalf("precondition: missing setup and cast snapshots: %d", len(res.Snapshots))
			}
			setup := res.Snapshots[0]
			if !slices.Contains(setup.Players[0].Graveyard, "Opt") {
				t.Fatalf("precondition: Opt absent from the zone read by Melek: %+v", setup.Players[0])
			}
			if !melekOnBattlefield(setup) {
				t.Fatalf("precondition: Melek did not survive setup: %+v", setup.Permanents)
			}
			cast := res.Snapshots[1]
			if !melekOnBattlefield(cast) {
				t.Fatalf("Melek absent at cast checkpoint: %+v", cast.Permanents)
			}
			stacked := false
			for _, obj := range cast.Stack {
				stacked = stacked || obj.Kind == "spell" && obj.Source == "p0:"+tc.spell
			}
			if !stacked || cast.Players[0].Pool != "" {
				t.Fatalf("cast checkpoint stack=%+v pool=%q", cast.Stack, cast.Players[0].Pool)
			}
		})
	}

	// Mutation control: disable only Melek's spell-cost reduction while
	// retaining its CDA. The same live-source probe must then fail at {4}.
	muted := withoutMelekReduction(reg)
	if res := runSteps(t, muted, generated.Scenario, generated.Steps); len(res.Fails) == 0 {
		t.Fatal("reduction mutation: All Is Dust still cast at {4} with Melek's reduction disabled")
	}

	// Without the graveyard type that sets Melek's CDA, the 0/0 dies during
	// setup, and the {4} cast is not offered. A full-price cast in this state
	// would not be evidence about the reduction.
	dead := generated.Scenario
	dead.Setup = map[string]oraclegen.Seat{"p0": generated.Scenario.Setup["p0"], "p1": generated.Scenario.Setup["p1"]}
	seat := dead.Setup["p0"]
	seat.Graveyard = nil
	dead.Setup["p0"] = seat
	res := runSteps(t, reg, dead, generated.Steps)
	if len(res.Fails) == 0 || len(res.Snapshots) == 0 {
		t.Fatalf("precondition/control: empty-graveyard Melek unexpectedly cast All Is Dust at {4}; fails=%v", res.Fails)
	}
	if melekOnBattlefield(res.Snapshots[0]) || !slices.Contains(res.Snapshots[0].Players[0].Graveyard, melekName) {
		t.Fatalf("precondition/control: empty-graveyard Melek did not die into its owner's graveyard during setup: %+v", res.Snapshots[0])
	}
}

func withoutMelekReduction(reg *cards.Registry) *cards.Registry {
	muted := cards.NewRegistry()
	muted.Tokens = reg.Tokens
	for _, card := range reg.Cards {
		if card.Faces[0].Name == melekName {
			copyCard := *card
			copyCard.Faces = append([]*cards.Face(nil), card.Faces...)
			face := *card.Faces[0]
			face.Statics = append([]cards.Static(nil), face.Statics...)
			for i, st := range face.Statics {
				if st.ModeKind() == cards.StaticReduceCost && st.Params["ValidSpell"] != "" {
					face.Statics[i] = cards.Static{Mode: "Continuous", Params: map[string]string{"Mode": "Continuous"}}
				}
			}
			copyCard.Faces[0] = &face
			card = &copyCard
		}
		muted.Add(card)
	}
	return muted
}

func melekOnBattlefield(snap rules.OracleSnapshot) bool {
	for _, perm := range snap.Permanents {
		if perm.Name == melekName && perm.Controller == 0 && strings.HasSuffix(perm.PT, "/2") {
			return true
		}
	}
	return false
}
