package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func TestBotObstinateFamiliarAIHandling(t *testing.T) {
	for _, tc := range []struct {
		name        string
		librarySize int32
		want        int
	}{
		{name: "library nonempty declines skip", librarySize: 3, want: 1},
		{name: "library empty applies skip", librarySize: 0, want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := Board{
				LibrarySize: tc.librarySize,
				Cards: TableOf(map[state.ObjID]Card{
					743: {PrintedName: obstinateFamiliarName, OnBattlefield: true},
				}),
			}
			d := &decision.Decision{
				Player: 0, Kind: decision.KReplacement, Min: 1, Max: 1,
				Options: []decision.Option{
					{Index: 0, Kind: "apply", Obj: 743, Label: "Yes — skip that draw"},
					{Index: 1, Kind: "decline", Obj: 743, Label: "No — draw the card"},
				},
			}
			// The AIHandling count reads the deciding player's own library;
			// assert the board value under test really differs across cases.
			if !b.Cards.Get(743).OnBattlefield || b.LibrarySize != tc.librarySize {
				t.Fatalf("test setup: Familiar battlefield=%t library=%d, want library=%d", b.Cards.Get(743).OnBattlefield, b.LibrarySize, tc.librarySize)
			}
			if len(d.Options) != 2 || d.Options[0].Kind != "apply" || d.Options[1].Kind != "decline" || d.Options[0].Index == d.Options[1].Index || d.Options[0].Obj != d.Options[1].Obj {
				t.Fatalf("test setup: options do not form distinct apply/decline choices for one source: %+v", d.Options)
			}
			if (tc.librarySize == 0) != (tc.want == d.Options[0].Index) {
				t.Fatalf("test setup: library count %d does not match expected EQ0 choice %d", tc.librarySize, tc.want)
			}
			in := Decide(b, d, rng(1))
			if len(in.Choices) != 1 || in.Choices[0] != tc.want {
				t.Fatalf("choices = %v, want [%d]", in.Choices, tc.want)
			}
			if err := d.Validate(in); err != nil {
				t.Fatalf("bot answer %v rejected by Decision.Validate: %v", in.Choices, err)
			}
		})
	}
}
