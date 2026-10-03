package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// Beseech the Queen is the corpus example of Forge's 2B monocolour hybrid.
// Its payment allows BB or two generic mana, but neither a third colour nor
// one generic mana; cumulative scaling must preserve that complete alternative.
func TestTwobridCostParsingPaymentAndScaling(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	if _, ok := reg.Lookup("Beseech the Queen"); !ok {
		t.Fatal("precondition: corpus card Beseech the Queen missing")
	}
	c := ParseCost("2B")
	if c.Generic != 0 || len(c.Twobrid) != 1 || c.Twobrid[0] != (Twobrid{Generic: 2, Col: 'B'}) {
		t.Fatalf("ParseCost(2B) = %+v, want a two-generic-or-black alternative", c)
	}
	if !poolCanPay(c, pool(0, 0, 1, 0, 0, 0)) || poolCanPay(c, pool(0, 0, 0, 1, 0, 0)) {
		t.Fatal("2B must accept one black and reject one red")
	}
	if poolCanPay(c, pool(0, 0, 0, 0, 0, 1)) || !poolCanPay(c, pool(0, 0, 0, 0, 0, 2)) {
		t.Fatal("2B must reject one generic mana and accept two")
	}
	one, two := scaleCost(c, 1), scaleCost(c, 2)
	if len(one.Twobrid) != 1 || len(two.Twobrid) != 2 || len(two.Twobrid) == len(one.Twobrid) {
		t.Fatalf("precondition/scale: age one=%+v age two=%+v, want 1 vs 2 twobrid pips", one, two)
	}
	if poolCanPay(two, pool(0, 0, 1, 0, 0, 0)) || !poolCanPay(two, pool(0, 0, 2, 0, 0, 0)) {
		t.Fatal("scaled 2B/2B upkeep must require two black or four generic mana")
	}
}
