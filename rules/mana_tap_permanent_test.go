package rules

// Mana abilities whose cost taps another permanent (tapXType<N/Spec>):
// Springleaf Drum, Heritage Druid, Birchlore Rangers, Relic of Legends and
// the rest of the census' `tap_other` family. manaAbilityPayablePool refused
// every part of that shape unconditionally, so the ability was never offered
// on the manual path even though the cast/activation flow already pays it
// (rules/cast.go tapPermanentCostAsk). These tests pin the offer, the tap
// election, the taps themselves and CR 302.6 (tapping a summoning-sick
// creature to pay a cost that is not that creature's own {T} ability is
// legal).

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// manaTapBoard seeds seat 0's battlefield with every named corpus card and
// returns the engine, the replay config and the id map.
func manaTapBoard(t *testing.T, seed uint64, names ...string) (*Engine, Config, map[string]state.ObjID) {
	t.Helper()
	moves := make(map[string]state.Zone, len(names))
	for _, n := range names {
		moves[n] = state.ZBattlefield
	}
	return edrBoard(t, testutil.CorpusRegistry(t), seed, moves)
}

// TestManaTapPermanentCostSpringleafDrum: "{T}, Tap an untapped creature you
// control: Add one mana of any color". With one creature (entered this turn,
// so summoning-sick) the offer is present; activating it taps BOTH the Drum
// and the creature, then asks the colour and adds exactly one mana.
func TestManaTapPermanentCostSpringleafDrum(t *testing.T) {
	e, cfg, ids := manaTapBoard(t, 9901, "Springleaf Drum", "Llanowar Elves")
	drum, elf := ids["Springleaf Drum"], ids["Llanowar Elves"]
	// Precondition: the creature must be untapped and summoning-sick, so the
	// assertion below is really testing that a susceptible creature can pay a
	// foreign tap cost (CR 302.6 restricts only the creature's OWN {T}).
	// edrBoard placed the cards before turn 2 began; re-enter the Elf during
	// turn 2 (a real MoveZone pair) so the entry flag its apply sets survives
	// the log-only replay rebuild.
	e.emit(events.Event{Kind: events.MoveZone, Obj: elf, From: state.ZBattlefield, To: state.ZExile})
	e.emit(events.Event{Kind: events.MoveZone, Obj: elf, From: state.ZExile, To: state.ZBattlefield})
	e.priorityRound()
	edrSeatZeroPriority(t, e)
	if e.G.Obj(elf).Tapped {
		t.Fatal("fixture: Llanowar Elves must start untapped")
	}
	if !e.G.Obj(elf).SummonSick {
		t.Fatal("fixture: Llanowar Elves re-entered this turn, want summoning-sick")
	}
	if !hasActivateOption(e, drum) {
		t.Fatalf("Springleaf Drum not offered for mana: %+v", e.Pending().Options)
	}
	submitChoices(t, e, activateOption(t, e, drum))
	// The tap cost is a single forced candidate, so no tap election is asked;
	// the Produced$ Any colour choice is.
	answerManaChoose(t, e, "Add G")
	if !e.G.Obj(drum).Tapped {
		t.Error("Springleaf Drum did not tap as its own {T} cost")
	}
	if !e.G.Obj(elf).Tapped {
		t.Error("Springleaf Drum's tapXType<1/Creature> cost did not tap the creature")
	}
	if got := e.G.Players[0].Pool.Total(); got != 1 {
		t.Fatalf("pool total after activation = %d, want 1", got)
	}
	if got := e.G.Players[0].Pool[state.MG]; got != 1 {
		t.Fatalf("pool G after activation = %d, want 1", got)
	}
	replayCheck(t, e, cfg)
}

// TestManaTapPermanentCostSpringleafDrumNeedsACreature: with no creature to
// tap the ability is not offered. Assert the negation AND that the ability is
// still a real mana ability (the offer machinery evaluated it): the same
// fixture with a creature does offer it.
func TestManaTapPermanentCostSpringleafDrumNeedsACreature(t *testing.T) {
	e, _, ids := manaTapBoard(t, 9902, "Springleaf Drum")
	drum := ids["Springleaf Drum"]
	if hasActivateOption(e, drum) {
		t.Fatalf("Springleaf Drum offered with no creature to tap: %+v", e.Pending().Options)
	}
	if abs := e.availableManaAbilities(0, drum); len(abs) != 0 {
		t.Fatalf("availableManaAbilities = %d, want 0 with no creature", len(abs))
	}

	// The positive control: the identical fixture plus a creature offers it,
	// so the negative above cannot pass because the card is unknown/unparsed.
	e2, _, ids2 := manaTapBoard(t, 9903, "Springleaf Drum", "Llanowar Elves")
	if !hasActivateOption(e2, ids2["Springleaf Drum"]) {
		t.Fatalf("positive control: Springleaf Drum not offered with a creature: %+v", e2.Pending().Options)
	}
}

// TestManaTapPermanentCostHeritageDruid: "Tap three untapped Elves you
// control: Add {G}{G}{G}". Heritage Druid is itself an Elf, so the three
// candidates include the source; the forced election taps all three and adds
// three green.
func TestManaTapPermanentCostHeritageDruid(t *testing.T) {
	e, cfg, ids := manaTapBoard(t, 9904, "Heritage Druid", "Llanowar Elves", "Elvish Mystic")
	druid := ids["Heritage Druid"]
	elves := []state.ObjID{druid, ids["Llanowar Elves"], ids["Elvish Mystic"]}
	for _, id := range elves {
		if e.G.Obj(id).Tapped {
			t.Fatalf("fixture: elf %d must start untapped", id)
		}
	}
	if !hasActivateOption(e, druid) {
		t.Fatalf("Heritage Druid not offered for mana: %+v", e.Pending().Options)
	}
	submitChoices(t, e, activateOption(t, e, druid))
	for _, id := range elves {
		if !e.G.Obj(id).Tapped {
			t.Errorf("Heritage Druid's tapXType<3/Elf> cost left elf %d untapped", id)
		}
	}
	pool := e.G.Players[0].Pool
	if pool[state.MG] != 3 || pool.Total() != 3 {
		t.Fatalf("pool after Heritage Druid = %v, want exactly {G}{G}{G}", pool)
	}
	replayCheck(t, e, cfg)
}

// TestManaTapPermanentCostHeritageDruidElection: with four untapped Elves the
// three-per-part election is a real choice: the ability is offered, the ask
// offers exactly the four candidates and the chosen three are tapped.
func TestManaTapPermanentCostHeritageDruidElection(t *testing.T) {
	e, cfg, ids := manaTapBoard(t, 9905, "Heritage Druid", "Llanowar Elves", "Elvish Mystic", "Fyndhorn Elves")
	druid := ids["Heritage Druid"]
	submitChoices(t, e, activateOption(t, e, druid))
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Min != 3 || d.Max != 3 {
		t.Fatalf("tap election = %+v, want KChoose Min=Max=3", d)
	}
	for _, o := range d.Options {
		if o.Kind != "tapcost" {
			t.Fatalf("tap election option kind = %q, want tapcost", o.Kind)
		}
	}
	if len(d.Options) != 4 {
		t.Fatalf("tap election offered %d options, want 4", len(d.Options))
	}
	// Tap exactly the three non-Druid Elves: the Druid itself is a legal
	// candidate too, so this proves the answer drives the taps.
	var chosen []int
	for _, o := range d.Options {
		if o.Obj != druid {
			chosen = append(chosen, o.Index)
		}
	}
	submitChoices(t, e, chosen...)
	if e.G.Obj(druid).Tapped {
		t.Error("Heritage Druid was tapped though it was not among the chosen three")
	}
	for _, id := range []state.ObjID{ids["Llanowar Elves"], ids["Elvish Mystic"], ids["Fyndhorn Elves"]} {
		if !e.G.Obj(id).Tapped {
			t.Errorf("chosen elf %d was not tapped", id)
		}
	}
	if got := e.G.Players[0].Pool.Total(); got != 3 {
		t.Fatalf("pool total = %d, want 3", got)
	}
	replayCheck(t, e, cfg)
}

// TestManaTapPermanentCostFoodAndTokenSpecs proves the class covers the two
// census shapes whose matching permanents the census board does not carry:
// The Cabbage Merchant's tapXType<2/Food> and Baylen's
// tapXType<2/Permanent.token/token>. The census' fixed board has artifacts,
// enchantments and creatures but no Food and no token permanent, so those two
// rows legitimately stay not_offered there; these boards supply the matching
// permanents and assert the offer and the payment.
func TestManaTapPermanentCostFoodAndTokenSpecs(t *testing.T) {
	e, _, _ := newFixtureDeck(t, 9906, "Name:Tap Probe\nManaCost:G\nTypes:Instant\nA:SP$ Draw | Num$ 1\nOracle:x\n")
	reg := testutil.CorpusRegistry(t)
	cabbage, ok := reg.Lookup("The Cabbage Merchant")
	if !ok {
		t.Fatal("corpus fixture: The Cabbage Merchant missing")
	}
	baylen, ok := reg.Lookup("Baylen, the Haymaker")
	if !ok {
		t.Fatal("corpus fixture: Baylen, the Haymaker missing")
	}
	cm := onBoardCard(t, e, 0, cabbage)
	by := onBoardCard(t, e, 0, baylen)
	// Two Foods pay the Cabbage Merchant; two token permanents pay Baylen.
	foods := []state.ObjID{
		onBoard(t, e, 0, "Name:Food Alpha\nTypes:Artifact Food\nOracle:x\n"),
		onBoard(t, e, 0, "Name:Food Beta\nTypes:Artifact Food\nOracle:x\n"),
	}
	tokens := []state.ObjID{
		onBoard(t, e, 0, "Name:Token Alpha\nTypes:Creature Soldier\nPT:1/1\nOracle:x\n"),
		onBoard(t, e, 0, "Name:Token Beta\nTypes:Creature Soldier\nPT:1/1\nOracle:x\n"),
	}
	for _, id := range tokens {
		e.G.Obj(id).IsToken = true
	}
	// The eventless placements above stale the pending decision; re-derive the
	// priority options from the new board.
	e.pending = nil
	e.priorityRound()
	edrSeatZeroPriority(t, e)
	// Baylen's other abilities (Draw/PutCounter) are not mana abilities; its
	// mana ability must still be the offered one.
	if !hasActivateOption(e, cm) {
		t.Fatalf("The Cabbage Merchant not offered with two Foods: %+v", e.Pending().Options)
	}
	if !hasActivateOption(e, by) {
		t.Fatalf("Baylen, the Haymaker not offered with two tokens: %+v", e.Pending().Options)
	}
	submitChoices(t, e, activateOption(t, e, cm))
	// The two-Food part is exactly its candidate count (forced); the
	// Produced$ Any colour ask follows.
	answerManaChoose(t, e, "Add G")
	for _, id := range foods {
		if !e.G.Obj(id).Tapped {
			t.Errorf("Cabbage Merchant left Food %d untapped", id)
		}
	}
	if got := e.G.Players[0].Pool.Total(); got != 1 {
		t.Fatalf("pool total after Cabbage Merchant = %d, want 1", got)
	}
	submitChoices(t, e, activateOption(t, e, by))
	answerManaChoose(t, e, "Add G")
	for _, id := range tokens {
		if !e.G.Obj(id).Tapped {
			t.Errorf("Baylen left token %d untapped", id)
		}
	}
	if got := e.G.Players[0].Pool.Total(); got != 2 {
		t.Fatalf("pool total after Baylen = %d, want 2", got)
	}
}
