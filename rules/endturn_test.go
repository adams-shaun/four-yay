package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// api:EndTurn (CR 723, "End the turn"): the driver is Time Stop. These tests
// prove the primitive is registered for every corpus carrier, that a resolved
// Time Stop exiles the whole stack (including itself), clears combat, skips
// the rest of the turn straight to cleanup, and replays identically from its
// log.

// TestEndTurnPrimitiveRegisteredForEveryCorpusCarrier walks the corpus for
// every card whose parsed primitives include api:EndTurn (the SP$/DB$/AB$
// EndTurn scripts) and asserts the registry no longer names it unsupported.
// It also pins the carrier count so a corpus-pin change that grows the set is
// noticed rather than silently skipped -- and names any carrier still missing
// a DIFFERENT primitive, so "fully supported" is a measurement, not a claim.
func TestEndTurnPrimitiveRegisteredForEveryCorpusCarrier(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	supported := effects.Supported()
	if !supported["api:EndTurn"] {
		t.Fatal("effects.Supported() does not name api:EndTurn: the primitive is not registered")
	}
	carriers := 0
	for _, c := range reg.Cards {
		has := false
		for _, p := range c.Primitives() {
			if p == "api:EndTurn" {
				has = true
				break
			}
		}
		if !has {
			continue
		}
		carriers++
		name := c.Faces[0].Name
		for _, m := range reg.Unsupported(c, supported) {
			if m == "api:EndTurn" {
				t.Errorf("card %q still reports api:EndTurn unsupported", name)
			}
		}
		if miss := reg.Unsupported(c, supported); len(miss) > 0 {
			t.Logf("carrier %q still missing other primitives: %v", name, miss)
		}
	}
	// The 9 corpus carriers named in the brief. If the corpus pin moves and
	// this count changes, the new carrier must be examined, not assumed.
	if carriers != 9 {
		t.Errorf("corpus carriers of api:EndTurn = %d, want 9", carriers)
	}
}

// endTurnFixture is the two-seat game the Time Stop scenario runs on. Seat 0
// (the active player) holds a hasty attacker (so it can attack on turn 1), a
// creature spell in hand, the end-step trigger source (Khabál Ghoul) and a
// creature that has died this turn; seat 1 holds Time Stop.
type endTurnFixture struct {
	e        *Engine
	cfg      Config
	attacker state.ObjID
	ghoul    state.ObjID
	spell    state.ObjID
	timeStop state.ObjID
	dead     state.ObjID
}

func newEndTurnFixture(t *testing.T, reg *cards.Registry) endTurnFixture {
	t.Helper()
	bears := lookup(t, reg, "Grizzly Bears")
	goblin := lookup(t, reg, "Raging Goblin")
	ghoul := lookup(t, reg, "Khabál Ghoul")
	timeStop := lookup(t, reg, "Time Stop")
	mountain := lookup(t, reg, "Mountain")
	fill := func(n int) []*cards.Card {
		out := make([]*cards.Card, n)
		for i := range out {
			out[i] = mountain
		}
		return out
	}
	// Seat 0: three Bears (the stacked creature spell and two creatures that
	// die), a hasty Goblin and the Ghoul. Seat 1: Time Stop only.
	deck0 := []*cards.Card{bears, bears, bears, goblin, ghoul}
	deck0 = append(deck0, fill(60-len(deck0))...)
	deck1 := append([]*cards.Card{timeStop}, fill(60-1)...)
	cfg := seatZeroStart(Config{Seed: 42, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{deck0, deck1}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	f := endTurnFixture{e: e, cfg: cfg}
	f.attacker = moveByName(t, e, 0, "Raging Goblin", state.ZBattlefield)
	f.ghoul = moveByName(t, e, 0, "Khabál Ghoul", state.ZBattlefield)
	f.spell = moveByName(t, e, 0, "Grizzly Bears", state.ZHand)
	// A creature dies this turn so the Ghoul's end-step trigger WOULD place a
	// +1/+1 counter if the end step were ever reached: the "did not fire"
	// assertion below is only meaningful against a live trigger.
	f.dead = moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	e.emit(events.Event{Kind: events.MoveZone, Obj: f.dead, From: state.ZBattlefield, To: state.ZGraveyard})
	f.timeStop = moveByName(t, e, 1, "Time Stop", state.ZHand)
	return f
}

func ghoulCounter(e *Engine, id state.ObjID) int32 {
	if o := e.G.Obj(id); o != nil {
		return o.Counter("P1P1")
	}
	return -1
}

// TestTimeStopExilesStackSkipsToCleanupAndReplays is the api:EndTurn leaf on
// the real corpus Time Stop. Seat 0 (the active player) is in its
// declare-attackers step with a hasty attacker declared, a creature spell of
// its own on the stack beneath Time Stop, marked damage on the attacker, and
// a live end-step trigger source (Khabál Ghoul) on its battlefield with a
// creature that died this turn. Seat 1 casts Time Stop. After it resolves:
// both spells are exiled, nothing is attacking, the attacker's damage is
// gone, the Ghoul's end-step trigger never fired, seat 0 discarded down to
// hand size in cleanup, and the next turn begins. The game then replays
// identically from its log.
func TestTimeStopExilesStackSkipsToCleanupAndReplays(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	f := newEndTurnFixture(t, reg)
	e := f.e
	if e.G.Step != state.StepMain1 || e.G.Active != 0 || e.G.Turn != 1 {
		t.Fatalf("fixture precondition: turn %d seat %d step %s, want seat 0's turn-1 Main1",
			e.G.Turn, e.G.Active, e.G.Step)
	}
	if e.G.Obj(f.dead).Zone != state.ZGraveyard || e.G.Obj(f.ghoul).Zone != state.ZBattlefield {
		t.Fatal("fixture precondition: dead creature / live Ghoul not set up")
	}
	driveToStep(t, e, 1, 0, state.StepDeclareAttackers)
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, f.attacker)
	if !e.G.Obj(f.attacker).IsAttacking {
		t.Fatal("precondition: attacker was not declared attacking")
	}
	// Mark damage on the attacker (removed by cleanup).
	e.emit(events.Event{Kind: events.Damage, Obj: f.attacker, Amount: 1})
	if e.G.Obj(f.attacker).Damage == 0 {
		t.Fatal("precondition: attacker carries no damage")
	}
	// Pad seat 0's hand over the maximum so cleanup owes a discard: move real
	// library cards into hand (a logged MoveZone, replayable).
	for _, id := range e.G.Zone(state.ZLibrary, 0) {
		if len(e.G.Zone(state.ZHand, 0)) > 8 {
			break
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZHand})
	}
	if handBefore := len(e.G.Zone(state.ZHand, 0)); handBefore <= 7 {
		t.Fatalf("precondition: hand was %d before resolution, want over 7", handBefore)
	}
	// Put a creature spell of seat 0 on the stack beneath Time Stop.
	e.emit(events.Event{Kind: events.PutOnStack, Obj: f.spell, Player: 0,
		From: state.ZHand, To: state.ZStack})
	if e.G.Obj(f.spell).Zone != state.ZStack {
		t.Fatal("precondition: creature spell not on the stack")
	}
	// Pay Time Stop (4UU) and cast it. This is a real cast: the cast
	// transaction emits its PutOnStack above the creature spell.
	for i := 0; i < 6; i++ {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 1, Counter: "U", Amount: 1})
	}
	e.beginCast(1, decision.Option{Kind: "cast", Obj: f.timeStop})
	if e.G.Obj(f.timeStop).Zone != state.ZStack {
		t.Fatalf("precondition: Time Stop was not cast (zone %s)", e.G.Obj(f.timeStop).Zone)
	}
	if top := e.G.Stack[len(e.G.Stack)-1]; top != f.timeStop {
		t.Fatalf("precondition: Time Stop is not on top of the stack (%v)", e.G.Stack)
	}
	if e.G.Turn != 1 {
		t.Fatalf("precondition: the cast happened on turn %d, want turn 1", e.G.Turn)
	}
	// Resolve everything (answering the cleanup discard along the way) but
	// stop the instant the next turn begins, so the assertions below describe
	// the END of the current turn and are not confused with the next turn's
	// own end step.
	for i := 0; i < 400 && !e.G.Over && e.G.Turn == 1; i++ {
		if answerIfDiscard(t, e) {
			continue
		}
		d := e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			t.Fatalf("non-priority decision %+v while ending the turn", d)
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit pass: %v", err)
		}
	}
	if e.G.Turn == 1 {
		t.Fatalf("the turn never ended (still turn %d, step %s)", e.G.Turn, e.G.Step)
	}

	// CR 723.1a: Time Stop and every other object on the stack are exiled,
	// including Time Stop itself (never the graveyard).
	if z := e.G.Obj(f.timeStop).Zone; z != state.ZExile {
		t.Errorf("Time Stop ended in %s, want exile", z)
	}
	if z := e.G.Obj(f.spell).Zone; z != state.ZExile {
		t.Errorf("the creature spell beneath Time Stop ended in %s, want exile", z)
	}
	// CR 723.1c: nothing is attacking (or blocking).
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if o := e.G.Obj(id); o != nil && o.IsAttacking {
			t.Errorf("object %d is still attacking after the turn ended", id)
		}
	}
	// CR 514.2: the marked damage is gone.
	if d := e.G.Obj(f.attacker).Damage; d != 0 {
		t.Errorf("attacker still has %d marked damage, want 0", d)
	}
	// CR 723.1d: the end step never happened, so the Ghoul's end-step trigger
	// never fired (it would have placed a +1/+1 counter after the death above).
	if n := ghoulCounter(e, f.ghoul); n != 0 {
		t.Errorf("Khabál Ghoul has %d +1/+1 counters, want 0 (its end-step trigger fired)", n)
	}
	// CR 514.1: cleanup discarded seat 0 down to hand size.
	if got := len(e.G.Zone(state.ZHand, 0)); got > 7 {
		t.Errorf("seat 0 hand = %d after cleanup, want <= 7", got)
	}
	if n := countEvents(e, func(ev events.Event) bool {
		return ev.Kind == events.MoveZone && ev.From == state.ZHand &&
			ev.To == state.ZGraveyard && ev.Player == 0
	}); n == 0 {
		t.Error("no hand-to-graveyard discard in the log: cleanup did not discard to hand size")
	}
	// CR 723.1d: the turn skipped straight to cleanup -- the end step's
	// StepChange never appears between the EndTurn and the next turn's untap.
	endTurnAt := -1
	for i, ev := range e.L.Events {
		if ev.Kind == events.EndTurn {
			endTurnAt = i
		}
	}
	if endTurnAt < 0 {
		t.Fatal("no EndTurn event in the log: the primitive did not run")
	}
	nextTurnAt := len(e.L.Events)
	for i := endTurnAt; i < len(e.L.Events); i++ {
		if ev := e.L.Events[i]; ev.Kind == events.StepChange && ev.Step == state.StepUntap {
			nextTurnAt = i
			break
		}
	}
	for _, ev := range e.L.Events[endTurnAt:nextTurnAt] {
		if ev.Kind == events.StepChange && ev.Step == state.StepEnd {
			t.Error("the end step was entered after the turn ended (CR 723.1d)")
		}
	}

	// Replay the whole game from its log (the deterministic event fold) and
	// assert the reconstructed state and the recomputed chain head match.
	replayCheck(t, e, f.cfg)
	lg := events.NewLog(f.cfg.Seed)
	g := state.NewGameLife(f.cfg.Names, 20)
	g.Tokens = f.cfg.Tokens
	for i := range f.cfg.Decks {
		p := state.PlayerID(i)
		ids := make([]state.ObjID, 0, len(f.cfg.Decks[i]))
		for _, c := range f.cfg.Decks[i] {
			ids = append(ids, g.AddObject(c, p).ID)
		}
		g.SetZone(state.ZLibrary, p, ids)
	}
	for _, ev := range e.L.Events {
		events.Emit(g, lg, ev)
	}
	if lg.Head() != e.L.Head() {
		t.Fatalf("chain head %s, log replay %s", e.L.Head(), lg.Head())
	}
}
