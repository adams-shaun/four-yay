package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// This file pins the registered `DB$ VillainousChoice` primitive end to end on
// a real compiled corpus carrier whose chosen body poses its own nested ask:
// The Dalek Emperor's begin-combat trigger, "each opponent faces a villainous
// choice — that player sacrifices a creature they control, or you create a
// 3/3 black Dalek". Defined$ Opponent names every opponent, so a three-seat
// game exercises the multi-victim cursor too.
//
// Two things the modes-contract shortcut got wrong and this test defends:
//   - the chooser is the VICTIM (the defined player), never the trigger's
//     controller; and
//   - the chosen sacrifice body reads Defined$ Remembered as the victim, so
//     the victim's own creature is chosen (not the trigger controller's, and
//     not a dropped no-op) even though the body suspends on its own nested
//     pick before the next victim is asked.
//
// The whole trigger is driven through the real engine: resolveTop places the
// trigger, handleModes applies the villainous mode answer, and
// resumeResolution runs the chosen body and its continuation. A test that
// only exercised effects.fakeHost (whose Ask returns false) could not detect
// either failure.

// villainousChoiceBoard builds a three-seat game with The Dalek Emperor on
// seat 0's battlefield and TWO creatures on each opponent's battlefield.
// The second creature is what makes the chosen sacrifice body pose its own
// nested pick (a single eligible creature is sacrificed without a decision,
// the effDiscard strict-supersets rule). Every placement is a logged
// MoveZone, so the whole board replays.
func villainousChoiceBoard(t *testing.T) (*Engine, Config) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	emperor := mustCorpusCard(t, reg, "The Dalek Emperor")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	cfg := Config{Seed: 91, Names: []string{"a", "b", "c"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{
			append([]*cards.Card{emperor}, mountainDeck(t, 39)...),
			append([]*cards.Card{bear, bear}, mountainDeck(t, 38)...),
			append([]*cards.Card{bear, bear}, mountainDeck(t, 38)...),
		}}
	e := New(cfg)
	// Start the pregame and settle into the first priority window (the same
	// e.Advance() newFixtureDeck makes) before placing the board, so the
	// drive below begins from a real priority decision rather than a nil one.
	e.Advance()
	emperorObj := placeInDeck(t, e, 0, emperor, state.ZBattlefield)
	placeInDeck(t, e, 1, bear, state.ZBattlefield)
	placeInDeck(t, e, 1, bear, state.ZBattlefield)
	placeInDeck(t, e, 2, bear, state.ZBattlefield)
	placeInDeck(t, e, 2, bear, state.ZBattlefield)
	// Preconditions the assertions below depend on: the trigger's source and
	// every victim's sacrifice candidates are real battlefield permanents.
	if z := e.G.Obj(emperorObj).Zone; z != state.ZBattlefield {
		t.Fatalf("emperor zone = %s, want Battlefield", z)
	}
	for _, p := range []state.PlayerID{1, 2} {
		n := 0
		for i := range e.G.Objs {
			if o := &e.G.Objs[i]; o.Zone == state.ZBattlefield && o.Owner == p {
				n++
			}
		}
		if n != 2 {
			t.Fatalf("seat %d battlefield permanents = %d, want the 2 sacrifice candidates", p, n)
		}
	}
	return e, cfg
}

// placeInDeck moves the first object of seat p whose card is c and that is
// not already on the battlefield to zone, and returns its id. The card must
// be in that seat's decklist (the corpus card is seeded there by
// villainousChoiceBoard), so the move is a real library -> zone MoveZone the
// replay rebuilds. The not-yet-placed filter lets a decklist hold two copies
// of one card and place both.
func placeInDeck(t *testing.T, e *Engine, p state.PlayerID, c *cards.Card, zone state.Zone) state.ObjID {
	t.Helper()
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner == p && o.Card == c && o.Zone != state.ZBattlefield {
			e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: zone})
			return o.ID
		}
	}
	t.Fatalf("seat %d has no unplaced deck object for %q", p, c.Faces[0].Name)
	return 0
}

// drainVillainousChoice drives the pending stack to empty, answering every
// decision it meets: priority passes, the villainous KModes pick takes its
// FIRST option (the sacrifice branch), and the victim's sacrifice KChoose
// takes its first candidate. It returns the decisions it answered in order
// and the two chosen victims' sacrifice targets, so the caller can assert on
// the chooser of each ask.
type answeredDecision struct {
	dec *decision.Decision
}

func drainVillainousChoice(t *testing.T, e *Engine, limit int) []answeredDecision {
	t.Helper()
	var answered []answeredDecision
	for i := 0; i < limit && !e.G.Over && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			e.Advance()
			d = e.Pending()
		}
		if d == nil {
			t.Fatalf("no decision while draining the villainous trigger (stack depth %d)", len(e.G.Stack))
		}
		answered = append(answered, answeredDecision{dec: &decision.Decision{
			Kind: d.Kind, Player: d.Player, Min: d.Min, Max: d.Max, ResumeKind: d.ResumeKind,
			Options: append([]decision.Option(nil), d.Options...)}})
		var choices []int
		switch d.Kind {
		case decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			choices = []int{idx}
		case decision.KModes:
			if d.ResumeKind != "villainous" {
				t.Fatalf("unexpected KModes resume kind %q on the villainous trigger", d.ResumeKind)
			}
			choices = []int{d.Options[0].Index}
		case decision.KChoose:
			choices = []int{d.Options[0].Index}
		default:
			t.Fatalf("unexpected decision %+v on the villainous trigger", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
			t.Fatalf("submit %v: %v", choices, err)
		}
	}
	return answered
}

// TestDalekEmperorVillainousChoiceAsksEachOpponentAndRunsTheirSacrifice pins
// the primitive end to end: each of the two opponents is asked (a KModes
// whose chooser is that opponent, never seat 0), the chosen sacrifice body
// asks THAT opponent to pick one of THEIR creatures, and both picks land in
// the graveyard while the controller's own board is untouched.
func TestDalekEmperorVillainousChoiceAsksEachOpponentAndRunsTheirSacrifice(t *testing.T) {
	t.Parallel()
	e, cfg := villainousChoiceBoard(t)
	// Fire the begin-combat trigger for seat 0. Drive to seat 0's main phase
	// first (idempotent) so the step transition into combat is a real one,
	// then enter combat through the engine's own step setter.
	driveToStep(t, e, e.G.Turn, 0, state.StepMain1)
	e.setStep(state.StepBeginCombat)
	if !e.putTriggersOnStack() {
		t.Fatal("The Dalek Emperor's begin-combat trigger did not fire")
	}
	if len(e.G.Stack) == 0 {
		t.Fatal("the trigger never went on the stack")
	}

	answered := drainVillainousChoice(t, e, 60)

	// The mode ask order is victim 1 then victim 2, each a real KModes on the
	// villainous resume; the sacrifice ask between them belongs to the SAME
	// victim. If the modes-contract shortcut were still used, the chooser
	// would be seat 0 (the trigger's controller) and there would be no
	// sacrifice KChoose at all.
	var modeChoosers []state.PlayerID
	var sacChoosers []state.PlayerID
	var sacrifices []state.ObjID
	for i := range answered {
		d := answered[i].dec
		switch d.Kind {
		case decision.KModes:
			if d.ResumeKind != "villainous" {
				t.Fatalf("KModes resume kind = %q, want villainous", d.ResumeKind)
			}
			modeChoosers = append(modeChoosers, d.Player)
		case decision.KChoose:
			sacChoosers = append(sacChoosers, d.Player)
			if len(d.Options) != 2 {
				t.Fatalf("victim sacrifice ask offered %d options, want exactly its two creatures", len(d.Options))
			}
			for _, o := range d.Options {
				if obj := e.G.Obj(o.Obj); obj == nil || obj.Owner != d.Player {
					t.Fatalf("sacrifice candidate %d is not owned by the chooser seat %d", o.Obj, d.Player)
				}
			}
			sacrifices = append(sacrifices, d.Options[0].Obj)
		}
	}
	if len(modeChoosers) != 2 || modeChoosers[0] != 1 || modeChoosers[1] != 2 {
		t.Fatalf("villainous mode choosers = %v, want [1 2] (each opponent, never the controller)", modeChoosers)
	}
	if len(sacChoosers) != 2 || sacChoosers[0] != 1 || sacChoosers[1] != 2 {
		t.Fatalf("sacrifice choosers = %v, want [1 2] (the nested body read Defined$ Remembered as the victim)", sacChoosers)
	}
	if len(sacrifices) != 2 || sacrifices[0] == 0 || sacrifices[1] == 0 {
		t.Fatalf("sacrificed objects = %v, want one per victim", sacrifices)
	}

	// Both victims' creatures are gone and the trigger controller's own board
	// is untouched: the body acted on the victim, not the controller.
	for i, id := range sacrifices {
		if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
			t.Fatalf("victim %d's chosen creature zone = %s, want Graveyard", i+1, z)
		}
		if o := e.G.Obj(id); o.Owner != state.PlayerID(i+1) {
			t.Fatalf("sacrificed object %d is owned by seat %d, want victim %d", id, o.Owner, i+1)
		}
	}
	replayCheck(t, e, cfg)
}

// driveVillainousToMode answers priority passes and the villainous choices
// preceding the nth (1-based) KModes ask, then RETURNS with that ask pending
// rather than answering it. modeCount is the number of KModes decisions seen
// so far; the caller's clone test uses it to stop at the second victim.
func driveVillainousToMode(t *testing.T, e *Engine, nth int, limit int) *decision.Decision {
	t.Helper()
	seen := 0
	for i := 0; i < limit && !e.G.Over && len(e.G.Stack) > 0; i++ {
		d := e.Pending()
		if d == nil {
			e.Advance()
			d = e.Pending()
		}
		if d == nil {
			t.Fatalf("no decision while driving to mode %d", nth)
		}
		if d.Kind == decision.KModes {
			seen++
			if seen == nth {
				return d
			}
		}
		var choices []int
		switch d.Kind {
		case decision.KPriority:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("priority decision with no pass option: %+v", d)
			}
			choices = []int{idx}
		case decision.KChoose:
			choices = []int{d.Options[0].Index}
		case decision.KModes:
			// An earlier victim's mode (we only return on the nth): take the
			// sacrifice branch, the same first option the drain uses.
			choices = []int{d.Options[0].Index}
		default:
			t.Fatalf("unexpected decision %+v while driving to mode %d", d, nth)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
			t.Fatalf("submit %v: %v", choices, err)
		}
	}
	t.Fatalf("never reached villainous mode %d", nth)
	return nil
}

// TestVillainousChoiceCloneKeepsTheVictimCursor pins the clone contract: a
// clone taken while the SECOND victim's mode answer is pending carries the
// victim cursor (Decision.ResumeVillainousIndex), so answering on the clone
// processes that victim exactly once and completes — it must not reset the
// cursor to zero and ask that victim a second time. This is the search/host
// snapshot path: a clone made at a pending decision must resume identically.
func TestVillainousChoiceCloneKeepsTheVictimCursor(t *testing.T) {
	t.Parallel()
	e, _ := villainousChoiceBoard(t)
	driveToStep(t, e, e.G.Turn, 0, state.StepMain1)
	e.setStep(state.StepBeginCombat)
	if !e.putTriggersOnStack() {
		t.Fatal("The Dalek Emperor's begin-combat trigger did not fire")
	}
	second := driveVillainousToMode(t, e, 2, 40)
	if second.Player != 2 {
		t.Fatalf("second villainous mode chooser = seat %d, want the second victim seat 2", second.Player)
	}

	clone := e.Clone()
	if clone.Pending() == nil || clone.Pending().Kind != decision.KModes {
		t.Fatalf("clone pending = %+v, want the second victim's KModes ask", clone.Pending())
	}

	// Answer the second victim's mode and sacrifice on the clone, then drain.
	// The cursor must advance past victim 2: with the clone bug it resets to
	// zero and the primitive re-asks victim 2 (a second KModes on seat 2).
	answered := drainVillainousChoice(t, clone, 40)
	modesAfterClone, sacsAfterClone := 0, 0
	var clonedSac state.ObjID
	for _, a := range answered {
		switch a.dec.Kind {
		case decision.KModes:
			if a.dec.Player != 2 {
				t.Fatalf("after the clone, a mode was asked of seat %d, want only the pending victim 2", a.dec.Player)
			}
			modesAfterClone++
		case decision.KChoose:
			if a.dec.Player != 2 {
				t.Fatalf("after the clone, a sacrifice was asked of seat %d, want only victim 2", a.dec.Player)
			}
			sacsAfterClone++
			clonedSac = a.dec.Options[0].Obj
		}
	}
	if modesAfterClone != 1 {
		t.Fatalf("the clone asked %d villainous modes, want exactly 1 (the pending second victim, never re-asking from zero)", modesAfterClone)
	}
	if sacsAfterClone != 1 {
		t.Fatalf("the clone asked %d sacrifices, want exactly 1 (the second victim's own pick)", sacsAfterClone)
	}
	if clonedSac == 0 {
		t.Fatal("the clone's second-victim sacrifice pick was never posed")
	}
	if o := clone.G.Obj(clonedSac); o == nil || o.Owner != 2 {
		t.Fatalf("the clone's sacrificed creature %d is not the second victim's", clonedSac)
	}
	if z := clone.G.Obj(clonedSac).Zone; z != state.ZGraveyard {
		t.Fatalf("the clone's answered victim creature zone = %s, want Graveyard", z)
	}
}

// TestDamoclesBaseVillainousChoiceAsksTheDamagedPlayer pins the brief's named
// carrier end to end: Damocles Base's combat-damage trigger resolves the real
// compiled `TrigVillainousChoice` SA, and its `Defined$ TriggeredTarget` names
// the DAMAGED player — the modes ask goes to that victim, the chosen DBSac
// body reads Defined$ Remembered as that victim, and the victim's own
// creature is sacrificed.
func TestDamoclesBaseVillainousChoiceAsksTheDamagedPlayer(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	damocles := mustCorpusCard(t, reg, "Damocles Base, Sword of Kang")
	bear := mustCorpusCard(t, reg, "Grizzly Bears")
	cfg := Config{Seed: 92, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{
			append([]*cards.Card{damocles}, mountainDeck(t, 39)...),
			append([]*cards.Card{bear, bear}, mountainDeck(t, 38)...),
		}}
	e := New(cfg)
	e.Advance()
	src := placeInDeck(t, e, 0, damocles, state.ZBattlefield)
	placeInDeck(t, e, 1, bear, state.ZBattlefield)
	placeInDeck(t, e, 1, bear, state.ZBattlefield)
	// Precondition: the trigger source is a real battlefield permanent and
	// the victim has the two creatures the sacrifice pick needs.
	if e.G.Obj(src).Zone != state.ZBattlefield {
		t.Fatal("Damocles Base is not on the battlefield")
	}
	if n := len(e.G.Zone(state.ZBattlefield, 1)); n != 2 {
		t.Fatalf("victim battlefield permanents = %d, want the 2 sacrifice candidates", n)
	}

	// The combat-damage state dealCombatDamage emits every assignment under
	// (rules/turn.go): e.damaging is the source, e.combatDamaging the flag the
	// CombatDamage$ True gate reads.
	e.damaging = src
	e.combatDamaging = true
	e.emit(events.Event{Kind: events.Damage, Player: 1, Amount: 5})
	e.combatDamaging = false
	e.damaging = 0
	e.priorityRound()
	if len(e.G.Stack) == 0 {
		t.Fatal("Damocles Base's combat-damage trigger did not go on the stack")
	}

	answered := drainVillainousChoice(t, e, 40)
	var modeChooser state.PlayerID = 255
	var sacChooser state.PlayerID = 255
	var sacObj state.ObjID
	for _, a := range answered {
		switch a.dec.Kind {
		case decision.KModes:
			modeChooser = a.dec.Player
		case decision.KChoose:
			sacChooser = a.dec.Player
			if len(a.dec.Options) != 2 {
				t.Fatalf("sacrifice ask offered %d options, want the victim's 2 creatures", len(a.dec.Options))
			}
			sacObj = a.dec.Options[0].Obj
		}
	}
	if modeChooser != 1 {
		t.Fatalf("villainous mode chooser = seat %d, want the damaged player seat 1 (Defined$ TriggeredTarget)", modeChooser)
	}
	if sacChooser != 1 {
		t.Fatalf("sacrifice chooser = seat %d, want the victim seat 1", sacChooser)
	}
	if sacObj == 0 {
		t.Fatal("the victim's sacrifice pick was never posed")
	}
	if o := e.G.Obj(sacObj); o == nil || o.Owner != 1 {
		t.Fatalf("sacrificed object %d is not the victim's", sacObj)
	}
	if z := e.G.Obj(sacObj).Zone; z != state.ZGraveyard {
		t.Fatalf("the victim's chosen creature zone = %s, want Graveyard", z)
	}
	// The other creature survives: exactly one sacrifice was applied.
	survivors := 0
	for i := range e.G.Objs {
		if o := &e.G.Objs[i]; o.Owner == 1 && o.Zone == state.ZBattlefield {
			survivors++
		}
	}
	if survivors != 1 {
		t.Fatalf("victim battlefield survivors = %d, want 1", survivors)
	}
	replayCheck(t, e, cfg)
}
