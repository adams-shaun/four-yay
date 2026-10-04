// Task att-optional: api:Attach.Optional was unread -- a may-attach Attach
// always attached, and (for Ajani's Chosen) attached the WRONG object, because
// Object$ TriggeredCardLKICopy also fell to the Self default. Both defects are
// fixed in effAttach (effects/attach.go): the optional election is a real
// yes/no KChoose through the shared Ask boundary (an "attach_optional"
// ask answered in place), and every Object$ spec the shared definedSpec resolver supports
// now names the object it says. These tests pin both, on the real corpus
// cards, end to end -- never copied script text (the licensing rule).
//
// Batterskull's Living-Weapon SVar (Object$ Self) and the kw:Equip/kw:Enchant
// expansions (no Object$ at all) keep their byte-identical Self default: the
// TestHeads goldens cover them, and this file's own decline case additionally
// proves the pre-existing Enchant attach (Rancor onto Ajani's Chosen) still
// happens before the trigger resolves.
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// findAndMoveToHand returns the named corpus card's object id, emitting the
// same logged library→hand MoveZone the Batterskull test does when the card
// did not open in hand.
func findAndMoveToHand(t *testing.T, e *Engine, seat state.PlayerID, name string) state.ObjID {
	t.Helper()
	for _, cand := range e.G.Zone(state.ZHand, seat) {
		if e.G.Obj(cand).Face().Name == name {
			return cand
		}
	}
	for _, cand := range e.G.Zone(state.ZLibrary, seat) {
		if e.G.Obj(cand).Face().Name == name {
			e.emit(events.Event{Kind: events.MoveZone, Obj: cand, From: state.ZLibrary, To: state.ZHand})
			return cand
		}
	}
	t.Fatalf("%s not found in seat %d's hand or library", name, seat)
	return 0
}

func moveToBattlefield(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
}

// castFromPriority submits the cast option for id off the current priority
// decision.
func castFromPriority(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("expected a priority decision, got %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == id {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for obj %d: %+v", id, d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit cast: %v", err)
	}
}

// answerKTarget answers the pending KTarget decision with the option naming
// target.
func answerKTarget(t *testing.T, e *Engine, target state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Obj == target {
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{o.Index}}); err != nil {
				t.Fatalf("submit target: %v", err)
			}
			return
		}
	}
	t.Fatalf("no target option for obj %d: %+v", target, d.Options)
}

// drainToAttachAsk passes priority until the Optional$ Attach election is
// pending, failing on any other non-priority decision along the way.
func drainToAttachAsk(t *testing.T, e *Engine, limit int) *decision.Decision {
	t.Helper()
	for n := 0; n < limit; n++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while draining (stack depth %d)", len(e.G.Stack))
		}
		if d.Kind == decision.KChoose && d.ResumeKind == "attach_optional" {
			return d
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("non-priority decision %+v while draining", d)
		}
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "pass" {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("priority decision with no pass option: %+v", d.Options)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
			t.Fatalf("submit pass: %v", err)
		}
	}
	t.Fatal("the may-attach ask was never posed")
	return nil
}

func soleBattlefieldToken(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	var id state.ObjID
	for i := range e.G.Objs {
		o := e.G.Objs[i]
		if o.IsToken && o.Zone == state.ZBattlefield {
			if id != 0 && id != o.ID {
				t.Fatalf("more than one battlefield token: %d and %d", id, o.ID)
			}
			id = o.ID
		}
	}
	if id == 0 {
		t.Fatal("no battlefield token")
	}
	return id
}

// tokenCreateIndex returns the log index of the named token's TokenCreate
// event -- the marker every trigger-own Attach sits after (the source's own
// Enchant/Equip attach happens before the trigger resolves).
func tokenCreateIndex(t *testing.T, e *Engine, key string) int {
	t.Helper()
	for i := range e.L.Events {
		if e.L.Events[i].Kind == events.TokenCreate && e.L.Events[i].Text == key {
			return i
		}
	}
	t.Fatalf("no TokenCreate event for %s -- the trigger never reached the optional attach", key)
	return -1
}

// attachEventsAfter returns the Attach events after tokIdx.
func attachEventsAfter(e *Engine, tokIdx int) []events.Event {
	var out []events.Event
	for _, ev := range e.L.Events[tokIdx+1:] {
		if ev.Kind == events.Attach {
			out = append(out, ev)
		}
	}
	return out
}

// ajanisChosenGame builds a 2-seat game with the real corpus Ajani's Chosen
// on seat 0's battlefield and the real Rancor in its hand, drives to seat 0's
// turn-1 Main1, casts Rancor at Ajani's Chosen, and drains to the may-attach
// election. It returns the engine, the config (for replayCheck), both ids,
// and the pending ask.
func ajanisChosenGame(t *testing.T, seed uint64) (*Engine, Config, state.ObjID, state.ObjID, *decision.Decision) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	ajaniCard, ok := reg.Lookup("Ajani's Chosen")
	if !ok {
		t.Fatal("Ajani's Chosen not found in the compiled corpus registry")
	}
	rancorCard, ok := reg.Lookup("Rancor")
	if !ok {
		t.Fatal("Rancor not found in the compiled corpus registry")
	}
	if reg.Tokens["w_2_2_cat"] == nil {
		t.Fatal("w_2_2_cat not found in the compiled corpus registry's token scripts")
	}
	cfg := Config{Seed: seed, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{ajaniCard, rancorCard}, mountainDeck(t, 38)...),
			mountainDeck(t, 40),
		},
		Tokens: reg.Tokens,
	}
	e := New(seatZeroStart(cfg))
	e.Advance()
	ajaniID := findAndMoveToHand(t, e, 0, "Ajani's Chosen")
	moveToBattlefield(t, e, ajaniID)
	rancorID := findAndMoveToHand(t, e, 0, "Rancor")
	addMana(t, e, 0, "G")
	castFromPriority(t, e, rancorID)
	answerKTarget(t, e, ajaniID)
	d := drainToAttachAsk(t, e, 60)
	return e, cfg, ajaniID, rancorID, d
}

// TestAjanisChosenMayAttachAskPosesAndYesAttachesTheAura: the trigger's
// Optional$ Attach poses a real yes/no KChoose, and YES attaches the ENTERING
// AURA (Object$ TriggeredCardLKICopy -- Rancor, not Ajani's Chosen itself) to
// the Cat token. Before the fix the ask never fired and the resolving source
// attached itself to the Aura.
func TestAjanisChosenMayAttachAskPosesAndYesAttachesTheAura(t *testing.T) {
	t.Parallel()
	e, cfg, ajaniID, rancorID, d := ajanisChosenGame(t, 901)
	if d.Player != 0 {
		t.Fatalf("ask player = %d, want 0 (the controller)", d.Player)
	}
	if len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
		t.Fatalf("ask options = %+v, want yes then no", d.Options)
	}
	// Rancor is already attached to Ajani's Chosen by its own K:Enchant --
	// the entering Aura sits with the source until the election says so.
	if e.G.Obj(rancorID).AttachedTo != ajaniID {
		t.Fatalf("Rancor AttachedTo = %d, want %d before the election", e.G.Obj(rancorID).AttachedTo, ajaniID)
	}
	submitChoices(t, e, 0) // yes
	passUntilStackEmpty(t, e, 60)
	tokenID := soleBattlefieldToken(t, e)
	tokIdx := tokenCreateIndex(t, e, "w_2_2_cat")
	att := attachEventsAfter(e, tokIdx)
	if len(att) != 1 {
		t.Fatalf("got %d Attach events after the TokenCreate, want 1: %+v", len(att), att)
	}
	if att[0].Obj != rancorID {
		t.Fatalf("attach Obj = %d, want %d (the ENTERING AURA, not Ajani's Chosen %d)", att[0].Obj, rancorID, ajaniID)
	}
	if len(att[0].IDs) != 1 || att[0].IDs[0] != tokenID {
		t.Fatalf("attach IDs = %v, want [%d] (the Cat token)", att[0].IDs, tokenID)
	}
	if e.G.Obj(rancorID).AttachedTo != tokenID {
		t.Fatalf("Rancor AttachedTo = %d, want the token %d", e.G.Obj(rancorID).AttachedTo, tokenID)
	}
	replayCheck(t, e, cfg)
}

// TestAjanisChosenMayAttachDeclineLeavesTheAuraAndRunsTheChain: answering NO
// emits no Attach after the TokenCreate (the Aura stays attached to its cast
// target) and the chained SubAbility$ DBCleanup still runs -- the trigger's
// Remembered is cleared on the source object.
func TestAjanisChosenMayAttachDeclineLeavesTheAuraAndRunsTheChain(t *testing.T) {
	t.Parallel()
	e, cfg, ajaniID, rancorID, d := ajanisChosenGame(t, 902)
	if len(d.Options) != 2 || d.Options[1].Kind != "no" {
		t.Fatalf("ask options = %+v, want yes then no", d.Options)
	}
	submitChoices(t, e, 1) // no
	passUntilStackEmpty(t, e, 60)
	tokIdx := tokenCreateIndex(t, e, "w_2_2_cat")
	if att := attachEventsAfter(e, tokIdx); len(att) != 0 {
		t.Fatalf("decline emitted %d Attach events after the TokenCreate: %+v", len(att), att)
	}
	if e.G.Obj(rancorID).AttachedTo != ajaniID {
		t.Fatalf("Rancor AttachedTo = %d, want %d (stayed on its cast target)", e.G.Obj(rancorID).AttachedTo, ajaniID)
	}
	if len(e.G.Obj(ajaniID).Remembered) != 0 {
		t.Fatalf("Ajani's Chosen Remembered = %v, want empty (DBCleanup ran)", e.G.Obj(ajaniID).Remembered)
	}
	replayCheck(t, e, cfg)
}

// TestCoriSteelCutterOptionalAttachAttachesTheEquipment: the second Optional$
// carrier, whose DBAttach has NO Object$ -- the Self default -- so the yes
// answer attaches the Equipment itself to the Monk token (the cast spell is
// still on the stack when the trigger resolves, so the token is the only
// legal target).
func TestCoriSteelCutterOptionalAttachAttachesTheEquipment(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	cutterCard, ok := reg.Lookup("Cori-Steel Cutter")
	if !ok {
		t.Fatal("Cori-Steel Cutter not found in the compiled corpus registry")
	}
	memniteCard, ok := reg.Lookup("Memnite")
	if !ok {
		t.Fatal("Memnite not found in the compiled corpus registry")
	}
	if reg.Tokens["w_1_1_monk_prowess"] == nil {
		t.Fatal("w_1_1_monk_prowess not found in the compiled corpus registry's token scripts")
	}
	cfg := Config{Seed: 903, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			append([]*cards.Card{cutterCard, memniteCard, memniteCard}, mountainDeck(t, 37)...),
			mountainDeck(t, 40),
		},
		Tokens: reg.Tokens,
	}
	e := New(seatZeroStart(cfg))
	e.Advance()
	cutterID := findAndMoveToHand(t, e, 0, "Cori-Steel Cutter")
	moveToBattlefield(t, e, cutterID)
	memnite1 := findAndMoveToHand(t, e, 0, "Memnite")
	addMana(t, e, 0, "") // drive to Main1 and re-ask priority
	castFromPriority(t, e, memnite1)
	passUntilStackEmpty(t, e, 60)
	memnite2 := findAndMoveToHand(t, e, 0, "Memnite")
	e.priorityRound()                // refresh the priority options after the logged MoveZone
	castFromPriority(t, e, memnite2) // the SECOND spell each turn: fires the trigger
	d := drainToAttachAsk(t, e, 60)
	if d.Player != 0 {
		t.Fatalf("ask player = %d, want 0", d.Player)
	}
	submitChoices(t, e, 0) // yes
	passUntilStackEmpty(t, e, 60)
	monkID := soleBattlefieldToken(t, e)
	tokIdx := tokenCreateIndex(t, e, "w_1_1_monk_prowess")
	att := attachEventsAfter(e, tokIdx)
	if len(att) != 1 {
		t.Fatalf("got %d Attach events after the TokenCreate, want 1: %+v", len(att), att)
	}
	if att[0].Obj != cutterID {
		t.Fatalf("attach Obj = %d, want %d (the Equipment itself, Object$ absent = Self)", att[0].Obj, cutterID)
	}
	if len(att[0].IDs) != 1 || att[0].IDs[0] != monkID {
		t.Fatalf("attach IDs = %v, want [%d] (the Monk token)", att[0].IDs, monkID)
	}
	if e.G.Obj(cutterID).AttachedTo != monkID {
		t.Fatalf("Cutter AttachedTo = %d, want the Monk token %d", e.G.Obj(cutterID).AttachedTo, monkID)
	}
	replayCheck(t, e, cfg)
}
