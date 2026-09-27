package main

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// krenkoFixture builds a deterministic two-seat engine whose seat 0
// controls Krenko, Mob Boss plus `goblins` 1/1 red Goblin tokens, parked at
// a priority decision that offers Krenko's "{T}: create X Goblin tokens,
// where X is the number of Goblins you control" ability. Everything is
// authored through events -- the Krenko MoveZone and one TokenCreate per
// Goblin, the exact event shapes a real Krenko activation logs -- so the
// log replays exactly (TestKrenkoBoardCapFiresMidResolution asserts it).
//
// maxObjects > 0 arms the engine's mid-resolution object cap
// (rules.LoopGuard.MaxObjs) from genesis, so the caller can stage a board
// the very next resolution will cross; the caller asserts
// len(e.G.Objs) < maxObjects first. maxObjects == 0 leaves the watcher at
// its defaults (the boundary-only behaviour main's build used).
//
// The returned abilityIdx is Krenko's activation option in the pending
// priority decision, ready for a one-choice Submit; the caller submits
// Intent{Seq, Player, Choices: []int{abilityIdx}}.
func krenkoFixture(tb testing.TB, goblins, maxObjects int) (e *rules.Engine, cfg rules.Config, kid state.ObjID, abilityIdx int) {
	tb.Helper()
	reg := testutil.CorpusRegistry(tb)
	krenko, ok := reg.Lookup("Krenko, Mob Boss")
	if !ok {
		tb.Fatal(`corpus card "Krenko, Mob Boss" not found`)
	}
	mountain, ok := reg.Lookup("Mountain")
	if !ok {
		tb.Fatal(`corpus card "Mountain" not found`)
	}
	if _, ok := reg.Token("r_1_1_goblin"); !ok {
		tb.Fatal(`corpus token "r_1_1_goblin" not found`)
	}
	deck0 := []*cards.Card{krenko}
	for i := 0; i < 39; i++ {
		deck0 = append(deck0, mountain)
	}
	deck1 := make([]*cards.Card, 0, 40)
	for i := 0; i < 40; i++ {
		deck1 = append(deck1, mountain)
	}
	cfg = rules.Config{Names: []string{"krenko", "other"},
		Decks:  [][]*cards.Card{deck0, deck1},
		Tokens: reg.Tokens, Seed: 11}
	if maxObjects > 0 {
		cfg.LoopGuard = &rules.LoopGuard{MaxObjs: maxObjects}
	}
	e = rules.New(cfg)
	// Krenko starts in seat 0's library or opening hand; move it onto the
	// battlefield from wherever genesis placed it, as a logged MoveZone --
	// exactly the event shape every real "put onto the battlefield" effect
	// logs.
	from := state.ZLibrary
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Card != nil && o.Face().Name == "Krenko, Mob Boss" && o.Owner == 0 {
			kid = o.ID
			from = o.Zone
			break
		}
	}
	if kid == 0 {
		tb.Fatal("Krenko, Mob Boss not found in seat 0's arena at genesis")
	}
	e.Emit(events.Event{Kind: events.MoveZone, Obj: kid, From: from, To: state.ZBattlefield})
	// Mint the board: one TokenCreate per Goblin, the exact event a real
	// token engine logs.
	for i := 0; i < goblins; i++ {
		e.Emit(events.Event{Kind: events.TokenCreate, Player: 0, Text: "r_1_1_goblin"})
	}
	// Drive all-pass to the first priority ask that offers Krenko's ability
	// (a Goblin token's ability is never offered; only Krenko's Obj matches).
	// Combat asks that cross on the way (the fixture's turn-1 board can be
	// unsick) are answered with the legal empty declaration.
	e.Advance()
	const driveLimit = 4000
	for i := 0; i < driveLimit; i++ {
		if e.G.Over {
			tb.Fatal("fixture game ended before Krenko's ability was offered")
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			continue
		}
		switch d.Kind {
		case decision.KPriority:
			pass, ability := -1, -1
			for _, o := range d.Options {
				if o.Kind == "ability" && o.Obj == kid {
					ability = o.Index
				}
				if o.Kind == "pass" {
					pass = o.Index
				}
			}
			if ability >= 0 {
				return e, cfg, kid, ability
			}
			if pass < 0 {
				tb.Fatalf("priority ask without a pass option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pass}}); err != nil {
				tb.Fatalf("submit pass: %v", err)
			}
		case decision.KAttackers, decision.KBlockers:
			// The legal empty declaration.
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: nil}); err != nil {
				tb.Fatalf("submit empty combat declaration: %v", err)
			}
		default:
			tb.Fatalf("unexpected %v decision while driving the fixture: %+v", d.Kind, d)
		}
	}
	tb.Fatal("Krenko's ability never offered within the drive budget")
	return nil, cfg, 0, -1
}

// goblinTokens counts seat 0's Goblin Token permanents.
func goblinTokens(tb testing.TB, e *rules.Engine, seat state.PlayerID) int {
	tb.Helper()
	n := 0
	for _, id := range e.G.Zone(state.ZBattlefield, seat) {
		if o := e.G.Obj(id); o != nil && o.Face().Name == "Goblin Token" {
			n++
		}
	}
	return n
}

// passUntilStackDrained answers pass-priority decisions until the stack is
// empty (the shape rules' passUntilStackEmpty drains with, minus the dig
// arms a board this fixture cannot pose -- the fixture asserts that).
func passUntilStackDrained(tb testing.TB, e *rules.Engine, limit int) {
	tb.Helper()
	for i := 0; i < limit && len(e.G.Stack) > 0 && !e.G.Over; i++ {
		d := e.Pending()
		if d == nil {
			tb.Fatalf("no decision while the stack is live (depth %d)", len(e.G.Stack))
		}
		if d.Kind != decision.KPriority {
			tb.Fatalf("unexpected %v ask while draining the stack: %+v", d.Kind, d)
		}
		pass := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				pass = o.Index
			}
		}
		if pass < 0 {
			tb.Fatalf("priority ask without a pass option: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pass}}); err != nil {
			tb.Fatalf("submit drain pass: %v", err)
		}
	}
	if len(e.G.Stack) > 0 {
		tb.Fatalf("stack did not drain within %d passes (depth %d)", limit, len(e.G.Stack))
	}
}

// krenkoCapOffset is how far above the staged arena the mid-resolution cap
// sits in the staged tests: the armed abort must fire a few mints into the
// doubling resolution, not at its very first token, which is the shape the
// runaway explore game hit (the budget crossed deep inside one resolution).
const krenkoCapOffset = 64

// krenkoCap is the object cap for a fixture staged with goblins Goblins:
// the genesis arena (two 40-card decks = 80 objects) plus the board plus
// krenkoCapOffset.
func krenkoCap(goblins int) int { return 80 + goblins + krenkoCapOffset }

// TestKrenkoBoardCapFiresMidResolution is the deterministic replacement for
// the round-4 explore repro (seed 6181111140895991800), which no longer
// reproduces on main: a staged 4000-Goblin board whose very next Krenko
// resolution crosses the armed -max-objects budget mid-resolution, 64
// tokens into the doubling. The abort must carry the watcher's own reason,
// the crossing count (exactly one object past the cap), and the aborting
// event kind -- the mid-resolution provenance a decision-boundary record
// can never carry.
func TestKrenkoBoardCapFiresMidResolution(t *testing.T) {
	const goblins = 4000
	maxObjs := krenkoCap(goblins)
	e, _, kid, abilityIdx := krenkoFixture(t, goblins, maxObjs)
	// Preconditions: the staged board is what the assertion depends on.
	if got := goblinTokens(t, e, 0); got != goblins {
		t.Fatalf("staged %d Goblin tokens, board holds %d", goblins, got)
	}
	if o := e.G.Obj(kid); o == nil || o.Face().Name != "Krenko, Mob Boss" || o.Controller != 0 || o.Tapped {
		t.Fatalf("Krenko not staged untapped for seat 0: %+v", o)
	}
	if arena := len(e.G.Objs); arena >= maxObjs {
		t.Fatalf("arena %d already at/over maxObjects %d before the resolution", arena, maxObjs)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority ask at the fixture park point: %+v", d)
	}
	// One Krenko activation crosses the maxObjs mid-resolution. The ability
	// goes on the stack on Submit; the mints happen when priority passes drain
	// it, so the recover must wrap BOTH the activation and the drain. The
	// watcher panics with a *rules.LivelockError inside the drain; recover it
	// exactly the way internal/bench's drive loop does.
	var abort *rules.LivelockError
	func() {
		defer func() {
			if r := recover(); r != nil {
				l, ok := r.(*rules.LivelockError)
				if !ok {
					t.Fatalf("activation panicked with %T, want *rules.LivelockError", r)
				}
				abort = l
			}
		}()
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{abilityIdx}}); err != nil {
			t.Fatalf("submit Krenko activation: %v", err)
		}
		passUntilStackDrained(t, e, 9000)
	}()
	if abort == nil {
		t.Fatal("armed maxObjs did not abort the doubling resolution")
	}
	if abort.Reason != "object cap" {
		t.Fatalf("abort reason = %q, want object cap", abort.Reason)
	}
	if abort.Cap != maxObjs {
		t.Fatalf("abort cap = %d, want %d", abort.Cap, maxObjs)
	}
	// The crossing is exactly one object past the cap: the abort fired on
	// the first mint that pushed the arena past the budget.
	if abort.Count != maxObjs+1 {
		t.Fatalf("abort count = %d, want cap+1 = %d", abort.Count, maxObjs+1)
	}
	if abort.Kind != events.TokenCreate {
		t.Fatalf("aborting event kind = %v, want token_create", abort.Kind)
	}
	if !strings.Contains(abort.Error(), "kind token_create") {
		t.Fatalf("abort diagnostic %q lacks the aborting kind", abort.Error())
	}
}

// TestKrenkoBoardUnarmedDoubles pins the fixture's own premise: with no cap
// armed, the same staged 4000-Goblin board's Krenko resolution DOUBLES the
// board (X = the number of Goblins you control) and the log still replays.
// This is the "before" behaviour the armed cap truncates, and it is also
// the precondition the benchmark's before/after reading stands on.
func TestKrenkoBoardUnarmedDoubles(t *testing.T) {
	const goblins = 4000
	e, _, kid, abilityIdx := krenkoFixture(t, goblins, 0)
	if got := goblinTokens(t, e, 0); got != goblins {
		t.Fatalf("staged %d Goblin tokens, board holds %d", goblins, got)
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority ask at the fixture park point: %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{abilityIdx}}); err != nil {
		t.Fatalf("submit Krenko activation: %v", err)
	}
	passUntilStackDrained(t, e, 200)
	// Krenko is itself a Goblin, so X = staged Goblins + Krenko, and the
	// board after the resolution is the staged board plus the X new tokens.
	if got := goblinTokens(t, e, 0); got != 2*goblins+1 {
		t.Fatalf("board after Krenko = %d Goblin tokens, want %d (the doubling)", got, 2*goblins+1)
	}
	_ = kid
}

// BenchmarkKrenkoResolution measures ONE Krenko, Mob Boss resolution from a
// staged board of 4000 / 8000 Goblin tokens, with the mid-resolution object
// cap armed (after) and unarmed (before).
//
//   - before: the resolution runs to completion -- it mints the whole other
//     half of the doubling, so ns/op grows with the staged board, the shape
//     that burned the whole wall-clock budget on the round-4 explore game
//     before any boundary check could classify it.
//   - after: the armed cap aborts the resolution a few mints past the
//     budget, so ns/op is flat in the staged board size and the game is
//     classified as bigboard early.
//
// The fixture build (minting the staged board and driving to the park
// point) sits outside the timed section; the timed op is exactly one
// activation Submit and, in the after arm, the recover of its abort.
func BenchmarkKrenkoResolution(b *testing.B) {
	for _, bc := range []struct {
		name    string
		goblins int
		cap     int
	}{
		{"before-4k", 4000, 0},
		{"after-4k", 4000, krenkoCap(4000)},
		{"before-8k", 8000, 0},
		{"after-8k", 8000, krenkoCap(8000)},
	} {
		b.Run(bc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				e, _, _, abilityIdx := krenkoFixture(b, bc.goblins, bc.cap)
				d := e.Pending()
				if d == nil || d.Kind != decision.KPriority {
					b.Fatalf("no priority ask at the fixture park point: %+v", d)
				}
				b.StartTimer()
				if bc.cap == 0 {
					if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{abilityIdx}}); err != nil {
						b.Fatalf("submit Krenko activation: %v", err)
					}
					passUntilStackDrained(b, e, 200)
					continue
				}
				func() {
					defer func() {
						if r := recover(); r == nil {
							b.Fatal("armed cap did not abort the doubling resolution")
						} else if _, ok := r.(*rules.LivelockError); !ok {
							b.Fatalf("activation panicked with %T, want *rules.LivelockError", r)
						}
					}()
					if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{abilityIdx}}); err != nil {
						b.Fatalf("submit Krenko activation: %v", err)
					}
					passUntilStackDrained(b, e, 9000)
				}()
			}
		})
	}
}
