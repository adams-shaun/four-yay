package decision_test

import (
	"math/rand/v2"
	"testing"

	"github.com/adams-shaun/gorge/botpolicy"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

// TestValidateMatchesMapReferenceOnRealDecisions holds the map-free
// Validate to the map-based reference over decisions real games pose:
// bot-played games across the pinned Legacy decks at 2 and 4 seats, every
// pending decision validated against the bot's own answer and a spread of
// mutated answers (decision.AnswerVariants), verdict and error text both.
func TestValidateMatchesMapReferenceOnRealDecisions(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	names := testutil.LegacyDeckNames()
	r := rand.New(rand.NewPCG(7, 11))
	checked := map[decision.Kind]int{}
	for g := 0; g < 6; g++ {
		seats := 2 + 2*(g%2)
		cfg := rules.Config{Seed: uint64(4100 + g), Tokens: reg.Tokens}
		for s := 0; s < seats; s++ {
			name := names[(g*3+s)%len(names)]
			cfg.Names = append(cfg.Names, name)
			cfg.Decks = append(cfg.Decks, testutil.RepoDeck(t, reg, name))
		}
		e := rules.New(cfg)
		e.Advance()
		bots := make([]*rand.Rand, seats)
		for s := range bots {
			bots[s] = rand.New(rand.NewPCG(uint64(g), uint64(s)))
		}
		for step := 0; step < 1500 && !e.G.Over && e.Pending() != nil; step++ {
			d := e.Pending()
			in := botpolicy.Decide(botpolicy.BoardFromGame(e.G, e, d.Player), d, bots[d.Player])
			if err := decision.CheckValidateEquivalent(d, decision.AnswerVariants(d, in, r, 8)); err != nil {
				t.Fatalf("game %d step %d: %v", g, step, err)
			}
			checked[d.Kind]++
			if err := e.Submit(in); err != nil {
				t.Fatalf("game %d step %d: %v", g, step, err)
			}
		}
	}
	if checked[decision.KPriority] == 0 || checked[decision.KAttackers] == 0 || checked[decision.KBlockers] == 0 {
		t.Fatalf("real games posed too narrow a decision mix: %v", checked)
	}
	t.Logf("decisions checked by kind: %v", checked)
}
