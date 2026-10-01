package seat

import (
	"context"
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

// TestBotIsDeterministic is Ruling P8's whole point: the same bot seed
// answering the same decision sequence produces identical intents, so a
// match is reproducible from (engine seed, bot seed) alone. The sequence
// below deliberately includes the KMulligan keep/mulligan coin (the one
// rng-consuming path; dp1 made KBlockers/KTriggerOrder/KTriggerOptional
// deterministic), since a bot whose rng leaked from the engine or the
// process clock would only show it on paths that actually draw from it.
//
// M1 (fix round 1): run(7) is also compared against run(8) -- comparing
// run(7) to itself alone cannot tell "seeded" from "constant"; deleting the
// bot's rng entirely (a fixed choice in KBlockers/KTriggerOrder/
// KTriggerOptional) would still pass a same-seed-only check.
func TestBotIsDeterministic(t *testing.T) {
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
		b := NewBot(seed)
		out := make([]decision.Intent, len(seq))
		for i, d := range seq {
			in, err := b.Decide(context.Background(), view.View{}, d)
			if err != nil {
				t.Fatalf("Decide returned an error: %v", err)
			}
			out[i] = in
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

	// KBlockers/KTriggerOrder/KTriggerOptional are all deterministic now (no
	// rng); the KMulligan keep/mulligan coin above is the one path that still
	// draws from the stream, so across a spread of seeds the outcome sequence
	// must not be constant -- that is what proves the seed is load-bearing
	// rather than a fixed choice behind a seeded-looking API. The M1 fix
	// round's point stands: run(7) alone cannot tell "seeded" from
	// "constant", so we compare across seeds, not to itself.
	seen := map[string]bool{}
	for s := uint64(0); s < 64; s++ {
		seen[fmt.Sprint(run(s))] = true
	}
	if len(seen) < 2 {
		t.Fatal("64 seeds produced a single identical outcome sequence -- the rng may not be load-bearing")
	}
}

// TestBotOnlyChoosesOfferedOptions is Ruling P7's completeness requirement
// made concrete: every decision.Kind decision.go defines (eight today --
// the brief and supplement both say seven, predating KModes) gets a
// representative Decision here, including the two edge shapes the fuzz gate
// depends on (Min == Max == N for KTriggerOrder, Min == Max == 1 over two
// options for KTriggerOptional) and an empty option list with Min == 0 (the
// generic fallback's own edge case). Every resulting intent must validate
// against the decision it answered -- an intent Validate rejects is a bot
// bug, full stop. The priority case is answered in a main phase, so the
// activate branch (Ruling T25-b, fix round 1's isMain gate) gets exercised
// here too, not only in TestBotAlwaysAnswersEveryDecisionKind.
func TestBotOnlyChoosesOfferedOptions(t *testing.T) {
	cases := []struct {
		name string
		d    decision.Decision
		v    view.View
	}{
		{"priority", decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
			Options: []decision.Option{
				{Index: 0, Kind: "activate", Obj: 1},
				{Index: 1, Kind: "play_land", Obj: 2},
				{Index: 2, Kind: "cast", Obj: 3},
				{Index: 3, Kind: "pass"},
			}}, view.View{Phase: "main1", Viewer: 0,
			// A hand card the T1 tap gate prices: the empty pool cannot pay
			// it, so the activate branch genuinely fires in this case.
			Players: []view.PlayerView{
				{ID: 0, Hand: []view.CardView{{ID: 3, ManaCost: "2"}}},
			}}},
		{"target", decision.Decision{Seq: 2, Player: 0, Kind: decision.KTarget, Min: 1, Max: 1,
			Options: []decision.Option{
				{Index: 0, Kind: "player", Player: 0},
				{Index: 1, Kind: "player", Player: 1},
				{Index: 2, Kind: "permanent", Obj: 10, Player: 1},
			}}, view.View{}},
		{"attackers", decision.Decision{Seq: 3, Player: 0, Kind: decision.KAttackers, Min: 0, Max: 2,
			Options: []decision.Option{
				{Index: 0, Kind: "attacker", Obj: 20, Player: 1},
				{Index: 1, Kind: "attacker", Obj: 21, Player: 1},
			}}, view.View{}},
		{"blockers", decision.Decision{Seq: 4, Player: 1, Kind: decision.KBlockers, Min: 0, Max: 3,
			Options: []decision.Option{
				{Index: 0, Kind: "block", Obj: 30, Attacker: 20, Player: 1},
				{Index: 1, Kind: "block", Obj: 30, Attacker: 21, Player: 1},
				{Index: 2, Kind: "block", Obj: 31, Attacker: 20, Player: 1},
			}}, view.View{}},
		{"mulligan", decision.Decision{Seq: 5, Player: 0, Kind: decision.KMulligan, Min: 1, Max: 1,
			Options: []decision.Option{
				{Index: 0, Kind: "keep"},
				{Index: 1, Kind: "mulligan"},
			}}, view.View{}},
		{"modes-empty", decision.Decision{Seq: 6, Player: 0, Kind: decision.KModes, Min: 0, Max: 0}, view.View{}},
		{"trigger_order", decision.Decision{Seq: 7, Player: 0, Kind: decision.KTriggerOrder, Min: 3, Max: 3,
			Options: []decision.Option{
				{Index: 0, Kind: "trigger", Obj: 40},
				{Index: 1, Kind: "trigger", Obj: 41},
				{Index: 2, Kind: "trigger", Obj: 42},
			}}, view.View{}},
		{"trigger_optional", decision.Decision{Seq: 8, Player: 0, Kind: decision.KTriggerOptional, Min: 1, Max: 1,
			Options: []decision.Option{
				{Index: 0, Kind: "yes", Obj: 50},
				{Index: 1, Kind: "no", Obj: 50},
			}}, view.View{}},
	}

	seenKinds := map[decision.Kind]bool{}
	b := NewBot(99)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seenKinds[tc.d.Kind] = true
			in, err := b.Decide(context.Background(), tc.v, tc.d)
			if err != nil {
				t.Fatalf("Decide returned an error: %v", err)
			}
			if err := tc.d.Validate(in); err != nil {
				t.Fatalf("bot's own intent failed Validate: %v (intent=%+v)", err, in)
			}
		})
	}
	allKinds := []decision.Kind{decision.KPriority, decision.KTarget, decision.KAttackers,
		decision.KBlockers, decision.KMulligan, decision.KModes, decision.KTriggerOrder,
		decision.KTriggerOptional}
	for _, k := range allKinds {
		if !seenKinds[k] {
			t.Errorf("decision.Kind %q was never exercised by this test", k)
		}
	}
}

// TestBotPassesOutsideMainWithNoCastOrLandDrop is Ruling T25-g's regression
// test, using the engine's REAL shape for a non-main priority decision
// (rules/legal.go's legalActions never offers play_land or cast outside
// sorcery speed/an affordable instant): one or more "activate" options
// followed by "pass" last. Fix round 1's own regression case for this
// (TestBotAlwaysAnswersEveryDecisionKind's "priority (combat)" case, above)
// included a play_land option that the switch's un-gated second loop
// matches regardless of phase, so it never actually reached the fallback
// path that reintroduced I-1(b) in a live game (clamp topping up a
// Min:1/Max:1 decision with the first unused option, which legalActions
// always lists as an activation before pass). This decision has no
// play_land and no cast, so the bot must fall through to the explicit pass
// added in botDecide's KPriority case -- not clamp's top-up -- for every
// phase that isn't a main phase.
func TestBotPassesOutsideMainWithNoCastOrLandDrop(t *testing.T) {
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KPriority, Min: 1, Max: 1,
		Options: []decision.Option{
			{Index: 0, Kind: "activate", Obj: 100},
			{Index: 1, Kind: "activate", Obj: 101},
			{Index: 2, Kind: "pass"},
			{Index: 3, Kind: "concede"},
		}}
	for _, phase := range []string{"", "beginning", "combat", "ending"} {
		in, err := NewBot(1).Decide(context.Background(), view.View{Phase: phase}, d)
		if err != nil {
			t.Fatalf("phase %q: Decide returned an error: %v", phase, err)
		}
		if err := d.Validate(in); err != nil {
			t.Fatalf("phase %q: intent %+v failed Validate: %v", phase, in, err)
		}
		if len(in.Choices) != 1 || d.Options[in.Choices[0]].Kind != "pass" {
			t.Errorf("phase %q: priority = %+v, want pass chosen -- not an activation -- outside a main phase", phase, in)
		}
	}
	// And inside a main phase, the same decision DOES prefer activate --
	// confirming this test isn't just checking "never activate ever". The
	// View's player record carries a hand card the tap gate (tap.go, T1)
	// prices: the empty pool cannot pay its cost, so a tap is wanted.
	wanting := view.View{Phase: "main1", Viewer: 0,
		Players: []view.PlayerView{
			{ID: 0, Hand: []view.CardView{{ID: 200, ManaCost: "1"}}},
		}}
	in, err := NewBot(1).Decide(context.Background(), wanting, d)
	if err != nil || len(in.Choices) != 1 || d.Options[in.Choices[0]].Kind != "activate" {
		t.Errorf("phase \"main1\": priority = %+v (err=%v), want an activation chosen", in, err)
	}
	// M2d-3: the trailing "concede" option is never chosen in either phase
	// -- the whole reason it sits after "pass", where only an explicit
	// pick (or a blind default-to-final client) could reach it.
	for _, phase := range []string{"", "beginning", "combat", "ending", "main1", "main2"} {
		in, err := NewBot(1).Decide(context.Background(), view.View{Phase: phase}, d)
		if err != nil || len(in.Choices) == 0 || d.Options[in.Choices[0]].Kind == "concede" {
			t.Errorf("phase %q: bot chose concede or errored: %+v (err=%v)", phase, in, err)
		}
	}
}

// TestBoardFromViewCommanderClockFill pins the view-shaped half's CR 903.10
// clock fill directly: the projected CmdDamage maps (keyed by commander
// object id, one per damaged player) must land in the Board's per-commander
// Damage maps, keyed by the player who took the damage. The whole-game
// commander parity test cannot pin this on its own — a whole game of sample
// commanders can end without a single unblocked commander swing — so the
// fill gets a hand-built View here, exactly the shape a real client
// receives.
func TestBoardFromViewCommanderClockFill(t *testing.T) {
	cmdA := state.ObjID(500)
	cmdB := state.ObjID(501)
	v := view.View{Viewer: 0, Players: []view.PlayerView{
		{
			ID: 0, Life: 20,
			Commanders:     []view.CardView{{ID: cmdA}},
			CommanderCasts: []int32{2},
			Command:        []view.CardView{{ID: cmdA}},
			// seat 0 has taken 7 from seat 1's commander, 0 from its own
			CmdDamage: map[state.ObjID]int32{cmdB: 7},
		},
		{
			ID: 1, Life: 20,
			Commanders:     []view.CardView{{ID: cmdB}},
			CommanderCasts: []int32{0},
			// seat 1 has taken 4 from seat 0's commander
			CmdDamage: map[state.ObjID]int32{cmdA: 4},
		},
	}}
	b := BoardFromView(v)
	a := b.Commanders.Get(cmdA)
	if !a.InCommandZone || a.Casts != 2 {
		t.Errorf("seat 0's commander = %+v, want in the zone with 2 casts", a)
	}
	if a.Damage[1] != 4 || len(a.Damage) != 1 {
		t.Errorf("seat 0's commander damage = %v, want {seat 1: 4}", a.Damage)
	}
	bb := b.Commanders.Get(cmdB)
	if bb.InCommandZone || bb.Casts != 0 {
		t.Errorf("seat 1's commander = %+v, want not in seat 0's zone with 0 casts", bb)
	}
	if bb.Damage[0] != 7 || len(bb.Damage) != 1 {
		t.Errorf("seat 1's commander damage = %v, want {seat 0: 7}", bb.Damage)
	}
}

// TestCombinedLethalBotForcesCombinedAttack pins the AR8 seat wiring: the
// bench-only NewCombinedLethalBot answers a KAttackers decision through the
// game-shaped adapter with the combined-attacker subset (the brief's board:
// two 3/3s against 5 life and a lone 2/2 blocker), while the production bot
// and the AR7 bot keep one 3/3 home (AR4 holds a blocker back when no single
// attacker is lethal). This is the seat-level proof that the opt-in policy
// reaches DecideBoard and that the default bot is untouched.
func TestCombinedLethalBotForcesCombinedAttack(t *testing.T) {
	brd := botpolicy.Board{
		Creatures: botpolicy.TableOf(map[state.ObjID]botpolicy.Creature{
			101: {Power: 3, Toughness: 3, Controller: 0},
			102: {Power: 3, Toughness: 3, Controller: 0},
			201: {Power: 2, Toughness: 2, Controller: 1},
		}),
		Life: botpolicy.TableOf(map[state.PlayerID]int32{0: 20, 1: 5}),
	}
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KAttackers, Min: 0, Max: 2,
		Options: []decision.Option{
			{Index: 0, Kind: "attacker", Obj: 101, Player: 1},
			{Index: 1, Kind: "attacker", Obj: 102, Player: 1},
		}}
	for _, tc := range []struct {
		name string
		bot  *Bot
		want int
	}{{"bot", NewBot(1), 1}, {"lethal-pressure", NewLethalPressureBot(1), 1}, {"ar8", NewCombinedLethalBot(1), 2}} {
		in, err := tc.bot.DecideBoard(context.Background(), brd, d)
		if err != nil {
			t.Fatalf("%s: DecideBoard: %v", tc.name, err)
		}
		if len(in.Choices) != tc.want {
			t.Fatalf("%s choices = %v, want %d choices", tc.name, in.Choices, tc.want)
		}
	}
}

// TestBlocksBotDeclinesTradeDown pins the BLK seat wiring: the bench-only
// NewBlocksBot answers a KBlockers decision through the game-shaped adapter
// with the whole-assignment heuristic, and the production bot is untouched.
// The board: my 3/2 kills the attacking 2/3 but dies. BR1 counts an even
// pt trade (both pt 5) as blockable, so the default bot blocks; B2's power
// test (attacker power 2 < blocker power 3) declines it, so the blocks bot
// leaves it home -- the two policies demonstrably differ.
func TestBlocksBotDeclinesTradeDown(t *testing.T) {
	brd := botpolicy.Board{
		Creatures: botpolicy.TableOf(map[state.ObjID]botpolicy.Creature{
			101: {Power: 3, Toughness: 2, Controller: 0},
			201: {Power: 2, Toughness: 3, Controller: 1},
		}),
		Life: botpolicy.TableOf(map[state.PlayerID]int32{0: 20, 1: 20}),
	}
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KBlockers, Min: 0, Max: 1,
		Options: []decision.Option{{Index: 0, Kind: "block", Obj: 101, Attacker: 201, Player: 0}}}
	for _, tc := range []struct {
		name   string
		bot    *Bot
		chumps bool
	}{{"bot", NewBot(1), true}, {"blocks", NewBlocksBot(1), false}} {
		in, err := tc.bot.DecideBoard(context.Background(), brd, d)
		if err != nil {
			t.Fatalf("%s: DecideBoard: %v", tc.name, err)
		}
		got := len(in.Choices) > 0
		if got != tc.chumps {
			t.Errorf("%s blocked = %v, want %v", tc.name, got, tc.chumps)
		}
	}
}

// TestBlocksBotTakesLethalChumps pins the BLK seat wiring's lethal half:
// facing lethal (6/6 + 3/3 against 5 life), NewBlocksBot chumps BOTH
// attackers (the largest takes the cheapest chump, the second takes what
// remains), while the per-blocker default (measured: choices [0]) chumps
// only the 6/6 — BR2 recomputes the unblocked total after the first chump
// and finds the 3/3 no longer lethal, so it leaves the second blocker
// home. The whole assignment keeps 3 more life than the per-blocker one.
func TestBlocksBotTakesLethalChumps(t *testing.T) {
	brd := botpolicy.Board{
		Creatures: botpolicy.TableOf(map[state.ObjID]botpolicy.Creature{
			101: {Power: 1, Toughness: 1, Controller: 0},
			102: {Power: 2, Toughness: 2, Controller: 0},
			201: {Power: 6, Toughness: 6, Controller: 1},
			202: {Power: 3, Toughness: 3, Controller: 1},
		}),
		Life: botpolicy.TableOf(map[state.PlayerID]int32{0: 5, 1: 20}),
	}
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KBlockers, Min: 0, Max: 4,
		Options: []decision.Option{
			{Index: 0, Kind: "block", Obj: 101, Attacker: 201, Player: 0},
			{Index: 1, Kind: "block", Obj: 102, Attacker: 201, Player: 0},
			{Index: 2, Kind: "block", Obj: 101, Attacker: 202, Player: 0},
			{Index: 3, Kind: "block", Obj: 102, Attacker: 202, Player: 0},
		}}
	in, err := NewBlocksBot(1).DecideBoard(context.Background(), brd, d)
	if err != nil {
		t.Fatalf("DecideBoard: %v", err)
	}
	if err := d.Validate(in); err != nil {
		t.Errorf("blocks intent failed Validate: %v", err)
	}
	if len(in.Choices) != 2 {
		t.Errorf("blocks lethal choices = %v, want both chumps", in.Choices)
	}
}

// TestBoardFromViewCarriesRestrictedPool pins the view half of
// Board.PoolRestricted (the T3 converter gate's restricted share of the
// pool, cardfuzz batch5 line 9): the viewer's own PoolRestrictions land in
// the slot their producing counter names, the same fold the game half's
// botpolicy.RestrictedPool makes from state.Player.RestrictedMana.
func TestBoardFromViewCarriesRestrictedPool(t *testing.T) {
	v := view.View{Viewer: 0, Players: []view.PlayerView{
		{ID: 0, Life: 20, Pool: map[string]int32{"B": 1, "C": 2},
			PoolRestrictions: []view.PoolRestrictionView{{Color: "C", Amount: 2, Text: "Myr only"}}},
		{ID: 1, Life: 20, Pool: map[string]int32{"R": 3},
			PoolRestrictions: []view.PoolRestrictionView{{Color: "R", Amount: 3, Text: "not ours"}}},
	}}
	b := BoardFromView(v)
	if b.PoolRestricted != (state.Mana{0, 0, 0, 0, 0, 2}) {
		t.Fatalf("PoolRestricted = %v, want the viewer's two {C}", b.PoolRestricted)
	}
	want := botpolicy.RestrictedPool([]state.ManaRestriction{{Color: "C", Amount: 2, Valid: "Spell.Myr"}})
	if b.PoolRestricted != want {
		t.Fatalf("view half %v disagrees with the game half %v", b.PoolRestricted, want)
	}
}
