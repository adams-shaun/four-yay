# Search-Probe Engine Reset Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cut the search probe's deep-engine-clone allocations by transferring/resetting engines in a pool instead of deep-copying per world and per teacher rollout.

**Architecture:** The probes already recycle arrays via `rules.Spare`; the cost is deep clones (`state.Game.CloneIntoDirty`, `rules.cloneWith`). Task 1 stops cloning a proposal engine that is selected once (the common case). Task 2 recycles the parallel teacher's engines. Tasks 3-4 add a real in-place `rules.Engine.Reset` and a per-worker engine pool that replaces `NewHypotheticalPlanned` per attempt. Tasks 1-2 need no `rules/genesis.go` edit and carry the measured win; Tasks 3-4 do and must land after cpu-redeal.

**Tech Stack:** Go (no cgo, no third-party deps in the rules/searchprobe core).

**Spec:** `docs/superpowers/specs/2026-10-07-searchprobe-engine-reset-design.md`.

## Global Constraints

- Every change in its own worktree; never move the shared main checkout's HEAD. Stage explicit paths, never `git add -A`.
- No code comments unless the surrounding file already documents the contract (this repo does document contracts heavily; match it).
- Focused, capped tests only — never `go test ./...`:
  `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run <X> ./pkg`
- Output-identical is a hard requirement: botbench hosted-root `run.log` (sampler attempts/accepted/rejected, ESS lines, `ms/asked decision`) and returned worlds must be byte-identical to the pre-change run on the same seeds.
- Gate before landing: `scripts/postmerge_full.sh <worktree> <sha>` must end exit 0 / ALL GREEN.
- `rules/genesis.go` and `internal/searchprobe/redeal.go` are cpu-redeal-contended (hotfiles). Tasks 3-4 keep their `genesis.go` hunk minimal and land after cpu-redeal; Tasks 1-2 add no edit there.

## File Structure

- Modify `internal/searchprobe/sample.go` — world materialization (Task 1); attempt construction (Task 4).
- Modify `internal/searchprobe/teacher.go` — parallel rollout recycling (Task 2).
- Create `internal/searchprobe/engine_pool.go` — per-worker engine pool (Task 4).
- Create `rules/reset.go` — `Engine.Reset` + factored `resetEngineFields` (Task 3).
- Modify `rules/genesis.go` — `newEngineShell` delegates to `resetEngineFields` (Task 3, minimal).
- Tests: `internal/searchprobe/world_transfer_test.go` (Task 1), `internal/searchprobe/teacher_test.go` (Task 2, extend), `rules/reset_test.go` (Task 3), `internal/searchprobe/engine_pool_test.go` (Task 4).

---

### Task 1: Transfer unique proposal engines instead of cloning every world (C3)

**Files:**
- Modify: `internal/searchprobe/sample.go:561-571`
- Create: `internal/searchprobe/world_transfer_test.go`

**Interfaces:**
- Consumes: `World` (`internal/searchprobe/sample.go:77`), `(*rules.Engine).Clone()` (`rules/clone.go:30`), `(*Collector).clone()`.
- Produces: `materializeWorlds(proposals []World, indices []int) []World` — returns one `World` per index; a first-seen proposal transfers its engine, a repeat clones it.

- [ ] **Step 1: Write the failing test**

Create `internal/searchprobe/world_transfer_test.go`:

```go
package searchprobe

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules"
)

func TestMaterializeWorldsTransfersUniqueAndClonesRepeats(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	names := testutil.LegacyDeckNames()
	cfg := rules.Config{
		Seed: 3, Names: []string{names[0], names[1]},
		Decks: [][]*cards.Card{testutil.RepoDeck(t, reg, names[0]), testutil.RepoDeck(t, reg, names[1])},
		Tokens: reg.Tokens, NameUniverse: reg.Universe(),
	}
	mk := func() World {
		return World{Engine: rules.New(cfg), Observer: NewCollector(0)}
	}
	p0, p1 := mk(), mk()

	// Unique selections transfer the proposal's own engine.
	got := materializeWorlds([]World{p0, p1}, []int{0, 1})
	if got[0].Engine != p0.Engine || got[1].Engine != p1.Engine {
		t.Fatalf("unique selections must transfer the engine, not clone")
	}

	// A repeat clones, so the two returned worlds never alias one engine.
	rep := materializeWorlds([]World{p0}, []int{0, 0})
	if rep[0].Engine == rep[1].Engine {
		t.Fatalf("a repeated proposal must yield independent engines")
	}
	if rep[0].Engine.L.Head() != rep[1].Engine.L.Head() {
		t.Fatalf("cloned world head %s != original %s", rep[1].Engine.L.Head(), rep[0].Engine.L.Head())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run TestMaterializeWorlds ./internal/searchprobe`
Expected: FAIL — `undefined: materializeWorlds`.

- [ ] **Step 3: Add the helper and call it**

In `internal/searchprobe/sample.go`, add near the `World` type (line 77):

```go
// materializeWorlds returns one World per index. A proposal selected once hands
// its own engine over; the same proposal selected again is cloned so the
// returned worlds never alias one engine. Cloning only the repeats leaves the
// common one-selection case free of the deep clone.
func materializeWorlds(proposals []World, indices []int) []World {
	seen := make(map[int]bool, len(indices))
	out := make([]World, 0, len(indices))
	for _, i := range indices {
		w := proposals[i]
		if seen[i] {
			w.Engine = w.Engine.Clone()
		}
		seen[i] = true
		w.Observer = w.Observer.clone()
		out = append(out, w)
	}
	return out
}
```

Replace the loop at `sample.go:561-571`:

```go
	seen := make(map[int]bool)
	for _, i := range indices {
		if seen[i] {
			result.Duplicates++
		}
		seen[i] = true
		w := proposals[i]
		w.Engine = w.Engine.Clone()
		w.Observer = w.Observer.clone()
		result.Worlds = append(result.Worlds, w)
	}
```

with:

```go
	for _, i := range indices {
		if seen[i] {
			result.Duplicates++
		}
		seen[i] = true
	}
	result.Worlds = materializeWorlds(proposals, indices)
```

(`seen` is still needed for the duplicate count only; `materializeWorlds` keeps its own.)

- [ ] **Step 4: Run the test and the package**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run 'TestMaterializeWorlds|TestSample' ./internal/searchprobe`
Expected: PASS.

- [ ] **Step 5: Output-identical check**

Build before/after botbench, run each twice on a pinned core, diff `run.log` sampler/ESS lines:

```bash
go build -o /tmp/botbench-t1 ./cmd/botbench
for i in 1 2; do taskset -c 8 env GOMAXPROCS=1 GOMEMLIMIT=2GiB /tmp/botbench-t1 \
  -dir .cards -a search -b bot -hosted-root -games 8 -workers 1 -format constructed \
  > /tmp/t1-run-$i.log 2>&1; done
grep -E 'sampler:|ESS weighting|ms/asked' /tmp/t1-run-1.log
```
Expected: sampler attempts/accepted/rejected and ESS lines identical to the pre-change run; `result.Duplicates` small.

- [ ] **Step 6: Commit**

```bash
git add internal/searchprobe/sample.go internal/searchprobe/world_transfer_test.go
git commit -m "perf(searchprobe): transfer a once-selected world engine instead of cloning"
```

---

### Task 2: Recycle the parallel teacher's engines (C4)

**Files:**
- Modify: `internal/searchprobe/teacher.go:235-258`
- Test: `internal/searchprobe/teacher_test.go` (extend)

**Interfaces:**
- Consumes: `(*rules.Engine).CloneInto(sp *rules.Spare)` (`rules/clone.go:43`), `(*rules.Engine).Release() rules.Spare` (`rules/genesis.go:112`) — already used by the sequential path at `teacher.go:267-270`.
- Produces: no new names; the parallel path mirrors the sequential one.

- [ ] **Step 1: Write the failing test**

Add to `internal/searchprobe/teacher_test.go` (follow the file's existing World/History fixture; reuse its helpers rather than duplicating them):

```go
func TestTeacherRolloutsAreRecyclingInvariant(t *testing.T) {
	t.Parallel()
	// Build one fixed World and candidate set with the file's existing
	// helper, then run the teacher twice with Parallelism 1 and >1 and
	// assert identical per-candidate results. The two paths clone the same
	// world and must score identically whether or not they recycle storage.
	world, candidates, opts := teacherFixture(t) // existing helper in teacher_test.go
	seq := teacherValues(t, world, candidates, opts, 1)
	par := teacherValues(t, world, candidates, opts, 4)
	if !equalFloatSlices(seq, par) {
		t.Fatalf("sequential %v != parallel %v", seq, par)
	}
}
```

If `teacher_test.go` has no such helper, add `teacherFixture`, `teacherValues`, and `equalFloatSlices` as thin wrappers over the existing test entry points in that file, keeping the test self-contained.

- [ ] **Step 2: Run test to verify it fails or passes-for-the-wrong-reason**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run TestTeacherRolloutsAreRecyclingInvariant ./internal/searchprobe`
Expected: PASS for the current `Clone()` path — this test pins behavior, then must stay green after Step 3.

- [ ] **Step 3: Mirror the sequential recycle in the parallel path**

Replace `teacher.go:235-258`:

```go
	if workers := min(opts.Parallelism, len(outs)); workers > 1 {
		engines := make([]*rules.Engine, len(outs))
		for k := range engines {
			engines[k] = worlds[k/len(candidates)].Engine.Clone()
			engines[k].SetDecisionArena(true)
		}
		var wg sync.WaitGroup
		var next atomic.Int64
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var lv view.View
				for {
					k := int(next.Add(1)) - 1
					if k >= len(outs) {
						return
					}
					run(k, engines[k], &lv)
					engines[k] = nil
				}
			}()
		}
		wg.Wait()
	} else {
```

with:

```go
	if workers := min(opts.Parallelism, len(outs)); workers > 1 {
		var wg sync.WaitGroup
		var next atomic.Int64
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var lv view.View
				var sp rules.Spare
				for {
					k := int(next.Add(1)) - 1
					if k >= len(outs) {
						return
					}
					e := worlds[k/len(candidates)].Engine.CloneInto(&sp)
					e.SetDecisionArena(true)
					run(k, e, &lv)
					sp = e.Release()
				}
			}()
		}
		wg.Wait()
	} else {
```

- [ ] **Step 4: Run the test and the package**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run 'TestTeacher|TestRollout' ./internal/searchprobe`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/searchprobe/teacher.go internal/searchprobe/teacher_test.go
git commit -m "perf(searchprobe): recycle the parallel teacher's rollout engines"
```

---

### Task 3: `rules.Engine.Reset` + invisibility test (C1)

**Depends on:** cpu-redeal has landed (touches `rules/genesis.go`).

**Files:**
- Create: `rules/reset.go`
- Modify: `rules/genesis.go:224-325` (factor `newEngineShell`)
- Test: `rules/reset_test.go`

**Interfaces:**
- Produces: `func (e *Engine) Reset(cfg Config, prefix []ChanceDraw, planner ShufflePlanner) error`; unexported `func resetEngineFields(e *Engine, cfg Config, random *rng)`.
- Consumes: `newRNG`, `chanceState`, `genesisDeal`, `Release`.

- [ ] **Step 1: Factor `newEngineShell`**

In `rules/genesis.go`, change `newEngineShell` (line 224) so its body is:

```go
func newEngineShell(cfg Config, random *rng) *Engine {
	e := &Engine{}
	resetEngineFields(e, cfg, random)
	return e
}
```

Move the existing body from `e := &Engine{ G: ... }` (line 258) through `return e` (line 324) verbatim into `rules/reset.go` as `func resetEngineFields(e *Engine, cfg Config, random *rng)`, changing the `e := &Engine{...}` literal into field assignments on the passed `e` (`e.G = ...`, `e.deckManifests = ...`, etc.). Behavior is unchanged; `newEngineShell` still builds a full shell.

- [ ] **Step 2: Write the failing test**

Create `rules/reset_test.go`:

```go
package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// TestResetIsInvisible pins Engine.Reset's contract: an engine reset in place
// from a spent game deals byte-identically to a fresh NewHypotheticalPlanned --
// same chain head, event count and intent count.
func TestResetIsInvisible(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	all := testutil.LegacyDeckNames()
	cfgFor := func(seed uint64) Config {
		names := []string{all[int(seed)%len(all)], all[(int(seed)+1)%len(all)]}
		decks := [][]*cards.Card{testutil.RepoDeck(t, reg, names[0]), testutil.RepoDeck(t, reg, names[1])}
		return Config{Seed: seed, Names: names, Decks: decks, Tokens: reg.Tokens, NameUniverse: reg.Universe()}
	}
	type result struct {
		head            string
		events, intents int
	}
	summary := func(e *Engine) result {
		return result{head: e.L.Head(), events: len(e.L.Events), intents: len(e.L.Intents)}
	}
	for _, s := range []uint64{3, 4, 5, 6} {
		cfg := cfgFor(s)
		fresh, err := NewHypotheticalPlanned(cfg, nil, nil)
		if err != nil {
			t.Fatalf("seed %d: %v", s, err)
		}
		want := summary(fresh)
		// A spent engine from a DIFFERENT seed, reset to cfg.
		spent, err := NewHypotheticalPlanned(cfgFor(s+1), nil, nil)
		if err != nil {
			t.Fatalf("seed %d spent: %v", s, err)
		}
		if err := spent.Reset(cfg, nil, nil); err != nil {
			t.Fatalf("seed %d Reset: %v", s, err)
		}
		if got := summary(spent); got != want {
			t.Fatalf("seed %d: reset %+v, fresh %+v", s, got, want)
		}
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run TestResetIsInvisible ./rules`
Expected: FAIL — `spent.Reset undefined`.

- [ ] **Step 4: Implement `Reset`**

Create `rules/reset.go` (with `resetEngineFields` from Step 1) plus:

```go
// Reset rebuilds e, a spent engine, in place to the state
// NewHypotheticalPlanned(cfg, prefix, planner) would produce. It reuses e's
// backing storage: e is Released to a Spare, that Spare is adopted back by the
// re-run of the shell and genesis, so the event log and object arena are the
// same arrays. The rebuilt game is byte-identical to a fresh construction
// (TestResetIsInvisible); the search caller sets the arena mode itself, as
// after NewHypotheticalPlanned.
func (e *Engine) Reset(cfg Config, prefix []ChanceDraw, planner ShufflePlanner) error {
	for i, d := range prefix {
		if d.Bound <= 0 {
			return fmt.Errorf("hypothetical chance draw %d: invalid bound %d", i, d.Bound)
		}
		if d.Value < 0 || d.Value >= d.Bound {
			return fmt.Errorf("hypothetical chance draw %d: value %d outside [0,%d)", i, d.Value, d.Bound)
		}
	}
	sp := e.Release()
	ecfg := cfg
	ecfg.Spare = &sp
	r := newRNG(ecfg.Seed)
	r.chance = &chanceState{prefix: append([]ChanceDraw(nil), prefix...), planner: planner, shuffleOrdinals: make(map[state.PlayerID]int)}
	resetEngineFields(e, ecfg, r)
	enableLiveArena(e)
	e.emit(events.Event{Kind: events.GameStart, Amount: int32(len(ecfg.Names))})
	e.genesisDeal(ecfg, false)
	return nil
}
```

Add the imports `rules/reset.go` needs (`fmt`, `github.com/adams-shaun/gorge/decision` only if required, `.../events`, `.../state`). `enableLiveArena` is needed because `newWithRNG` calls it; the search caller then switches the arena on for simulations as it already does.

- [ ] **Step 5: Run the test and the package**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run 'TestResetIsInvisible|TestSpareReuseIsInvisible' ./rules`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add rules/reset.go rules/genesis.go rules/reset_test.go
git commit -m "perf(rules): add Engine.Reset, an in-place genesis rebuild reusing storage"
```

---

### Task 4: Per-worker attempt-engine pool (C2)

**Depends on:** Task 3.

**Files:**
- Create: `internal/searchprobe/engine_pool.go`
- Modify: `internal/searchprobe/sample.go:256-277` (`runAttempt`)
- Test: `internal/searchprobe/engine_pool_test.go`

**Interfaces:**
- Consumes: `(*rules.Engine).Reset` (Task 3), `(*rules.Engine).SetDecisionArena(true)`.
- Produces: `type enginePool struct{...}` with `func newEnginePool() *enginePool`, `func (p *enginePool) get() *rules.Engine`, `func (p *enginePool) put(*rules.Engine)`.

- [ ] **Step 1: Write the failing test**

Create `internal/searchprobe/engine_pool_test.go`:

```go
package searchprobe

import (
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

func TestEnginePoolReusesRemainingEngines(t *testing.T) {
	t.Parallel()
	p := newEnginePool()
	if got := p.get(); got != nil {
		t.Fatalf("a fresh pool yields nil for the first engine, got %v", got)
	}
	e := &rules.Engine{}
	p.put(e)
	if got := p.get(); got != e {
		t.Fatalf("the pool must hand the put engine back, got %v", got)
	}
	if got := p.get(); got != nil {
		t.Fatalf("an empty pool yields nil, got %v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m -run TestEnginePoolReuses ./internal/searchprobe`
Expected: FAIL — `undefined: newEnginePool`.

- [ ] **Step 3: Implement the pool**

Create `internal/searchprobe/engine_pool.go`:

```go
package searchprobe

import "github.com/adams-shaun/gorge/rules"

// enginePool recycles whole engines across sampling attempts. A pooled engine
// is Reset in place for the next attempt, so its event log and object arena
// survive instead of being released and rebuilt. One pool is owned by one
// goroutine, matching the per-worker plan cache and Spare.
type enginePool struct {
	free []*rules.Engine
}

func newEnginePool() *enginePool { return &enginePool{} }

// get returns a pooled engine, or nil when the pool is empty (the caller
// builds a fresh one with a constructor).
func (p *enginePool) get() *rules.Engine {
	if n := len(p.free); n > 0 {
		e := p.free[n-1]
		p.free[n-1] = nil
		p.free = p.free[:n-1]
		return e
	}
	return nil
}

func (p *enginePool) put(e *rules.Engine) { p.free = append(p.free, e) }
```

- [ ] **Step 4: Wire the pool into `runAttempt`**

`runAttempt` currently takes `spare *rules.Spare` (`sample.go:256`), builds with `rules.NewHypotheticalPlanned(hcfg, tape, proposal.plan)` (`:266`), and on rejection does `*spare = e.Release()` (`:274`). Change it to take a `pool *enginePool` instead:

```go
	e := pool.get()
	if e == nil {
		e, err = rules.NewHypotheticalPlanned(hcfg, tape, proposal.plan)
	} else {
		e.SetDecisionArena(false)
		err = e.Reset(hcfg, tape, proposal.plan)
	}
```

and on rejection:

```go
		defer func() {
			if !kept {
				if err == nil {
					pool.put(e)
				}
			}
		}()
```

The `hcfg.Spare = spare` aliasing (sample.go:262-265) is dropped: the pooled engine owns its storage; a fresh `NewHypotheticalPlanned` keeps its own. Update every `runAttempt` caller (`sample.go:482`, the probe rounds, the sequential/frozen loops) and the worker setup at `sample.go:488-491` to hand each worker its own `newEnginePool()` instead of `new(rules.Spare)`.

A `Reset` that returns an error, or an engine whose reset reused a Spare from a *different* game, still yields byte-identical output (Task 3 test); the pool never mutates game content, only which arrays back it.

- [ ] **Step 5: Run the tests and the package**

Run: `systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m ./internal/searchprobe`
Expected: PASS (whole package).

- [ ] **Step 6: Output-identical check on the redeal path**

```bash
go build -o /tmp/botbench-t4 ./cmd/botbench
taskset -c 8 env GOMAXPROCS=1 GOMEMLIMIT=2GiB /tmp/botbench-t4 \
  -dir .cards -a search -b bot -hosted-root -games 8 -workers 1 -format constructed \
  > /tmp/t4-run.log 2>&1
grep -E 'sampler:|ESS weighting|ms/asked' /tmp/t4-run.log
```
Expected: identical sampler/ESS lines to the pre-change run.

- [ ] **Step 7: Commit**

```bash
git add internal/searchprobe/engine_pool.go internal/searchprobe/sample.go internal/searchprobe/engine_pool_test.go
git commit -m "perf(searchprobe): recycle whole attempt engines through a per-worker pool"
```

---

### Task 5: Measure and record

**Files:**
- Modify: `docs/superpowers/specs/2026-10-07-searchprobe-engine-reset-design.md` (append a "Measured" section) or add a report under `docs/superpowers/reports/`.

- [ ] **Step 1: Profile before/after**

Build the pre-change (`main`) and post-change binaries; alternate 2x each on pinned core 8 with `-cpuprofile`/`-memprofile` (redirect to files; verify profiles are ~60 KB, not ~1 KB). Compare `alloc_space` total and the clone frames (`CloneIntoDirty`, `cloneWith`, `growEvents`, `newEngineShell`) plus CPU `Total samples`.

- [ ] **Step 2: Record the numbers** in the spec/report with the exact command and date. Don't quote stale figures.

- [ ] **Step 3: Full gate**

```bash
scripts/postmerge_full.sh /home/sadams/projects/gorge/.worktrees/searchprobe-clone <sha>
```
Expected: exit 0 / ALL GREEN.

- [ ] **Step 4: Commit** the report; merge to main and push per repo rules.

---

## Self-Review

- **Spec coverage:** C3 → Task 1; C4 → Task 2; C1 → Task 3; C2 → Task 4; measurement → Task 5. The spec's "duplicates served by replay" is implemented more cheaply as "duplicates cloned" (Task 1), which meets the same output-identical bar; note this in the Task 1 commit message.
- **Placeholders:** none intended; Task 2's test references the existing `teacher_test.go` helper surface (implementer must bind to the file's actual helpers).
- **Type consistency:** `materializeWorlds`, `enginePool.get/put`, `Engine.Reset(cfg, prefix, planner)`, `resetEngineFields(e, cfg, random)` used consistently across tasks.
- **Ordering note:** Tasks 1-2 are the measured win and avoid `rules/genesis.go`; they can land independently. Tasks 3-4 must land after cpu-redeal (hotfiles) and C2 depends on C1.
