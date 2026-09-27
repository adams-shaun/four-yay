package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestCommanderCastReplayReconstructsGenesisBookkeeping(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	ragavan, ok := reg.Lookup("Ragavan, Nimble Pilferer")
	if !ok {
		t.Fatal("registry lacks Ragavan")
	}
	cfg := seatZeroStart(Config{
		Seed: 913, Names: []string{"a", "b"}, Format: FormatCommander,
		StartingLife: 40,
		Decks: [][]*cards.Card{
			append([]*cards.Card{ragavan}, mountainDeck(t, 39)...),
			mountainDeck(t, 40),
		},
		Commanders: [][]int{{0}, nil}, Tokens: reg.Tokens,
	})
	e := New(cfg)
	e.Advance()
	driveToStep(t, e, 1, 0, state.StepMain1)
	id := e.G.Players[0].Commanders[0]
	if e.G.Obj(id).Zone != state.ZCommand {
		t.Fatalf("precondition: Ragavan in %s, want command zone", e.G.Obj(id).Zone)
	}
	if got := e.G.Players[0].Life; got != 40 {
		t.Fatalf("precondition: starting life = %d, want 40", got)
	}
	addMana(t, e, 0, "R")
	submitChoices(t, e, castModeOption(t, e, id, ""))
	passUntilStackEmpty(t, e, 40)
	if e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatalf("cast precondition: Ragavan in %s, want battlefield", e.G.Obj(id).Zone)
	}
	if got := e.G.Players[0].CmdCasts[0]; got != 1 {
		t.Fatalf("cast precondition: live CmdCasts = %v, want [1]", e.G.Players[0].CmdCasts)
	}
	placed, cast := false, false
	for _, ev := range e.L.Events {
		placed = placed || ev.Kind == events.MoveZone && ev.Obj == id && ev.From == state.ZLibrary && ev.To == state.ZCommand
		cast = cast || ev.Kind == events.PutOnStack && ev.Obj == id && ev.From == state.ZCommand
	}
	if !placed || !cast {
		t.Fatalf("precondition: commander placement logged=%v, command-zone cast logged=%v", placed, cast)
	}
	replayCheck(t, e, cfg)
}
