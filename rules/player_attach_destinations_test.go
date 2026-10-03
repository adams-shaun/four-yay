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

// This file covers the ordinary DB$ Attach destination walk for the three
// corpus carriers whose SVar names a PLAYER destination WITHOUT either a
// `Keyword$ Enchant` param or a `PlayerChoices$` param -- so they cannot ride
// effAttach's Enchant:Player branch or its PlayerChoices$ branch and must be
// admitted by the ordinary Defined$ destination walk itself:
//
//   - Archnemesis   `DB$ Attach | Object$ Self | Defined$ TriggeredAttackingPlayer`
//   - Maddening Hex `DB$ Attach | Defined$ ChosenPlayer`
//   - Ardenn        `DB$ Attach | Defined$ Targeted | ValidTgts$ Permanent,Player
//                   | Object$ Valid Aura.YouCtrl,Equipment.YouCtrl | Optional$ True`
//
// and the fourth, Curse of Leeches, whose PlayerChoices$ Player pool is
// delivered through a `R:Event$ Transform` replacement rather than a cast.

// drivePlayerAttach answers the decisions an Attach resolution poses --
// priority passes, optional yes/no, a target or a player choice -- until the
// object lands attached (HasAttachedPlayer) or the budget runs out. It
// records which decision kinds it actually saw so a test can prove the ask it
// depends on was posed.
func drivePlayerAttach(t *testing.T, e *Engine, obj state.ObjID, want seatWants) (sawTarget, sawPlayerAsk, sawOptional bool) {
	t.Helper()
	for i := 0; i < 3000; i++ {
		o := e.G.Obj(obj)
		if o != nil && o.HasAttachedPlayer && want.hasSeat && o.AttachedPlayer == want.seat {
			return
		}
		if o != nil && o.HasAttachedPlayer && !want.hasSeat {
			return
		}
		if e.G.Over {
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
			sawOptional = true
			submitChoices(t, e, 0) // accept
		case decision.KChoose:
			// A player-destination ask (PlayerChoices$) offers Kind "player";
			// a yes/no Attach optional offers "yes"/"no"; a discard offers
			// "discard". Prefer the destination the test wants.
			if idx := attachOptionForSeat(d, want.seat); idx >= 0 && want.hasSeat {
				sawPlayerAsk = true
				submitChoices(t, e, idx)
				continue
			}
			if idx := yesOption(d); idx >= 0 {
				sawOptional = true
				submitChoices(t, e, idx)
				continue
			}
			if len(d.Options) > 0 {
				submitChoices(t, e, d.Options[0].Index)
				continue
			}
			t.Fatalf("KChoose with no options: %+v", d)
		case decision.KTarget:
			wantIdx := -1
			for _, o := range d.Options {
				if o.Kind == "player" && o.Player == want.seat && want.hasSeat {
					wantIdx = o.Index
				}
			}
			if wantIdx < 0 {
				t.Fatalf("target ask does not offer seat %d: %+v", want.seat, d.Options)
			}
			sawTarget = true
			submitChoices(t, e, wantIdx)
		default:
			t.Fatalf("unexpected decision while driving a player attach: %+v", d)
		}
	}
	t.Fatal("the player attach never landed")
	return
}

type seatWants struct {
	seat    state.PlayerID
	hasSeat bool
}

func attachOptionForSeat(d *decision.Decision, seat state.PlayerID) int {
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == seat {
			return o.Index
		}
	}
	return -1
}

func yesOption(d *decision.Decision) int {
	for _, o := range d.Options {
		if o.Kind == "yes" {
			return o.Index
		}
	}
	return -1
}

// TestArdennAttachesPluralAurasToChosenPlayer drives Ardenn's begin-combat
// trigger over the ordinary destination walk: `Object$ Valid
// Aura.YouCtrl,Equipment.YouCtrl` resolves a PLURAL object list and `Defined$
// Targeted | ValidTgts$ Permanent,Player` names the chosen player, so both the
// plural walk and the player admission are exercised at once. The Aura is
// Enchant:Player so it is admitted; the Equipment in the same pool must NOT
// attach to the player.
func TestArdennAttachesPluralAurasToChosenPlayer(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, ids := dsBoard(t, reg, "Ardenn, Intrepid Archaeologist", "Curse of the Pierced Heart", "Bonesplitter")
	if e.G.Obj(ids["Ardenn, Intrepid Archaeologist"]).Zone != state.ZBattlefield {
		t.Fatal("precondition: Ardenn must be on the battlefield")
	}
	auraID := ids["Curse of the Pierced Heart"]
	equipID := ids["Bonesplitter"]
	// Park the Aura attached to seat 1 so the unattached-Aura SBA cannot
	// sweep it before Ardenn's trigger resolves; the trigger's own resolution
	// then re-attaches it to the chosen player.
	e.emit(events.Event{Kind: events.Attach, Obj: auraID, Player: 1, Text: "attach to player"})
	for _, id := range []state.ObjID{auraID, equipID} {
		if e.G.Obj(id).Zone != state.ZBattlefield {
			t.Fatalf("precondition: object %d must be on the battlefield", id)
		}
	}
	if !e.G.Obj(auraID).HasAttachedPlayer || e.G.Obj(auraID).AttachedPlayer != 1 {
		t.Fatalf("precondition: the Aura must start attached to seat 1: %+v", e.G.Obj(auraID))
	}
	if e.G.Obj(equipID).HasAttachedPlayer || e.G.Obj(equipID).AttachedTo != 0 {
		t.Fatalf("precondition: the Equipment must start unattached: %+v", e.G.Obj(equipID))
	}
	if param, ok := e.G.Obj(auraID).Face().KeywordParam("Enchant"); !ok || param != "Player" {
		t.Fatalf("precondition: the Aura's Enchant spec = %q, want Player", param)
	}
	if _, ok := e.G.Obj(equipID).Face().KeywordParam("Enchant"); ok {
		t.Fatal("precondition: the Equipment must carry no Enchant keyword")
	}

	// Begin combat on seat 0's turn, which fires Ardenn's Phase trigger. The
	// Aura starts on seat 1 and the chosen destination is seat 0, so the
	// re-attachment is observable as a seat change.
	e.G.Active = 0
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepBeginCombat})

	sawTarget, _, sawOptional := drivePlayerAttach(t, e, auraID, seatWants{seat: 0, hasSeat: true})
	if !sawTarget {
		t.Fatal("Ardenn's `Defined$ Targeted` player target ask was never posed")
	}
	if !sawOptional {
		t.Fatal("Ardenn's Attach Optional$ True election was never posed")
	}

	o := e.G.Obj(auraID)
	if !o.HasAttachedPlayer || o.AttachedPlayer != 0 {
		t.Fatalf("the Aura did not attach to the chosen player 0: %+v", o)
	}
	if q := e.G.Obj(equipID); q.HasAttachedPlayer || q.AttachedTo != 0 {
		t.Fatalf("the Equipment must NOT attach to a player: %+v", q)
	}
	replayCheck(t, e, cfg)
}

// TestArchnemesisAttackTriggerAttachesToAttackingPlayer drives Archnemesis'
// `T:Mode$ AttackersDeclared | AttackedTarget$ You` trigger, whose
// `DB$ Attach | Object$ Self | Defined$ TriggeredAttackingPlayer` names a
// player destination with neither Keyword$ nor PlayerChoices$. Seat 1 attacks
// seat 0; accepting the may must attach the Aura to seat 1 through the
// ordinary destination walk.
func TestArchnemesisAttackTriggerAttachesToAttackingPlayer(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	archnemesis := mustCorpusCard(t, reg, "Archnemesis")
	// A THREE-seat table so the attacked seat (0), the currently enchanted
	// opponent (1) and the attacking player (2) are all distinct: the
	// re-attachment is then observable as a seat change between two seats the
	// Aura's Enchant:Opponent spec both admit.
	cfg := Config{Seed: 47, Names: []string{"a", "b", "c"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{archnemesis}, mountainDeck(t, 39)...),
			mountainDeck(t, 40),
			mountainDeck(t, 40),
		}}
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	var id state.ObjID
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner == 0 && o.Card == archnemesis {
			id = o.ID
			if o.Zone != state.ZBattlefield {
				// Hand-built: the Aura sits unattached until the test
				// attaches it (a real entry would pose CR 303.4f).
				stageRawEntry(t, e, o.ID)
			}
		}
	}
	if id == 0 {
		t.Fatal("setup: no seat-0 Archnemesis copy")
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	e.pending = nil
	e.pendingTriggers = nil

	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Archnemesis must be on the battlefield: %+v", o)
	}
	if param, ok := o.Face().KeywordParam("Enchant"); !ok || param != "Opponent" {
		t.Fatalf("precondition: Archnemesis Enchant spec = %q, want Opponent", param)
	}
	// Start it attached to opponent seat 1 so the destination (the attacking
	// player, seat 2) differs from the precondition.
	e.emit(events.Event{Kind: events.Attach, Obj: id, Player: 1, Text: "attach to player"})
	if !e.G.Obj(id).HasAttachedPlayer || e.G.Obj(id).AttachedPlayer != 1 {
		t.Fatal("precondition: Archnemesis is not attached to seat 1")
	}

	// Seat 2 declares an attacker against seat 0 (the seat Archnemesis's
	// controller defends).
	atk := onBoardReady(t, e, 2, "Name:Raider\nManaCost:1 R\nTypes:Creature Goblin\nPT:2/2\nOracle:x\n")
	e.G.Active = 2
	e.G.Step = state.StepDeclareAttackers
	e.pending = nil
	e.Advance()
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected seat 2's declare-attackers decision, got %+v", d)
	}
	idx := -1
	for _, opt := range d.Options {
		if opt.Obj == atk && opt.Player == 0 {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no attacker option for the Raider at seat 0: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("declare attacker: %v", err)
	}

	sawOptional := false
	for i := 0; i < 3000; i++ {
		cur := e.G.Obj(id)
		if cur != nil && cur.HasAttachedPlayer && cur.AttachedPlayer == 2 {
			break
		}
		if e.G.Over {
			t.Fatal("game ended before Archnemesis reattached")
		}
		if len(e.pendingTriggers) > 0 {
			e.putTriggersOnStack()
			continue
		}
		dd := e.Pending()
		if dd == nil {
			e.Advance()
			continue
		}
		switch dd.Kind {
		case decision.KPriority, decision.KAttackers:
			if dd.Kind == decision.KAttackers {
				if err := e.Submit(decision.Intent{Seq: dd.Seq, Player: dd.Player, Choices: nil}); err != nil {
					t.Fatalf("submit attackers: %v", err)
				}
				continue
			}
			submitPass(t, e)
		case decision.KTriggerOrder:
			order := make([]int, 0, len(dd.Options))
			for _, x := range dd.Options {
				order = append(order, x.Index)
			}
			if err := e.Submit(decision.Intent{Seq: dd.Seq, Player: dd.Player, Choices: order}); err != nil {
				t.Fatalf("submit trigger order: %v", err)
			}
		case decision.KTriggerOptional:
			sawOptional = true
			submitChoices(t, e, 0)
		case decision.KChoose:
			if yi := yesOption(dd); yi >= 0 {
				submitChoices(t, e, yi)
				continue
			}
			submitChoices(t, e, dd.Options[0].Index)
		default:
			t.Fatalf("unexpected decision driving Archnemesis: %+v", dd)
		}
	}
	if !sawOptional {
		t.Fatal("Archnemesis' may-ability was never offered")
	}
	cur := e.G.Obj(id)
	if !cur.HasAttachedPlayer || cur.AttachedPlayer != 2 {
		t.Fatalf("Archnemesis did not attach to the attacking player (seat 2): %+v", cur)
	}
	// The player attachment reconstructs from the logged Attach event alone.
	// (A full log-only replay is not asserted here: this test hand-forces the
	// combat step fields, which the log does not carry.)
	replay := e.G.Clone()
	ev := e.emit(events.Event{Kind: events.Attach, Obj: id, Player: 2, Text: "attach to player"})
	events.Apply(replay, ev)
	if !replay.Obj(id).HasAttachedPlayer || replay.Obj(id).AttachedPlayer != 2 {
		t.Fatal("the player attachment must reconstruct by folding the logged Attach")
	}
}

// TestMaddeningHexAttachToChosenPlayer drives Maddening Hex's `DBAttach`
// SVar (`DB$ Attach | Defined$ ChosenPlayer`) -- the "attach CARDNAME to
// another one of your opponents" half of its cast trigger. Its `Defined$`
// names a player with neither a Keyword$ param nor a PlayerChoices$ param, so
// it too rides the ordinary destination walk. The real SVar is resolved with
// the ChosenPlayer binding the ChoosePlayer predecessor writes (seat 2, a
// different opponent from the enchanted seat 1), which is exactly the context
// the trigger's resolution carries.
func TestMaddeningHexAttachToChosenPlayer(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	hex := mustCorpusCard(t, reg, "Maddening Hex")
	cfg := Config{Seed: 47, Names: []string{"a", "b", "c"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{hex}, mountainDeck(t, 39)...),
			mountainDeck(t, 40),
			mountainDeck(t, 40),
		}}
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	var id state.ObjID
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner == 0 && o.Card == hex {
			id = o.ID
			if o.Zone != state.ZBattlefield {
				// Hand-built: the Aura sits unattached until the test
				// attaches it (a real entry would pose CR 303.4f).
				stageRawEntry(t, e, o.ID)
			}
		}
	}
	if id == 0 {
		t.Fatal("setup: no seat-0 Maddening Hex copy")
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepMain1})
	e.pending = nil
	e.pendingTriggers = nil

	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Maddening Hex must be on the battlefield: %+v", o)
	}
	if param, ok := o.Face().KeywordParam("Enchant"); !ok || param != "Player" {
		t.Fatalf("precondition: Maddening Hex Enchant spec = %q, want Player", param)
	}
	// Attach it to seat 1 (the enchanted player the trigger belongs to), so
	// the destination under test (seat 2) differs from the precondition.
	e.emit(events.Event{Kind: events.Attach, Obj: id, Player: 1, Text: "attach to player"})
	if !e.G.Obj(id).HasAttachedPlayer || e.G.Obj(id).AttachedPlayer != 1 {
		t.Fatal("precondition: Maddening Hex is not attached to seat 1")
	}

	sa := cards.ResolveSVar(e.G.Obj(id).Face().SVars, "DBAttach")
	if sa == nil || sa.API != "Attach" {
		t.Fatalf("precondition: DBAttach did not resolve to an Attach body: %+v", sa)
	}
	if got := sa.Params["Defined"]; got != "ChosenPlayer" {
		t.Fatalf("precondition: DBAttach Defined$ = %q, want ChosenPlayer", got)
	}
	effects.Resolve(e, &effects.Ctx{Source: id, Controller: 0,
		Chosen: []state.Target{{Player: 2, IsPlayer: true}}}, sa)

	cur := e.G.Obj(id)
	if !cur.HasAttachedPlayer || cur.AttachedPlayer != 2 {
		t.Fatalf("Maddening Hex attached to %d (Has=%v), want the ChosenPlayer seat 2", cur.AttachedPlayer, cur.HasAttachedPlayer)
	}
}
