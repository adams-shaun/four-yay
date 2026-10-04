package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestEntryCounterStageReplacementBodyTwoGrants restages a replacement
// body's nested entry: the fetched creature carries TWO absorbable
// PutCounter|ETB$ True grants, each competing on its own under Hardened
// Scales and Branching Evolution, so the first grant's answer poses the
// second grant's order through a fresh stage. That stage must keep the
// entry's in-body provenance: its final re-emit happens while the answer
// runs (applyingReplacement false), and without the guard the entry is
// re-matched by the very replacement whose body emitted it -- the body's
// GainLife runs twice. Both grants answered the same way: 4+4 or 3+3.
func TestEntryCounterStageReplacementBodyTwoGrants(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		pick int
		want int32
	}{{"scales-first", 0, 8}, {"evolution-first", 1, 6}} {
		t.Run(tc.name, func(t *testing.T) {
			scales := tokenReplCorpusCard(t, "Hardened Scales")
			evolution := tokenReplCorpusCard(t, "Branching Evolution")
			echo := card(t, "Name:Entry Echo\nTypes:Enchantment\n"+
				"R:Event$ Moved | ActiveZones$ Battlefield | ValidCard$ Creature.Bear+YouOwn,Creature.Elf+YouOwn | Destination$ Battlefield | ReplaceWith$ Echo | ReplacementResult$ Updated | Description$ echo\n"+
				"SVar:Echo:DB$ GainLife | Defined$ You | LifeAmount$ 1 | SubAbility$ Fetch\n"+
				"SVar:Fetch:DB$ ChangeZoneAll | ChangeType$ Creature.Elf+YouOwn | Origin$ Graveyard | Destination$ Battlefield\nOracle:x\n")
			bear := card(t, "Name:Echo Starter\nManaCost:0\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
			fetched := card(t, "Name:Twice Graced\nTypes:Creature Elf\nPT:1/1\n"+
				"R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ GraceA | ReplacementResult$ Updated | Description$ first grace\n"+
				"R:Event$ Moved | ValidCard$ Card.Self | Destination$ Battlefield | ReplaceWith$ GraceB | ReplacementResult$ Updated | Description$ second grace\n"+
				"SVar:GraceA:DB$ PutCounter | Defined$ Self | CounterType$ P1P1 | CounterNum$ 1 | ETB$ True\n"+
				"SVar:GraceB:DB$ PutCounter | Defined$ Self | CounterType$ P1P1 | CounterNum$ 1 | ETB$ True\nOracle:x\n")
			e, cfg := tokenReplGame(t, 1005, scales, evolution, echo, bear, fetched)
			for _, c := range []*cards.Card{scales, evolution, echo} {
				if o := e.G.Obj(moveSeededCard(t, e, 0, c, state.ZBattlefield)); o == nil || o.Zone != state.ZBattlefield {
					t.Fatal("precondition: a replacement source is absent")
				}
			}
			e.SetCounterAdder(0)
			fetchedID := moveSeededCard(t, e, 0, fetched, state.ZGraveyard)
			bearID := moveSeededCard(t, e, 0, bear, state.ZHand)
			if o := e.G.Obj(fetchedID); o == nil || o.Zone != state.ZGraveyard {
				t.Fatalf("precondition: fetched creature not in the graveyard: %+v", o)
			}
			toMain1(t, e)
			e.priorityRound()
			castSpellOption(t, e, "Echo Starter")
			life := e.G.Players[0].Life
			asks := 0
			for i := 0; i < 30; i++ {
				d := e.Pending()
				if d == nil {
					t.Fatal("no decision while resolving")
				}
				if d.Kind == decision.KReplacement {
					asks++
					if o := e.G.Obj(fetchedID); o == nil || o.Zone != state.ZGraveyard {
						t.Fatalf("ask %d: nested entry folded before its order answers: %+v", asks, o)
					}
					if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{tc.pick}}); err != nil {
						t.Fatal(err)
					}
					continue
				}
				if d.Kind != decision.KPriority {
					t.Fatalf("unexpected decision %+v", d)
				}
				if len(e.G.Stack) == 0 {
					break
				}
				passPriorityOnce(t, e)
			}
			if asks != 2 {
				t.Fatalf("answered %d order asks, want 2 (one per competing grant)", asks)
			}
			if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZBattlefield {
				t.Fatalf("starter = %+v, want it on the battlefield", o)
			}
			o := e.G.Obj(fetchedID)
			if o == nil || o.Zone != state.ZBattlefield {
				t.Fatalf("fetched creature = %+v, want it on the battlefield", o)
			}
			if got := o.Counter("P1P1"); got != tc.want {
				t.Fatalf("fetched creature counters = %d, want %d", got, tc.want)
			}
			if n := countMoves(e.L.Events, fetchedID, state.ZBattlefield); n != 1 {
				t.Fatalf("nested entry logged %d battlefield moves, want exactly 1", n)
			}
			if got := e.G.Players[0].Life; got != life+1 {
				t.Fatalf("Entry Echo's body life = %d, want %d: the body ran %d times, want once (the restaged entry re-matched its own replacement)",
					got, life+1, got-life)
			}
			replayCheck(t, e, cfg)
		})
	}
}
