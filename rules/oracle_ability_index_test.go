package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// Level-B activate template support: an `activate` step may name an activated
// ability by its IR index (the index into the source's Face().Abilities, the
// same anchor decision.Option.Ability carries) instead of pasting the ability's
// SpellDescription text into the scenario. Cases are driven through
// RunOracleScenarioJSON, the entry the compliance pipeline uses.

// Krovikan Elementalist carries two non-mana activated abilities:
//
//	AB 0: {2}{R}: target creature gets +1/+0 until end of turn.
//	AB 1: {U}{U}: target creature you control gains flying until end of turn.
//
// Three red plus two blue pay both, so only ability_index decides which fires.
func TestOracleAbilityIndexSelectsSecondNonManaAbility(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const sc = `{"name":"ability-index-nonmana","cr":["602.2"],"why":"ability_index picks the second activated ability","setup":{"p0":{"battlefield":["Krovikan Elementalist"]}},"steps":[{"op":"activate","seat":0,"card":"p0:Krovikan Elementalist","mana":"RRRUU","ability_index":1,"targets":["p0:Krovikan Elementalist"]},{"op":"resolve","seat":0}]}`
	res, err := RunOracleScenarioJSON(reg, []byte(sc))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	setup := res.Snapshots[0]
	if p, ok := snapPerm(setup, "p0:Krovikan Elementalist"); !ok || p.PT != "1/1" {
		t.Fatalf("precondition: Krovikan Elementalist = %+v, want an untapped 1/1 on the battlefield", p)
	}
	final := res.Snapshots[len(res.Snapshots)-1]
	p, ok := snapPerm(final, "p0:Krovikan Elementalist")
	if !ok {
		t.Fatal("Krovikan Elementalist left the battlefield")
	}
	// Ability 1's effect: flying, and NOT ability 0's +1/+0.
	if !oracleHasFold(p.Keywords, "Flying") {
		t.Fatalf("keywords = %v, want Flying (ability_index 1)", p.Keywords)
	}
	if p.PT != "1/1" {
		t.Fatalf("P/T = %s, want 1/1 (ability 0's +1/+0 must not have applied)", p.PT)
	}
	// RRR paid ability 0's price had it been chosen; only UU was spent.
	if got := final.Players[0].Pool; got != "RRR" {
		t.Fatalf("pool = %q, want RRR (ability 1 paid UU)", got)
	}
}

// A label given alongside ability_index must agree with the indexed ability,
// and the index wins: here ability 0's description names ability 1, so the
// step fails instead of silently activating ability 0.
func TestOracleAbilityIndexLabelMustAgree(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const sc = `{"name":"ability-index-label-mismatch","cr":["602.2"],"why":"ability_index and a disagreeing label fail","setup":{"p0":{"battlefield":["Krovikan Elementalist"]}},"steps":[{"op":"activate","seat":0,"card":"p0:Krovikan Elementalist","mana":"RRRUU","ability_index":1,"ability":"gets +1/+0","targets":["p0:Krovikan Elementalist"]}]}`
	res, err := RunOracleScenarioJSON(reg, []byte(sc))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) == 0 {
		t.Fatalf("ability_index 1 with the label for ability 0 succeeded: %s", strings.Join(res.Transcript, "\n"))
	}
	named := false
	for _, f := range res.Fails {
		if strings.Contains(f, "harness:") && strings.Contains(f, "ability_index: 1") {
			named = true
		}
	}
	if !named {
		t.Fatalf("expected a harness failure naming ability_index 1, got %v", res.Fails)
	}
}

// A wrong index must fail loudly with a harness error naming the index and
// dumping the options, exactly as a wrong label does.
func TestOracleAbilityIndexWrongIndexFails(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const sc = `{"name":"ability-index-bad","cr":["602.2"],"why":"a wrong ability_index fails loudly","setup":{"p0":{"battlefield":["Krovikan Elementalist"]}},"steps":[{"op":"activate","seat":0,"card":"p0:Krovikan Elementalist","mana":"RRRUU","ability_index":5}]}`
	res, err := RunOracleScenarioJSON(reg, []byte(sc))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) == 0 {
		t.Fatal("ability_index 5 succeeded: the harness silently activated a different ability")
	}
	named := false
	for _, f := range res.Fails {
		if strings.Contains(f, "harness:") && strings.Contains(f, "ability_index: 5") && strings.Contains(f, "not offered") {
			named = true
		}
	}
	if !named {
		t.Fatalf("expected a harness failure naming ability_index 5 and the options, got %v", res.Fails)
	}
}

// Whisperer of the Wilds has two distinct green mana abilities:
//
//	AB 0: {T}: Add {G}.
//	AB 1: Ferocious -- {T}: Add {G}{G}. Activate only if you control a
//	      creature with power 4 or greater.
//
// Craw Wurm (6/4) satisfies the ferocious gate, so both are offered and only
// ability_index decides which the pool reflects.
func TestOracleAbilityIndexSelectsSecondManaAbility(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const sc = `{"name":"ability-index-mana","cr":["605.1","602.2"],"why":"ability_index picks the second mana ability","setup":{"p0":{"battlefield":["Whisperer of the Wilds","Craw Wurm"]}},"steps":[{"op":"activate","seat":0,"card":"p0:Whisperer of the Wilds","ability_index":1}]}`
	res, err := RunOracleScenarioJSON(reg, []byte(sc))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	setup := res.Snapshots[0]
	if p, ok := snapPerm(setup, "p0:Whisperer of the Wilds"); !ok || p.Tapped {
		t.Fatalf("precondition: Whisperer = %+v, want an untapped permanent", p)
	}
	if p, ok := snapPerm(setup, "p0:Craw Wurm"); !ok || p.PT != "6/4" {
		t.Fatalf("precondition: Craw Wurm = %+v, want a 6/4 to satisfy ferocious", p)
	}
	final := res.Snapshots[len(res.Snapshots)-1]
	if got := final.Players[0].Pool; got != "GG" {
		t.Fatalf("pool = %q, want GG (ability_index 1), not G (ability 0)", got)
	}
	if p, ok := snapPerm(final, "p0:Whisperer of the Wilds"); !ok || !p.Tapped {
		t.Fatalf("Whisperer = %+v, want tapped (ability 1 costs {T})", p)
	}
}

// Regression: ability_index 0 on a source that ALSO offers the generic
// "Activate <card> for mana" placeholder. The placeholder is Kind "activate"
// and leaves decision.Option.Ability at the zero value, so its anchor equals
// index 0 -- indistinguishable from the first real ability. Before the fix,
// ability_index 0 selected the placeholder (activating a mana ability) instead
// of the first non-mana ability. White Mana Battery's IR order is
//
//	AB 0: {2}, {T}: Put a charge counter on CARDNAME. (non-mana)
//	AB 1: {T}, Remove any number of charge counters: Add {W}... (mana)
//
// untapped with no counters, both are offered, and the mana placeholder sorts
// first in the priority option list.
func TestOracleAbilityIndexZeroSelectsAbilityNotManaPlaceholder(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	const sc = `{"name":"ability-index-zero-vs-mana","cr":["602.2"],"why":"ability_index 0 must not select the generic for-mana placeholder","setup":{"p0":{"battlefield":["White Mana Battery"]}},"steps":[{"op":"activate","seat":0,"card":"p0:White Mana Battery","mana":"RR","ability_index":0},{"op":"resolve","seat":0}]}`
	res, err := RunOracleScenarioJSON(reg, []byte(sc))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fails) != 0 {
		t.Fatalf("scenario failed: %v\n%s", res.Fails, strings.Join(res.Transcript, "\n"))
	}
	if len(res.Snapshots) != 3 {
		t.Fatalf("%d snapshots, want 3 (setup + activate + resolve)", len(res.Snapshots))
	}
	setup := res.Snapshots[0]
	if p, ok := snapPerm(setup, "p0:White Mana Battery"); !ok || p.Tapped || len(p.Counters) != 0 {
		t.Fatalf("precondition: White Mana Battery = %+v, want an untapped permanent with no counters", p)
	}
	final := res.Snapshots[len(res.Snapshots)-1]
	p, ok := snapPerm(final, "p0:White Mana Battery")
	if !ok {
		t.Fatal("White Mana Battery left the battlefield")
	}
	// Ability 0's effect: a charge counter, and {2} spent from the pool.
	if got := p.Counters["CHARGE"]; got != 1 {
		t.Fatalf("charge counters = %d, want 1 (ability_index 0 = PutCounter); counters=%v", got, p.Counters)
	}
	if !p.Tapped {
		t.Fatal("White Mana Battery is untapped, want tapped (ability 0 costs {T})")
	}
	if got := final.Players[0].Pool; got != "" {
		t.Fatalf("pool = %q, want empty (the {2} cast fee was spent)", got)
	}
}
