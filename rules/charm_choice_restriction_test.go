package rules

// ChoiceRestriction$ ThisTurn (task charm-choice-restriction): a Charm whose
// oracle reads "choose one that hasn't been chosen this turn" must not offer a
// mode an earlier instance of the SAME Charm on the SAME source already chose
// this turn, and must offer it again next turn. Parapet Thrasher's real
// corpus script is the carrier: a DamageDoneOnce trigger fires once per
// opponent damaged, so two Dragons striking two opponents in one combat
// produce two trigger instances of one Parapet Thrasher -- the exact shape the
// brief names. Everything here runs the shipped compiled scripts; only the
// board is authored.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// placeFromDeck emits the logged MoveZone that carries one named corpus card
// from wherever genesis dealt it (library or hand) onto seat p's battlefield.
// Placement is EVENTFUL, unlike onBoardCard's eventless splice, so the
// log-only replay rebuilds the same board -- the prerequisite for the
// replayCheck at the end of the headline test.
func placeFromDeck(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	var found state.ObjID
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner != p || o.Face() == nil || o.Face().Name != name {
			continue
		}
		if o.Zone != state.ZLibrary && o.Zone != state.ZHand {
			continue
		}
		if found == 0 {
			found = o.ID
		}
	}
	if found == 0 {
		t.Fatalf("seat %d holds no %q in library or hand", p, name)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: found,
		From: e.G.Obj(found).Zone, To: state.ZBattlefield})
	return found
}

// parapetThrasherGame builds a three-seat table (so Parapet Thrasher can have
// two opponents) with the real corpus Parapet Thrasher and two real corpus
// Shivan Dragons moved onto seat 0's battlefield, seat 0 active in its
// declare-attackers step. Every placement and the turn/step are logged, so
// the whole game replays. It returns the engine, the Config its log replays
// against, and the three permanent ids.
func parapetThrasherGame(t *testing.T, seed uint64) (*Engine, Config, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	parapet := mustCorpusCard(t, reg, "Parapet Thrasher")
	dragon := mustCorpusCard(t, reg, "Shivan Dragon")
	if parapet.Faces[0].Name != "Parapet Thrasher" {
		t.Fatalf("corpus lookup returned %q", parapet.Faces[0].Name)
	}
	if dragon.Faces[0].Name != "Shivan Dragon" {
		t.Fatalf("corpus lookup returned %q", dragon.Faces[0].Name)
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"a", "b", "c"},
		Tokens: reg.Tokens,
		Decks: [][]*cards.Card{
			append([]*cards.Card{parapet, dragon, dragon}, mountainDeck(t, 37)...),
			mountainDeck(t, 40), mountainDeck(t, 40),
		}})
	e := New(cfg)
	e.Advance()

	// Move the three carriers onto the battlefield through logged events.
	pt := placeFromDeck(t, e, 0, "Parapet Thrasher")
	d1 := placeFromDeck(t, e, 0, "Shivan Dragon")
	d2 := placeFromDeck(t, e, 0, "Shivan Dragon")
	// A logged turn boundary clears seat 0's summoning sickness (CR 302.6a's
	// "since the beginning of your most recent turn") and parks the clock at
	// seat 0's declare-attackers step -- the replay-consistent arming the Jitte
	// combat fixture uses.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})

	// Precondition the assertions below depend on: the three permanents are on
	// the battlefield and not summoning-sick, so the two Dragons can attack
	// and the trigger's ValidSource$ Dragon.YouCtrl can match.
	for _, id := range []state.ObjID{pt, d1, d2} {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield || o.SummonSick {
			t.Fatalf("fixture precondition: permanent %d zone=%v sick=%v", id, o.Zone, o.SummonSick)
		}
	}
	return e, cfg, pt, d1, d2
}

// submitAttackersAt declares ids each attacking the named defender (the
// KAttackers option carries Obj = attacker, Player = defender), then crosses
// the declare-attackers priority window.
func submitAttackersAt(t *testing.T, e *Engine, pairs [][2]state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected an attackers decision, got %+v", d)
	}
	var choices []int
	for _, want := range pairs {
		idx := -1
		for _, o := range d.Options {
			if o.Obj == want[0] && o.Player == state.PlayerID(want[1]) {
				idx = o.Index
				break
			}
		}
		if idx < 0 {
			t.Fatalf("no attack option for attacker %d at defender %d: %+v",
				want[0], want[1], d.Options)
		}
		choices = append(choices, idx)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
		t.Fatalf("submit attackers: %v", err)
	}
	drainCombatPriority(t, e)
}

// driveToModes answers the mechanical combat decisions (priority passes,
// trigger ordering, empty block declarations) until the engine poses the
// modal placement ask the test is about, or the game stalls. It never answers
// a KModes decision -- that is the caller's.
func driveToModes(t *testing.T, e *Engine, limit int) *decision.Decision {
	t.Helper()
	for i := 0; i < limit; i++ {
		d := e.Pending()
		if d == nil {
			return nil
		}
		switch d.Kind {
		case decision.KModes:
			return d
		case decision.KTriggerOrder:
			idx := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				idx = append(idx, o.Index)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: idx}); err != nil {
				t.Fatalf("submit trigger order: %v", err)
			}
		case decision.KBlockers:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player}); err != nil {
				t.Fatalf("submit empty blocks: %v", err)
			}
		case decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
					break
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("pass priority: %v", err)
			}
		default:
			t.Fatalf("unexpected decision while driving to the modes ask: %+v", d)
		}
	}
	t.Fatalf("no modes ask within %d decisions", limit)
	return nil
}

// modeNameIndex returns the option index whose server-side ResumeModes name
// equals name (-1 absent). The wire only carries dense indices, so this is
// the binding a seat uses to express "I choose DBSwoop".
func modeNameIndex(d *decision.Decision, name string) int {
	for i, n := range d.ResumeModes {
		if n == name {
			return i
		}
	}
	return -1
}

// TestParapetThrasherChoiceRestrictionThisTurn is the brief's pin: two trigger
// instances in one combat/one turn, and the same mode must not be offered
// twice; a fresh trigger on the next turn offers every mode again. The
// assertions read d.ResumeModes -- the eligible SVar vocabulary the engine
// builds the options from -- so an option-label drift cannot fake the result.
func TestParapetThrasherChoiceRestrictionThisTurn(t *testing.T) {
	t.Parallel()
	e, cfg, pt, d1, d2 := parapetThrasherGame(t, 4212)

	// Two Dragons, each attacking a different opponent: exactly the two
	// opponents the DamageDoneOnce trigger names.
	e.askAttackers()
	submitAttackersAt(t, e, [][2]state.ObjID{{d1, 1}, {d2, 2}})

	// First trigger instance: every mode is still eligible.
	first := driveToModes(t, e, 60)
	if first == nil {
		t.Fatal("first trigger never posed its modal placement ask")
	}
	firstNames := append([]string(nil), first.ResumeModes...)
	if len(firstNames) != 3 {
		t.Fatalf("first ask eligible modes = %v, want all three", firstNames)
	}
	if i := modeNameIndex(first, "DBStrafe"); i < 0 {
		t.Fatalf("first ask does not offer DBStrafe: %v", firstNames)
	} else if err := e.Submit(decision.Intent{Seq: first.Seq, Player: first.Player, Choices: []int{i}}); err != nil {
		t.Fatalf("submit DBStrafe: %v", err)
	}

	// The pick must be recorded on the SOURCE permanent (the per-turn log a
	// later instance reads), or the second ask below is vacuous.
	if got := e.G.Obj(pt).ModeChoices; len(got) != 1 || got[0].Mode != "DBStrafe" ||
		got[0].Scope != state.ModeScopeThisTurn {
		t.Fatalf("Parapet Thrasher ModeChoices after first pick = %+v, want one ThisTurn DBStrafe", got)
	}

	// Second trigger instance of the SAME Charm on the SAME source: the
	// already-chosen mode must be withheld, so the same mode cannot be picked
	// twice this turn.
	second := driveToModes(t, e, 60)
	if second == nil {
		t.Fatal("second trigger never posed its modal placement ask")
	}
	secondNames := append([]string(nil), second.ResumeModes...)
	if modeNameIndex(second, "DBStrafe") >= 0 {
		t.Fatalf("second ask still offers the already-chosen DBStrafe: %v", secondNames)
	}
	if len(secondNames) != 2 {
		t.Fatalf("second ask eligible modes = %v, want the two unchosen modes", secondNames)
	}
	if i := modeNameIndex(second, "DBSwoop"); i < 0 {
		t.Fatalf("second ask does not offer the unchosen DBSwoop: %v", secondNames)
	} else if err := e.Submit(decision.Intent{Seq: second.Seq, Player: second.Player, Choices: []int{i}}); err != nil {
		t.Fatalf("submit DBSwoop: %v", err)
	}
	passUntilStackEmpty(t, e, 60)

	// The whole game still replays byte-for-byte with the ChoiceRestriction$
	// picks in the log.
	replayCheck(t, e, cfg)

	// This turn only: drive to seat 0's next turn, attack the same two
	// opponents again, and the same Charm must offer every mode.
	driveToStep(t, e, e.G.Turn+3, 0, state.StepDeclareAttackers)
	if got := e.G.Obj(pt).ModeChoices; len(got) != 0 {
		t.Fatalf("ModeChoices after the turn boundary = %+v, want pruned", got)
	}
	e.askAttackers()
	submitAttackersAt(t, e, [][2]state.ObjID{{d1, 1}, {d2, 2}})
	next := driveToModes(t, e, 60)
	if next == nil {
		t.Fatal("next turn's trigger never posed its modal placement ask")
	}
	for _, want := range []string{"DBSmash", "DBStrafe", "DBSwoop"} {
		if modeNameIndex(next, want) < 0 {
			t.Fatalf("next turn's ask omits %s: %v -- the ThisTurn restriction did not reset", want, next.ResumeModes)
		}
	}
	if len(next.ResumeModes) != 3 {
		t.Fatalf("next turn's ask eligible modes = %v, want all three", next.ResumeModes)
	}
	if i := modeNameIndex(next, "DBStrafe"); i < 0 {
		t.Fatalf("next turn's ask does not offer DBStrafe: %v", next.ResumeModes)
	} else if err := e.Submit(decision.Intent{Seq: next.Seq, Player: next.Player, Choices: []int{i}}); err != nil {
		t.Fatalf("submit DBStrafe next turn: %v", err)
	}
}

// TestParapetThrasherChoiceRestrictionClearsOnBlink pins the CR 400.7 stake:
// a ThisTurn pick is battlefield-stint state, so a permanent that leaves and
// returns in the SAME turn is a new object and must offer every mode again.
// The round-trip is eventful (logged MoveZone events), and the eligibility
// check calls the real compiled Parapet Thrasher Charm SA through
// effects.CharmEligibleModes -- the exact filter the three ask sites share --
// so this cannot pass by inspecting a marker alone. A control assertion proves
// the filter binds BEFORE the blink, making the clear the load-bearing act.
func TestParapetThrasherChoiceRestrictionClearsOnBlink(t *testing.T) {
	t.Parallel()
	e, cfg, pt, _, _ := parapetThrasherGame(t, 99)

	// Resolve the real compiled Charm SA (the trigger's Execute$ SVar) so the
	// eligibility call reads the corpus parameter, not a fixture.
	parapet := e.G.Obj(pt).Face()
	charm := cards.ResolveSVar(parapet.SVars, "TrigCharm")
	if charm == nil {
		t.Fatal("Parapet Thrasher's TrigCharm SVar did not compile")
	}
	if got := charm.Params["ChoiceRestriction"]; got != state.ModeScopeThisTurn {
		t.Fatalf("compiled Charm ChoiceRestriction$ = %q, want ThisTurn", got)
	}
	modes := []string{"DBSmash", "DBStrafe", "DBSwoop"}

	// Record a real ThisTurn pick through the same event the answer handler
	// emits, then confirm the shared filter binds on it.
	e.emit(events.Event{Kind: events.Choose, Obj: pt,
		Counter: state.ModeChoiceCounterPrefix + state.ModeScopeThisTurn, Text: "DBStrafe"})
	if got := e.G.Obj(pt).ModeChoices; len(got) != 1 || got[0].Mode != "DBStrafe" {
		t.Fatalf("fixture precondition: recorded pick = %+v, want one DBStrafe", got)
	}
	before := effects.CharmEligibleModes(e, pt, charm, modes)
	for _, n := range before {
		if n == "DBStrafe" {
			t.Fatalf("control failed: CharmEligibleModes offered the already-picked DBStrafe: %v", before)
		}
	}
	if len(before) != 2 {
		t.Fatalf("control: eligible modes = %v, want the two unchosen", before)
	}

	// One logged battlefield -> exile, then exile -> battlefield. The same
	// object ID is reused (CR 400.7), so without the Move clear the stale
	// ThisTurn pick would survive and withhold DBStrafe forever this turn.
	e.emit(events.Event{Kind: events.MoveZone, Obj: pt,
		From: state.ZBattlefield, To: state.ZExile})
	if got := e.G.Obj(pt).ModeChoices; len(got) != 0 {
		t.Fatalf("ModeChoices after leaving the battlefield = %+v, want cleared", got)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: pt,
		From: state.ZExile, To: state.ZBattlefield})
	if e.G.Obj(pt).Zone != state.ZBattlefield {
		t.Fatalf("precondition: permanent is in zone %v, want battlefield", e.G.Obj(pt).Zone)
	}

	after := effects.CharmEligibleModes(e, pt, charm, modes)
	if len(after) != 3 {
		t.Fatalf("eligible modes after blink = %v, want all three", after)
	}
	if got := e.G.Obj(pt).ModeChoices; len(got) != 0 {
		t.Fatalf("ModeChoices after re-entering = %+v, want empty", got)
	}

	// Everything above is logged, so the whole fixture still replays.
	replayCheck(t, e, cfg)
}

// TestParapetThrasherAllModesExhaustedDoesNotWedge pins the exhaustion guard.
// When every mode is already recorded, askTriggerModes has no legal set to
// pose, and the drain must keep placing triggers rather than wait forever for
// an answer nobody was asked for (a stale drainAwaitsModes): the two triggers
// resolve doing nothing, combat finishes, and no KModes decision is ever
// offered. Pre-fix this path was unreachable by ChoiceRestriction (the filter
// did not exist), which is why the guard ships with the feature.
func TestParapetThrasherAllModesExhaustedDoesNotWedge(t *testing.T) {
	t.Parallel()
	e, _, pt, d1, d2 := parapetThrasherGame(t, 77)
	for _, m := range []string{"DBSmash", "DBStrafe", "DBSwoop"} {
		e.emit(events.Event{Kind: events.Choose, Obj: pt,
			Counter: state.ModeChoiceCounterPrefix + state.ModeScopeThisTurn, Text: m})
	}
	if got := e.G.Obj(pt).ModeChoices; len(got) != 3 {
		t.Fatalf("fixture precondition: recorded picks = %d, want 3", len(got))
	}

	e.askAttackers()
	submitAttackersAt(t, e, [][2]state.ObjID{{d1, 1}, {d2, 2}})

	// Drive the combat to a close, answering the mechanical decisions. A
	// KModes ask would mean the exhausted restriction failed to withhold the
	// already-chosen modes; a nil pending decision at the end with the game
	// still running means the drain did not wedge.
	modesPosed := 0
	done := false
	for i := 0; i < 120; i++ {
		d := e.Pending()
		if d == nil {
			break
		}
		switch d.Kind {
		case decision.KModes:
			modesPosed++
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
				t.Fatalf("submit exhausted-mode pick: %v", err)
			}
		case decision.KTriggerOrder:
			idx := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				idx = append(idx, o.Index)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: idx}); err != nil {
				t.Fatalf("submit trigger order: %v", err)
			}
		case decision.KBlockers:
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player}); err != nil {
				t.Fatalf("submit empty blocks: %v", err)
			}
		case decision.KChoose:
			// The CR 514.1 cleanup discard is a real decision on the way to
			// the next turn; answer it naively (front card) so the drill can
			// reach a turn boundary and finish.
			if len(d.Options) == 0 {
				t.Fatalf("KChoose with no options: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
				t.Fatalf("submit cleanup discard: %v", err)
			}
		case decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
					break
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
				t.Fatalf("pass priority: %v", err)
			}
		case decision.KAttackers:
			// A fresh declare-attackers decision after the exhausted combat
			// means the drill survived: the drain placed and resolved both
			// trigger instances and the game advanced a full turn.
			done = true
		default:
			t.Fatalf("unexpected decision while the modes are exhausted: %+v", d)
		}
		if done {
			break
		}
	}
	if modesPosed != 0 {
		t.Fatalf("exhausted ChoiceRestriction still posed %d KModes ask(s)", modesPosed)
	}
	if !done {
		t.Fatal("drill never reached a later turn -- progress stalled after the exhausted combat")
	}
	// The exhausted ask posed no decision, so the drain must NOT claim it is
	// waiting for a modes answer: a stale true would misroute the next
	// unrelated KModes ask (a mid-resolution Charm) through the CR 603.3c
	// placement branch. This is the invariant askTriggerModes' no-ask return
	// has to uphold, and it is what e.Pending() guards.
	if e.drainAwaitsModes {
		t.Fatal("drainAwaitsModes is stale after an exhausted ChoiceRestriction asked nothing")
	}
	if e.G.Over {
		t.Fatal("game ended during an exhaustion drill -- the fixture is wrong")
	}
}
