package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// lyndeBoard3 builds a three-seat board (so Lynde's controller has TWO
// opponents and the PlayerChoices$ Opponent attach ask is genuinely posed
// rather than auto-taken as a single legal answer) with the named cards on
// seat 0's battlefield.
func lyndeBoard3(t *testing.T, reg *cards.Registry, names ...string) (*Engine, map[string]state.ObjID) {
	t.Helper()
	deck := make([]*cards.Card, 0, len(names))
	for _, n := range names {
		deck = append(deck, mustCorpusCard(t, reg, n))
	}
	cfg := Config{Seed: 47, Names: []string{"a", "b", "c"},
		Decks: [][]*cards.Card{
			append(deck, mountainDeck(t, 40-len(deck))...),
			mountainDeck(t, 40),
			mountainDeck(t, 40),
		}}
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	ids := make(map[string]state.ObjID, len(names))
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner != 0 || !want[o.Face().Name] {
			continue
		}
		if o.Zone != state.ZBattlefield {
			if f := o.Face(); f != nil && faceIsAura(f) {
				// Hand-built: the Curse is placed unattached and the test
				// attaches it (a real entry would pose CR 303.4f).
				stageRawEntry(t, e, o.ID)
			} else {
				e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: state.ZBattlefield})
			}
		}
		ids[o.Face().Name] = o.ID
	}
	for n := range want {
		if _, ok := ids[n]; !ok {
			t.Fatalf("no %q copy found in seat 0's genesis", n)
		}
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	e.pending = nil
	e.pendingTriggers = nil
	return e, ids
}

// answerLyndeUpkeep drives the engine until the Curse of the Pierced Heart is
// attached to a player (the trigger's own resolution) or the loop budget is
// exhausted, answering the decisions the chain poses. It reports whether the
// `Curse.AttachedTo You` ChooseCard ask and the PlayerChoices$ opponent ask
// were each POSED -- the two reader surfaces this ticket wires -- and the hand
// size at the moment the card choice was answered (so the draw-2 rider can be
// measured across exactly the trigger's resolution).
func answerLyndeUpkeep(t *testing.T, e *Engine, curse state.ObjID) (sawCardAsk, sawPlayerAsk bool, handAtCardAsk int) {
	t.Helper()
	for i := 0; i < 2000; i++ {
		if o := e.G.Obj(curse); o != nil && o.HasAttachedPlayer && o.AttachedPlayer != 0 {
			return sawCardAsk, sawPlayerAsk, handAtCardAsk
		}
		if e.G.Over {
			t.Fatalf("game ended before the Curse landed on an opponent")
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			continue
		}
		switch d.Kind {
		case decision.KPriority:
			submitPass(t, e)
		case decision.KTriggerOrder:
			idx := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				idx = append(idx, o.Index)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: idx}); err != nil {
				t.Fatalf("submit trigger order: %v", err)
			}
		case decision.KTriggerOptional:
			submitChoices(t, e, 0) // yes
		case decision.KAttackers, decision.KBlockers:
			// The drive to seat 0's NEXT upkeep crosses seat 0's own combat
			// on turn 2: declare no attackers and no blockers.
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: nil}); err != nil {
				t.Fatalf("submit %s: %v", d.Kind, err)
			}
		case decision.KChoose:
			// A cleanup-step discard down to the hand-size limit crossed on the
			// way to seat 0's next upkeep: answer it naively (first offered).
			if len(d.Options) > 0 && d.Options[0].Kind == "discard" {
				submitChoices(t, e, d.Options[0].Index)
				continue
			}
			// The `Curse.AttachedTo You` ChooseCard ask: option 0 is the
			// curse; record the hand size here, immediately before the
			// trigger's resolution, so the draw-2 rider is measured across
			// exactly this resolution.
			playerIdx := -1
			for _, o := range d.Options {
				if o.Kind == "player" && o.Player == 1 {
					playerIdx = o.Index
				}
			}
			if playerIdx >= 0 {
				sawPlayerAsk = true
				submitChoices(t, e, playerIdx)
				continue
			}
			cardIdx, cardObj := -1, state.ObjID(0)
			for _, o := range d.Options {
				if o.Obj != 0 {
					cardIdx, cardObj = o.Index, o.Obj
				}
			}
			if cardIdx >= 0 {
				if cardObj != curse {
					t.Fatalf("the Curse ChooseCard ask offered %v, want the attached Curse", d.Options)
				}
				sawCardAsk = true
				handAtCardAsk = len(e.G.Zone(state.ZHand, 0))
				submitChoices(t, e, cardIdx)
				continue
			}
			t.Fatalf("unrecognised KChoose ask: %+v", d.Options)
		default:
			t.Fatalf("unexpected decision while driving Lynde's upkeep: %+v", d)
		}
	}
	t.Fatal("Lynde's upkeep chain never landed the Curse on an opponent")
	return false, false, 0
}

// TestLyndeUpkeepMovesCurseOntoOpponent is the end-to-end proof of all three
// reader surfaces at once: (c) the `Curse.AttachedTo You` ChooseCard pool is
// non-empty only because the `.AttachedTo You` word is recognised, (b) the
// `PlayerChoices$ Opponent` Attach ask is posed only because effAttach reads
// the param, and the `RememberAttached$ True` remember is what makes DBDraw's
// ConditionPresent$ Card pass for the two cards. Before the fix the trigger
// fired, the may ask was accepted, and one deterministic
// `cannot attach: no legal target` Note was emitted with no Curse chosen and
// no cards drawn.
func TestLyndeUpkeepMovesCurseOntoOpponent(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, ids := lyndeBoard3(t, reg, "Lynde, Cheerful Tormentor", "Curse of the Pierced Heart")
	lynde := ids["Lynde, Cheerful Tormentor"]
	curse := ids["Curse of the Pierced Heart"]
	if e.G.Obj(lynde).Zone != state.ZBattlefield || e.G.Obj(curse).Zone != state.ZBattlefield {
		t.Fatal("precondition: Lynde and the Curse must be on the battlefield")
	}
	// Attach the Curse to Lynde's controller through the logged player-attach
	// event (the same fold the cast path emits).
	e.emit(events.Event{Kind: events.Attach, Obj: curse, Player: 0, Text: "attach to player"})
	if !e.G.Obj(curse).HasAttachedPlayer || e.G.Obj(curse).AttachedPlayer != 0 {
		t.Fatal("precondition: the Curse is not attached to seat 0")
	}
	if e.G.Obj(curse).AttachedPlayer == 1 {
		t.Fatal("precondition: the Curse's enchanted seat must differ from the destination")
	}

	sawCardAsk, sawPlayerAsk, handAtCardAsk := answerLyndeUpkeep(t, e, curse)

	if !sawCardAsk {
		t.Fatal("the `Curse.AttachedTo You` ChooseCard ask was never posed")
	}
	if !sawPlayerAsk {
		t.Fatal("the PlayerChoices$ opponent Attach ask was never posed")
	}
	o := e.G.Obj(curse)
	if o.Zone != state.ZBattlefield || !o.HasAttachedPlayer || o.AttachedPlayer != 1 {
		t.Fatalf("the Curse did not land attached to seat 1: %+v", o)
	}
	if o.AttachedTo != 0 {
		t.Fatalf("a player attachment must not also carry an object bearer: AttachedTo=%d", o.AttachedTo)
	}
	if got := len(e.G.Zone(state.ZHand, 0)) - handAtCardAsk; got != 2 {
		t.Fatalf("DBDraw drew %d cards, want 2", got)
	}
	// The attachment reconstructs from the logged event alone.
	replay := e.G.Clone()
	ev := e.emit(events.Event{Kind: events.Attach, Obj: curse, Player: 1, Text: "attach to player"})
	events.Apply(replay, ev)
	if !replay.Obj(curse).HasAttachedPlayer || replay.Obj(curse).AttachedPlayer != 1 {
		t.Fatal("the player attachment must reconstruct by folding the logged Attach")
	}
}

// TestAccursedWitchReturnAttachesToParentTarget is the (a) proof on a second
// carrier: Accursed Witch's death returns it transformed as Infectious Curse
// via `ChangeZone | AttachedToPlayer$ ParentTarget`, so the returned Curse
// enters attached to the opponent its death trigger targeted. Before the fix
// the param was unread, the Curse entered unattached, and the CR 704.5m
// attachment SBA swept it to the graveyard.
func TestAccursedWitchReturnAttachesToParentTarget(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, ids := dsBoard(t, reg, "Accursed Witch")
	witch := ids["Accursed Witch"]
	if e.G.Obj(witch).Zone != state.ZBattlefield {
		t.Fatal("precondition: Accursed Witch must be on the battlefield")
	}
	if e.G.Obj(witch).Face() == nil || e.G.Obj(witch).Face().Name != "Accursed Witch" {
		t.Fatal("precondition: wrong front face")
	}

	e.emit(events.Event{Kind: events.MoveZone, Obj: witch, From: state.ZBattlefield, To: state.ZGraveyard})
	if e.G.Obj(witch).Zone != state.ZGraveyard {
		t.Fatal("precondition: the Witch did not reach the graveyard")
	}

	targeted := false
	for i := 0; i < 2000; i++ {
		o := e.G.Obj(witch)
		if o != nil && o.Zone == state.ZBattlefield && o.HasAttachedPlayer {
			if o.AttachedPlayer != 1 {
				t.Fatalf("returned Curse attached to seat %d, want the targeted seat 1", o.AttachedPlayer)
			}
			if !targeted {
				t.Fatal("the return landed without ever posing the opponent target ask")
			}
			return
		}
		if len(e.pendingTriggers) > 0 {
			e.putTriggersOnStack()
			continue
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			continue
		}
		switch d.Kind {
		case decision.KPriority:
			submitPass(t, e)
		case decision.KTriggerOrder:
			idx := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				idx = append(idx, o.Index)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: idx}); err != nil {
				t.Fatalf("submit trigger order: %v", err)
			}
		case decision.KTriggerOptional:
			submitChoices(t, e, 0)
		case decision.KChoose:
			// A cleanup-step discard (or any other incidental choose)
			// crossed while driving: take the first offered option.
			submitChoices(t, e, d.Options[0].Index)
		case decision.KTarget:
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "player" && o.Player == 1 {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("opponent target missing: %+v", d.Options)
			}
			targeted = true
			submitChoices(t, e, idx)
		default:
			t.Fatalf("unexpected decision resolving the Witch's death: %+v", d)
		}
	}
	t.Fatal("the Witch's return never completed")
}

// TestWitchbaneOrbDestroysCurseAttachedToYou drives the same `.AttachedTo You`
// word through Witchbane Orb's `DestroyAll | ValidCards$ Curse.AttachedTo
// You`: a Curse attached to the Orb's controller is destroyed when the Orb
// enters. Before the word was recognised the DestroyAll pool was empty.
func TestWitchbaneOrbDestroysCurseAttachedToYou(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, ids := dsBoard(t, reg, "Witchbane Orb", "Curse of the Pierced Heart")
	orb := ids["Witchbane Orb"]
	curse := ids["Curse of the Pierced Heart"]
	if e.G.Obj(orb).Zone != state.ZBattlefield || e.G.Obj(curse).Zone != state.ZBattlefield {
		t.Fatal("precondition: Orb and Curse must be on the battlefield")
	}
	e.emit(events.Event{Kind: events.Attach, Obj: curse, Player: 0, Text: "attach to player"})
	if !e.G.Obj(curse).HasAttachedPlayer || e.G.Obj(curse).AttachedPlayer != 0 {
		t.Fatal("precondition: the Curse is not attached to seat 0")
	}

	// Re-enter the Orb so its ETB triggers (dsBoard cleared the genesis
	// triggers): off the battlefield through a logged move, then back on.
	e.emit(events.Event{Kind: events.MoveZone, Obj: orb, From: state.ZBattlefield, To: state.ZHand})
	e.emit(events.Event{Kind: events.MoveZone, Obj: orb, From: state.ZHand, To: state.ZBattlefield})

	for i := 0; i < 500; i++ {
		if e.G.Obj(curse).Zone == state.ZGraveyard {
			return
		}
		if len(e.pendingTriggers) > 0 {
			e.putTriggersOnStack()
			continue
		}
		d := e.Pending()
		if d == nil {
			e.Advance()
			continue
		}
		switch d.Kind {
		case decision.KPriority:
			submitPass(t, e)
		case decision.KTriggerOrder:
			idx := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				idx = append(idx, o.Index)
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: idx}); err != nil {
				t.Fatalf("submit trigger order: %v", err)
			}
		case decision.KTriggerOptional:
			submitChoices(t, e, 0)
		default:
			t.Fatalf("unexpected decision resolving Witchbane Orb: %+v", d)
		}
	}
	t.Fatal("Witchbane Orb never destroyed the Curse attached to its controller")
}

// TestEnchantOpponentAuraCastAttachesToOpponent covers the absorbed
// ticket's K:Enchant:Opponent clause through the same player-destination
// branch: an Aura whose printed Enchant spec is Opponent attaches to the
// opponent it targeted. Before the branch accepted `Opponent` the cast fell
// through to the object walker, which drops players, and the Aura entered
// unattached (then swept by the CR 704.5m SBA).
func TestEnchantOpponentAuraCastAttachesToOpponent(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	aura := mustCorpusCard(t, reg, "Tenuous Truce")
	e := handEngine(t, aura)
	id := e.G.Zone(state.ZHand, 0)[0]
	if e.G.Obj(id).Face() == nil || e.G.Obj(id).Face().Name != "Tenuous Truce" {
		t.Fatal("precondition: wrong card in hand")
	}
	param, ok := e.G.Obj(id).Face().KeywordParam("Enchant")
	if !ok || param != "Opponent" {
		t.Fatalf("precondition: Tenuous Truce's Enchant spec = %q, want Opponent", param)
	}
	addMana(t, e, 0, "WW")
	found := false
	for _, opt := range castOptions(t, e) {
		if opt.Obj == id {
			submitChoices(t, e, opt.Index)
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Tenuous Truce cast not offered")
	}
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected player target decision: %+v", d)
	}
	index := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			index = o.Index
		}
	}
	if index < 0 {
		t.Fatalf("opponent target missing: %+v", d.Options)
	}
	submitChoices(t, e, index)
	passUntilStackEmpty(t, e, 40)
	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield || !o.HasAttachedPlayer || o.AttachedPlayer != 1 {
		t.Fatalf("Enchant:Opponent Aura not attached to seat 1: %+v", o)
	}
}

// ChangeZone AttachedToPlayer$ You brings the Curse back to the battlefield
// ATTACHED to Lynde's controller. Before the fix AttachedToPlayer$ was unread,
// the Curse entered unattached, the CR 704.5m attachment SBA swept it straight
// back to the graveyard, and that re-fired Lynde's own trigger -- a
// deterministic battlefield<->graveyard ping-pong. The test asserts the return
// is on the battlefield and attached to seat 0, and that no second
// battlefield->graveyard MoveZone happened in the same window.
func TestLyndeGraveyardReturnAttachesToYou(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, _, ids := dsBoard(t, reg, "Lynde, Cheerful Tormentor", "Curse of the Pierced Heart")
	curse := ids["Curse of the Pierced Heart"]
	e.emit(events.Event{Kind: events.Attach, Obj: curse, Player: 0, Text: "attach to player"})
	if !e.G.Obj(curse).HasAttachedPlayer || e.G.Obj(curse).AttachedPlayer != 0 {
		t.Fatal("precondition: the Curse is not attached to seat 0")
	}
	// Put the Curse into the graveyard from the battlefield: this is what
	// arms Lynde's delayed return.
	e.emit(events.Event{Kind: events.MoveZone, Obj: curse, From: state.ZBattlefield, To: state.ZGraveyard})
	if e.G.Obj(curse).Zone != state.ZGraveyard {
		t.Fatal("precondition: the Curse did not reach the graveyard")
	}
	graveyardMoves := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == curse && ev.To == state.ZGraveyard {
			graveyardMoves++
		}
	}

	driveToStepAll(t, e, e.G.Turn, 0, state.StepEnd)
	passUntilStackEmpty(t, e, 80)

	o := e.G.Obj(curse)
	if o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the Curse did not return to the battlefield: %v", o)
	}
	if !o.HasAttachedPlayer || o.AttachedPlayer != 0 {
		t.Fatalf("the returned Curse is not attached to seat 0: AttachedPlayer=%d Has=%v", o.AttachedPlayer, o.HasAttachedPlayer)
	}
	// No ping-pong: exactly the one initial move to the graveyard, never the
	// SBA sweep a second time in this window.
	total := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == curse && ev.To == state.ZGraveyard {
			total++
		}
	}
	if total != graveyardMoves {
		t.Fatalf("the returned Curse was swept back to the graveyard (%d -> %d graveyard moves)", graveyardMoves, total)
	}
}
