package rules

// The performance/profile half of the loop research harness: the Miner cycle's
// iteration-count and verify-mode knobs and the single test that drives them.
// It is split out of loops_prototype_test.go so the per-card line tests and the
// profiling instrumentation are separate files. The seam is real -- main's
// profiling commit added this block to the same file three live
// loops/shortcuts branches carry, and two of those branches are byte-identical
// to main apart from this block. Splitting does not resolve the branches'
// current add/add (their merge base predates the file on main, so only a
// rebase clears that), but it keeps a line-test ticket and a profiling ticket
// from ever colliding again.
import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// loopProfileN returns the iteration counts TestLoopPrototypeMiner runs. The
// default is the historical fixed set; GORGE_LOOP_MINER_N overrides it with a
// comma-separated list so a profile run can target one N.
func loopProfileN() []int {
	if s := os.Getenv("GORGE_LOOP_MINER_N"); s != "" {
		var out []int
		for _, f := range strings.Split(s, ",") {
			n, err := strconv.Atoi(strings.TrimSpace(f))
			if err != nil || n <= 0 {
				panic("bad GORGE_LOOP_MINER_N: " + s)
			}
			out = append(out, n)
		}
		return out
	}
	return []int{1, 20, 100}
}

// loopProfileDisableVerify turns off the rules test binary's verification-only
// checks for one profile run and returns a restore func. The verify modes only
// add re-derivations and panics; they never change the engine's emitted events
// or answers, so turning them off for a profile yields the production cost
// shape instead of the instrumented one. Gated by env, so the default run is
// byte-for-byte unchanged.
//
// The restore MUST be registered with t.Cleanup rather than deferred:
// loopBegin registers a cleanup that replays every recorded intent on a
// clone, and t.Cleanup runs LIFO (the last registered runs first). A deferred
// restore would run before that replay and re-enable verify for it, which is
// most of the loop's second pass and would swamp the profile. Registering the
// restore first keeps verify off through loopBegin's replay too.
func loopProfileDisableVerify() func() {
	if os.Getenv("GORGE_LOOP_NO_VERIFY") != "1" {
		return func() {}
	}
	ptrs := []*bool{
		&layerInertVerify, &layer4PrecheckVerify, &derivedMemoVerify,
		&pricedCandidatesVerify, &castsOnlyWalkVerify,
		&potentialMembersVerify, &walkCacheVerify, &trigZoneSkipVerify,
		&manaPayFastVerify, &activeSummaryVerify, &manaSAFactsVerify,
		&faceScanVerify, &livelockCandVerify, &priorityFlowVerify,
		&replZoneSkipVerify, &sbaQuietVerify, &provenanceGateVerify,
		&specDerivedVerify, &staticZoneSkipVerify, &manaPlainVerify,
	}
	old := make([]bool, len(ptrs))
	for i, p := range ptrs {
		old[i] = *p
		*p = false
	}
	return func() {
		for i, p := range ptrs {
			*p = old[i]
		}
	}
}

func TestLoopPrototypeMiner(t *testing.T) {
	for _, n := range loopProfileN() {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			t.Cleanup(loopProfileDisableVerify())
			var previous *events.Log
			for run := 0; run < 2; run++ {
				e, ids := loopBoard(t, []string{"Rakdos, the Muscle", "Phyrexian Altar", "Forsaken Miner"}, []state.Zone{state.ZBattlefield, state.ZBattlefield, state.ZBattlefield})
				loopDrain(t, e, 0, 0, 1)
				start := loopBegin(t, e)
				clock := time.Now()
				library := len(e.G.Zone(state.ZLibrary, 1))
				for i := 0; i < n; i++ {
					loopAction(t, e, ids[1], "activate")
					loopDrain(t, e, ids[2], 0, 1)
					if e.G.Obj(ids[2]).Zone != state.ZBattlefield {
						t.Fatalf("iteration %d: Miner in %v", i+1, e.G.Obj(ids[2]).Zone)
					}
					if e.G.Players[0].Pool.Total() != 0 || len(e.G.Zone(state.ZLibrary, 1)) != max(0, library-i-1) || e.G.Players[1].Life != 20 || e.G.Over {
						t.Fatal("Miner cycle net resources wrong")
					}
				}
				if run == 0 {
					loopRecord(t, e, "miner-"+fmt.Sprint(n), n, start)
					t.Logf("ms/iteration=%.3f", float64(time.Since(clock).Microseconds())/1000/float64(n))
					previous = e.L
				} else if previous.Head() != e.L.Head() || !reflect.DeepEqual(previous.Events, e.L.Events) {
					t.Fatal("deterministic rerun diverged")
				}
			}
		})
	}
}
