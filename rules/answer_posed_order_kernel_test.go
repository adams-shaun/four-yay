package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Restored from the W3 legacy removal: an inner replacement-order
// competition posed from inside the answer to an outer in-resolution one
// resumes the resolution exactly once (life and Updated-entry shapes).

// TestAnswerPosedLifeOrderResumesTheResolution: a resolving sorcery gains
// life while two doublers and one adder all apply. The first life order
// competition is posed in-resolution; its answer's re-drive sees the
// remaining Twice/Plus pair and poses an inner replChoiceLife from inside the
// answer. Answering the inner ask must resume the suspended sorcery exactly
// once. Both inner answer orders.
func TestAnswerPosedLifeOrderResumesTheResolution(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		pick int
	}{{"twice-first", 0}, {"plus-first", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			a := card(t, lifeReplSrc("Life Doubler A", "Twice"))
			b := card(t, lifeReplSrc("Life Doubler B", "Twice"))
			c := card(t, lifeReplSrc("Life Adder", "Plus.1"))
			spell := card(t, "Name:Answer Posed Life\nManaCost:0\nTypes:Sorcery\n"+
				"A:SP$ GainLife | LifeAmount$ 3 | SubAbility$ Rider | SpellDescription$ x\n"+
				"SVar:Rider:DB$ Draw | NumCards$ 1\nOracle:x\n")
			e, cfg := tokenReplGame(t, 9301, a, b, c, spell)
			for _, cd := range []*cards.Card{a, b, c} {
				if o := e.G.Obj(moveSeededCard(t, e, 0, cd, state.ZBattlefield)); o == nil || o.Zone != state.ZBattlefield {
					t.Fatalf("precondition: replacement source %q absent", cd.Faces[0].Name)
				}
			}
			moveSeededCard(t, e, 0, spell, state.ZHand)
			addMana(t, e, 0, "")
			castSpellOption(t, e, "Answer Posed Life")
			life := e.G.Players[0].Life
			tasks := 0
			for i := 0; i < 60; i++ {
				d := e.Pending()
				if d == nil {
					t.Fatal("no decision while resolving")
				}
				pick := 0
				switch d.Kind {
				case decision.KPriority:
					if len(e.G.Stack) == 0 {
						i = 60
						continue
					}
					passPriorityOnce(t, e)
					continue
				case decision.KReplacement:
					tasks++
					if tc.pick < len(d.Options) {
						pick = tc.pick
					}
				default:
					t.Fatalf("unexpected decision %+v", d)
				}
				if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
					t.Fatal(err)
				}
			}
			if tasks != 2 {
				t.Fatalf("asked %d life order choices, want 2 (outer + inner)", tasks)
			}
			if len(e.G.Stack) != 0 {
				t.Fatalf("the sorcery never left the stack (depth %d)", len(e.G.Stack))
			}
			// Precondition: the replacements actually applied -- the two gains
			// (3 and then 1) are strictly larger than their printed amounts.
			if got := e.G.Players[0].Life; got <= life+4 {
				t.Fatalf("life = %d, want more than %d (the replacements did not apply)", got, life+4)
			}
			resolves := 0
			for _, ev := range e.L.Events {
				if ev.Kind == events.Resolve {
					resolves++
				}
			}
			if resolves != 1 {
				t.Fatalf("the sorcery resolved %d times, want exactly 1", resolves)
			}
			replayCheck(t, e, cfg)
		})
	}
}

// TestAnswerPosedUpdatedOrderResumesTheResolution: a creature spell enters
// while three all-Updated entry replacements (tap, untap, tap) compete. The
// first Updated order competition is posed in-resolution; its answer's
// re-drive sees the remaining tap/untap pair and poses an inner
// replChoiceUpdated from inside the answer. Answering the inner ask must
// resume the suspended creature spell exactly once. Both inner answer orders.
func TestAnswerPosedUpdatedOrderResumesTheResolution(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		pick int
	}{{"tap-first", 0}, {"untap-first", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			t1 := card(t, entryReplSrc("Entry Tap One", "Tap"))
			t2 := card(t, entryReplSrc("Entry Untap One", "Untap"))
			t3 := card(t, entryReplSrc("Entry Tap Two", "Tap"))
			t4 := card(t, entryReplSrc("Entry Untap Two", "Untap"))
			creature := card(t, "Name:Answer Posed Entrant\nManaCost:0\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
			e, cfg := tokenReplGame(t, 9401, t1, t2, t3, t4, creature)
			for _, cd := range []*cards.Card{t1, t2, t3, t4} {
				if o := e.G.Obj(moveSeededCard(t, e, 0, cd, state.ZBattlefield)); o == nil || o.Zone != state.ZBattlefield {
					t.Fatalf("precondition: replacement source %q absent", cd.Faces[0].Name)
				}
			}
			moveSeededCard(t, e, 0, creature, state.ZHand)
			addMana(t, e, 0, "")
			castSpellOption(t, e, "Answer Posed Entrant")
			tasks := 0
			for i := 0; i < 60; i++ {
				d := e.Pending()
				if d == nil {
					t.Fatal("no decision while resolving")
				}
				pick := 0
				switch d.Kind {
				case decision.KPriority:
					if len(e.G.Stack) == 0 {
						i = 60
						continue
					}
					passPriorityOnce(t, e)
					continue
				case decision.KReplacement:
					tasks++
					if tc.pick < len(d.Options) {
						pick = tc.pick
					}
				default:
					t.Fatalf("unexpected decision %+v", d)
				}
				if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
					t.Fatal(err)
				}
			}
			if tasks != 3 {
				t.Fatalf("asked %d entry order choices, want 3 (outer + two inner re-poses)", tasks)
			}
			if len(e.G.Stack) != 0 {
				t.Fatalf("the creature never left the stack (depth %d)", len(e.G.Stack))
			}
			// Precondition: the entrant is on the battlefield.
			entered := false
			for _, id := range e.G.Zone(state.ZBattlefield, 0) {
				if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == "Answer Posed Entrant" {
					entered = true
				}
			}
			if !entered {
				t.Fatal("precondition: the entrant is not on the battlefield")
			}
			resolves := 0
			for _, ev := range e.L.Events {
				if ev.Kind == events.Resolve {
					resolves++
				}
			}
			if resolves != 1 {
				t.Fatalf("the creature resolved %d times, want exactly 1", resolves)
			}
			replayCheck(t, e, cfg)
		})
	}
}
