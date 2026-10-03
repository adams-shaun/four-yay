package paymirror

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestRoundNineFindingsMirror replays the round-9 paymirror finding games
// (testdata/round9.jsonl: repo deck names or random card-name lists, no
// script text) end to end: no cast is a mismatch, the live-vs-clone control
// is equivalent, and each named cast gets its root-caused verdict.
//
//   - 10860 seq 8083 (Three Visits) and 10141 (Worldly Tutor), commander4
//     production_differs "Command Tower planned G, manual GGG": Command
//     Tower's Produced$ Combo ColorIdentity reaches the wheel as the raw
//     "Add Combo ColorIdentity", which no label class parses, so the
//     any-colour class matched Tectonic Split's granted "Add three mana of
//     any one color" on the same land. The plan was right (the tower makes
//     one mana); the float now proves the option on a clone
//     (verifiedProductions) and rejects an any-colour label naming another
//     amount (anyLabelFits). After both command-zone fixes merged (the
//     trigger walk of fb-20260927T160557Z-b958ef31 plus the payment plan of
//     fb-20260927T163321Z-69285807) the Three Visits cast no longer occurs
//     and Worldly Tutor lands at seq 3239, now pinned equivalent.
//   - 11056's former seq 6387 (Artisan of Kozilek) finding is no longer reached
//     after fb-20260927T160557Z-b958ef31 correctly fires Sidar Jabari's
//     command-zone Eminence trigger; the added draw/discard changes the game
//     trajectory. The seed remains a full-game clean-mirror check.
//     Before that behavior change it was a commander4 mirror_missing_decision:
//     run A's window queued Syr Konrad's two dies triggers (two sacrificed
//     Eldrazi Spawn) under one CR 603.3b order ask; the float put each on the
//     stack as it triggered, with no ask (floatOnlyTriggerOrder). The end
//     state then differs only by the float's triggers preceding the cast,
//     once floatTriggerOnly masks the since-fork ObjIDs that a "countered: no
//     legal targets" event names (maskNewObjects): expected
//     float_trigger_precedes_cast.
//   - 10056 seq 6108 (Lagomos, Hand of Hatred), random4
//     mirror_missing_decision: the same order ask over Scrap Trawler's two
//     triggers from two sacrificed Treasures; the float's triggers were each
//     removed as they were put on the stack (no legal target), so the stack
//     is untouched and the route is equivalent.
//   - 8175 seq 5587 (Sacrifice), commander4-r8 state_differs: the float's
//     painland life payment triggered an opponent's ability before the cast
//     (a float_trigger_precedes_cast shape), and floatTriggerOnly failed only
//     on "targets_chosen" naming the permuted trigger ObjID -- a mask gap in
//     the harness, not an engine difference. It surfaced with 87586f456,
//     which made Sacrifice (fixed-count mandatory sacrifice) plannable at all.
//
// fb-20260927T163321Z-69285807 moved the commander seeds here (10860, 11056,
// 8175) with the command-zone payment-plan fix: a commander in the command
// zone now gets a plan, the auto-pay bots cast it through one, and those games
// move. 11056's Artisan of Kozilek finding is no longer reached once the
// merged trigger-walk fix fires Sidar Jabari's command-zone Eminence trigger,
// so the seed keeps an empty pin (the round-10 convention, seed 11828) and
// asserts the whole game is mismatch-free and control-equivalent. 8175's
// Sacrifice cast no longer occurs, but the same float_trigger_precedes_cast
// shape now surfaces on Songs of the Damned, so that seed keeps a pin.
// 10860's Three Visits cast no longer occurs and Worldly Tutor moves to
// seq 3239, so the seed pins that cast as equivalent; its Command Tower
// production root cause stays pinned by
// TestMatchProductionsRespectsAnyColourAmount.
// lifeLost1's AFLifeLost publication moves the same Lagomos and Songs of the
// Damned casts to seq 6111 and 3694; both retain their recorded verdicts. The
// crew-tracking fix (tmt-crewedthisturn, CR 702.122) emits one Crew event per
// crewing creature, so seed 10056's trajectory renumbers: the Lagomos cast
// moves from seq 6111 to 6128, still equivalent. agent-20261003T030241Z-d3324728
// (the exiled-with fix: `Defined$ ExiledWith` and `Card.ExiledWithSource` now
// read the source's forward ExiledCards list over every seat, and ChangeZoneAll
// records it) moves seed 8175's Songs of the Damned cast from seq 3694 to 3695:
// foundations-calling-all-angels runs Oblivion Ring, whose leave-the-battlefield
// trigger now returns the card it exiled, changing the trajectory. The
// float_trigger_precedes_cast verdict is unchanged. fdn-fix8's CR 117.3b
// priority reset after a permanent spell's own as-enters choice adds one
// Priority event, moving it again from 3695 to 3696, verdict unchanged.
// The empty-continuation-frame fix (spike S3 legacy defect 1) drops the
// spurious "no sub-ability recorded" Note from 10860's game (log index 2137,
// a shock land's as-enters answer inside a search; nothing else differs but
// the payment hashes), so Worldly Tutor moves from 3239 to 3238, still
// equivalent.
func TestRoundNineFindingsMirror(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	d, err := LoadDecks(reg)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open("testdata/round9.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var specs []GameSpec
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var s GameSpec
		if err := json.Unmarshal(sc.Bytes(), &s); err != nil {
			t.Fatal(err)
		}
		specs = append(specs, s)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(specs) != 4 {
		t.Fatalf("testdata holds %d specs, want 4", len(specs))
	}
	const precedes = "expected:float_then_cast:float_trigger_precedes_cast"
	want := map[uint64]map[uint64]string{ // seed -> seq -> verdict key ("" = equivalent)
		10860: {3238: ""}, // Worldly Tutor; Three Visits no longer occurs after both command-zone fixes
		11056: {},         // the Artisan finding is no longer reached; assert a clean, control-equivalent game
		10056: {6128: ""},
		8175:  {3696: precedes},
	}
	for _, spec := range specs {
		reports := round6Game(t, d, spec)
		if len(reports) == 0 {
			t.Errorf("seed %d: no planned-cast reports; clean-game assertions would be vacuous", spec.Seed)
		}
		seen := map[uint64]bool{}
		for _, r := range reports {
			st, key := r.Verdict()
			if st == Mismatch || (st == Unmirrorable && !r.ExpectedUnmirrorable()) {
				t.Errorf("seed %d seq %d %q: %s %s", spec.Seed, r.Seq, r.Card, st, key)
			}
			if r.Control == nil || r.Control.Status != Equivalent {
				t.Errorf("seed %d seq %d %q: control %+v", spec.Seed, r.Seq, r.Card, r.Control)
			}
			w, ok := want[spec.Seed][r.Seq]
			if !ok {
				continue
			}
			seen[r.Seq] = true
			if key != w {
				t.Errorf("seed %d seq %d %q: verdict %s %q, want %q", spec.Seed, r.Seq, r.Card, st, key, w)
			}
		}
		for seq := range want[spec.Seed] {
			if !seen[seq] {
				t.Errorf("seed %d: no planned cast at seq %d (the game no longer reaches the finding)", spec.Seed, seq)
			}
		}
	}
}

// TestMatchProductionsRespectsAnyColourAmount pins anyLabelFits: an
// any-colour option whose label names a literal amount is no candidate for a
// witness of another total, while "Add any color" (no amount) still is.
func TestMatchProductionsRespectsAnyColourAmount(t *testing.T) {
	d := &decision.Decision{Options: []decision.Option{
		{Index: 0, Kind: "mana", Ability: 0, Label: "Add Combo ColorIdentity"},
		{Index: 1, Kind: "mana", Ability: 1, Label: "Add three mana of any one color"},
	}}
	one := decision.ManaAmount{0, 0, 0, 0, 1, 0}
	if got := matchProductions(d, one, 0); len(got) != 0 {
		t.Fatalf("matchProductions(G) = %v, want none (the three-mana grant cannot make one G)", got)
	}
	three := decision.ManaAmount{0, 0, 0, 0, 3, 0}
	if got := matchProductions(d, three, 1); len(got) != 1 || got[0] != 1 {
		t.Fatalf("matchProductions(GGG) = %v, want [1]", got)
	}
	d.Options[0].Label = "Add any color"
	if got := matchProductions(d, one, 0); len(got) != 1 || got[0] != 0 {
		t.Fatalf("matchProductions(G) with a plain any-colour option = %v, want [0]", got)
	}
	for tail, n := range map[string]int{"three mana of any one color": 3, "21 mana in any combination of colors": 21, "any color": 0} {
		if got, _ := labeledAnyCount(tail); got != n {
			t.Errorf("labeledAnyCount(%q) = %d, want %d", tail, got, n)
		}
	}
}

// TestFloatOnlyTriggerOrderNeedsFloatCreatedTriggers pins the proof gating
// the skipped order ask: every option but at most one must claim a distinct
// triggered-ability object the float created for the asking player.
func TestFloatOnlyTriggerOrderNeedsFloatCreatedTriggers(t *testing.T) {
	konrad, other := state.ObjID(78), state.ObjID(90)
	trig := func(i int, src state.ObjID) decision.Option {
		return decision.Option{Index: i, Kind: "trigger", Obj: src}
	}
	created := map[floatTriggerKey]int{{0, konrad}: 2}
	r := Recorded{Kind: decision.KTriggerOrder, Player: 0, Options: []decision.Option{trig(0, konrad), trig(1, konrad)}}
	if !floatOnlyTriggerOrder(r, created) {
		t.Fatal("two float-created triggers: want skippable")
	}
	r3 := r
	r3.Options = append(append([]decision.Option(nil), r.Options...), trig(2, other))
	if !floatOnlyTriggerOrder(r3, created) {
		t.Fatal("two float triggers plus one other (no ask without them): want skippable")
	}
	r4 := r3
	r4.Options = append(append([]decision.Option(nil), r3.Options...), trig(3, other))
	if floatOnlyTriggerOrder(r4, created) {
		t.Fatal("two non-float triggers would still be ordered on the float route: want not skippable")
	}
	r5 := r
	r5.Options = append(append([]decision.Option(nil), r.Options...), trig(2, konrad))
	if floatOnlyTriggerOrder(r5, map[floatTriggerKey]int{{0, konrad}: 1}) {
		t.Fatal("two A options past the float-created objects from the source: want not skippable")
	}
	if floatOnlyTriggerOrder(r, map[floatTriggerKey]int{{1, konrad}: 2}) {
		t.Fatal("the float's triggers belong to another player: want not skippable")
	}
	rt := r
	rt.Kind = decision.KTarget
	if floatOnlyTriggerOrder(rt, created) {
		t.Fatal("a target ask is never skipped")
	}
}

// TestMaskNewObjectsKeepsOldIDsAndPayloadWords pins maskNewObjects: only an
// ObjID of an object created since the fork is masked; older objects and
// tagged payload words past the arena are kept.
func TestMaskNewObjectsKeepsOldIDsAndPayloadWords(t *testing.T) {
	evs := []events.Event{
		{Kind: events.MoveZone, Obj: 461, IDs: []state.ObjID{12, 470}},
		{Kind: events.Damage, Obj: 5, IDs: []state.ObjID{2147483649}, Pairs: [][2]state.ObjID{{455, 2147483650}}},
	}
	got := maskNewObjects(evs, 450, 480)
	if got[0].Obj != 0 || got[0].IDs[0] != 12 || got[0].IDs[1] != 0 {
		t.Errorf("masked %+v, want Obj 0 and IDs [12 0]", got[0])
	}
	if got[1].Obj != 5 || got[1].IDs[0] != 2147483649 || got[1].Pairs[0] != [2]state.ObjID{0, 2147483650} {
		t.Errorf("masked %+v, want Obj 5, IDs kept, Pairs [0 word]", got[1])
	}
	if evs[0].Obj != 461 || evs[0].IDs[1] != 470 || evs[1].Pairs[0][0] != 455 {
		t.Error("maskNewObjects wrote through to its input")
	}
}
