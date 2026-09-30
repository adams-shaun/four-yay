package searchbench

import (
	"context"
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/internal/azmcts/clairvoyant"
)

// BenchmarkArms is the compute plan's input: one op is one RunArm of one
// arm at a mid-game (turn >= 6) seat-0 main-phase priority of two 60-card
// repo decks, heuristic leaf, benchmark options. Run it on one core:
//
//	go test ./internal/searchbench/ -run '^$' -bench BenchmarkArms -benchtime 3x -cpu 1
//
// sims/s is completed simulations per second of the op; plies/sim and
// edges/sim are the leaf depth means.
func BenchmarkArms(b *testing.B) {
	clairvoyant.AllowClairvoyant()
	p := newArmPosition(b, armSeed, 6)
	for _, sims := range []int{100, 1000} {
		for _, arm := range []SearchArm{ArmClairvoyant, ArmPIMC1, ArmPIMC4, ArmISMCTS} {
			b.Run(fmt.Sprintf("%s/%d", arm, sims), func(b *testing.B) {
				completed, plies, edges, env := 0, 0.0, 0.0, 0
				for i := 0; i < b.N; i++ {
					r, err := RunArm(context.Background(), p.input(arm, sims, uint64(i+1)))
					if err != nil {
						b.Fatal(err)
					}
					if !r.Searched {
						b.Fatalf("%s did not search", arm)
					}
					completed += r.Stats.Completed
					plies += r.Stats.MeanLeafPlies()
					edges += r.Stats.MeanLeafEdges()
					env += r.Stats.EnvSteps
				}
				sec := b.Elapsed().Seconds()
				b.ReportMetric(float64(completed)/sec, "sims/s")
				b.ReportMetric(plies/float64(b.N), "plies/sim")
				b.ReportMetric(edges/float64(b.N), "edges/sim")
				b.ReportMetric(float64(env)/float64(max(completed, 1)), "env/sim")
			})
		}
	}
}
