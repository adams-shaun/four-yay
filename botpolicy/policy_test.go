package botpolicy

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// rng mirrors the seed scheme both hosts use (seat.NewBot, rules.newTestBot):
// a PCG source seeded from the same two constants, so a test that needs a
// known stream can reproduce the policy's exact rng consumption. The seed
// itself stays with the hosts -- this is only a test-side copy of the
// formula, so an intent a test derives here is byte-identical to what a
// game would derive from the same (engine seed, bot seed).
func rng(seed uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
}

// TestChoosePolicy pins the KChoose branch: the offer vocabulary decides
// the pick, not d.Kind -- "x" takes the highest option (the most an {X}
// cost can pay for; options ascend), "exile", mandatory "sacrifice" and
// cleanup "discard" rank offered cards; "yes"
// answers yes, and "name"/"type"/"number" take the first offer. Moved here from seat/bot_test.go's TestBotChoosePolicy and
// rules/testbot_test.go's mirror of it, which were the same test twice
// (Ruling F7).
func TestChoosePolicy(t *testing.T) {
	r := rng(1)
	choose := func(kind string, n, min, max int) decision.Intent {
		d := decision.Decision{Player: 0, Kind: decision.KChoose, Min: min, Max: max}
		for i := 0; i < n; i++ {
			d.Options = append(d.Options, decision.Option{Index: i, Kind: kind, Label: kind})
		}
		if kind == "yes" {
			d.Options = []decision.Option{{Index: 0, Kind: "yes"}, {Index: 1, Kind: "no"}}
		}
		return Decide(Board{}, &d, r)
	}
	if got := choose("x", 4, 1, 1).Choices; len(got) != 1 || got[0] != 3 {
		t.Fatalf("x: %v, want the highest", got)
	}
	// Exile retains the pre-task least-value policy whether optional or
	// mandatory; sacrifice's new player-facing ask is scoped separately.
	if got := choose("exile", 5, 0, 3).Choices; len(got) != 3 || got[0] != 0 || got[2] != 2 {
		t.Fatalf("exile optional: %v, want the three least valuable", got)
	}
	if got := choose("exile", 5, 2, 3).Choices; len(got) != 3 || got[0] != 0 || got[2] != 2 {
		t.Fatalf("exile mandatory: %v, want the three least valuable", got)
	}
	if got := choose("sacrifice", 2, 0, 1).Choices; len(got) != 1 || got[0] != 0 {
		t.Fatalf("sacrifice optional: %v, want the default first option", got)
	}
	if got := choose("sacrifice", 2, 1, 1).Choices; len(got) != 1 || got[0] != 0 {
		t.Fatalf("sacrifice: %v", got)
	}
	discard := decision.Decision{Kind: decision.KChoose, Min: 2, Max: 2, Options: []decision.Option{
		{Index: 0, Kind: "discard", Obj: 1}, {Index: 1, Kind: "discard", Obj: 2}, {Index: 2, Kind: "discard", Obj: 3},
	}}
	b := Board{Cards: TableOf(map[state.ObjID]Card{1: {Basic: true}, 2: {CMC: 1}, 3: {Creature: true, CMC: 8}})}
	if got := Decide(b, &discard, r).Choices; len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("discard: %v, want both spells, keeping land development", got)
	}
	if got := choose("yes", 2, 1, 1).Choices; len(got) != 1 || got[0] != 0 {
		t.Fatalf("yes/no: %v, want yes", got)
	}
	for _, k := range []string{"name", "type", "number"} {
		if got := choose(k, 3, 1, 1).Choices; len(got) != 1 || got[0] != 0 {
			t.Fatalf("%s: %v, want the first", k, got)
		}
	}
}

// TestPassesOutsideMainWithNoCastOrLandDrop is Ruling T25-g's regression
// test, using the engine's REAL shape for a non-main priority decision
// (rules/legal.go's legalActions never offers play_land or cast outside
// sorcery speed/an affordable instant): one or more "activate" options
// followed by "pass" last -- no play_land, so the switch's un-gated
// play_land/cast loop cannot mask the fallback path the way fix round 1's
// own regression case accidentally did. Board.IsMain false must pass, true
// must prefer activate (when a castable card needs mana; the need gate of
// tap.go, T1, gates the tap on the pool -- the main-phase Board carries an
// unpayable hand card and an empty pool, so a tap is wanted), and the
// trailing "concede" option is never chosen either way (M2d-3) -- the
// guarantee that keeps the acceptance games from ending on turn one. Moved
// here from the two mirror copies (seat/bot_test.go, rules/testbot_test.go).
func TestPassesOutsideMainWithNoCastOrLandDrop(t *testing.T) {
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "activate", Obj: 100},
			{Index: 1, Kind: "activate", Obj: 101},
			{Index: 2, Kind: "pass"},
			{Index: 3, Kind: "concede"},
		}}
	in := Decide(Board{}, &d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("isMain=false: intent %+v failed Validate: %v", in, err)
	}
	if len(in.Choices) != 1 || d.Options[in.Choices[0]].Kind != "pass" {
		t.Errorf("isMain=false: priority = %+v, want pass chosen -- not an activation -- outside a main phase", in)
	}
	// And inside a main phase, the same decision DOES prefer activate --
	// the tap gate (T1) wants a tap when a castable card in hand needs
	// mana the empty pool does not yet have.
	wanting := Board{IsMain: true,
		Cards: TableOf(map[state.ObjID]Card{
			// A hand card whose CMC 1 the empty pool cannot pay: a tap.
			200: {CMC: 1, ManaCost: "1", Castable: true},
		})}
	in = Decide(wanting, &d, rng(1))
	if len(in.Choices) != 1 || d.Options[in.Choices[0]].Kind != "activate" {
		t.Errorf("isMain=true: priority = %+v, want an activation chosen", in)
	}
	// M2d-3: the trailing "concede" option is never chosen in either phase
	// -- the whole reason it sits after "pass", where only an explicit pick
	// (or a blind default-to-final client) could reach it, so the
	// acceptance games (this policy drives them, via either adapter) cannot
	// end on turn one.
	for _, isMain := range []bool{false, true} {
		b := Board{IsMain: isMain}
		if isMain {
			b = wanting
		}
		in := Decide(b, &d, rng(1))
		if len(in.Choices) == 0 || d.Options[in.Choices[0]].Kind == "concede" {
			t.Errorf("isMain=%v: bot chose concede: %+v", isMain, in)
		}
	}
}

// TestEveryKind pins the choices each decision.Kind branch makes, not just
// that they validate: priority taps mana over a land drop or a cast (only
// in a main phase), target prefers an opponent, attackers commits every
// legal attacker, blockers never assigns one blocker to two attackers, the
// London mulligan keeps or mulligans exactly off the rng and bottoms the
// lowest-indexed cards, modes takes the first Min options in order, trigger
// order is a full permutation, and trigger optional takes the rng's coin.
// Ported here from seat/bot_test.go's TestBotAlwaysAnswersEveryDecisionKind
// and widened to the kinds that test did not reach.
func TestEveryKind(t *testing.T) {
	// Priority: in a main phase the lone activate option is tapped first --
	// the T1 need gate wants it: a hand card (Obj 3's cast) costs more than
	// the empty pool holds.
	priority := decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "play_land", Obj: 1},
			{Index: 1, Kind: "activate", Obj: 2},
			{Index: 2, Kind: "cast", Obj: 3},
			{Index: 3, Kind: "pass"},
		}}
	wanting := Board{IsMain: true,
		Cards: TableOf(map[state.ObjID]Card{
			3: {CMC: 2, ManaCost: "2", Castable: true}, // in hand, pool can't pay it
		})}
	if in := Decide(wanting, &priority, rng(1)); len(in.Choices) != 1 || priority.Options[in.Choices[0]].Kind != "activate" {
		t.Errorf("priority (main phase) = %+v, want the lone activate option chosen first", in)
	}
	// Outside a main phase the same options must NOT prefer activate -- it
	// would rather play its land.
	if in := Decide(Board{}, &priority, rng(1)); len(in.Choices) != 1 || priority.Options[in.Choices[0]].Kind == "activate" {
		t.Errorf("priority (non-main) = %+v, want activate NOT chosen outside a main phase", in)
	}
	// T1 (tap.go): the need gate stops the tap the moment the pool can pay a
	// castable card -- the pool of 2 pays the CMC-2 hand card, so the land
	// drop (play_land, first offered) is chosen instead of another tap, and
	// a hand with no castable card never taps at all.
	satisfied := wanting
	satisfied.Pool = state.Mana{state.MC: 2} // 2 colourless already floats
	if in := Decide(satisfied, &priority, rng(1)); len(in.Choices) != 1 || priority.Options[in.Choices[0]].Kind == "activate" {
		t.Errorf("priority (pool pays the hand) = %+v, want no tap -- the cast can already be made", in)
	}
	// The coloured-pip gate: {U}{U} against a pool of one blue still wants a
	// tap (a colour-blind tap would stop here and never cast). The offered
	// activate source carries a blue-production fact: the satisfiability
	// filter (tap_satisfiable_test.go) keeps the card a tap target only when
	// an offered source can close the unmet pip, and a source with no facts
	// claims no colour (fail closed).
	pipWant := Board{IsMain: true,
		Cards: TableOf(map[state.ObjID]Card{
			3: {CMC: 2, ManaCost: "U U", Castable: true},
			2: {Produces: cards.ManaProduction{Colour: [6]int32{0, 1, 0, 0, 0, 0}}},
		}),
		Pool: state.Mana{state.MU: 1},
	}
	if in := Decide(pipWant, &priority, rng(1)); len(in.Choices) != 1 || priority.Options[in.Choices[0]].Kind != "activate" {
		t.Errorf("priority (pool one blue vs UU) = %+v, want a tap -- the pool cannot pay {U}{U}", in)
	}
	// A battlefield permanent (Castable false) and a hand land (CMC 0) never
	// justify a tap: nothing in hand wants mana, so the policy passes even
	// with activate options on the table.
	idle := Board{IsMain: true,
		Cards: TableOf(map[state.ObjID]Card{
			4: {CMC: 2, ManaCost: "2", Castable: false}, // already on the battlefield
			5: {CMC: 0, ManaCost: "", Castable: true},   // a land in hand
		})}
	if in := Decide(idle, &priority, rng(1)); len(in.Choices) != 1 || priority.Options[in.Choices[0]].Kind == "activate" {
		t.Errorf("priority (nothing castable wants mana) = %+v, want no tap", in)
	}

	target := decision.Decision{Seq: 2, Player: 0, Kind: decision.KTarget, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "player", Player: 0},
			{Index: 1, Kind: "player", Player: 1},
		}}
	if in := Decide(Board{}, &target, rng(1)); len(in.Choices) != 1 || target.Options[in.Choices[0]].Player != 1 {
		t.Errorf("target = %+v, want the opposing player chosen", in)
	}

	attackers := decision.Decision{Seq: 3, Player: 0, Kind: decision.KAttackers, Min: 0, Max: 2,
		Options: []decision.Option{
			{Index: 0, Kind: "attacker", Obj: 10, Player: 1},
			{Index: 1, Kind: "attacker", Obj: 11, Player: 1},
		}}
	// Combat-specific expectations live in combat_test.go; here, with a
	// defender that has no creatures, both legal 2/2 attackers go (AR2 —
	// there is nothing to punish the swing).
	if in := Decide(Board{Creatures: TableOf(map[state.ObjID]Creature{
		10: {Power: 2, Toughness: 2, Controller: 0},
		11: {Power: 2, Toughness: 2, Controller: 0},
	})}, &attackers, rng(1)); len(in.Choices) != 2 {
		t.Errorf("attackers = %+v, want every legal attacker chosen against a defender with no creatures", in)
	}

	// M2 (fix round 1): a single fixed seed is fragile -- with seed 1 the
	// two coin flips below did not both land "block" even with the used map
	// deleted entirely, so the assertion passed for the wrong reason. Three
	// options share blocker 20 here, and every one of 50 seeds is checked:
	// with the used map removed, at least one seed assigns blocker 20 to two
	// (or three) attackers with overwhelming probability (1 - 0.5^50 that
	// SOME seed shows it), while the real policy must never do so for ANY
	// seed.
	blockers := decision.Decision{Seq: 4, Player: 1, Kind: decision.KBlockers, Min: 0, Max: 4,
		Options: []decision.Option{
			{Index: 0, Kind: "block", Obj: 20, Attacker: 10},
			{Index: 1, Kind: "block", Obj: 20, Attacker: 11},
			{Index: 2, Kind: "block", Obj: 20, Attacker: 12},
			{Index: 3, Kind: "block", Obj: 21, Attacker: 10},
		}}
	for seed := uint64(0); seed < 50; seed++ {
		in := Decide(Board{}, &blockers, rng(seed))
		if err := blockers.Validate(in); err != nil {
			t.Fatalf("seed %d: blockers intent failed Validate: %v", seed, err)
		}
		used := map[state.ObjID]bool{}
		for _, c := range in.Choices {
			obj := blockers.Options[c].Obj
			if used[obj] {
				t.Fatalf("seed %d: blockers = %+v assigned blocker %d to more than one attacker", seed, in, obj)
			}
			used[obj] = true
		}
	}

	// The London mulligan round (rules/mulligan.go). Keep/mulligan: the rng
	// coin decides, mirrored by drawing the same stream in the test -- a
	// 1-in-3 mulligan when a "mulligan" option is offered, else keep (the
	// "keep" option at index 0). The decision here is deterministic per
	// seed, so this is an exact consumption-point check, not a probability.
	mull := decision.Decision{Seq: 5, Player: 0, Kind: decision.KMulligan, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "keep"},
			{Index: 1, Kind: "mulligan"},
		}}
	mulligans, keeps := 0, 0
	for seed := uint64(0); seed < 60; seed++ {
		want := rng(seed).IntN(3) // the exact draw the policy will make
		in := Decide(Board{}, &mull, rng(seed))
		if len(in.Choices) != 1 {
			t.Fatalf("seed %d: mulligan = %+v, want exactly one choice", seed, in)
		}
		wantIdx := 0 // keep
		if want == 0 {
			wantIdx = 1 // mulligan
			mulligans++
		} else {
			keeps++
		}
		if in.Choices[0] != wantIdx {
			t.Fatalf("seed %d: mulligan = %+v, want option %d (%s)", seed, in, wantIdx, mull.Options[wantIdx].Kind)
		}
	}
	if mulligans == 0 || keeps == 0 {
		t.Fatalf("60 seeds produced only %d mulligans and %d keeps -- the rng branch never flipped", mulligans, keeps)
	}
	// A keep/mulligan ask with only "keep" offered never consumes the rng
	// and keeps (the len(d.Options) > 1 gate).
	keepOnly := decision.Decision{Seq: 6, Player: 0, Kind: decision.KMulligan, Min: 1, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "keep"}}}
	if in := Decide(Board{}, &keepOnly, rng(1)); len(in.Choices) != 1 || in.Choices[0] != 0 {
		t.Errorf("keep-only mulligan = %+v, want keep", in)
	}
	// Bottoming: every option is a "bottom"; bottom the d.Min LEAST valuable
	// kept cards (chooseWorst), no rng -- so a kept hand keeps its best and
	// puts its worst on the bottom, not its oldest-held (lowest-indexed).
	bottoming := decision.Decision{Seq: 7, Player: 0, Kind: decision.KMulligan, Min: 2, Max: 2,
		Options: []decision.Option{
			{Index: 0, Kind: "bottom", Obj: 100},
			{Index: 1, Kind: "bottom", Obj: 101},
			{Index: 2, Kind: "bottom", Obj: 102},
		}}
	worstBoard := Board{Cards: TableOf(map[state.ObjID]Card{
		100: {Creature: true, Power: 4, CMC: 0}, // cheap — kept
		101: {CMC: 1},                           // cardWorth 1 — worst, bottomed
		102: {Creature: true, Power: 2, CMC: 4}, // expensive — bottomed
	})}
	if in := Decide(worstBoard, &bottoming, rng(1)); len(in.Choices) != 2 || in.Choices[0] != 1 || in.Choices[1] != 2 {
		t.Errorf("bottoming = %+v, want the two least valuable cards (obj 101, 102) bottomed", in)
	}

	// Modes (M2d-2): the first Min options in order, no rng.
	modes := decision.Decision{Seq: 8, Player: 0, Kind: decision.KModes, Min: 2, Max: 2,
		Options: []decision.Option{
			{Index: 0, Kind: "mode"}, {Index: 1, Kind: "mode"}, {Index: 2, Kind: "mode"},
		}}
	if in := Decide(Board{}, &modes, rng(1)); len(in.Choices) != 2 || in.Choices[0] != 0 || in.Choices[1] != 1 {
		t.Errorf("modes = %+v, want the first Min options in order", in)
	}

	// A Repeatable modal ask (Fiery Confluence) may need MORE picks than it
	// has distinct options: CharmNum$ 3 over 2 legal modes means one mode is
	// repeated. The answer must satisfy Min by repetition and still pass
	// Validate -- an under-filled intent is rejected by arity and would wedge
	// the game.
	repeat := decision.Decision{Seq: 10, Player: 0, Kind: decision.KModes, Min: 3, Max: 3,
		Repeatable: true,
		Options: []decision.Option{
			{Index: 0, Kind: "mode"}, {Index: 1, Kind: "mode"},
		}}
	in := Decide(Board{}, &repeat, rng(1))
	if err := repeat.Validate(in); err != nil {
		t.Fatalf("repeatable modes failed Validate: %v (%+v)", err, in)
	}
	if len(in.Choices) != 3 {
		t.Fatalf("repeatable modes = %+v, want three picks", in)
	}

	// Trigger order: whatever the rng draws, the answer must be a full
	// permutation of the offered indices -- every trigger exactly once.
	order := decision.Decision{Seq: 9, Player: 0, Kind: decision.KTriggerOrder, Min: 3, Max: 3,
		Options: []decision.Option{
			{Index: 0, Kind: "trigger", Obj: 30},
			{Index: 1, Kind: "trigger", Obj: 31},
			{Index: 2, Kind: "trigger", Obj: 32},
		}}
	for seed := uint64(0); seed < 30; seed++ {
		in := Decide(Board{}, &order, rng(seed))
		if err := order.Validate(in); err != nil {
			t.Fatalf("seed %d: trigger order failed Validate: %v", seed, err)
		}
		seen := map[int]bool{}
		for _, c := range in.Choices {
			if seen[c] {
				t.Fatalf("seed %d: trigger order %v has duplicate index %d", seed, in.Choices, c)
			}
			seen[c] = true
		}
	}

	// Printed optional triggers and Miracle retain the deterministic accept;
	// only marked api:Effect OptionalDecider$ elections decline.
	opt := decision.Decision{Seq: 10, Player: 0, Kind: decision.KTriggerOptional, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "yes", Obj: 50},
			{Index: 1, Kind: "no", Obj: 50},
		}}
	for seed := uint64(0); seed < 40; seed++ {
		in := Decide(Board{}, &opt, rng(seed))
		if len(in.Choices) != 1 || in.Choices[0] != 0 {
			t.Fatalf("seed %d: trigger optional = %+v, want deterministic accept", seed, in)
		}
	}
}

// TestDeterministic is Ruling P8's point at the policy level: the same rng
// seed answering the same decision sequence produces identical intents, and
// the rng is still load-bearing somewhere. dp1 made KTriggerOrder and
// KTriggerOptional deterministic (they no longer draw from the rng), so the
// one remaining rng consumer in this sequence is the KMulligan keep/mulligan
// coin (a 1-in-3 mulligan); the combat kinds (KBlockers here) consume no rng
// at all — B2, see chooseAttackers/chooseBlockers. The seat package keeps a
// copy of this through Bot, for when a bot whose rng leaked would only show
// it on paths that draw from it.
func TestDeterministic(t *testing.T) {
	seq := []decision.Decision{
		{Seq: 1, Player: 0, Kind: decision.KBlockers, Min: 0, Max: 3, Options: []decision.Option{
			{Index: 0, Kind: "block", Obj: 10, Attacker: 20},
			{Index: 1, Kind: "block", Obj: 10, Attacker: 21},
			{Index: 2, Kind: "block", Obj: 11, Attacker: 20},
		}},
		{Seq: 2, Player: 0, Kind: decision.KTriggerOrder, Min: 3, Max: 3, Options: []decision.Option{
			{Index: 0, Kind: "trigger", Obj: 30},
			{Index: 1, Kind: "trigger", Obj: 31},
			{Index: 2, Kind: "trigger", Obj: 32},
		}},
		{Seq: 3, Player: 0, Kind: decision.KTriggerOptional, Min: 1, Max: 1, Options: []decision.Option{
			{Index: 0, Kind: "yes"}, {Index: 1, Kind: "no"},
		}},
		{Seq: 4, Player: 0, Kind: decision.KMulligan, Min: 1, Max: 1, Options: []decision.Option{
			{Index: 0, Kind: "keep"}, {Index: 1, Kind: "mulligan"},
		}},
	}
	run := func(seed uint64) []decision.Intent {
		r := rng(seed)
		out := make([]decision.Intent, len(seq))
		for i, d := range seq {
			out[i] = Decide(Board{}, &d, r)
		}
		return out
	}
	same := func(a, b decision.Intent) bool {
		if len(a.Choices) != len(b.Choices) {
			return false
		}
		for i := range a.Choices {
			if a.Choices[i] != b.Choices[i] {
				return false
			}
		}
		return true
	}

	a, b := run(7), run(7)
	for i := range seq {
		if !same(a[i], b[i]) {
			t.Fatalf("decision %d: choices differ between two runs of the same seed: %v vs %v", i, a[i], b[i])
		}
	}

	// The same seed is deterministic (above); the together-with-other-kinds
	// note is that the rng is still consumed SOMEWHERE -- the KMulligan coin
	// -- so across a spread of seeds the outcome sequence is not constant.
	// KTriggerOrder/KTriggerOptional/KBlockers are now deterministic, so this
	// is the one load-bearing rng path left in the sequence.
	seen := map[string]bool{}
	for s := uint64(0); s < 64; s++ {
		seen[fmt.Sprint(run(s))] = true
	}
	if len(seen) < 2 {
		t.Fatal("64 seeds produced a single identical outcome sequence -- the rng stream is not load-bearing")
	}
}

// TestTotalityUnderArbitraryMinMax is I-4/Ruling T25-c's regression test:
// every kind-specific branch used to ignore d.Min/d.Max entirely, so it only
// validated by coincidence of the shapes today's engine happens to emit.
// 10,000 random decisions (random kind among all eight, 0-8 options with
// random Kind/Obj/Player fields, random 0 <= Min <= Max <= len(Options)) must
// all validate and never panic. Moved here from the two mirror copies
// (seat/bot_test.go, rules/testbot_test.go).
func TestTotalityUnderArbitraryMinMax(t *testing.T) {
	kinds := []decision.Kind{decision.KPriority, decision.KTarget, decision.KAttackers,
		decision.KBlockers, decision.KMulligan, decision.KModes, decision.KTriggerOrder,
		decision.KTriggerOptional, decision.KCommanderZone}
	optKinds := []string{"activate", "play_land", "cast", "pass", "concede", "player", "permanent",
		"attacker", "block", "trigger", "yes", "no", "keep", "mulligan", "whatever",
		"command_zone", "leave"}

	// A small, dependency-free xorshift, seeded once and consumed
	// sequentially: reproducible on its own, never math/rand's global
	// functions and never the bot's own rng.
	var s uint64 = 0x2545F4914F6CDD1D
	next := func(n int) int {
		s ^= s << 13
		s ^= s >> 7
		s ^= s << 17
		if n <= 0 {
			return 0
		}
		return int(s % uint64(n))
	}

	b := rng(42)
	for i := 0; i < 10000; i++ {
		nOpts := next(9) // 0..8
		opts := make([]decision.Option, nOpts)
		for j := range opts {
			opts[j] = decision.Option{
				Index:  j,
				Kind:   optKinds[next(len(optKinds))],
				Obj:    state.ObjID(next(5)),
				Player: state.PlayerID(next(4)),
			}
		}
		min := next(nOpts + 1)
		max := min + next(nOpts-min+1)
		d := decision.Decision{Seq: uint64(i), Player: state.PlayerID(next(4)),
			Kind: kinds[next(len(kinds))], Min: min, Max: max, Options: opts}
		board := Board{IsMain: next(2) == 0}

		var in decision.Intent
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("case %d: Decide panicked on %+v (board=%+v): %v", i, d, board, r)
				}
			}()
			in = Decide(board, &d, b)
		}()
		if verr := d.Validate(in); verr != nil {
			t.Fatalf("case %d: intent %+v failed Validate against %+v (board=%+v): %v", i, in, d, board, verr)
		}
	}
}
