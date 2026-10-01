package botpolicy

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// TestRepeatableModesFillFromOneOption: CharmNum$ 3 over a single legal
// mode (Mystic Confluence with nothing to counter or bounce) must be
// answered with that mode three times; a single top-up pass stopped at two.
func TestRepeatableModesFillFromOneOption(t *testing.T) {
	d := decision.Decision{Seq: 1, Player: 0, Kind: decision.KModes, Min: 3, Max: 3,
		Repeatable: true, Options: []decision.Option{{Index: 0, Kind: "mode"}}}
	in := Decide(Board{}, &d, rng(1))
	if err := d.Validate(in); err != nil {
		t.Fatalf("repeatable one-mode answer rejected: %v (%+v)", err, in)
	}
}

// TestLegalBlockChoicesDropsALoneMenaceBlocker: when the affordability
// filter drops a tap-costed second blocker, the Menace attacker's remaining
// lone blocker is dropped too -- whether the floor comes from the option's
// published MinBlockers or only from the Board's keyword census.
func TestLegalBlockChoicesDropsALoneMenaceBlocker(t *testing.T) {
	for _, tc := range []struct {
		name      string
		published int
		keywords  []string
	}{
		{name: "published floor", published: 2},
		{name: "board keyword", keywords: []string{"Menace"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &decision.Decision{Seq: 1, Player: 0, Kind: decision.KBlockers, Min: 0, Max: 2,
				Options: []decision.Option{
					{Index: 0, Kind: "block", Obj: 10, Attacker: 20, MinBlockers: tc.published},
					{Index: 1, Kind: "block", Obj: 11, Attacker: 20, MinBlockers: tc.published, CostTaps: 1},
				}}
			b := Board{Creatures: TableOf(map[state.ObjID]Creature{20: {Power: 3, Toughness: 2, Keywords: tc.keywords}}),
				Life: TableOf(map[state.PlayerID]int32{0: 20})}
			if got := legalBlockChoices(b, d, []int{0, 1}); len(got) != 0 {
				t.Fatalf("legalBlockChoices = %v, want the lone Menace block dropped", got)
			}
		})
	}
}
