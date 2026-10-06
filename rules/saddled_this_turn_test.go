package rules

// TestSaddledThisTurnProvenance (Level B D8): cards that read "a creature that
// saddled it this turn" (Creature.SaddledThisTurn) or "each creature that
// crewed it this turn" (Count$CrewSize) need the engine to remember WHO paid
// the Saddle or Crew cost. events.Apply's Saddle fold records the
// saddler-to-Mount pairing in the same per-turn list the Crew fold keeps, and
// the SaddledThisTurn predicate / CrewSize head read it, source-relative.

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// d8PayTapElection pays the pending Saddle/Crew tap election (minSum) of id's
// ability 0 with payers, then resolves the stack.
func d8PayTapElection(t *testing.T, e *Engine, id state.ObjID, minSum int, payers ...state.ObjID) {
	t.Helper()
	d := saddleElection(t, e, id, minSum)
	var choices []int
	for _, p := range payers {
		for _, o := range d.Options {
			if o.Obj == p {
				choices = append(choices, o.Index)
			}
		}
	}
	if len(choices) != len(payers) {
		t.Fatalf("tap election offered %d of %d payers: %+v", len(choices), len(payers), d.Options)
	}
	submitChoices(t, e, choices...)
	passUntilStackEmpty(t, e, 20)
	for _, p := range payers {
		if !e.G.Obj(p).Tapped {
			t.Fatalf("precondition: payer %d was not tapped as the cost", p)
		}
	}
}

// d8Attack moves to declare attackers and attacks with id; the attack trigger
// is left pending / on the stack.
func d8Attack(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	driveToStep(t, e, e.G.Turn, 0, state.StepDeclareAttackers)
	d := e.Pending()
	if d == nil || d.Kind != decision.KAttackers {
		t.Fatalf("attacker declaration = %+v, want KAttackers", d)
	}
	attack := -1
	for _, o := range d.Options {
		if o.Obj == id {
			attack = o.Index
		}
	}
	if attack < 0 {
		t.Fatalf("precondition: %d is not offered as an attacker", id)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{attack}}); err != nil {
		t.Fatalf("declare attacker: %v", err)
	}
}

// d8Answer answers every pending decision with its first option (preferring
// one that names pick, when nonzero) until the stack is empty.
func d8Answer(t *testing.T, e *Engine, pick state.ObjID) {
	t.Helper()
	for i := 0; i < 40; i++ {
		d := e.Pending()
		if d == nil {
			if len(e.G.Stack) == 0 {
				return
			}
			passUntilStackEmpty(t, e, 1)
			continue
		}
		if d.Kind == decision.KPriority {
			if len(e.G.Stack) == 0 {
				return
			}
			passUntilStackEmpty(t, e, 1)
			continue
		}
		if d.Kind == decision.KAttackers || d.Kind == decision.KBlockers {
			return
		}
		t.Logf("answering %v %q min=%d opts=%+v pick=%d", d.Kind, d.Prompt, d.Min, d.Options, pick)
		idx := []int{}
		for _, o := range d.Options {
			if pick != 0 && o.Obj == pick {
				idx = []int{o.Index}
			}
		}
		if len(idx) == 0 && len(d.Options) > 0 && d.Min > 0 {
			idx = []int{d.Options[0].Index}
		}
		submitChoices(t, e, idx...)
	}
	t.Fatalf("decisions did not settle: %+v", e.Pending())
}

func d8BattlefieldNamed(e *Engine, name string) []state.ObjID {
	var out []state.ObjID
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Zone == state.ZBattlefield && o.Face() != nil && o.Face().Name == name {
			out = append(out, o.ID)
		}
	}
	return out
}

func TestSaddledThisTurnProvenance(t *testing.T) {
	reg := testutil.CorpusRegistry(t)

	t.Run("CalamityCopiesItsSaddlerTwice", func(t *testing.T) {
		t.Parallel()
		calamity := lookup(t, reg, "Calamity, Galloping Inferno")
		e := corpusEngine(t, reg, append([]*cards.Card{calamity}, saddleExtraCards(t, reg)...), nil)
		cal := moveByName(t, e, 0, "Calamity, Galloping Inferno", state.ZBattlefield)
		e.G.Obj(cal).SummonSick = false
		b1, b2 := saddleBears(t, e)
		if n := len(d8BattlefieldNamed(e, "Runeclaw Bear")); n != 2 {
			t.Fatalf("precondition: %d bears on the battlefield, want 2", n)
		}
		// Only b1 saddles; b2 stays untapped and must NOT be a copy candidate.
		d8PayTapElection(t, e, cal, 1, b1)
		if e.G.Obj(b2).Tapped {
			t.Fatal("precondition: the second bear must stay untapped")
		}
		if !effects.MatchesObjectCtx(e.G, "Creature.nonLegendary+SaddledThisTurn", e.G.Obj(b1), effects.SpecContext{Source: cal}) {
			t.Fatal("the saddler does not match Creature.SaddledThisTurn")
		}
		if effects.MatchesObjectCtx(e.G, "Creature.nonLegendary+SaddledThisTurn", e.G.Obj(b2), effects.SpecContext{Source: cal}) {
			t.Fatal("a creature that did not saddle matches Creature.SaddledThisTurn")
		}
		d8Attack(t, e, cal)
		if len(e.G.Stack) == 0 {
			t.Fatal("precondition: the saddled attack trigger did not go on the stack")
		}
		d8Answer(t, e, b1)
		bears := d8BattlefieldNamed(e, "Runeclaw Bear")
		tokens := 0
		for _, id := range bears {
			o := e.G.Obj(id)
			if o.IsToken {
				tokens++
				if !o.Tapped {
					t.Errorf("token %d must enter tapped", id)
				}
			}
		}
		d8DumpNotes(t, e)
		if tokens != 2 {
			t.Fatalf("Calamity made %d Runeclaw Bear tokens, want 2 (battlefield %v)", tokens, bears)
		}
	})

	t.Run("RamblingPossumReturnsItsSaddler", func(t *testing.T) {
		t.Parallel()
		possum := lookup(t, reg, "Rambling Possum")
		e := corpusEngine(t, reg, append([]*cards.Card{possum}, saddleExtraCards(t, reg)...), nil)
		pos := moveByName(t, e, 0, "Rambling Possum", state.ZBattlefield)
		e.G.Obj(pos).SummonSick = false
		b1, b2 := saddleBears(t, e)
		d8PayTapElection(t, e, pos, 1, b1)
		d8Attack(t, e, pos)
		if len(e.G.Stack) == 0 {
			t.Fatal("precondition: the saddled attack trigger did not go on the stack")
		}
		d8Answer(t, e, b1)
		if z := e.G.Obj(b1).Zone; z != state.ZHand {
			t.Fatalf("the saddler is in zone %v, want its owner's hand", z)
		}
		if z := e.G.Obj(b2).Zone; z != state.ZBattlefield {
			t.Fatalf("the non-saddler bear is in zone %v, want the battlefield", z)
		}
	})

	t.Run("LuxuriousLocomotiveTreasurePerCrewer", func(t *testing.T) {
		t.Parallel()
		loco := lookup(t, reg, "Luxurious Locomotive")
		e := corpusEngine(t, reg, append([]*cards.Card{loco}, saddleExtraCards(t, reg)...), nil)
		lo := moveByName(t, e, 0, "Luxurious Locomotive", state.ZBattlefield)
		e.G.Obj(lo).SummonSick = false
		b1, b2 := saddleBears(t, e)
		d8PayTapElection(t, e, lo, 1, b1)
		if e.G.Obj(b2).Tapped {
			t.Fatal("precondition: the second bear must stay untapped")
		}
		if !e.IsCreature(lo) {
			t.Fatal("precondition: the crewed Locomotive is not a creature")
		}
		d8Attack(t, e, lo)
		if len(e.G.Stack) == 0 {
			t.Fatal("precondition: the attack trigger did not go on the stack")
		}
		d8Answer(t, e, 0)
		d8DumpNotes(t, e)
		if n := tokensNamed(e, 0, "Treasure"); n != 1 {
			t.Fatalf("Locomotive made %d Treasures, want 1 (one crewer)", n)
		}
	})
}

func d8DumpNotes(t *testing.T, e *Engine) {
	for _, ev := range e.L.Events {
		if ev.Kind.String() == "note" || ev.Text != "" {
			t.Logf("%v %q", ev.Kind, ev.Text)
		}
	}
}
