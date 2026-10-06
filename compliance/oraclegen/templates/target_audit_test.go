package templates

import (
	"os"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

const fullTargetAuditEnv = "GORGE_ORACLEGEN_FULL_TARGET_AUDIT"

// representativeTargetCards keeps ordinary package gates focused: two
// corpus cards whose cast requires a creature target, which the two named
// target audits check by default. The exhaustive pinned-corpus audit is the
// TestTargetAuditChunkNN family below.
var representativeTargetCards = []string{"Conduct Electricity", "Repulsive Mutation"}

// targetAuditChunks is how many independent tests the exhaustive target
// audit is sharded into. The audit generates and replays every identity in
// target-carriers.json (~6k cards); run as one test it took 64-101 s wall and
// ~100-155 CPU-s under the per-test budget's 2 vCPU (2026-10-05), over the
// <= 1 min, <= 2 vCPU, <= 2 GB budget. Chunk i covers
// carriers[i*n/K : (i+1)*n/K], so the chunks partition the pinned census
// exactly -- a census that grows grows every chunk and never drops a carrier
// (TestTargetAuditChunksPartitionTheCensus) -- and each chunk applies BOTH
// target audits to each carrier's single Generate + replay.
//
// Run the full audit with
//
//	GORGE_ORACLEGEN_FULL_TARGET_AUDIT=1 go test -run 'TestTargetAuditChunk' ./compliance/oraclegen/templates/
//
// or one chunk with -run 'TestTargetAuditChunk03$'. The chunks are uneven,
// but no longer lopsided: Unite the Coalition (126 repeat-mode charm plans)
// once cost ~45 s at 2 vCPU here because every repeating plan failed the
// runner's one-use-per-label mode matcher; the repeat-free combination now
// comes first and the runner resubmits a repeated index, so chunk 11 went
// from 38.6 s to 2.2 s (2026-10-06).
const targetAuditChunks = 12

// targetAuditChunk returns chunk i's carrier slice.
func targetAuditChunk(carriers []string, i int) []string {
	n := len(carriers)
	return carriers[i*n/targetAuditChunks : (i+1)*n/targetAuditChunks]
}

// runTargetAuditChunk is one shard of the exhaustive target audit.
func runTargetAuditChunk(t *testing.T, i int) {
	if os.Getenv(fullTargetAuditEnv) != "1" {
		t.Skipf("opt-in exhaustive target audit: set %s=1", fullTargetAuditEnv)
	}
	reg := loadGenRegistry(t)
	chunk := targetAuditChunk(targetCarriers(t, reg), i)
	if len(chunk) == 0 {
		t.Fatalf("precondition: target audit chunk %d is empty", i)
	}
	replayed, mandatory := 0, 0
	for _, c := range targetAuditCases(t, reg, chunk) {
		if c.skip != nil {
			continue // Census still pins skipped carriers; no scenario was emitted.
		}
		replayed++
		checkGorgeChoseTargets(t, c)
		mandatory += checkCastTargetsSurvive(t, c)
	}
	t.Logf("target audit chunk %d: %d carriers, %d scenarios replayed, %d mandatory target decisions", i, len(chunk), replayed, mandatory)
}

func TestTargetAuditChunk00(t *testing.T) { runTargetAuditChunk(t, 0) }
func TestTargetAuditChunk01(t *testing.T) { runTargetAuditChunk(t, 1) }
func TestTargetAuditChunk02(t *testing.T) { runTargetAuditChunk(t, 2) }
func TestTargetAuditChunk03(t *testing.T) { runTargetAuditChunk(t, 3) }
func TestTargetAuditChunk04(t *testing.T) { runTargetAuditChunk(t, 4) }
func TestTargetAuditChunk05(t *testing.T) { runTargetAuditChunk(t, 5) }
func TestTargetAuditChunk06(t *testing.T) { runTargetAuditChunk(t, 6) }
func TestTargetAuditChunk07(t *testing.T) { runTargetAuditChunk(t, 7) }
func TestTargetAuditChunk08(t *testing.T) { runTargetAuditChunk(t, 8) }
func TestTargetAuditChunk09(t *testing.T) { runTargetAuditChunk(t, 9) }
func TestTargetAuditChunk10(t *testing.T) { runTargetAuditChunk(t, 10) }
func TestTargetAuditChunk11(t *testing.T) { runTargetAuditChunk(t, 11) }

// TestTargetAuditChunksPartitionTheCensus holds the sharding to full
// coverage: one TestTargetAuditChunkN per chunk, and the chunks,
// concatenated, are exactly the pinned census.
func TestTargetAuditChunksPartitionTheCensus(t *testing.T) {
	reg := loadGenRegistry(t)
	carriers := targetCarriers(t, reg)
	var joined []string
	for i := 0; i < targetAuditChunks; i++ {
		joined = append(joined, targetAuditChunk(carriers, i)...)
	}
	if len(joined) != len(carriers) {
		t.Fatalf("chunks cover %d of %d carriers", len(joined), len(carriers))
	}
	for i := range carriers {
		if joined[i] != carriers[i] {
			t.Fatalf("chunk concatenation diverges at %d: %q vs %q", i, joined[i], carriers[i])
		}
	}
	if tests := len([]func(*testing.T){TestTargetAuditChunk00, TestTargetAuditChunk01, TestTargetAuditChunk02,
		TestTargetAuditChunk03, TestTargetAuditChunk04, TestTargetAuditChunk05, TestTargetAuditChunk06,
		TestTargetAuditChunk07, TestTargetAuditChunk08, TestTargetAuditChunk09, TestTargetAuditChunk10,
		TestTargetAuditChunk11}); tests != targetAuditChunks {
		t.Fatalf("%d chunk tests for %d chunks", tests, targetAuditChunks)
	}
}

// checkGorgeChoseTargets is TestGeneratedTargetsAreGorgeChoices' per-carrier
// assertion: the emitted scenario names no target gorge did not choose.
func checkGorgeChoseTargets(t *testing.T, c targetAuditCase) {
	t.Helper()
	if c.err != nil {
		t.Errorf("%s: replay: %v", c.name, c.err)
		return
	}
	for _, f := range c.fails {
		if strings.Contains(f, rules.OracleUnusedTargetMarker) {
			t.Errorf("%s emitted a scenario with a target gorge did not choose: %s", c.name, f)
		}
	}
}

// checkCastTargetsSurvive is TestGeneratedMandatoryCastTargetsSurvive's
// per-carrier assertion: the replay is clean, every mandatory target
// decision was answered, and each cast step's scenario targets are exactly
// gorge's picks. It returns the mandatory target decisions it saw.
func checkCastTargetsSurvive(t *testing.T, c targetAuditCase) (mandatory int) {
	t.Helper()
	name := c.name
	if c.err != nil || len(c.fails) != 0 {
		t.Errorf("%s: replay err=%v fails=%v", name, c.err, c.fails)
		return 0
	}
	for i, st := range c.steps {
		if st.op != "cast" {
			continue
		}
		var picks []string
		for _, d := range c.decisions {
			if d.Step != i || d.Via != "target" {
				continue
			}
			if d.Min > 0 {
				mandatory++
				if len(d.PickRefs) < d.Min {
					t.Errorf("%s: mandatory target decision %+v was not answered", name, d)
				}
			}
			picks = append(picks, d.PickRefs...)
		}
		if len(picks) == 0 && len(st.targets) == 0 {
			continue
		}
		if !reflect.DeepEqual(st.targets, picks) {
			t.Errorf("%s cast step %d: scenario targets %v, gorge picked %v", name, i, st.targets, picks)
		}
	}
	return mandatory
}

// targetAuditCase is one carrier's Generate + replay outcome, trimmed to
// what the two target audits assert: the cast steps' scenario targets and
// the replay's fails and decisions (no snapshots or transcript).
type targetAuditCase struct {
	name      string
	skip      *oraclegen.Skip
	steps     []targetAuditStep
	err       error
	fails     []string
	decisions []rules.OracleDecision
}

type targetAuditStep struct {
	op      string
	targets []string
}

// targetAuditMemo runs the audit's Generate + replay ONCE per process for
// both TestGeneratedTargetsAreGorgeChoices and
// TestGeneratedMandatoryCastTargetsSurvive: each used to generate and replay
// every carrier itself, doubling the full audit's cost for identical work.
var targetAuditMemo struct {
	sync.Mutex
	names []string
	cases []targetAuditCase
}

// targetAuditCases returns every carrier's case, in carrier order. The
// carriers are independent scenarios on the process-shared read-only
// registry, so a bounded worker pool plays them concurrently; each result
// lands in its own index, so the order the workers finish in never reaches
// an assertion.
func targetAuditCases(t *testing.T, reg *cards.Registry, names []string) []targetAuditCase {
	t.Helper()
	targetAuditMemo.Lock()
	defer targetAuditMemo.Unlock()
	if sameNames(targetAuditMemo.names, names) {
		return targetAuditMemo.cases
	}
	out := make([]targetAuditCase, len(names))
	workers := min(runtime.GOMAXPROCS(0), 8, len(names))
	next := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				out[i] = runTargetAuditCase(reg, names[i])
			}
		}()
	}
	for i := range names {
		next <- i
	}
	close(next)
	wg.Wait()
	targetAuditMemo.names = append([]string(nil), names...)
	targetAuditMemo.cases = out
	return out
}

func runTargetAuditCase(reg *cards.Registry, name string) targetAuditCase {
	c := targetAuditCase{name: name}
	it, skip := Generate(reg, name)
	if skip != nil {
		c.skip = skip
		return c
	}
	for _, st := range it.Scenario.Steps {
		c.steps = append(c.steps, targetAuditStep{op: st.Op, targets: st.Targets})
	}
	res, err := rules.RunOracleScenarioJSON(reg, it.Raw())
	c.err, c.fails, c.decisions = err, res.Fails, res.Decisions
	return c
}

func sameNames(a, b []string) bool {
	if a == nil || len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestTargetCarrierCensus preserves the exhaustive identity ratchet without
// invoking Generate or replaying scenarios. The opt-in full audit above uses
// this same exact-set census before traversing all identities.
func TestTargetCarrierCensus(t *testing.T) {
	reg := loadGenRegistry(t)
	carriers := targetCarriers(t, reg)
	if len(carriers) == 0 {
		t.Fatal("precondition: target carrier census is empty")
	}
}
