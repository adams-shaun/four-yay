package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestMillReplacementOrderComposesBothWays(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		name string
		pick state.ObjID
		want int
	}{
		{"Water Crystal then Bruvac", 0, 12},
		{"Bruvac then Water Crystal", 1, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := millTriggerEngine(t)
			waterID := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "The Water Crystal"))
			bruvacID := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Bruvac the Grandiloquent"))
			for _, id := range []state.ObjID{waterID, bruvacID} {
				if o := e.G.Obj(id); o == nil || o.Zone != state.ZBattlefield {
					t.Fatalf("precondition: replacement source %d must be on battlefield", id)
				}
			}
			if len(e.G.Zone(state.ZLibrary, 1)) < 12 {
				t.Fatal("precondition: opponent library must hold all twelve possible mill cards")
			}
			spell := e.G.AddObject(card(t, "Name:Mill Two\nTypes:Sorcery\n"+
				"A:SP$ Mill | NumCards$ 2 | Defined$ Opponent\nOracle:x\n"), 0)
			spell.Zone = state.ZStack
			e.G.SetZone(state.ZStack, 0, []state.ObjID{spell.ID})
			e.G.Stack = []state.ObjID{spell.ID}
			e.pending = nil
			e.resolveTop()
			d := e.Pending()
			if d == nil || d.Kind != decision.KReplacement || len(d.Options) != 2 {
				t.Fatalf("pending = %+v, want integrated two-option replacement decision", d)
			}
			first := waterID
			if tc.pick == 1 {
				first = bruvacID
			}
			if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{optionFor(t, d, first)}}); err != nil {
				t.Fatal(err)
			}
			if pending := e.Pending(); pending != nil && pending.Kind == decision.KReplacement {
				t.Fatalf("unexpected second replacement decision after recheck: %+v", pending)
			}
			if got := millEventCount(e); got != tc.want {
				t.Fatalf("completed per-card mill events = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestMillUnsupportedReplacementIsNotSilent(t *testing.T) {
	t.Parallel()
	for _, mixed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unsupported only", true: "mixed"}[mixed], func(t *testing.T) {
			e, _ := millTriggerEngine(t)
			unsupportedID := onBoard(t, e, 0, "Name:Unsupported Mill\nTypes:Enchantment\n"+
				"R:Event$ Mill | ActiveZones$ Battlefield | ValidPlayer$ Player.Opponent | ReplaceWith$ Bad\n"+
				"SVar:Bad:DB$ GainLife | LifeAmount$ 2\nOracle:x\n")
			unsupported := e.G.Obj(unsupportedID)
			matches := []replMatch{{id: unsupportedID, repl: &unsupported.Face().Repls[0]}}
			amount := int32(2)
			if mixed {
				bruvacID := onBoardCard(t, e, 0, mustCorpusCard(t, testutil.CorpusRegistry(t), "Bruvac the Grandiloquent"))
				bruvac := e.G.Obj(bruvacID)
				matches = append(matches, replMatch{id: bruvacID, repl: &bruvac.Face().Repls[0]})
				amount = 4
			}
			ev := events.Event{Kind: events.MillProposal, Player: 1, Amount: 2}
			got, handled := e.continueMillReplacements(ev, matches)
			if !handled || got.Amount != amount {
				t.Fatalf("proposal=(%+v,%v), want amount %d and handled", got, handled, amount)
			}
			notes := 0
			for _, logged := range e.L.Events {
				if logged.Kind == events.Note && logged.Text == "unimplemented Mill replacement" && logged.Obj == unsupportedID {
					notes++
				}
			}
			if notes != 1 {
				t.Fatalf("unsupported-body notes = %d, want exactly one", notes)
			}
		})
	}
}
