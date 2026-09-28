package paymirror

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestRoundSevenFindingsMirror replays round-7 paymirror finding games end to
// end: no cast is a mismatch, the live-vs-clone control is equivalent, and
// each named cast gets its root-caused verdict.
//
//   - 4038 (commander4-fresh, 14x + 11x in commander4-new):
//     no_matching_mana_option on Urza's Workshop's metalcraft ability, whose
//     non-literal Amount$ the wheel labels "Add C" exactly like its plain
//     {T}: Add {C}; the float now proves the option on a clone
//     (verifiedProductions) and the cast mirrors equivalently.
//   - 6085: Chain of Vapor's only target is the Lotus Petal paying for it;
//     the float sacrificed it before the cast (cast_not_offered_after_float,
//     now expected float_removed_every_target).
//   - 6191: Roaming Throne's cast-trigger order in A batched Butcher of
//     Malakir's trigger from the float-sacrificed Eldrazi Spawn
//     (follow_up_unmappable); A's order is kept over the rest and the end
//     state passes floatTriggerOnly (expected float_trigger_precedes_cast).
//   - 5108 (constructed): Warping Wail's exile mode's only target is the
//     Eldrazi Scion paying for it (follow_up_unmappable on the modes ask,
//     now expected float_removed_every_target, CR 700.2a).
//
// fb-20260927T163321Z-69285807 re-pinned the commander seeds (4038, 6191)
// after the command-zone payment-plan fix: a commander in the command zone
// now gets a plan, the auto-pay bots cast it through one, and those games
// move. fb-20260927T212721Z-1f9fc4b0 restores active-player priority after
// an as-enters election; this adds priority/pass events and moves seed 4038's
// Eldrazi Monument from seq 7684 to 7696 and seed 6191's Roaming Throne from
// seq 9956 to 9969. Reverting that continuation restores both old sequences.
func TestRoundSevenFindingsMirror(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	d, err := LoadDecks(reg)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		seed      uint64
		decks     []string
		commander bool
		seq       uint64
		want      string // the named cast's verdict key ("" = equivalent)
	}{
		{4038, []string{"ulalek-eldrazi", "rakdos-muscle-scam-exe", "vivi-ornitier-cedh", "foundations-calling-all-angels"}, true, 7696, ""},
		{6085, []string{"vivi-ornitier-cedh", "ulalek-eldrazi", "deadly-disguise", "rakdos-muscle-scam-exe"}, true, 173, "expected:float_then_cast:float_removed_every_target"},
		{6191, []string{"foundations-wretched-ranks", "avengers-assemble", "ulalek-eldrazi", "hearthhull-worldseed-landfall"}, true, 9969, "expected:float_then_cast:float_trigger_precedes_cast"},
		{5108, []string{"eldrazi-stompy", "mono-red-prowess"}, false, 675, "expected:float_then_cast:float_removed_every_target"},
	} {
		reports := round6Game(t, d, GameSpec{Seed: tc.seed, Decks: tc.decks, Commander: tc.commander, Policy: "bot"})
		found := false
		for _, r := range reports {
			st, key := r.Verdict()
			if st == Mismatch || (st == Unmirrorable && !r.ExpectedUnmirrorable()) {
				t.Errorf("seed %d seq %d %q: %s %s", tc.seed, r.Seq, r.Card, st, key)
			}
			if r.Control == nil || r.Control.Status != Equivalent {
				t.Errorf("seed %d seq %d %q: control %+v", tc.seed, r.Seq, r.Card, r.Control)
			}
			if r.Seq != tc.seq {
				continue
			}
			found = true
			if key != tc.want {
				t.Errorf("seed %d seq %d %q: verdict %s %q, want %q", tc.seed, r.Seq, r.Card, st, key, tc.want)
			}
		}
		if !found {
			t.Errorf("seed %d: no planned cast at seq %d (the game no longer reaches the finding)", tc.seed, tc.seq)
		}
	}
}

// TestFloatTrimmedTriggerOrderAllowsOnlyFloatTriggers pins the proof gating
// the trimmed trigger order: an option only run A was offered is dropped only
// when it is a trigger whose source is the source of an ability the float
// put on the stack; the mirror may offer nothing A was not offered.
func TestFloatTrimmedTriggerOrderAllowsOnlyFloatTriggers(t *testing.T) {
	base := fixtureEngine(t)
	n := len(base.G.Objs)
	butcher, cicada, unsealing := state.ObjID(2), state.ObjID(3), state.ObjID(4)
	float := state.ObjID(n) // the float's trigger, sourced from butcher
	b := base.Clone()
	b.G.Objs[float-1].Ability = &cards.SA{API: "Sacrifice"}
	b.G.Objs[float-1].Source = butcher
	b.G.Stack = []state.ObjID{float}
	trig := func(i int, src state.ObjID) decision.Option {
		return decision.Option{Index: i, Kind: "trigger", Obj: src, Label: objName(base, src)}
	}
	r := Recorded{Kind: decision.KTriggerOrder, Player: 0, Min: 3, Max: 3,
		Options: []decision.Option{trig(0, butcher), trig(1, unsealing), trig(2, cicada)}, Choices: []int{2, 0, 1}}
	d := &decision.Decision{Kind: decision.KTriggerOrder, Player: 0, Min: 2, Max: 2,
		Options: []decision.Option{trig(0, unsealing), trig(1, cicada)}}
	got, ok := floatTrimmedTriggerOrder(b, r, d, nil)
	if !ok || len(got) != 2 || got[0] != 1 || got[1] != 0 {
		t.Fatalf("trimmed order = %v %v, want [1 0] (A's cicada, unsealing)", got, ok)
	}
	// The A-only trigger's source put nothing on the stack: not trimmed.
	if _, ok := floatTrimmedTriggerOrder(b, r, d, []state.ObjID{float}); ok {
		t.Fatal("a trigger the float did not add was dropped")
	}
	// The mirror offered an option A was not offered: not trimmed.
	d2 := &decision.Decision{Kind: decision.KTriggerOrder, Player: 0, Min: 2, Max: 2,
		Options: []decision.Option{trig(0, unsealing), trig(1, state.ObjID(5))}}
	if _, ok := floatTrimmedTriggerOrder(b, r, d2, nil); ok {
		t.Fatal("a mirror-only option was accepted")
	}
	// A non-order decision is never trimmed.
	r3 := r
	r3.Kind = decision.KTarget
	if _, ok := floatTrimmedTriggerOrder(b, r3, d, nil); ok {
		t.Fatal("a target ask was trimmed")
	}
}
