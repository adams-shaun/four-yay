package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// priorityCards builds the Board the casting rules read: a Cards census
// keyed by ObjID (the deciding seat's own hand/graveyard/battlefield facts
// both adapters fill), with IsMain true so the land drop and cast groups
// are reachable. A test that wants a factless board passes nil.
func priorityCards(facts map[state.ObjID]Card) Board {
	b := Board{IsMain: true, Cards: TableOf(facts)}
	return b
}

// castDecision runs Decide on a KPriority decision whose options carry
// exactly the kinds it supplies (cast/play_land/activate/pass) and returns
// the option index Decide chose. The option's Index is its position, as the
// engine numbers options.
func castDecision(b Board, opts []decision.Option) (int, *decision.Decision) {
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1, Options: opts}
	in := Decide(b, &d, rng(1))
	if len(in.Choices) != 1 {
		return -1, &d
	}
	return in.Choices[0], &d
}

// cast option shortcuts: a cast of a creature and a cast of a non-creature,
// and a play_land, each at the given index, so a test can choose the order
// it draws its crime and which of two is listed first (the positional
// "/ first-of-list" defect casts the earlier one).
func castCreature(idx int, id state.ObjID) decision.Option {
	return decision.Option{Index: idx, Kind: "cast", Obj: id}
}
func castSpell(idx int, id state.ObjID) decision.Option {
	return decision.Option{Index: idx, Kind: "cast", Obj: id}
}
func playLand(idx int, id state.ObjID) decision.Option {
	return decision.Option{Index: idx, Kind: "play_land", Obj: id}
}

// TestCastCreatureOverSpell is C1: a creature (the only card type that
// attacks every turn and thereby ends the game) is cast ahead of a
// one-shot spell, even when the spell is the first cast option in the list.
// The position-of-legalActions defect (cast whichever "cast" came first)
// and a "cast the first cast option" mutation both pick the option at index
// 0 here and fail.
func TestCastCreatureOverSpell(t *testing.T) {
	b := priorityCards(map[state.ObjID]Card{
		1: {Creature: true, Power: 1}, // a 1/1 in the deciding seat's hand
		2: {CMC: 1},                   // a one-cost instant, same mana
	})
	got, d := castDecision(b, []decision.Option{
		castSpell(0, 2),    // first in the list
		castCreature(1, 1), // second
	})
	if got != 1 {
		t.Fatalf("cast = option %d (obj %d), want the creature (obj 1) over the spell", got, d.Options[got].Obj)
	}
}

// TestCastHigherPowerCreature is C2: among creatures, the higher power is
// cast first -- the more damage it will deal the next time it attacks and
// every time after.
func TestCastHigherPowerCreature(t *testing.T) {
	b := priorityCards(map[state.ObjID]Card{
		1: {Creature: true, Power: 1},
		2: {Creature: true, Power: 4},
	})
	got, d := castDecision(b, []decision.Option{
		castCreature(0, 1), // a 1/1 listed first
		castCreature(1, 2), // a 4/4 listed second
	})
	if got != 1 {
		t.Fatalf("cast = option %d (obj %d), want the 4-power creature (obj 2)", got, d.Options[got].Obj)
	}
}

// TestCastBiggerSpell is C3: among non-creatures the higher mana value is
// cast first -- the bot's best guess at the bigger effect when it cannot
// read effects, so the mana it spends buys the largest opening it can
// afford.
func TestCastBiggerSpell(t *testing.T) {
	b := priorityCards(map[state.ObjID]Card{
		1: {CMC: 1},
		2: {CMC: 3},
	})
	got, d := castDecision(b, []decision.Option{
		castSpell(0, 1), // a one-mana spell listed first
		castSpell(1, 2), // a three-mana spell listed second
	})
	if got != 1 {
		t.Fatalf("cast = option %d (obj %d), want the 3-mana spell (obj 2)", got, d.Options[got].Obj)
	}
}

// TestCastAlternativeCostIsNotOrdinary is C4: a cast paying an alternative
// cost (read off Option.Mode, never the label) is ranked differently from
// the same card's ordinary cast, so a double-up (kicker) or a second use
// (flashback) outranks the plain cast of the same card. Treating every cast
// as an ordinary one -- the "ignore Mode" mutation -- ties the two options
// and the positional tiebreak picks the first (index 0), failing here.
func TestCastAlternativeCostIsNotOrdinary(t *testing.T) {
	b := priorityCards(map[state.ObjID]Card{
		1: {Creature: true, Power: 2},
	})
	// Ordinary cast at index 0, kicked (a strictly bigger effect for the
	// extra mana the bot is committing anyway) at index 1.
	got, d := castDecision(b, []decision.Option{
		{Index: 0, Kind: "cast", Obj: 1},
		{Index: 1, Kind: "cast", Obj: 1, Mode: "kicked"},
	})
	if got != 1 {
		t.Fatalf("cast = option %d (mode %q), want the kicked cast (mode \"kicked\") over the ordinary one", got, d.Options[got].Mode)
	}
	// Same card, ordinary vs flashback (a spent card returning is free
	// value): the flashback is preferred.
	got2, d2 := castDecision(b, []decision.Option{
		{Index: 0, Kind: "cast", Obj: 1},
		{Index: 1, Kind: "cast", Obj: 1, Mode: "flashback"},
	})
	if got2 != 1 {
		t.Fatalf("flashback cast = option %d (mode %q), want the flashback over the ordinary cast", got2, d2.Options[got2].Mode)
	}
}

// TestCastUnreadableOptionIsLowRank is C5: a cast whose Obj is in no zone
// the Board reads cards for is a zero-facts card -- lower rank than a real
// creature, but the choice never panics and still validates.
func TestCastUnreadableOptionIsLowRank(t *testing.T) {
	b := priorityCards(map[state.ObjID]Card{
		1: {Creature: true, Power: 1},
	})
	got, d := castDecision(b, []decision.Option{
		castSpell(0, 999), // no census facts
		castCreature(1, 1),
	})
	if err := d.Validate(decision.Intent{Seq: 1, Player: 0, Choices: []int{got}}); err != nil {
		t.Fatalf("intent %d failed Validate: %v", got, err)
	}
	if got != 1 {
		t.Fatalf("cast = option %d (obj %d), want the readable creature (obj 1) over the unreadable spell", got, d.Options[got].Obj)
	}
}

// TestCastTiesBreakOnIndex is C6: two casts scoring identically (an equal
// pair the policy cannot separate) break on option index, the deterministic
// tiebreak, so no map iteration order reaches the answer.
func TestCastTiesBreakOnIndex(t *testing.T) {
	b := priorityCards(map[state.ObjID]Card{
		1: {Creature: true, Power: 2},
		2: {Creature: true, Power: 2},
	})
	got, _ := castDecision(b, []decision.Option{
		castCreature(0, 1),
		castCreature(1, 2),
	})
	if got != 0 {
		t.Fatalf("tied cast = option %d, want the lower index (0)", got)
	}
}

// TestPlayLandBasicOverNonbasic is L1: a basic land (unconditional colour,
// never enters tapped, never demands life) is played before a nonbasic,
// even when the nonbasic is offered first. Playing the first "play_land"
// option -- the pre-B4 positional defect, and the "play the first land"
// mutation -- picks the nonbasic at index 0 and fails.
func TestPlayLandBasicOverNonbasic(t *testing.T) {
	b := priorityCards(map[state.ObjID]Card{
		1: {Basic: false}, // a nonbasic (e.g. a dual or a fetchland)
		2: {Basic: true},  // a basic
	})
	got, d := castDecision(b, []decision.Option{
		playLand(0, 1), // nonbasic listed first
		playLand(1, 2), // basic listed second
	})
	if got != 1 {
		t.Fatalf("land drop = option %d (obj %d), want the basic land (obj 2) over the nonbasic", got, d.Options[got].Obj)
	}
}

// TestChooseLandColourAware is L1's bl1 fix: the land drop is colour-aware
// instead of preferring basics by category. A hand that needs {U} holds a
// basic Plains (white) and a basic Island (blue); the blind basic-first rule
// plays whichever basic is offered first (here the Plains at index 0), a
// colour-blind drop, and the hand never gets the blue it needs. The fix
// ranks a land by how much of the hand's unmet colour need it covers, so the
// Island is played. Index 1 is blue (U); index 0 white (W).
func TestChooseLandColourAware(t *testing.T) {
	b := priorityCards(map[state.ObjID]Card{
		1:  {CMC: 1, ManaCost: "U", Castable: true},    // a one-blue spell in hand
		10: {Basic: true, Produces: prod(state.MW, 1)}, // a Plains (white), offered first
		11: {Basic: true, Produces: prod(state.MU, 1)}, // an Island (blue), offered second
	})
	got, d := castDecision(b, []decision.Option{
		playLand(0, 10), // the wrong-colour basic, listed first
		playLand(1, 11), // the blue Island, listed second
	})
	if d.Options[got].Obj != 11 {
		t.Fatalf("land drop = obj %d (option %d), want the blue Island (obj 11) the hand's {U} needs, not the basic-first Plains", d.Options[got].Obj, got)
	}
}

// TestLandBeforeSpell is G0: with a land and a cast both offered and nothing
// to tap for mana, the land drop is taken first -- it is free, unconditional
// and only raises the mana ceiling for the rest of the turn, so it weakly
// strictly dominates an order that casts first. Inverting the land-vs-spell
// order (the mutation) casts the spell, failing here.
func TestLandBeforeSpell(t *testing.T) {
	b := priorityCards(map[state.ObjID]Card{
		1: {Creature: true, Power: 2},
		2: {Basic: true},
	})
	got, d := castDecision(b, []decision.Option{
		castCreature(0, 1), // a cast listed first
		playLand(1, 2),     // a land to play listed second
	})
	if got != 1 || d.Options[got].Kind != "play_land" {
		t.Fatalf("priority = option %d (kind %q), want the land drop (play_land) before the cast", got, d.Options[got].Kind)
	}
}

// TestChooseCastAndLandPickOneWithoutClamp calls the two ranking branches
// directly, so clamp cannot mask a pick: chooseCast returns exactly one
// offered cast option (or -1 with none), chooseLand the same for lands —
// the total answer any mix of options still validates at the Decide level.
func TestChooseCastAndLandPickOneWithoutClamp(t *testing.T) {
	b := priorityCards(map[state.ObjID]Card{
		1: {Creature: true, Power: 3},
		2: {Creature: true, Power: 1},
	})
	castOpts := []decision.Option{castCreature(0, 2), castCreature(1, 1)}
	if got := b.chooseCast(&decision.Decision{Player: 0, Kind: decision.KPriority, Options: castOpts}); got != 1 {
		t.Fatalf("chooseCast = option %d, want the 3-power creature (index 1)", got)
	}
	if got := b.chooseCast(&decision.Decision{Player: 0, Kind: decision.KPriority, Options: []decision.Option{}}); got != -1 {
		t.Fatalf("chooseCast with no offer = %d, want -1", got)
	}
	landOpts := []decision.Option{
		{Index: 0, Kind: "play_land", Obj: 1},
		{Index: 1, Kind: "play_land", Obj: 2},
	}
	b.Cards = TableOf(map[state.ObjID]Card{1: {Basic: false}, 2: {Basic: true}})
	if got := b.chooseLand(&decision.Decision{Player: 0, Kind: decision.KPriority, Options: landOpts}); got != 1 {
		t.Fatalf("chooseLand = option %d, want the basic (index 1)", got)
	}
	if got := b.chooseLand(&decision.Decision{Player: 0, Kind: decision.KPriority, Options: []decision.Option{}}); got != -1 {
		t.Fatalf("chooseLand with no offer = %d, want -1", got)
	}
}

// TestCastEqualCostNonCreaturesTieOnIndex pins the withdrawal of the
// effect-class bonus the task checkpoint added: two non-creatures at equal
// CMC previously separated by the oracle-phrase class their text matched
// (classifyCard read an anti-removal word like "destroy"/"exile" and priced
// it above a draw or a burn). That bonus rested on oracle-phrase guessing
// about what a spell does -- it never read what the removal answers, never
// read a card's printed type line or oracle text, and cannot tell a burn from
// a draw whose texts differ only in words the matcher did not happen to
// list -- so it is gone. A cast the policy cannot separate ranks by CMC
// alone, and an equal-CMC tie breaks on option index (C6), the
// deterministic answer: the first-offered spell is the pick, whatever it
// does, because the policy has no honest basis to tell it apart.
func TestCastEqualCostNonCreaturesTieOnIndex(t *testing.T) {
	b := priorityCards(map[state.ObjID]Card{
		1: {CMC: 2},
		2: {CMC: 2},
	})
	got, d := castDecision(b, []decision.Option{
		castSpell(0, 2), // draw listed first
		castSpell(1, 1), // removal listed second
	})
	if got != 0 {
		t.Fatalf("cast = option %d (obj %d), want the lower-index equal-cost spell (obj 2 at index 0): no class bonus separates them", got, d.Options[got].Obj)
	}
}

// TestCastNonCreatureRanksByCostNotOracle is C3's honest contract now that
// the class bonus is gone: a non-creature ranks by printed mana value, its
// best guess at the bigger effect when it cannot read effects, and an
// unreadable (blank-text) card is ranked by the same cost -- never by an
// oracle phrase. A higher-CMC card outranks a lower-CMC one regardless of
// what their texts say.
func TestCastNonCreatureRanksByCostNotOracle(t *testing.T) {
	b := priorityCards(map[state.ObjID]Card{
		1: {CMC: 4},
		2: {CMC: 1},
	})
	got, d := castDecision(b, []decision.Option{
		castSpell(0, 2), // a 1-mana burn listed first
		castSpell(1, 1), // a 4-mana unreadable artifact listed second
	})
	if got != 1 {
		t.Fatalf("cast = option %d (obj %d), want the higher-CMC unreadable card (obj 1): cost ranks, not oracle", got, d.Options[got].Obj)
	}
}

// of Forge ManaCost strings, mirroring rules/mana.go's ParseCost.CMC() the
// way both adapter halves need it (no rules import, Ruling F7). {X} counts
// as 0 off the stack, ordinary hybrid/Phyrexian/colourless as one generic,
// monocolour hybrid at its generic face, brace form normalised, "no cost"/"" as 0.
func TestCmcOf(t *testing.T) {
	cases := map[string]int32{
		"R":         1,
		"U U":       2,
		"2 U U":     4,
		"1 BP BP":   3,
		"3":         3,
		"X":         0,
		"X G":       1,
		"G":         1,
		"C":         1,
		"{2}{U}{U}": 4,
		"2W":        2,
		"2/W":       2,
		"{2/W}":     2,
		"no cost":   0,
		"":          0,
	}
	for mc, want := range cases {
		if got := CmcOf(mc); got != want {
			t.Errorf("CmcOf(%q) = %d, want %d", mc, got, want)
		}
	}
}

// TestCmcOfPlainMatchesSlow pins CmcOf's allocation-free path to the
// general one on every string it accepts: a fixed table of real and odd
// Forge spellings plus every 1-3 symbol combination over a symbol alphabet
// covering each per-symbol branch.
func TestCmcOfPlainMatchesSlow(t *testing.T) {
	syms := []string{"X", "W", "U", "B", "R", "G", "C", "S", "0", "1", "12", "+3", "-1", "2W", "2/W", "W/U",
		"BP", "UP", "WU", "x", "no", "cost", "1a", "99999999999", ""}
	seps := []string{" ", "  ", "\t", " \n"}
	var inputs []string
	for _, a := range syms {
		inputs = append(inputs, a)
		for _, b := range syms {
			for _, s := range seps {
				inputs = append(inputs, a+s+b, s+a+s+b+s)
				for _, c := range syms[:8] {
					inputs = append(inputs, a+s+b+" "+c)
				}
			}
		}
	}
	inputs = append(inputs, "no cost", "No Cost", " no cost ", "no  cost", "1 BP BP", "X X R", "2 U U")
	for _, mc := range inputs {
		got, ok := cmcOfPlain(mc)
		if !ok {
			t.Fatalf("cmcOfPlain rejected plain ASCII %q", mc)
		}
		if want := cmcOfSlow(mc); got != want {
			t.Errorf("cmcOfPlain(%q) = %d, slow path = %d", mc, got, want)
		}
	}
	for _, mc := range []string{"{2}{U}", "{X}", "U U", "{2/W}"} {
		if _, ok := cmcOfPlain(mc); ok {
			t.Errorf("cmcOfPlain accepted %q, which must take the slow path", mc)
		}
		if CmcOf(mc) != cmcOfSlow(mc) {
			t.Errorf("CmcOf(%q) differs from the slow path", mc)
		}
	}
	if n := testing.AllocsPerRun(100, func() { _ = CmcOf("2 W/U BP X") }); n != 0 {
		t.Errorf("CmcOf plain path allocates %.1f times", n)
	}
}
