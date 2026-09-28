package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Regressions for the cardfuzz batch10 livelock (all real corpus cards).

// TestAbortedCastDoesNotRefireAttackerUnblocked (cardfuzz batch10 line 1):
// Senu, Keen-Eyed Protector sits in exile with "when a legendary creature you
// control attacks and isn't blocked, put it onto the battlefield attacking"
// (Mode$ AttackerUnblocked). After the declare-blockers round completed, its
// controller cast Convoke Devouring Light and announced too few creatures, so
// the cast aborted "cost no longer payable" (CR 733.1). No handler re-grants
// priority after an abort, so the Advance loop re-entered step()'s
// StepDeclareBlockers arm, which re-ran the round-complete walk and queued
// Senu's trigger AGAIN -- a second "isn't blocked" event from one combat (CR
// 509.2 fires it once). Its TriggerPush was a genuine state change, so it
// cleared the F05-2 held-out suppression and the same abort re-offered
// forever, stacking one Senu trigger per attempt. The walk is now latched to
// one run per combat: the stack keeps its single trigger across both aborts,
// and the second identical abort holds the cast out of the window.
func TestAbortedCastDoesNotRefireAttackerUnblocked(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	zones := []struct {
		name string
		to   state.Zone
	}{
		{"Senu, Keen-Eyed Protector", state.ZExile},
		{"Yoshimaru, Ever Faithful", state.ZBattlefield},
		{"Elite Vanguard", state.ZBattlefield},
		{"Elite Vanguard", state.ZBattlefield},
		{"Elite Vanguard", state.ZBattlefield},
		{"Devouring Light", state.ZHand},
	}
	var deck0 []*cards.Card
	for _, z := range zones {
		deck0 = append(deck0, mustCorpusCard(t, reg, z.name))
	}
	bears := mustCorpusCard(t, reg, "Grizzly Bears")
	cfg := Config{Seed: 1010, Names: []string{"a", "b"}, Tokens: reg.Tokens,
		Decks: [][]*cards.Card{
			append(deck0, mountainDeck(t, 40-len(deck0))...),
			append([]*cards.Card{bears}, mountainDeck(t, 39)...),
		}}
	e := New(cfg)
	// Seat 1's untapped Bears make the blockers ask real, so the round is
	// answered (unblocked) rather than skipped.
	for j := range e.G.Objs {
		if o := &e.G.Objs[j]; o.Owner == 1 && o.Card == bears {
			e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: state.ZBattlefield})
			break
		}
	}
	placed := map[state.ObjID]bool{}
	for i, z := range zones {
		for j := range e.G.Objs {
			o := &e.G.Objs[j]
			if o.Owner == 0 && o.Card == deck0[i] && !placed[o.ID] {
				placed[o.ID] = true
				if o.Zone != z.to {
					e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: z.to})
				}
				break
			}
		}
	}
	// A logged TurnChange clears summoning sickness (CR 302.6) the replayable
	// way, then the clock is parked at seat 0's declare-attackers step.
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	yoshimaru := findBattlefield(t, e, 0, "Yoshimaru, Ever Faithful", 0)
	var light state.ObjID
	for _, id := range e.G.Zone(state.ZHand, 0) {
		if e.G.Obj(id).Face().Name == "Devouring Light" {
			light = id
		}
	}
	if light == 0 {
		t.Fatal("Devouring Light is not in seat 0's hand")
	}

	e.askAttackers()
	submitAttackers(t, e, yoshimaru)
	declareUnblocked(t, e)
	if e.G.Step != state.StepDeclareBlockers || len(e.G.Stack) != 1 {
		t.Fatalf("after the unblocked attack: step %v, stack %d, want declare-blockers with Senu's one trigger", e.G.Step, len(e.G.Stack))
	}

	// Two identical no-progress aborts: announce ONE creature of the three a
	// {1}{W}{W} Convoke cost needs, with no mana source to cover the rest.
	for attempt := 1; attempt <= 2; attempt++ {
		if !castOffered(e, light) {
			t.Fatalf("attempt %d: Devouring Light not offered (Convoke with three untapped white Elite Vanguards pays it; the attacking Yoshimaru is tapped)", attempt)
		}
		submitChoices(t, e, castOptionFor(t, e, light).Index)
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose {
			t.Fatalf("attempt %d: expected the Convoke announcement, got %+v", attempt, d)
		}
		submitChoices(t, e, 0)
		if o := e.G.Obj(light); o.Zone != state.ZHand {
			t.Fatalf("attempt %d: Devouring Light in %v, want the aborted cast reversed to hand", attempt, o.Zone)
		}
		if len(e.G.Stack) != 1 {
			t.Fatalf("attempt %d: stack holds %d objects, want only Senu's single trigger (the aborted cast re-fired AttackerUnblocked)", attempt, len(e.G.Stack))
		}
	}
	if castOffered(e, light) {
		t.Fatal("the second identical no-progress abort still re-offers Devouring Light (F05-2 suppression)")
	}
	replayCheck(t, e, cfg)
}
