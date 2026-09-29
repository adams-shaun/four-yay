package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestNoCallLiteralBranchRunsOncePerOutcome(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	const src = "Name:Test Literal Flip Turns\nManaCost:2 R\nTypes:Sorcery\n" +
		"A:AB$ FlipCoin | Cost$ 0 | Amount$ 5 | NoCall$ True | HeadsSubAbility$ DBAddTurn\n" +
		"SVar:DBAddTurn:DB$ AddTurn | Defined$ You | NumTurns$ 1\n" +
		"Oracle:Flip five coins. Take an extra turn for each head.\n"
	c, diags := cards.ParseBytes("flip_turns.txt", []byte(src))
	for _, d := range diags {
		t.Fatalf("synthetic card failed to parse: %v", d)
	}
	e, _ := corpusEngineCfg(t, reg, []*cards.Card{c}, nil)
	id := moveByName(t, e, 0, "Test Literal Flip Turns", state.ZBattlefield)
	if e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatal("precondition: synthetic card must be on the battlefield")
	}
	e.beginActivation(0, skipTurnAbilityOption(t, e, 0, id, 0))
	passUntilStackEmpty(t, e, 60)

	heads := countFlipHeads(e, 0)
	if heads < 2 || heads > 5 {
		t.Fatalf("precondition: got %d heads, want 2..5 so one-call-per-head differs from one total grant", heads)
	}
	grants := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.ExtraTurn && ev.Amount > 0 && ev.Player == 0 {
			grants += int(ev.Amount)
		}
	}
	if grants != heads || e.G.ExtraTurns[0] != heads {
		t.Fatalf("literal NoCall branch granted %d extra turns (state %d), want one per head (%d)", grants, e.G.ExtraTurns[0], heads)
	}
}
