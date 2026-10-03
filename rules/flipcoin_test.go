package rules

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// flipEngine is corpusEngine with the seed a parameter and seat 0 guaranteed
// the starting seat (seatZeroStart bumps the seed until the toss names seat
// 0): seat 0's deck is extras0 then Mountains, seat 1's Mountains, driven to
// seat 0's turn-1 Main1.
func flipEngine(t *testing.T, reg *cards.Registry, seed uint64, extras0, extras1 []*cards.Card) (*Engine, Config) {
	t.Helper()
	m, ok := reg.Lookup("Mountain")
	if !ok {
		t.Fatal("corpus fixture: Mountain missing")
	}
	fill := func(extras []*cards.Card) []*cards.Card {
		deck := append([]*cards.Card{}, extras...)
		for len(deck) < 40 {
			deck = append(deck, m)
		}
		return deck
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{fill(extras0), fill(extras1)}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// flipNotes returns the canonical coin-flip result Notes in the log, in
// order, with their win flags.
func flipNotes(e *Engine) []bool {
	var out []bool
	for _, ev := range e.L.Events {
		if _, win, ok := effects.FlipNoteResult(ev); ok {
			out = append(out, win)
		}
	}
	return out
}

// flipAbilityOption submits the pending priority option that activates obj's
// FlipCoin ability (rules' ability offer walk), leaving whatever ask the
// activation's cost/target step poses pending.
func flipAbilityOption(t *testing.T, e *Engine, obj state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision to activate FlipCoin from: %+v", d)
	}
	idx := -1
	face := e.G.Obj(obj).Face()
	for _, o := range d.Options {
		if o.Kind != "ability" || o.Obj != obj || face == nil ||
			o.Ability < 0 || o.Ability >= len(face.Abilities) {
			continue
		}
		if face.Abilities[o.Ability].API == "FlipCoin" {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("FlipCoin ability not offered: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit FlipCoin activation: %v", err)
	}
}

// submitTargetOnSeat answers a pending target decision with the first option
// naming seat p (Goblin Bangchuckers' "any target": a player target is what
// makes the win branch's damage observable on the life total).
func submitTargetOnSeat(t *testing.T, e *Engine, p state.PlayerID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("no target decision pending: %+v", d)
	}
	for _, o := range d.Options {
		if o.Player == p {
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
				t.Fatalf("submit target: %v", err)
			}
			return
		}
	}
	t.Fatalf("no seat-%d target option: %+v", p, d.Options)
}

// answerFirst submits the pending decision's first option.
func answerFirst(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending")
	}
	if len(d.Options) == 0 {
		t.Fatalf("empty decision %+v", d)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
		t.Fatalf("submit: %v", err)
	}
}

// flipOnce drives ONE activation of obj's FlipCoin ability (the creature
// enters untapped; summoning sickness cleared so the {T} cost is payable):
// submit the ability, answer any target ask on seat 1, and drain the stack
// (and any FlippedCoin triggers the flip queued).
func flipOnce(t *testing.T, e *Engine, obj state.ObjID) {
	t.Helper()
	e.G.Obj(obj).SummonSick = false
	e.priorityRound() // a fresh priority decision that offers the ability
	flipAbilityOption(t, e, obj)
	d := e.Pending()
	if d != nil && d.Kind == decision.KTarget {
		submitTargetOnSeat(t, e, 1)
	}
	passUntilStackEmpty(t, e, 80)
}

// activateFlipAbility activates the flip ability, answering the
// sacrifice-cost pick it poses with the first option (Tavern Scoundrel's
// Sac<1/Permanent.Other> — a battlefield Mountain), then drains the stack.
func activateFlipAbility(t *testing.T, e *Engine, obj state.ObjID) {
	t.Helper()
	e.G.Obj(obj).SummonSick = false
	e.priorityRound() // a fresh priority decision that offers the ability
	flipAbilityOption(t, e, obj)
	for i := 0; i < 8; i++ {
		d := e.Pending()
		if d == nil || d.Kind == decision.KPriority {
			break // the ability is on the stack; the cost step is settled
		}
		if d.Kind != decision.KChoose {
			t.Fatalf("unexpected decision kind %v during activation: %+v", d.Kind, d)
		}
		answerFirst(t, e)
	}
	passUntilStackEmpty(t, e, 80)
}

// damageTo counts Damage events naming target (object id) with the given
// amount.
func damageTo(e *Engine, target state.ObjID, amount int32) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Damage && ev.Obj == target && ev.Amount == amount {
			n++
		}
	}
	return n
}

// damageToPlayer counts Damage events naming a player target with the given
// amount.
func damageToPlayer(e *Engine, p state.PlayerID, amount int32) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Damage && ev.Obj == 0 && ev.Player == p && ev.Amount == amount {
			n++
		}
	}
	return n
}

// countDamageAmount counts every Damage event with the given amount.
func countDamageAmount(e *Engine, amount int32) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Damage && ev.Amount == amount {
			n++
		}
	}
	return n
}

// tokensNamed counts seat p's battlefield tokens whose face name contains
// name ("Treasure").
func tokensNamed(e *Engine, p state.PlayerID, name string) int {
	n := 0
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		o := e.G.Obj(id)
		if o != nil && o.IsToken && o.Face() != nil && strings.Contains(o.Face().Name, name) {
			n++
		}
	}
	return n
}

// flipTrig records one resolved FlippedCoin trigger: the Execute$-resolved
// body's TargetingPlayer$ value ("" for the win side, "Opponent" for the
// lose side) and the target option the trigger's own target ask was answered
// with (Obj/Player). Tying the recorded target to the trigger is what lets a
// test assert the 1 damage landed where that side aimed, not merely that
// SOME 1-damage hit happened (the review's MINOR on the Karplusan leaf).
type flipTrig struct {
	side   string
	target decision.Option
}

// drainFlippedCoinTriggers resolves the n FlippedCoin triggers the n flips
// just queued: the first trigger's target ask is already pending when the
// paying submission returns (pushTrigger poses it right after the TriggerPush),
// later ones surface as the stack drains. Handles the trigger-order ask when
// several fired together, answers every target ask with its first option, and
// returns one flipTrig per resolved trigger.
func drainFlippedCoinTriggers(t *testing.T, e *Engine, n int) []flipTrig {
	t.Helper()
	var execs []flipTrig
	for budget := 0; budget < 200; budget++ {
		done := len(execs) >= n && len(e.G.Stack) == 0
		d := e.Pending()
		if done && (d == nil || d.Kind == decision.KPriority) {
			return execs
		}
		if d == nil {
			e.priorityRound()
			d = e.Pending()
			if d == nil {
				if done {
					return execs
				}
				t.Fatalf("engine idle before all FlippedCoin triggers resolved (%d/%d)", len(execs), n)
			}
		}
		switch d.Kind {
		case decision.KTriggerOrder:
			var order []int
			for j := range d.Options {
				order = append(order, j)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: order}); err != nil {
				t.Fatalf("submit trigger order: %v", err)
			}
		case decision.KTarget:
			if len(e.G.Stack) == 0 {
				t.Fatalf("target ask with an empty stack (trigger %d/%d)", len(execs)+1, n)
			}
			trig := e.G.Obj(e.G.Stack[len(e.G.Stack)-1])
			if trig == nil || trig.Ability == nil {
				t.Fatalf("stack top is not a trigger object: %+v", trig)
			}
			// The pushed trigger's Ability is the Execute$-resolved body; the
			// two sides' bodies differ exactly by the lose side's
			// TargetingPlayer$ Opponent (the unread per-opponent-choice
			// stand-in). The chosen option is the trigger's OWN target, so the
			// damage it deals can be tied back to this side.
			var chosen decision.Option
			if len(d.Options) > 0 {
				chosen = d.Options[0]
			}
			execs = append(execs, flipTrig{side: trig.Ability.Params["TargetingPlayer"], target: chosen})
			submitChoices(t, e, 0)
		case decision.KPriority:
			passOnce(t, e)
		default:
			t.Fatalf("unexpected decision %v while resolving FlippedCoin triggers", d.Kind)
		}
	}
	t.Fatal("drainFlippedCoinTriggers did not converge")
	return nil
}

// execMultisetMatches reports whether execs (order-insensitive; each entry's
// side is "" for the win side, "Opponent" for the lose side) are exactly one
// per flip, matched to the flips' win flags.
func execMultisetMatches(t *testing.T, execs []flipTrig, flips []bool) {
	t.Helper()
	if len(execs) != len(flips) {
		t.Fatalf("resolved %d FlippedCoin triggers for %d flips", len(execs), len(flips))
	}
	got := make([]string, len(execs))
	for i, x := range execs {
		got[i] = x.side
	}
	sort.Strings(got)
	var want []string
	for _, win := range flips {
		if win {
			want = append(want, "")
		} else {
			want = append(want, "Opponent")
		}
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolved triggers %v, want %v for flips %v", got, want, flips)
	}
}

// TestFlipCoinGoblinBangchuckers is api:FlipCoin's both-branch leaf (real
// corpus Goblin Bangchuckers): {T} flips a coin — win deals 2 to the chosen
// target, lose deals 2 to itself. The flip comes from the seeded rng and a
// losing flip kills the 2/2, so the two branches are asserted on the ONE
// card's ability across two pinned seeds whose first flips differ (measured:
// seed 4 wins, seed 1 loses).
func TestFlipCoinGoblinBangchuckers(t *testing.T) {
	t.Parallel()
	if !effects.Supported()["api:FlipCoin"] {
		t.Fatal("api:FlipCoin not registered in effects.Supported()")
	}
	reg := testutil.CorpusRegistry(t)
	run := func(seed uint64) (*Engine, state.ObjID, bool) {
		e, _ := flipEngine(t, reg, seed,
			[]*cards.Card{lookup(t, reg, "Goblin Bangchuckers")}, []*cards.Card{})
		moveByName(t, e, 0, "Goblin Bangchuckers", state.ZBattlefield)
		id := firstCreature(t, e, 0)
		flipOnce(t, e, id)
		notes := flipNotes(e)
		if len(notes) != 1 {
			t.Fatalf("seed %d: want exactly one flip Note, got %d", seed, len(notes))
		}
		return e, id, notes[0]
	}
	eWin, idWin, winA := run(4)
	eLose, idLose, winB := run(1)
	if winA == winB {
		t.Fatalf("pinned seeds no longer cover both outcomes: %v and %v", winA, winB)
	}
	if got := damageToPlayer(eWin, 1, 2); winA && got != 1 {
		t.Fatalf("win branch did not deal exactly one 2-damage hit to the chosen target (got %d)", got)
	}
	if got := damageTo(eWin, idWin, 2); winA && got != 0 {
		t.Fatalf("win branch also dealt 2 to itself %d times", got)
	}
	if got := damageTo(eLose, idLose, 2); !winB && got != 1 {
		t.Fatalf("lose branch did not deal exactly one 2-damage hit to itself (got %d)", got)
	}
	if got := damageToPlayer(eLose, 1, 2); !winB && got != 0 {
		t.Fatalf("lose branch also dealt 2 to the target %d times", got)
	}
}

// TestFlipCoinTavernSwindler is the single-branch leaf (real corpus Tavern
// Swindler): {T}, Pay 3 life — win gains 6 life (net +3), lose gains nothing
// (net -3). Two pinned seeds cover both sides of the one-branch ability.
func TestFlipCoinTavernSwindler(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	run := func(seed uint64) (*Engine, bool) {
		e, _ := flipEngine(t, reg, seed,
			[]*cards.Card{lookup(t, reg, "Tavern Swindler")}, []*cards.Card{})
		moveByName(t, e, 0, "Tavern Swindler", state.ZBattlefield)
		id := firstCreature(t, e, 0)
		base := e.G.Players[0].Life
		flipOnce(t, e, id)
		notes := flipNotes(e)
		if len(notes) != 1 {
			t.Fatalf("seed %d: want exactly one flip Note, got %d", seed, len(notes))
		}
		want := base - 3
		if notes[0] {
			want += 6
		}
		if got := e.G.Players[0].Life; got != want {
			t.Fatalf("seed %d (win=%v): life = %d, want %d", seed, notes[0], got, want)
		}
		return e, notes[0]
	}
	_, winA := run(4)
	_, winB := run(1)
	if winA == winB {
		t.Fatalf("pinned seeds no longer cover both outcomes: %v and %v", winA, winB)
	}
}

// TestFlippedCoinTavernScoundrel is trig:FlippedCoin's leaf (real corpus
// Tavern Scoundrel): its own {1}{T}-sac-another-permanent ability flips, and
// its ValidResult$ Win trigger creates exactly two Treasure tokens per
// winning flip — and none on a losing flip. Two pinned seeds cover both
// sides.
func TestFlippedCoinTavernScoundrel(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	run := func(seed uint64) (*Engine, bool) {
		e, _ := flipEngine(t, reg, seed,
			[]*cards.Card{lookup(t, reg, "Tavern Scoundrel")}, []*cards.Card{})
		moveByName(t, e, 0, "Tavern Scoundrel", state.ZBattlefield)
		moveByName(t, e, 0, "Mountain", state.ZBattlefield)
		id := firstCreature(t, e, 0)
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "C", Amount: 1})
		activateFlipAbility(t, e, id)
		notes := flipNotes(e)
		if len(notes) != 1 {
			t.Fatalf("seed %d: want exactly one flip Note, got %d", seed, len(notes))
		}
		want := 0
		if notes[0] {
			want = 2
		}
		if got := tokensNamed(e, 0, "Treasure"); got != want {
			t.Fatalf("seed %d (win=%v): %d Treasures, want %d", seed, notes[0], got, want)
		}
		return e, notes[0]
	}
	_, winA := run(4)
	_, winB := run(1)
	if winA == winB {
		t.Fatalf("pinned seeds no longer cover both outcomes: %v and %v", winA, winB)
	}
}

// hitsOn1 counts, over the whole log, every 1-damage Damage event aimed at the
// target identity key: key[0] == -1 means a PLAYER target (key[1] is the
// seat), otherwise key[0] is an object id. Used by the Karplusan leaf to tie
// each FlippedCoin trigger's damage to the target that side aimed at.
func hitsOn1(e *Engine, key [2]int32) int {
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind != events.Damage || ev.Amount != 1 {
			continue
		}
		if key[0] == -1 {
			if ev.Obj == 0 && int32(ev.Player) == key[1] {
				n++
			}
		} else if int32(ev.Obj) == key[0] {
			n++
		}
	}
	return n
}

// karplusanScenario drives seat 0's Karplusan Minotaur through three upkeeps
// (1+2+3 = six cumulative-upkeep flips), paying every upkeep and resolving
// every FlippedCoin trigger. Returns the engine and the config a same-seed
// rerun can be compared against.
func karplusanScenario(t *testing.T, reg *cards.Registry) (*Engine, Config) {
	t.Helper()
	e, cfg := flipEngine(t, reg, 42,
		[]*cards.Card{lookup(t, reg, "Karplusan Minotaur")}, []*cards.Card{})
	moveByName(t, e, 0, "Karplusan Minotaur", state.ZBattlefield)
	id := firstCreature(t, e, 0)
	// chosenTotal accumulates, per target identity, how many triggers have
	// chosen it across every turn so far. Every trigger deals exactly one
	// 1-damage hit to its chosen target, so the all-time hit count on that
	// target must equal this running total -- which is what ties each
	// trigger's damage to the target ITS side aimed (win and lose both aim
	// "any target", but only the lose side carries TargetingPlayer$
	// Opponent).
	chosenTotal := map[[2]int32]int{}
	for turn := int32(2); turn <= 4; turn++ {
		e.G.Turn = turn
		e.beginTurn(0)
		e.priorityRound()
		if len(e.G.Stack) != 1 || e.G.Obj(e.G.Stack[0]).Ability.API != "CumulativeUpkeep" {
			t.Fatalf("turn %d: cumulative upkeep not placed: %v", turn, e.G.Stack)
		}
		e.resolveTop()
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 ||
			d.Options[0].Kind != "cumulative_pay" {
			t.Fatalf("turn %d: expected the pay-or-sacrifice ask, got %+v", turn, d)
		}
		before := len(flipNotes(e))
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
			t.Fatalf("pay upkeep: %v", err)
		}
		flips := flipNotes(e)[before:]
		if len(flips) != int(turn)-1 {
			t.Fatalf("turn %d: want %d flips (1 per age counter), got %d",
				turn, turn-1, len(flips))
		}
		dmgBefore := countDamageAmount(e, 1)
		execs := drainFlippedCoinTriggers(t, e, len(flips))
		execMultisetMatches(t, execs, flips)
		if got := countDamageAmount(e, 1) - dmgBefore; got != len(flips) {
			t.Fatalf("turn %d: the triggers dealt %d one-damage hits, want %d",
				turn, got, len(flips))
		}
		// Tie each trigger's damage to the target that side's own target ask
		// was answered with. Several triggers may be answered with the SAME
		// target, so count the triggers per target and assert that target's
		// all-time 1-damage total equals the running total of triggers that
		// chose it -- each resolved body dealt its 1 to the target IT chose,
		// not merely that len(flips) hits happened somewhere.
		chosen := map[[2]int32]int{}
		for _, x := range execs {
			key := [2]int32{int32(x.target.Obj), int32(x.target.Player)}
			if x.target.Obj == 0 {
				key[0] = -1
			}
			chosen[key]++
			chosenTotal[key]++
		}
		keys := make([][2]int32, 0, len(chosenTotal))
		for key := range chosenTotal {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i][0] != keys[j][0] {
				return keys[i][0] < keys[j][0]
			}
			return keys[i][1] < keys[j][1]
		})
		for _, key := range keys {
			if got, want := hitsOn1(e, key), chosenTotal[key]; got != want {
				t.Fatalf("turn %d: target %v has taken %d one-damage hits, want %d (one per trigger that chose it)",
					turn, key, got, want)
			}
		}
	}
	if got := e.G.Obj(id).Counter("AGE"); got != 3 {
		t.Fatalf("three upkeeps left AGE=%d, want 3", got)
	}
	return e, cfg
}

// TestFlipCoinManaCryptTriggerDriven is the trigger-Execute-driven flip (real
// corpus Mana Crypt): at its controller's upkeep the Phase trigger resolves
// DB$ FlipCoin | Defined$ You — a losing flip deals 3 to its controller, a
// winning flip nothing.
func TestFlipCoinManaCryptTriggerDriven(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _ := flipEngine(t, reg, 4,
		[]*cards.Card{lookup(t, reg, "Mana Crypt")}, []*cards.Card{})
	moveByName(t, e, 0, "Mana Crypt", state.ZBattlefield)
	e.G.Turn = 2
	e.beginTurn(0)
	e.priorityRound()
	if len(e.G.Stack) != 1 {
		t.Fatalf("upkeep triggers not placed: %v", e.G.Stack)
	}
	e.resolveTop()
	notes := flipNotes(e)
	if len(notes) != 1 {
		t.Fatalf("want exactly one upkeep flip, got %d", len(notes))
	}
	passUntilStackEmpty(t, e, 50)
	if notes[0] {
		if got := damageToPlayer(e, 0, 3); got != 0 {
			t.Fatalf("winning flip dealt 3 to the controller %d times, want 0", got)
		}
	} else {
		if got := damageToPlayer(e, 0, 3); got != 1 {
			t.Fatalf("losing flip did not deal exactly one 3-damage hit (got %d)", got)
		}
	}
}

// TestFlipCoinUntilYouLoseCrazedFirecat is the FlipUntilYouLose$ leaf (real
// corpus Crazed Firecat): its ETB trigger resolves DB$ FlipCoin |
// FlipUntilYouLose$ True | WinSubAbility$ DBPutCounter, so the loop keeps
// flipping while it wins and stops on the FIRST loss. Pinned seeds: seed 21
// flips three heads then a tail; seed 1 loses the opening flip. This is the
// behaviour the FlipUntilYouLose$ loop adds and is asserted on the flip-Note
// sequence itself (the win branch's CounterNum$ Wins reads the unread
// remember-flip-count memory, so the counter is not the observable here --
// the flips are).
func TestFlipCoinUntilYouLoseCrazedFirecat(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	run := func(seed uint64) []bool {
		e, _ := flipEngine(t, reg, seed,
			[]*cards.Card{lookup(t, reg, "Crazed Firecat")}, []*cards.Card{})
		moveByName(t, e, 0, "Crazed Firecat", state.ZBattlefield)
		e.priorityRound()
		passUntilStackEmpty(t, e, 80)
		return flipNotes(e)
	}
	multi := run(21)
	if len(multi) != 4 {
		t.Fatalf("seed 21: want 4 flips (three wins then a loss), got %v", multi)
	}
	for i := 0; i < 3; i++ {
		if !multi[i] {
			t.Fatalf("seed 21: flip %d = tails, want the loop to keep flipping while winning: %v", i, multi)
		}
	}
	if multi[3] {
		t.Fatalf("seed 21: the loop did not stop on the first tails: %v", multi)
	}
	one := run(1)
	if len(one) != 1 || one[0] {
		t.Fatalf("seed 1: want exactly one (losing) flip, got %v", one)
	}
}
