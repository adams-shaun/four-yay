package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
)

var parsedCostSink Cost

// The pure-parse hotspot benchmarks and allocation budgets live with the
// parser in rules/cost (parse_hotspots_test.go); this one measures the
// Engine's compiled-cost cache in front of it.
func BenchmarkEngineParseCostCachedHybridNonMana(b *testing.B) {
	c := card(b, "Name:Cache Cost\nManaCost:1 G\nTypes:Creature Test\nPT:1/1\nA:AB$ Draw | Cost$ GWP 2B Sac<1/Creature>\nOracle:x\n")
	e := New(Config{Names: []string{"you"}, Decks: [][]*cards.Card{{c}}})
	b.ReportAllocs()
	for range b.N {
		parsedCostSink = e.parseCost("GWP 2B Sac<1/Creature>")
	}
}
