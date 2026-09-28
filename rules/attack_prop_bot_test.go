package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestAttackPropRequiredBotAnswerNeverLivelocks pins the attackprop1 review's
// MAJOR end to end, through the real bot (botpolicy.Decide + Clamp) and the
// real Submit path: two Valley Dashers ("attacks each combat if able", CR
// 508.1d) on seat 1, Ghostly Prison on seat 0, three untapped Mountains, and
// seat 0 at 2 life. The pair list offers each Dasher at seat 0 for {2}
// (Required) and at seat 2 for free (Required), under a {3} budget. The
// combat heuristic wants both Dashers at the 2-life seat ({4}, over budget).
// Before the fix the engine derived the requirement from each creature's
// CHEAPEST pair (quota 2: both free seat-2 pairs fit) while the bot's
// budget trim dropped a Required pick priced on the DEAR pair, so every
// answer the deterministic bot produced was rejected by Submit and the
// decision was never consumed -- a livelock. Now both sides read one rule
// (decision.RequiredQuota / FitRequired): the bot's answer is accepted, both
// Dashers attack, and the one pair the budget can carry keeps the bot's
// preferred defender.
func TestAttackPropRequiredBotAnswerNeverLivelocks(t *testing.T) {
	t.Parallel()
	cfg := Config{Seed: 716, Names: []string{"a", "b", "c"},
		Decks: [][]*cards.Card{mountainDeck(t, 40), mountainDeck(t, 40), mountainDeck(t, 40)}}
	e := New(cfg)
	onBoardCard(t, e, 0, mshCorpusCard(t, "Ghostly Prison"))
	var dashers []state.ObjID
	for i := 0; i < 2; i++ {
		id := onBoardCard(t, e, 1, mshCorpusCard(t, "Valley Dasher"))
		e.G.Obj(id).SummonSick = false
		dashers = append(dashers, id)
	}
	for i := 0; i < 3; i++ {
		onBoardCard(t, e, 1, card(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n"))
	}
	e.G.Players[0].Life = 2
	e.G.Active = 1
	e.G.Step = state.StepDeclareAttackers
	e.askAttackers()

	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("expected an attackers decision, got %+v", d)
	}
	// Preconditions: the board is the reachable shape the livelock needs --
	// a {3} budget, both Dashers Required, each with a {2} pair at the 2-life
	// seat and a free pair at seat 2. Two dear pairs cannot both fit; two
	// cheap ones can, so the requirement quota is 2.
	if d.MaxSum != 3 {
		t.Fatalf("budget MaxSum = %d, want 3 (three untapped Mountains)", d.MaxSum)
	}
	for _, id := range dashers {
		var dear, free bool
		for _, o := range d.Options {
			if o.Obj != id {
				continue
			}
			if !o.Required {
				t.Fatalf("Dasher pair not marked Required: %+v", o)
			}
			switch {
			case o.Player == 0 && o.Value == 2:
				dear = true
			case o.Player == 2 && o.Value == 0:
				free = true
			}
		}
		if !dear || !free {
			t.Fatalf("Dasher %d lacks the {2}@seat0 / free@seat2 pair shape: %+v", id, d.Options)
		}
	}
	if q := d.RequiredQuota(); q != 2 {
		t.Fatalf("RequiredQuota = %d, want 2", q)
	}

	bot := newTestBot(7)
	for i := 0; i < 16; i++ {
		d := e.Pending()
		if d == nil || e.G.Step != state.StepDeclareAttackers || d.Kind == decision.KPriority {
			break
		}
		in := bot.answer(e, d)
		if err := e.Submit(in); err != nil {
			t.Fatalf("bot's own %v answer %v rejected by the engine (the decision is never consumed: livelock): %v",
				d.Kind, in.Choices, err)
		}
	}
	atSeat0 := 0
	for _, id := range dashers {
		o := e.G.Obj(id)
		if !o.IsAttacking {
			t.Fatalf("required Dasher %d is not attacking", id)
		}
		if o.Attacking == 0 {
			atSeat0++
		}
	}
	if atSeat0 != 1 {
		t.Fatalf("%d Dashers attack the 2-life seat, want exactly the 1 the {3} budget can pay for", atSeat0)
	}
}
