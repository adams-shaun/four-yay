package rules

import (
	"math/rand/v2"
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// TestPermuteTriggersInPlaceMatchesCopy holds the in-place cycle rotation
// handleTriggerOrder applies to the copy-out-and-back it replaced, over
// random permutations on both sides of the stack-buffer bound.
func TestPermuteTriggersInPlaceMatchesCopy(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 11))
	for _, n := range []int{1, 2, 3, 5, 17, 64, 65, 200} {
		for trial := 0; trial < 50; trial++ {
			q := make([]pendingTrigger, n)
			for i := range q {
				q[i].Source = state.ObjID(i + 1)
				q[i].Controller = state.PlayerID(i % 4)
			}
			choices := r.Perm(n)
			want := make([]pendingTrigger, 0, n)
			for _, c := range choices {
				want = append(want, q[c])
			}
			permuteTriggersInPlace(q, choices)
			for i := range q {
				if q[i].Source != want[i].Source || q[i].Controller != want[i].Controller {
					t.Fatalf("n=%d choices=%v: slot %d holds %d, want %d", n, choices, i, q[i].Source, want[i].Source)
				}
			}
		}
	}
}
