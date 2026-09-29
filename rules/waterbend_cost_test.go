package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// waterbendAbilityCostCards are the corpus cards whose ACTIVATED ABILITY
// prints a `Cost$ Waterbend<...>` (CR 701.67a): before ParseCost modelled the
// head it landed in Cost.Unknown and substituted one generic, so the
// activation was withheld unless the pool already held the {N} and tapping
// artifacts/creatures could never help pay it. The census below re-measures
// this list against the pinned corpus; a `FORGE_REF` bump that adds or
// removes a carrier fails it, the same ratchet the other corpus censuses use.
var waterbendAbilityCostCards = []string{
	"Aang's Iceberg",
	"Aang, Swift Savior",
	// the_legend_of_kuruk_avatar_kuruk.txt: the AB$ waterbend ability is on
	// the back face, whose printed name is Avatar Kuruk.
	"Avatar Kuruk",
	"Flexible Waterbender",
	"Foggy Swamp Vinebender",
	"Geyser Leaper",
	"Giant Koi",
	"Invasion Submersible",
	"Katara, Bending Prodigy",
	"Katara, Water Tribe's Hope",
	"North Pole Patrol",
	"Ruthless Waterbender",
	"Water Tribe Rallier",
	"Waterbender Ascension",
	"Watery Grasp",
	"Yue, the Moon Spirit",
}

// waterbendOptionalCostCards are the corpus cards carrying a self-spell
// `S:Mode$ OptionalCost | Cost$ Waterbend<...>`: optionalCostViews dropped the
// view whenever ParseCost left a head in Unknown, so the "(optional cost)"
// cast branch was never built for any of them (CR 601.2f makes paying it the
// payer's choice, so both offers must exist).
var waterbendOptionalCostCards = []string{
	"Katara, Seeking Revenge",
	"Ruinous Waterbending",
	"Secret of Bloodbending",
	"Spirit Water Revival",
}

// TestParseCostModelsWaterbendLiteral pins the literal head. The {N} prices
// as generic (you may always just pay it) and the same N is the CAP on how
// much of that generic tapping artifacts and creatures may cover (CR
// 701.67a). Before this the head hit the unrecognised-symbol fallback: one
// phantom generic and a `Waterbend` Unknown entry, which is what withheld
// every ability and optional-cost carrier.
func TestParseCostModelsWaterbendLiteral(t *testing.T) {
	c := ParseCost("Waterbend<3>")
	if c.Generic != 3 || c.Waterbend != 3 {
		t.Fatalf("Waterbend<3> parsed to Generic=%d Waterbend=%d, want 3 and 3", c.Generic, c.Waterbend)
	}
	if len(c.Unknown) != 0 {
		t.Fatalf("Waterbend<3> reported Unknown %v, want none", c.Unknown)
	}
	if c.X != 0 || c.WaterbendX {
		t.Fatalf("Waterbend<3> announced X=%d WaterbendX=%v, want 0 and false", c.X, c.WaterbendX)
	}
}

// TestParseCostModelsWaterbendX pins the announced-X head: it contributes one
// {X} (exactly like a RaiseCost Waterbend<X>) and marks the waterbend amount
// open, since the cap is the announced value.
func TestParseCostModelsWaterbendX(t *testing.T) {
	c := ParseCost("Waterbend<X>")
	if c.X != 1 || !c.WaterbendX {
		t.Fatalf("Waterbend<X> parsed to X=%d WaterbendX=%v, want 1 and true", c.X, c.WaterbendX)
	}
	if c.Generic != 0 || c.Waterbend != 0 {
		t.Fatalf("Waterbend<X> priced Generic=%d Waterbend=%d, want 0 and 0", c.Generic, c.Waterbend)
	}
	if len(c.Unknown) != 0 {
		t.Fatalf("Waterbend<X> reported Unknown %v, want none", c.Unknown)
	}
}

// TestWaterbendCostCensus names every corpus carrier of the two cost shapes
// the parser used to drop, and asserts each one now parses to a modelled
// waterbend part (no Unknown entry). This is the class test behind the two
// set-audit leaves: fixing only Giant Koi or only Ruinous Waterbending would
// leave the other 18 carriers broken, and this census fails if any of them
// regresses.
func TestWaterbendCostCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	abilityNames := map[string]bool{}
	optionalNames := map[string]bool{}
	for _, card := range reg.Cards {
		for _, f := range card.Faces {
			for _, ab := range f.Abilities {
				raw := strings.TrimSpace(ab.Params["Cost"])
				if !strings.Contains(raw, "Waterbend<") {
					continue
				}
				// Precondition: the cost really carries the head, and it
				// parses to a modelled waterbend part. A `Waterbend` Unknown
				// here is the defect this ticket closed.
				c := ParseCost(raw)
				if len(c.Unknown) != 0 {
					t.Errorf("%s ability Cost$ %q still reports Unknown %v", f.Name, raw, c.Unknown)
					continue
				}
				if c.Waterbend <= 0 && !c.WaterbendX {
					t.Errorf("%s ability Cost$ %q parsed without a waterbend part: %+v", f.Name, raw, c)
					continue
				}
				abilityNames[f.Name] = true
			}
			for _, st := range f.Statics {
				if strings.TrimSpace(st.Mode) != "OptionalCost" {
					continue
				}
				raw := strings.TrimSpace(st.Params["Cost"])
				if !strings.Contains(raw, "Waterbend<") {
					continue
				}
				c := ParseCost(raw)
				if len(c.Unknown) != 0 {
					t.Errorf("%s OptionalCost Cost$ %q still reports Unknown %v", f.Name, raw, c.Unknown)
					continue
				}
				if c.Waterbend <= 0 && !c.WaterbendX {
					t.Errorf("%s OptionalCost Cost$ %q parsed without a waterbend part: %+v", f.Name, raw, c)
					continue
				}
				optionalNames[f.Name] = true
			}
		}
	}
	gotAbility := sortedKeys(abilityNames)
	gotOptional := sortedKeys(optionalNames)
	if !equalStrings(gotAbility, waterbendAbilityCostCards) {
		t.Errorf("ability-cost Waterbend carriers = %v, want %v", gotAbility, waterbendAbilityCostCards)
	}
	if !equalStrings(gotOptional, waterbendOptionalCostCards) {
		t.Errorf("optional-cost Waterbend carriers = %v, want %v", gotOptional, waterbendOptionalCostCards)
	}
}
