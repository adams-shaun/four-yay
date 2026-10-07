package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// Bargain (CR 702.166) corpus census (Ruling P12/D2-a's two-direction
// contract, the knownUnsupported shape): the EXACT set of corpus cards that
// carry the keyword primitive and that read each count head, pinned so a new
// carrier fails loudly rather than joining the playable pool with an
// unimplemented primitive. Measured 2026-10-04 over .cards/ at FORGE_REF.
//
// The three sets are disjoint in the pin they exercise:
//   - kw:Bargain — every K:Bargain carrier (22).
//   - count:Bargained — the "\..yes.\.no" spelling (5).
//   - count:Bargain — the bare "\..yes.\.no" spelling (1; Torch the Tower).
//
// A new carrier of any of the three fails the test and names itself, so the
// next implementer has to come back to the mechanic instead of it silently
// degrading to an unanswered ask.

var bargainKeywordCarriers = []string{
	"Agatha's Champion",
	"Archon's Glory",
	"Back for Seconds",
	"Beseech the Mirror",
	"Brave the Wilds",
	"Candy Grapple",
	"Diminisher Witch",
	"Dunbarrow Revivalist",
	"Farsight Ritual",
	"Hamlet Glutton",
	"High Fae Negotiator",
	"Ice Out",
	"Johann's Stopgap",
	"Kellan's Lightblades",
	"Realm-Scorcher Hellkite",
	"Rowan's Grim Search",
	"Stonesplitter Bolt",
	"Talion's Throneguard",
	"Tenacious Tomeseeker",
	"Thunderous Debut",
	"Torch the Tower",
	"Troublemaker Ouphe",
}

var bargainedCountCarriers = []string{
	"Brave the Wilds",
	"Candy Grapple",
	"Farsight Ritual",
	"Kellan's Lightblades",
	"Stonesplitter Bolt",
}

var bargainCountCarriers = []string{
	"Torch the Tower",
}

// TestBargainCarrierCensus pins the carrier sets in both directions: a new
// carrier is named (not silently tolerated) and a removed/unparsed carrier
// makes the pinned entry stale.
func TestBargainCarrierCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	if reg.Len() == 0 {
		t.Fatal("precondition: the corpus registry is empty, so the census would be vacuous")
	}
	// The registration itself is part of the ratchet: a revert of
	// effects.RegisterNonAPI / the value-head list must fail here, not just
	// in the coverage gate.
	for _, p := range []string{"kw:Bargain", "count:Bargained", "count:Bargain"} {
		if !effects.Supported()[p] {
			t.Fatalf("primitive %q is not registered as supported", p)
		}
	}
	gotKW, gotBargained, gotBargain := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, c := range reg.AllCards() {
		if c == nil || len(c.Faces) == 0 {
			continue
		}
		name := c.Faces[0].Name
		for _, p := range c.Primitives() {
			if p == "kw:Bargain" {
				gotKW[name] = true
			}
		}
		for _, h := range c.ValueHeads() {
			switch h {
			case "count:Bargained":
				gotBargained[name] = true
			case "count:Bargain":
				gotBargain[name] = true
			}
		}
	}
	checkCensus := func(label string, got map[string]bool, want []string) {
		t.Helper()
		names := make([]string, 0, len(got))
		for n := range got {
			names = append(names, n)
		}
		slices.Sort(names)
		sortedWant := append([]string(nil), want...)
		slices.Sort(sortedWant)
		if !slices.Equal(names, sortedWant) {
			t.Errorf("%s carrier set changed:\n got %q\nwant %q", label, names, sortedWant)
		}
	}
	checkCensus("kw:Bargain", gotKW, bargainKeywordCarriers)
	checkCensus("count:Bargained", gotBargained, bargainedCountCarriers)
	checkCensus("count:Bargain", gotBargain, bargainCountCarriers)
}
