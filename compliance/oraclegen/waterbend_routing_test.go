package oraclegen

import (
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// A declined waterbend tap-helpers ask (rules/pay/castasks.go's
// "Tap <name> to waterbend for 1" option) maps to NO XMage answer: XMage
// pays the waterbend generic from the prefilled mana pool and never poses
// the tap ask, so neither half of the generic declined pair ("no" boolean,
// target skip) has an ask to answer and the pair would break the next
// scripted ask (measured on the 2026-10-09 census: TLA's twelve waterbend
// activations were harness rows "Found wrong choice command").
func TestWaterbendHelperDeclinedIsNotScripted(t *testing.T) {
	cases := []struct {
		name string
		d    rules.OracleDecision
		want []XAnswer
	}{
		{
			// Giant Koi's declined helper pick: XMage never asks.
			name: "declined waterbend helper: nothing",
			d: rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Via: "activate",
				Options: 1, Min: 0, Max: 1, First: "Tap Giant Koi to waterbend for 1"},
			want: nil,
		},
		{
			// Katara, Water Tribe's Hope (Waterbend<X>): the helper ask is
			// declined there too; the X announce that follows is a separate
			// decision, still scripted by the ordinary mapping.
			name: "declined waterbend helper with X announce pending: nothing",
			d: rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Via: "answer",
				Options: 1, Min: 0, Max: 1, First: "Tap Katara, Water Tribe's Hope to waterbend for 1"},
			want: nil,
		},
		{
			// A pick (not a decline) of the same ask is an ordinary accepted
			// contribution, still scripted by the ordinary mapping.
			name: "accepted waterbend helper: ordinary mapping",
			d: rules.OracleDecision{Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Via: "answer",
				Options: 1, Min: 0, Max: 1, First: "Tap Giant Koi to waterbend for 1", Picks: []string{"Tap Giant Koi to waterbend for 1"}},
			want: nil, // routed by nothing here; the point is it is not the declined rule's shape
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !waterbendHelperDeclined(tc.d) && len(tc.d.Picks) == 0 {
				t.Fatalf("waterbendHelperDeclined missed %+v", tc.d)
			}
			if len(tc.d.Picks) > 0 && waterbendHelperDeclined(tc.d) {
				t.Fatalf("waterbendHelperDeclined claimed an accepted pick: %+v", tc.d)
			}
			r := newAnswerRouting([]rules.OracleDecision{tc.d})
			as, owned := r.route(0)
			if len(tc.d.Picks) == 0 {
				if !owned || len(as) != 0 {
					t.Fatalf("declined helper routed (%v, %v), want owned and empty", as, owned)
				}
			} else if owned && len(as) != 0 {
				t.Fatalf("accepted helper was routed to empty: %v", as)
			}
		})
	}
}
