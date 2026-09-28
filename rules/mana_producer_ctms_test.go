// Tests for the ctms sub-shape of the frozen castfilter1/2 row: the positive
// ManaAdd producers OTHER than the acted AB$ Mana ability -- the AB$
// ManaReflected replacement body and the cumulative-upkeep AddMana action --
// must carry the same Treasure/Cave/Desert/Snow producer tag effMana stamps,
// so a cast that spends their mana counts it under the matching
// Count$CastTotalManaSpent <Type> head. The count head's own grammar is
// driven from state.TypedManaTags, so a modelled type counts and an unknown
// one stays the fail-closed 0.
//
// Kept in its own file so the ticket cannot conflict on a shared test file.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestManaReflectedProducerTagsItsSourceType is the ManaReflected half,
// driven end to end on the real corpus carrier Cactus Preserve (a Land
// Desert whose {T}: ManaReflected ability reflects a land you control's
// mana). Before the fix the reflected unit was emitted with a bare "R"
// Counter, so it landed in the pool untagged and never counted as a Desert.
func TestManaReflectedProducerTagsItsSourceType(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	preserve := onBoardCard(t, e, 0, corpusAlternativeCard(t, "Cactus Preserve"))
	// The reflected-from land, so ManaReflectedCandidates offers a colour.
	onBoard(t, e, 0, "Name:Plain Mountain\nTypes:Basic Land Mountain\nA:AB$ Mana | Cost$ T | Produced$ R\nOracle:x\n")

	// Preconditions: the producer is where the rule reads it, prints the
	// Desert type the head filters on, and starts untagged.
	po := e.G.Obj(preserve)
	if po == nil || po.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Cactus Preserve not on the battlefield: %+v", po)
	}
	isDesert := false
	for _, ty := range po.Face().Types {
		if ty == "Desert" {
			isDesert = true
		}
	}
	if !isDesert {
		t.Fatal("precondition: Cactus Preserve does not print the Desert type")
	}
	if got := e.G.Players[0].TypedMana[state.TypedDesert].Total(); got != 0 {
		t.Fatalf("precondition: Desert tally = %d, want 0 before the activation", got)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("precondition: pool = %d, want 0 before the activation", got)
	}

	e.pending = nil
	e.priorityRound()
	d := e.Pending()
	if d == nil {
		t.Fatal("no priority decision")
	}
	act := -1
	for _, o := range d.Options {
		if o.Kind == "activate" && o.Obj == preserve {
			act = o.Index
		}
	}
	if act < 0 {
		t.Fatalf("Cactus Preserve's reflected mana ability is not offered: %+v", d.Options)
	}
	submitChoices(t, e, act)
	// A single reflectable colour resolves directly; a colour wheel (more
	// than one candidate) would be a different board, so fail loudly.
	if cd := e.Pending(); cd != nil && cd.Kind == decision.KChoose {
		t.Fatalf("unexpected colour ask on a one-candidate reflection: %+v", cd.Options)
	}

	// The unit is in the pool AND tagged DesertR (not the untagged R the fix
	// removes).
	if got := e.G.Players[0].Pool[state.MR]; got != 1 {
		t.Fatalf("pool R = %d, want 1 (the reflected red unit)", got)
	}
	if got := e.G.Players[0].TypedMana[state.TypedDesert][state.MR]; got != 1 {
		t.Fatalf("Desert tally R = %d, want 1 (ManaReflected must tag the unit from its source's Types)", got)
	}
	if got := e.G.Players[0].Snow[state.MR]; got != 0 {
		t.Fatalf("snow tally R = %d, want 0 (a Desert is not snow)", got)
	}
}

// cumulativeDesertSource is a synthetic cumulative-upkeep carrier whose upkeep
// COST produces the mana (Braid of Fire's AddMana<1/R> shape) and whose type
// is Desert. No corpus card pairs a typed permanent with a cumulative
// AddMana, so the fixture is synthetic; Braid of Fire itself is the corpus
// shape and stays an untagged Enchantment.
const cumulativeDesertSource = "Name:Ctms Oasis\nManaCost:0\nTypes:Land Desert\n" +
	"K:Cumulative upkeep:AddMana<1/C>:Add {C}.\nOracle:x\n"

// TestCumulativeUpkeepAddManaTagsItsSourceType is the cumulative half: paying
// the AddMana upkeep cost of a Desert permanent must tag the produced unit
// Desert<colour>, so it counts as Desert in the filtered head. Before the fix
// the action emitted a bare "C" Counter.
func TestCumulativeUpkeepAddManaTagsItsSourceType(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	oasis := onBoard(t, e, 0, cumulativeDesertSource)
	// Preconditions: the permanent is where the rule reads it and prints
	// both the type and the keyword the action rides on.
	oo := e.G.Obj(oasis)
	if oo == nil || oo.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Ctms Oasis not on the battlefield: %+v", oo)
	}
	if !oo.Face().HasKeyword("Cumulative upkeep") {
		t.Fatal("precondition: Ctms Oasis does not print Cumulative upkeep")
	}
	if got := e.G.Players[0].TypedMana[state.TypedDesert].Total(); got != 0 {
		t.Fatalf("precondition: Desert tally = %d, want 0 before the upkeep", got)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("precondition: pool = %d, want 0 before the upkeep", got)
	}

	resolveUpkeepCumulative(t, e)
	d := e.Pending()
	if d == nil {
		t.Fatal("no cumulative pay/sacrifice ask after resolution")
	}
	pay := -1
	for _, o := range d.Options {
		if o.Kind == "cumulative_pay" {
			pay = o.Index
		}
	}
	if pay < 0 {
		t.Fatalf("the AddMana upkeep cost is not payable: %+v", d.Options)
	}
	submitChoices(t, e, pay)

	// The unit is in the pool AND tagged DesertC.
	if got := e.G.Players[0].Pool[state.MC]; got != 1 {
		t.Fatalf("pool C = %d, want 1 (the upkeep cost's produced unit)", got)
	}
	if got := e.G.Players[0].TypedMana[state.TypedDesert][state.MC]; got != 1 {
		t.Fatalf("Desert tally C = %d, want 1 (the cumulative AddMana action must tag the unit from its source's Types)", got)
	}
	// The permanent survived paying its own upkeep cost.
	if e.G.Obj(oasis).Zone != state.ZBattlefield {
		t.Fatalf("Ctms Oasis left the battlefield after paying: %s", e.G.Obj(oasis).Zone)
	}
}

// TestCastTotalManaSpentGrammarCountsModelledAndFailsClosed drives the count
// head end to end: the SAME cast, paid with a Desert-tagged unit the
// ManaReflected producer created, counts for a MODELLED type (Desert, from
// state.TypedManaTags) and stays the fail-closed 0 for a type the pool
// cannot tag (Artifact) -- so the compared values genuinely differ and the
// unknown-type assertion cannot pass vacuously. The modelled subtest fails
// without the producer tagging fix (the reflected unit is untagged, so the
// Desert spend reads 0).
func TestCastTotalManaSpentGrammarCountsModelledAndFailsClosed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		arg       string
		wantDraws int
	}{
		{"modelled type counts", "Desert", 1},
		{"unknown type fails closed", "Artifact", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := "Name:Type Prober\nManaCost:R\nTypes:Creature\n" +
				"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigDraw | TriggerDescription$x\n" +
				"SVar:TrigDraw:DB$ Draw | NumCards$ X\n" +
				"SVar:X:Count$CastTotalManaSpent " + tc.arg + "\nOracle:x\n"
			e := handEngine(t, card(t, fixture))
			spell := e.G.Zone(state.ZHand, 0)[0]
			preserve := onBoardCard(t, e, 0, corpusAlternativeCard(t, "Cactus Preserve"))
			onBoard(t, e, 0, "Name:Plain Mountain\nTypes:Basic Land Mountain\nA:AB$ Mana | Cost$ T | Produced$ R\nOracle:x\n")
			// Produce one Desert-tagged red unit through the ManaReflected
			// producer under test.
			e.pending = nil
			e.priorityRound()
			d := e.Pending()
			if d == nil {
				t.Fatal("no priority decision")
			}
			act := -1
			for _, o := range d.Options {
				if o.Kind == "activate" && o.Obj == preserve {
					act = o.Index
				}
			}
			if act < 0 {
				t.Fatalf("Cactus Preserve's reflected mana ability is not offered: %+v", d.Options)
			}
			submitChoices(t, e, act)
			// Precondition: the compared values differ -- the pool holds a
			// tagged Desert red unit, so the cast below really spends one.
			if got := e.G.Players[0].TypedMana[state.TypedDesert][state.MR]; got != 1 {
				t.Fatalf("precondition: Desert tally R = %d, want 1", got)
			}
			e.pending = nil
			castMode(t, e, spell, "")
			finishCast(t, e, spell)
			drainEtb(t, e)
			if got := e.G.Obj(spell).ManaDesertSpent; got != 1 {
				t.Fatalf("precondition: ManaDesertSpent = %d, want 1", got)
			}
			if got := len(e.G.Zone(state.ZHand, 0)); got != tc.wantDraws {
				t.Fatalf("drew %d cards, want %d (Count$CastTotalManaSpent %s)", got, tc.wantDraws, tc.arg)
			}
		})
	}
}
