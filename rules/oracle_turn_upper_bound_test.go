package rules

import (
	"encoding/json"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
)

// The accepted upper bound must execute real turns, not just relabel setup.
// Ordinary draw history distinguishes that from a jump straight to turn 100.
func TestOracleScenarioRequestedTurnUpperBound(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	for _, named := range []int{0, 40} {
		t.Run(map[int]string{0: "empty", 40: "named-outside-library"}[named], func(t *testing.T) {
			sc := oracleScenario{Turn: new(int), Setup: map[string]oracleSeat{}}
			*sc.Turn = 100
			for _, seat := range []string{"p0", "p1"} {
				setup := oracleSeat{}
				for i := 0; i < named; i++ {
					setup.Exile = append(setup.Exile, "Wastes")
				}
				sc.Setup[seat] = setup
			}
			data, err := json.Marshal(sc)
			if err != nil {
				t.Fatal(err)
			}
			res, err := RunOracleScenarioJSON(reg, data)
			if err != nil || len(res.Fails) != 0 || len(res.Snapshots) != 1 {
				t.Fatalf("upper-bound setup failed: err=%v fails=%v snapshots=%d", err, res.Fails, len(res.Snapshots))
			}
			snap := res.Snapshots[0]
			if snap.Turn != 100 || snap.Step != "main1" || snap.Active != 1 || snap.Priority != 1 || snap.Over || len(snap.Players) != 2 {
				t.Fatalf("upper-bound checkpoint = %+v, want live turn 100 main1, seat 1 active/priority", snap)
			}
			for seat, p := range snap.Players {
				// Precondition: named cards are still outside the library, so
				// their presence cannot conceal insufficient filler padding.
				if len(p.Exile) != named {
					t.Fatalf("p%d exile = %d, want %d setup cards", seat, len(p.Exile), named)
				}
				// Seat 0 skips the first draw; seat 1 has drawn 50 times.
				// Discards at cleanup move surplus draws into the graveyard.
				draws := 49 + seat
				if got := len(p.Hand) + len(p.Graveyard); got != draws {
					t.Errorf("p%d draw history = %d cards, want %d", seat, got, draws)
				}
				if p.LibraryCount != 51-draws {
					t.Errorf("p%d library = %d, want %d (draw reserve actually consumed)", seat, p.LibraryCount, 51-draws)
				}
			}
		})
	}
}
