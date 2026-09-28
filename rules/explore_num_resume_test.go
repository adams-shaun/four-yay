package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestExploreNumSurvivesTheDestinationAsk pins the round-10 cardfuzz mirror
// livelock (seed 12282246443002902452, Jadelight Spelunker: "When this
// enters, it explores X times", X = 6; the cycle note / choose / counter /
// explore repeated until the livelock detector fired). Every nonland explore
// suspends the resolution on its CR 701.35a "put the card back or put it into
// your graveyard" election, and the resumed effExplore restarted its Num$
// loop at zero: each resume applied the pending explore and then posed a new
// one, so a creature told to explore N times never stopped while its reveals
// were nonland -- forever once the card was put back on top. The resume point
// now carries how many explores were already done (Ctx.ExploreCount), so the
// explorer explores exactly Num$ times whatever it answers.
func TestExploreNumSurvivesTheDestinationAsk(t *testing.T) {
	t.Parallel()
	const bear = "Name:Filler Bear\nManaCost:1\nTypes:Creature Bear\nPT:1/1\nOracle:x\n"
	for _, tc := range []struct {
		name   string
		answer string // the option kind answered at every election
	}{
		{"put_back_on_top", "top"},
		{"into_the_graveyard", "graveyard"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			extras := make([]string, 16)
			for i := range extras {
				extras[i] = bear
			}
			e, _, _ := newFixtureDeck(t, 11282, "Name:Plains\nTypes:Basic Land Plains\nOracle:x\n", extras...)
			toMain1(t, e)
			explorer := onBoard(t, e, 0, "Name:Thrice Explorer\nManaCost:G\nTypes:Creature Merfolk\nPT:1/1\n"+
				"A:AB$ Explore | Cost$ 0 | Num$ 3 | SpellDescription$ x\nOracle:x\n")
			e.G.Obj(explorer).SummonSick = false
			top := arrangeLibraryTop(t, e, "Filler Bear", "Filler Bear", "Filler Bear", "Filler Bear")
			e.pending = nil
			e.askPriority(0)
			d := e.Pending()
			idx := -1
			for _, o := range d.Options {
				if o.Kind == "ability" && o.Obj == explorer {
					idx = o.Index
				}
			}
			if idx < 0 {
				t.Fatalf("no activate option for the explorer: %+v", d.Options)
			}
			submitChoices(t, e, idx)

			asks := 0
			for {
				d := passUntilAsk(t, e)
				if d.Kind != decision.KChoose || d.ResumeKind != "explore" {
					t.Fatalf("unexpected ask %+v", d)
				}
				asks++
				if asks > 3 {
					t.Fatalf("explore election %d for a Num$ 3 explore: the resumed loop restarted its count", asks)
				}
				pick := -1
				for _, o := range d.Options {
					if o.Kind == tc.answer {
						pick = o.Index
					}
				}
				submitChoices(t, e, pick)
				if len(exploreRecords(e)) == 3 {
					break
				}
			}
			if got := counterOn(t, e, explorer, "P1P1"); got != 3 {
				t.Fatalf("explorer P1P1 counters = %d, want 3", got)
			}
			// No fourth election follows: the next ask is priority (or the
			// stack is empty), never another explore.
			if d := e.Pending(); d != nil && d.ResumeKind == "explore" {
				t.Fatalf("a fourth explore election %+v", d)
			}
			lib := e.G.Zone(state.ZLibrary, 0)
			switch tc.answer {
			case "top":
				if lib[0] != top[0] {
					t.Fatalf("library top = %d, want the put-back bear %d", lib[0], top[0])
				}
			case "graveyard":
				for _, id := range top[:3] {
					if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
						t.Fatalf("revealed bear %d in zone %s, want the graveyard", id, z)
					}
				}
				if lib[0] != top[3] {
					t.Fatalf("library top = %d, want the unrevealed fourth bear %d", lib[0], top[3])
				}
			}
			_ = strings.TrimSpace
		})
	}
}
