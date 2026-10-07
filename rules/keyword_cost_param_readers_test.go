// keyword_cost_param_readers_test.go — every remaining cost-bearing keyword
// reader must read its cost through cards.Face.KeywordCostParam (the first
// colon-field, Forge's KeywordWithCost.parse), not KeywordParam's whole
// remainder.
//
// The corpus carries no second-colon line for these nine heads today (measured
// at the pin: 0 each), so this is a latent-trap removal, not a live charge bug.
// A synthetic line with a trailing colon-field is the only way to make the two
// reads differ, and this test does exactly that: if a reader still called
// KeywordParam, ParseCost would see the trailing ":ReduceCost$ X:reminder"
// text and mis-price the spell.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// TestCostReadersDropTrailingKeywordFields proves each reader resolves the
// first-field cost and omits the trailing field. Every case asserts the
// precondition that KeywordParam returns the WHOLE remainder, so the compared
// reads genuinely differ.
func TestCostReadersDropTrailingKeywordFields(t *testing.T) {
	t.Parallel()
	c, diags := cards.ParseBytes("kcp2.txt", []byte(
		"Name:Fixture\nTypes:Creature\n"+
			"K:Entwine:1 G:ReduceCost$ X:reminder one\n"+
			"K:Surge:2 R:ReduceCost$ X:reminder two\n"+
			"K:Replicate:2 R:ReduceCost$ X:reminder three\n"+
			"K:Multikicker:3 B:ReduceCost$ X:reminder four\n"+
			"K:Squad:1 W:ReduceCost$ X:reminder five\n"+
			"K:Buyback:4 U:ReduceCost$ X:reminder six\n"+
			"K:Harmonize:1 G:ReduceCost$ X:reminder seven\n"+
			"Oracle:fixture\n"))
	if len(diags) != 0 {
		t.Fatalf("fixture parse diagnostics: %v", diags)
	}
	f := c.Faces[0]

	// The precondition: KeywordParam really does hand back everything after
	// the first colon, so a plain KeywordParam read would hand ParseCost the
	// trailing field too.
	for _, head := range []string{"Entwine", "Surge", "Replicate", "Multikicker", "Squad", "Buyback", "Harmonize"} {
		raw, ok := f.KeywordParam(head)
		if !ok || raw == "" {
			t.Fatalf("precondition: KeywordParam(%q) = %q %v, want the whole remainder", head, raw, ok)
		}
		if got, ok := f.KeywordCostParam(head); !ok || got == raw {
			t.Fatalf("precondition: KeywordCostParam(%q) = %q %v, want a first field differing from the whole %q", head, got, ok, raw)
		}
	}

	// The reader assertions. Each reader must price exactly the first field
	// and carry no Unknown token from the trailing text.
	// col builds a mana cost with n of one colour at state's index.
	col := func(idx int, n int32) state.Mana {
		var m state.Mana
		m[idx] = n
		return m
	}
	for _, tc := range []struct {
		head string
		read func(*cards.Face) (Cost, bool)
		want Cost
	}{
		{"Entwine", entwineCost, Cost{Generic: 1, Colored: col(state.MG, 1)}},
		{"Surge", surgeCost, Cost{Generic: 2, Colored: col(state.MR, 1)}},
		{"Replicate", replicateCost, Cost{Generic: 2, Colored: col(state.MR, 1)}},
		{"Multikicker", multikickerCost, Cost{Generic: 3, Colored: col(state.MB, 1)}},
		{"Squad", squadCost, Cost{Generic: 1, Colored: col(state.MW, 1)}},
		{"Buyback", buybackCost, Cost{Generic: 4, Colored: col(state.MU, 1)}},
		{"Harmonize", harmonizeCost, Cost{Generic: 1, Colored: col(state.MG, 1)}},
	} {
		got, ok := tc.read(f)
		if !ok {
			t.Errorf("%s reader: ok = false, want a priced first field", tc.head)
			continue
		}
		if len(got.Unknown) != 0 {
			t.Errorf("%s reader: cost carries Unknown %v -- a trailing field reached ParseCost", tc.head, got.Unknown)
		}
		if got.Generic != tc.want.Generic || got.Colored != tc.want.Colored {
			t.Errorf("%s reader: cost = generic %d colored %v, want generic %d colored %v (the first-field cost only)",
				tc.head, got.Generic, got.Colored, tc.want.Generic, tc.want.Colored)
		}
	}
}

// TestKeywordCostParamPlotMiracleAndCaptures covers the three cast_begin.go
// reads: the Plot and Miracle branches of continueCast price their first field,
// and the replicate/multikicker/squad captures store the first field so the
// later payment re-parse (the ask handlers) prices the SAME string the offer
// gate read.
func TestKeywordCostParamPlotMiracleAndCaptures(t *testing.T) {
	t.Parallel()
	c, diags := cards.ParseBytes("kcp2b.txt", []byte(
		"Name:Fixture\nTypes:Sorcery\n"+
			"K:Plot:2 G:ReduceCost$ X:reminder plot\n"+
			"K:Miracle:1 W:ReduceCost$ X:reminder miracle\n"+
			"K:Replicate:3 U:ReduceCost$ X:reminder replicate\n"+
			"K:Multikicker:2 B:ReduceCost$ X:reminder multikicker\n"+
			"K:Squad:4 R:ReduceCost$ X:reminder squad\n"+
			"Oracle:fixture\n"))
	if len(diags) != 0 {
		t.Fatalf("fixture parse diagnostics: %v", diags)
	}
	f := c.Faces[0]

	for _, tc := range []struct {
		head string
		want string
	}{
		{"Plot", "2 G"},
		{"Miracle", "1 W"},
		{"Replicate", "3 U"},
		{"Multikicker", "2 B"},
		{"Squad", "4 R"},
	} {
		// Precondition: the whole-remainder read differs from the cost field,
		// so the two accessors really select different strings here.
		whole, ok := f.KeywordParam(tc.head)
		if !ok || whole == tc.want {
			t.Fatalf("precondition: KeywordParam(%q) = %q %v, want the whole remainder", tc.head, whole, ok)
		}
		got, ok := f.KeywordCostParam(tc.head)
		if !ok || got != tc.want {
			t.Errorf("KeywordCostParam(%q) = %q %v, want %q true", tc.head, got, ok, tc.want)
		}
		// The first field must be exactly what ParseCost prices here, with no
		// Unknown from the trailing text.
		if pc := ParseCost(got); len(pc.Unknown) != 0 {
			t.Errorf("ParseCost(KeywordCostParam(%q)) carries Unknown %v", tc.head, pc.Unknown)
		}
	}
}
