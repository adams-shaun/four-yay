package effects

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestPlayerCountRememberedValidCountsDistinctPlayers(t *testing.T) {
	g := state.NewGame(names(4))
	card := mkCard(t, "Name:Remembered creature\nTypes:Creature\nPT:1/1\nOracle:x\n")
	obj := g.AddObject(card, 1).ID
	h := &fakeHost{g: g}
	c := &Ctx{Controller: 0, Remembered: []state.Target{
		{Player: 1, IsPlayer: true},
		{Player: 1, IsPlayer: true}, // duplicate player: count once
		{Player: 3, IsPlayer: true},
		{Obj: obj}, // remembered cards are not players
	}}
	if g.Obj(obj) == nil || !c.Remembered[0].IsPlayer || c.Remembered[0].Player == c.Remembered[2].Player {
		t.Fatal("fixture must contain a live remembered object and two distinct remembered players")
	}
	got, ok := EvalCountOK(h, c, "Count$PlayerCountRemembered$Valid")
	if !ok {
		t.Fatal("PlayerCountRemembered$Valid reported UNRESOLVED")
	}
	if got != 2 {
		t.Fatalf("PlayerCountRemembered$Valid = %d, want 2 distinct remembered players", got)
	}
}
