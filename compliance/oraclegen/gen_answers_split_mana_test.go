package oraclegen

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/rules"
)

// TestXAnswersDamageSplitEmitsDividedTargets: a "damage_split" KChoose is a
// multiset over option indexes (one index per damage assigned), and XMage's
// chooseTargetAmount consumes one "<ref>^X=<share>" per chosen target on the
// TARGET queue -- never a makeChoose choice. Twin Bolt (DividedAsYouChoose$ 2)
// at two targets is the canonical shape.
func TestXAnswersDamageSplitEmitsDividedTargets(t *testing.T) {
	// Precondition: the two options are distinct targets, so a share of 1
	// each is a real division and not a fixture artifact.
	d := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "choose_n", Resume: "damage_split",
		Options: 2, Min: 2, Max: 2,
		Picks:     []string{"Grizzly Bears", "Serra Angel"},
		PickIdx:   []int{0, 1},
		PickRefs:  []string{"p1:Grizzly Bears", "p1:Serra Angel"},
		PickKinds: []string{"card", "card"},
	}
	if d.PickRefs[0] == d.PickRefs[1] {
		t.Fatalf("precondition: the two split targets are distinct: %v", d.PickRefs)
	}
	got := XAnswers([]rules.OracleDecision{d}, 1, nil)[0]
	want := []XAnswer{
		{0, "target", "p1:Grizzly Bears^X=1"},
		{0, "target", "p1:Serra Angel^X=1"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("answer %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestXAnswersDamageSplitRepeatsShareForMultiset: a target receiving more than
// one damage has its option index repeated, and the share must count repeats.
// Forked Bolt/Fury deal 2 to one target: one index twice, so ^X=2.
func TestXAnswersDamageSplitRepeatsShareForMultiset(t *testing.T) {
	d := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "choose_n", Resume: "damage_split",
		Options: 2, Min: 3, Max: 3,
		Picks:     []string{"Grizzly Bears", "Grizzly Bears", "Serra Angel"},
		PickIdx:   []int{0, 0, 1},
		PickRefs:  []string{"p1:Grizzly Bears", "p1:Grizzly Bears", "p1:Serra Angel"},
		PickKinds: []string{"card", "card", "card"},
	}
	got := XAnswers([]rules.OracleDecision{d}, 1, nil)[0]
	want := []XAnswer{
		{0, "target", "p1:Grizzly Bears^X=2"},
		{0, "target", "p1:Serra Angel^X=1"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("answer %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestXAnswersManaAllocationEmitsWUBRGAmounts: a "mana_color" allocation
// (Min==Max>1) is one unit per picked option. XMage's
// getMultiAmountWithIndividualConstraints iterates the effect's colours in
// WUBRG order and needs every message filled, zeroes included. Desolation of
// Smaug's Combo Any for 4 (one of each of W/U/B/R) is the canonical shape.
func TestXAnswersManaAllocationEmitsWUBRGAmounts(t *testing.T) {
	d := rules.OracleDecision{
		Step: 1, Seat: 0, Kind: "choose_n", Resume: "mana_color",
		Options: 20, Min: 4, Max: 4,
		Picks:     []string{"Add W", "Add U", "Add B", "Add R"},
		PickIdx:   []int{0, 1, 2, 3},
		PickRefs:  []string{"p0:Desolation of Smaug", "p0:Desolation of Smaug", "p0:Desolation of Smaug", "p0:Desolation of Smaug"},
		PickKinds: []string{"mana", "mana", "mana", "mana"},
	}
	// Precondition: the four picks name four distinct colours, so the counts
	// below are real.
	seen := map[string]bool{}
	for _, p := range d.Picks {
		if seen[p] {
			t.Fatalf("precondition: picks are distinct colours: %v", d.Picks)
		}
		seen[p] = true
	}
	got := XAnswers([]rules.OracleDecision{d}, 2, nil)[1]
	want := []XAnswer{
		{0, "amount", "1"}, {0, "amount", "1"}, {0, "amount", "1"}, {0, "amount", "1"}, {0, "amount", "0"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("answer %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestXAnswersSingleUnitManaIsNotAllocation: a one-unit Combo pick (Min==Max==1)
// is a genuine one-colour makeChoose choice, not a multi-amount allocation. It
// must stay a "choice", so the amount arm cannot swallow it.
func TestXAnswersSingleUnitManaIsNotAllocation(t *testing.T) {
	d := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "choose_n", Resume: "mana_color",
		Options: 5, Min: 1, Max: 1,
		Picks:     []string{"Add G"},
		PickIdx:   []int{4},
		PickRefs:  []string{"p0:Some Source"},
		PickKinds: []string{"mana"},
	}
	got := XAnswers([]rules.OracleDecision{d}, 1, nil)[0]
	if len(got) != 1 || got[0].Kind != "choice" || got[0].Value != "Green" {
		t.Fatalf("single-unit mana = %+v, want one choice answer Green", got)
	}
}

func twinBoltRegistry(t *testing.T) *cards.Registry {
	t.Helper()
	reg, err := cards.LoadRegistry(cards.CachePath("../../.cards"))
	if err != nil {
		t.Fatalf("the generator needs the corpus: %v", err)
	}
	return reg
}

// TestXAnswersTwinBoltEndToEnd exercises the divided-target derivation against
// a real engine run: cast Twin Bolt (TargetMin$ 1, TargetMax$ 2,
// DividedAsYouChoose$ 2) at two distinct creatures, and assert the engine's
// damage_split decision carries the two chosen targets and that XAnswers turns
// it into two "^X=" target answers.
func TestXAnswersTwinBoltEndToEnd(t *testing.T) {
	reg := twinBoltRegistry(t)
	const raw = `{
  "name": "twin-bolt-split", "cr": ["601.2c", "608.2"], "why": "divided target amounts",
  "setup": {
    "p0": {"hand": ["Twin Bolt"]},
    "p1": {"battlefield": ["Grizzly Bears", "Serra Angel"], "library_top": ["Shock", "Lightning Bolt"]}
  },
  "steps": [
    {"op": "cast", "seat": 0, "card": "p0:Twin Bolt", "mana": "CR", "targets": ["p1:Grizzly Bears", "p1:Serra Angel"]},
    {"op": "resolve"}
  ]
}`
	res, err := rules.RunOracleScenarioJSON(reg, []byte(raw))
	if err != nil {
		t.Fatalf("Twin Bolt scenario did not run: %v", err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("Twin Bolt scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	var split *rules.OracleDecision
	for i := range res.Decisions {
		if res.Decisions[i].Resume == "damage_split" {
			split = &res.Decisions[i]
			break
		}
	}
	if split == nil {
		t.Fatalf("no damage_split decision posed; decisions: %+v", res.Decisions)
	}
	if split.Kind != "choose_n" || split.Min != 2 || split.Max != 2 {
		t.Fatalf("split shape = kind %q min %d max %d, want choose_n 2 2", split.Kind, split.Min, split.Max)
	}
	if len(split.PickRefs) != 2 || split.PickRefs[0] == split.PickRefs[1] {
		t.Fatalf("split did not choose two distinct targets: %+v", split.PickRefs)
	}
	answers := XAnswers(res.Decisions, len(res.Snapshots), nil)
	var got []XAnswer
	for _, step := range answers {
		for _, a := range step {
			if a.Kind == "target" && strings.Contains(a.Value, "^X=") {
				got = append(got, a)
			}
		}
	}
	if len(got) != 2 {
		t.Fatalf("want two divided target answers, got %+v", got)
	}
	var sum int
	for _, a := range got {
		if !strings.HasSuffix(a.Value, "^X=1") {
			t.Errorf("answer %+v does not carry a share of 1", a)
		}
		sum++
	}
	if sum != 2 {
		t.Fatalf("sum of shares = %d, want 2", sum)
	}
	for i, ref := range split.PickRefs {
		want := ref + "^X=1"
		if got[i].Value != want {
			t.Errorf("answer %d = %q, want %q", i, got[i].Value, want)
		}
	}
}

// TestXAnswersDesolationOfSmaugEndToEnd exercises the Combo Any allocation
// against a real engine run: cast Desolation of Smaug (DB$ Mana | Produced$
// Combo Any | Amount$ 4) and assert the mana_color decision becomes five
// amount answers in WUBRG order.
func TestXAnswersDesolationOfSmaugEndToEnd(t *testing.T) {
	reg := twinBoltRegistry(t)
	const raw = `{
  "name": "desolation-mana", "cr": ["106.1"], "why": "combo any allocation",
  "setup": {
    "p0": {"hand": ["Desolation of Smaug"]},
    "p1": {"battlefield": ["Grizzly Bears", "Llanowar Elves"], "library_top": ["Shock", "Lightning Bolt"]}
  },
  "steps": [
    {"op": "cast", "seat": 0, "card": "p0:Desolation of Smaug", "mana": "CCRR"},
    {"op": "resolve"}
  ]
}`
	res, err := rules.RunOracleScenarioJSON(reg, []byte(raw))
	if err != nil {
		t.Fatalf("Desolation scenario did not run: %v", err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("Desolation scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	var mana *rules.OracleDecision
	for i := range res.Decisions {
		if res.Decisions[i].Resume == "mana_color" {
			mana = &res.Decisions[i]
			break
		}
	}
	if mana == nil {
		t.Fatalf("no mana_color decision posed; decisions: %+v", res.Decisions)
	}
	if mana.Kind != "choose_n" || mana.Min != 4 || mana.Max != 4 || mana.Options != 20 {
		t.Fatalf("allocation shape = kind %q min %d max %d opts %d, want choose_n 4 4 20", mana.Kind, mana.Min, mana.Max, mana.Options)
	}
	answers := XAnswers(res.Decisions, len(res.Snapshots), nil)
	var got []XAnswer
	for _, step := range answers {
		for _, a := range step {
			if a.Kind == "amount" {
				got = append(got, a)
			}
		}
	}
	want := []string{"1", "1", "1", "1", "0"}
	if len(got) != len(want) {
		t.Fatalf("want five amount answers %v, got %+v", want, got)
	}
	for i := range want {
		if got[i].Value != want[i] {
			t.Errorf("amount %d = %q, want %q", i, got[i].Value, want[i])
		}
	}
}
