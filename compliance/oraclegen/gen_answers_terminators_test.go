package oraclegen

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/rules"
)

// TestXAnswersTerminatorShapes is the census ratchet for the answer
// terminators and multi-pick grouping the std3 re-audit diverged on. Each row
// pins the ANSWER shape for one mechanism family, not a card: an "up to N"
// ask that XMage keeps posing needs [target_skip]/[mode_skip], a one-option
// optional ask is a real decline, a per-player target ask is answered seat by
// seat, and a single decision's multi-card pick is ONE '^'-joined definition.
func TestXAnswersTerminatorShapes(t *testing.T) {
	tests := []struct {
		name  string
		d     rules.OracleDecision
		modes map[string]int
		want  []XAnswer
	}{
		{
			// A mode decision with fewer picks than Max: XMage keeps asking
			// up to Max, so stop it with the mode queue's skip token.
			name:  "mode under max: mode then mode skip",
			d:     rules.OracleDecision{Step: 0, Seat: 0, Kind: "mode", GorgeKind: "modes", Options: 2, Min: 1, Max: 2, Picks: []string{"A"}, PickIdx: []int{0}, PickRefs: []string{"p0:Card"}, PickKinds: []string{"mode"}},
			modes: map[string]int{"A": 1},
			want:  []XAnswer{{0, "mode", "1"}, {0, "mode", "[mode_skip]"}},
		},
		{
			// An all-optional mode decision XMage still asks (choose none):
			// the skip is the whole answer.
			name:  "mode zero picks: mode skip only",
			d:     rules.OracleDecision{Step: 0, Seat: 0, Kind: "mode", GorgeKind: "modes", Options: 2, Min: 0, Max: 2, PickKinds: nil},
			modes: map[string]int{"A": 1, "B": 2},
			want:  []XAnswer{{0, "mode", "[mode_skip]"}},
		},
		{
			// A one-option mode ask is never dropped as forced: XMage still
			// chooses (BLB Season, WOE Rankle's Prank).
			name:  "mode one option: not skipped as forced",
			d:     rules.OracleDecision{Step: 0, Seat: 0, Kind: "mode", GorgeKind: "modes", Options: 1, Min: 1, Max: 1, Picks: []string{"A"}, PickIdx: []int{0}, PickRefs: []string{"p0:Card"}, PickKinds: []string{"mode"}},
			modes: map[string]int{"A": 1},
			want:  []XAnswer{{0, "mode", "1"}},
		},
		{
			// A "mode" decision whose pick label is not one of the face's
			// modes (the discard picker shares the decision kind) is a
			// makeChoose choice, never a mode number.
			name:  "mode label not a mode: choice queue",
			d:     rules.OracleDecision{Step: 0, Seat: 0, Kind: "mode", GorgeKind: "modes", Options: 2, Min: 1, Max: 1, Picks: []string{"Wastes"}, PickIdx: []int{0}, PickRefs: []string{"p0:Wastes#27"}, PickKinds: []string{"card"}},
			modes: map[string]int{"A": 1},
			want:  []XAnswer{{0, "choice", "Wastes"}},
		},
		{
			// Fewer picks than an "up to N" target ask allows: stop XMage.
			name: "target under max: target then target skip",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "target", Options: 2, Min: 0, Max: 3, Picks: []string{"Grizzly Bears (b)"}, PickIdx: []int{0}, PickRefs: []string{"p1:Grizzly Bears"}, PickKinds: []string{"permanent"}},
			want: []XAnswer{{0, "target", "Grizzly Bears"}, {0, "target", "[target_skip]"}},
		},
		{
			// A slot gorge never posed at all (zero picks): XMage still asks.
			name: "target zero picks: target skip only",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "target", Options: 1, Min: 0, Max: 2, PickKinds: nil},
			want: []XAnswer{{0, "target", "[target_skip]"}},
		},
		{
			// TargetsForEachPlayer$ (CR 601.2c): XMage asks one target per
			// player in seat order, so seat 0's empty ask gets a skip before
			// seat 1's pick.
			name: "per-player target: seat 0 skip, seat 1 pick",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "target", Options: 1, Min: 0, Max: 1, PerPlayer: true, SeatCount: 2, Picks: []string{"Grizzly Bears (b)"}, PickIdx: []int{0}, PickRefs: []string{"p1:Grizzly Bears"}, PickKinds: []string{"permanent"}},
			want: []XAnswer{{0, "target", "[target_skip]"}, {0, "target", "Grizzly Bears"}},
		},
		{
			name: "per-player target: both seats answered in seat order",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "target", Options: 2, Min: 0, Max: 2, PerPlayer: true, SeatCount: 2, Picks: []string{"Elves", "Bears"}, PickIdx: []int{1, 0}, PickRefs: []string{"p1:Bears", "p0:Elves"}, PickKinds: []string{"permanent", "permanent"}},
			want: []XAnswer{{0, "target", "Elves"}, {0, "target", "Bears"}},
		},
		{
			// A forced one-option ask (Min==Max==Options==1) XMage does not
			// pose at all.
			name: "forced single option: skipped",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 1, Min: 1, Max: 1, Picks: []string{"Pay B"}, PickIdx: []int{0}, PickRefs: []string{"Pay B"}, PickKinds: []string{"pay_B"}},
			want: nil,
		},
		{
			// A min-0 single option is a real decline XMage still asks
			// (MKM Break Out, TLA Destined Confrontation).
			name: "min-0 single option: decline scripted",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 1, Min: 0, Max: 1, Picks: nil, PickKinds: nil},
			want: []XAnswer{{0, "choice", "no"}, {0, "target", "[target_skip]"}},
		},
		{
			// One decision's multi-card pick is ONE makeChoose definition: the
			// driver splits it on '^' (TDM Rakshasa's Bargain, DFT Stock Up,
			// MKM Hide in Plain Sight/Polygraph Orb).
			name: "multi-card pick: one caret-joined choice",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 4, Min: 2, Max: 2, Picks: []string{"Wastes", "Wastes"}, PickIdx: []int{0, 1}, PickRefs: []string{"p0:Wastes#27", "p0:Wastes#39"}, PickKinds: []string{"dig", "dig"}},
			want: []XAnswer{{0, "choice", "Wastes^Wastes"}},
		},
		{
			// A short multi-card pick joins the picks and then stops the
			// dialog with the choice queue's own skip token.
			name: "short multi-card pick: caret-joined then choice skip",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 4, Min: 0, Max: 3, Picks: []string{"Wastes", "Wastes"}, PickIdx: []int{0, 1}, PickRefs: []string{"p0:Wastes#27", "p0:Wastes#39"}, PickKinds: []string{"dig", "dig"}},
			want: []XAnswer{{0, "choice", "Wastes^Wastes"}, {0, "choice", "[choice_skip]"}},
		},
		{
			// A multi-pick decision with a target-kind pick is NOT one
			// makeChoose dialog; each pick stays its own answer.
			name: "multi-pick with a target: not joined",
			d:    rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 2, Min: 0, Max: 2, Picks: []string{"Grizzly Bears", "Forest"}, PickIdx: []int{0, 1}, PickRefs: []string{"p1:Grizzly Bears", "p0:Forest"}, PickKinds: []string{"search", "search"}},
			want: []XAnswer{{0, "target", "Grizzly Bears"}, {0, "target", "Forest"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A vacuous fixture would assert nothing about the routing.
			if tt.d.Options < 1 {
				t.Fatal("fixture must offer a genuine ask")
			}
			got := XAnswers([]rules.OracleDecision{tt.d}, 1, tt.modes)
			var want [][]XAnswer
			if tt.want != nil {
				want = [][]XAnswer{tt.want}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("XAnswers = %#v, want %#v", got, want)
			}
		})
	}
}

// TestForcedSingleOptionOnlyExactOne proves the forced-ask predicate covers
// exactly Min==Max==Options==1, so a min-0 option stays a real ask.
func TestForcedSingleOptionOnlyExactOne(t *testing.T) {
	if !forcedSingleOption(rules.OracleDecision{Kind: "choose_n", Options: 1, Min: 1, Max: 1}) {
		t.Fatal("an exact-one choose_n must be forced")
	}
	for name, d := range map[string]rules.OracleDecision{
		"min zero":         {Kind: "choose_n", Options: 1, Min: 0, Max: 1},
		"two options":      {Kind: "choose_n", Options: 2, Min: 1, Max: 1},
		"target":           {Kind: "target", Options: 1, Min: 1, Max: 1},
		"mode":             {Kind: "mode", Options: 1, Min: 1, Max: 1},
		"order":            {Kind: "order", Options: 1, Min: 1, Max: 1},
		"with target pick": {Kind: "choose_n", Options: 1, Min: 1, Max: 1, Picks: []string{"Bears"}, PickRefs: []string{"p1:Bears"}, PickKinds: []string{"permanent"}},
	} {
		t.Run(name, func(t *testing.T) {
			if forcedSingleOption(d) {
				t.Fatalf("forcedSingleOption(%+v) = true; want a real ask", d)
			}
		})
	}
}

// TestOptionalCostCastNoShapes pins the optional-additional-cost families and
// the pool-affordability rule: XMage asks whenever the additional cost alone
// is payable from the cast's pool (ManaCostImpl.canPay is unconditionally
// true for generic/colorless, and a coloured pip needs its colour present),
// so only an affordable cost gets a scripted "no".
func TestOptionalCostCastNoShapes(t *testing.T) {
	cases := []struct {
		name, pool, cost string
		want             bool
	}{
		{name: "generic always payable", pool: "R", cost: "4", want: true},
		{name: "generic payable from coloured", pool: "CG", cost: "2", want: true},
		{name: "coloured pip present", pool: "CW", cost: "2 W W", want: true},
		{name: "coloured pip absent", pool: "CG", cost: "B", want: false},
		{name: "hybrid either half", pool: "G", cost: "W/U", want: false},
		{name: "hybrid first half", pool: "U", cost: "W/U", want: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := poolPaysCost(c.pool, c.cost); got != c.want {
				t.Fatalf("poolPaysCost(%q,%q) = %v, want %v", c.pool, c.cost, got, c.want)
			}
		})
	}
}

// mechanismFamilies is the corpus census of the cast-option mechanisms this
// ticket routes: how many card faces carry each. It is a ratchet -- a corpus
// pin that adds or removes a carrier changes a count here and fails loudly.
// Measured 2026-10-05 at FORGE_REF 95f04e8a04c8925fa97cb226fc3341cabcc90a53.
var mechanismFamilies = map[string]int{
	"Kicker":            239,
	"Offspring":         21,
	"OptionalCost":      40,
	"Spree":             21,
	"MinCharmNum":       121,
	"TargetMinZeroUpTo": 405,
}

// TestMechanismFamilyCensus pins the per-family carrier counts over the whole
// corpus, so a new Kicker/Offspring/OptionalCost/Spree carrier fails loudly
// and is named. The counts are printed on failure.
func TestMechanismFamilyCensus(t *testing.T) {
	reg := censusRegistry(t)
	counts := map[string]int{}
	names := map[string][]string{}
	add := func(fam, name string) {
		counts[fam]++
		names[fam] = append(names[fam], name)
	}
	for i := range reg.Cards {
		for fi := range reg.Cards[i].Faces {
			f := reg.Cards[i].Faces[fi]
			if _, ok := f.KeywordParam("Kicker"); ok {
				add("Kicker", f.Name)
			}
			if _, ok := f.KeywordParam("Offspring"); ok {
				add("Offspring", f.Name)
			}
			if _, ok := f.KeywordParam("Spree"); ok {
				add("Spree", f.Name)
			}
			if faceHasOptionalCostStatic(f) {
				add("OptionalCost", f.Name)
			}
			if faceHasMinCharmNum(f) {
				add("MinCharmNum", f.Name)
			}
			if faceHasTargetMinZeroUpTo(f) {
				add("TargetMinZeroUpTo", f.Name)
			}
		}
	}
	for fam, want := range mechanismFamilies {
		got := counts[fam]
		if got != want {
			sort.Strings(names[fam])
			t.Errorf("%s carriers = %d, want %d\n%s", fam, got, want, strings.Join(names[fam], "\n"))
		}
	}
	for fam, got := range counts {
		if _, pinned := mechanismFamilies[fam]; !pinned {
			sort.Strings(names[fam])
			t.Errorf("%s carriers = %d (not pinned; pin it deliberately)\n%s", fam, got, strings.Join(names[fam], "\n"))
		}
	}
}

func faceHasOptionalCostStatic(f *cards.Face) bool {
	for i := range f.Statics {
		if strings.EqualFold(strings.TrimSpace(f.Statics[i].Mode), "OptionalCost") {
			return true
		}
	}
	return false
}

func faceHasMinCharmNum(f *cards.Face) bool {
	for _, sa := range f.Abilities {
		if strings.TrimSpace(sa.Params["MinCharmNum"]) != "" {
			return true
		}
	}
	for i := range f.Statics {
		if strings.TrimSpace(f.Statics[i].Params["MinCharmNum"]) != "" {
			return true
		}
	}
	return false
}

// faceHasTargetMinZeroUpTo reports a ValidTgts$ ask with a literal TargetMin$ 0
// and a TargetMax$ above 1 on the spell or one of its SVar sub-abilities: the
// "up to N" target family whose terminators this ticket adds.
func faceHasTargetMinZeroUpTo(f *cards.Face) bool {
	hit := func(p map[string]string) bool {
		if strings.TrimSpace(p["ValidTgts"]) == "" {
			return false
		}
		if strings.TrimSpace(p["TargetMin"]) != "0" {
			return false
		}
		max := strings.TrimSpace(p["TargetMax"])
		if max == "" {
			return false
		}
		if n, ok := atoiOK(max); ok && n > 1 {
			return true
		}
		return false
	}
	for _, sa := range f.Abilities {
		for s := sa; s != nil; s = s.Sub {
			if hit(s.Params) {
				return true
			}
		}
	}
	for _, body := range f.SVars {
		if hit(svarParams(body)) {
			return true
		}
	}
	return false
}

func atoiOK(s string) (int, bool) {
	n := 0
	if s == "" {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}
