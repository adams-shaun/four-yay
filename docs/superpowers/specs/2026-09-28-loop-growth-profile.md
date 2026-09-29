# Loop prototype per-iteration growth profile

## Summary

Profiling `TestLoopPrototypeMiner` at N=20 and N=100 confirms the measured growth locally: 2.653 ms/iteration at N=20 and 49.021 ms/iteration at N=100. The dominant profiled work is in the continuous-effect/characteristics path as the game accumulates objects and events. This profile is of the rules test binary, not a production-mode benchmark.

At N=100, `rules.(*Engine).matchesWithCharsPTSlow` accounts for 2.36 s flat / 13.48 s cumulative CPU (10.96% / 62.58% of samples). `rules.(*Engine).active` accounts for 1.73 GB flat / 3.01 GB cumulative allocated bytes; `rules.(*Engine).verifyInertActive` accounts for 1.21 GB flat / 2.94 GB cumulative. Treat these as instrumented-test numbers: `rules/layer4types_verify_test.go` enables `layer4PrecheckVerify`, which can call the otherwise production-unused slow matcher to verify the `Card.Self` early rejection (`rules/layers.go:2809-2816`, `2832-2835`); `rules/layercache_verify_test.go` enables `layerInertVerify`, and `verifyInertActive` deliberately rebuilds `active()` on cache reuse. In this workload, the CPU and allocation shares therefore include verification overhead and must not be quoted as production cost. The measured elapsed loop time is likewise from the test binary. A production-mode profile is needed before choosing an optimization.

## Method and measurements

The corpus was present at `.cards` (not skipped). No test knob or production code change was needed: Go's subtest selection chooses the existing N-specific cases.

Commands were run with `GOMAXPROCS=3 GOMEMLIMIT=1GiB` and `-p 1`:

```sh
GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run '^TestLoopPrototypeMiner/20$' -cpuprofile=.ds4/scratch/miner20.cpu -memprofile=.ds4/scratch/miner20.alloc ./rules/
GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run '^TestLoopPrototypeMiner/100$' -cpuprofile=.ds4/scratch/miner100.cpu -memprofile=.ds4/scratch/miner100.alloc ./rules/
GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=10 -run '^TestLoopPrototypeMiner/20$' -cpuprofile=.ds4/scratch/miner20x10.cpu -memprofile=.ds4/scratch/miner20x10.alloc ./rules/
go tool pprof -top -nodecount=15 .ds4/scratch/miner20x10.cpu
go tool pprof -top -alloc_space -nodecount=15 .ds4/scratch/miner20x10.alloc
go tool pprof -top -nodecount=20 .ds4/scratch/miner100.cpu
go tool pprof -top -alloc_space -nodecount=20 .ds4/scratch/miner100.alloc
GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -v -run '^TestLoopPrototypeMiner/(20|100)$' ./rules/
```

N=20 was profiled at count 10 to increase CPU sample volume; it reports cumulative allocations over ten test invocations. N=100 was count 1. Do not compare total allocation sizes across those two profile files without normalizing for invocation count. CPU/alloc profiles include initial corpus loading and test setup, particularly visible at N=20; the verbose timing run isolates the test's own loop timer after board setup.

### N=20

Verbose timing: **2.653 ms/iteration**.

Ten-invocation CPU profile (4.00 s CPU samples): leading application/runtime entries included `matchesWithCharsPTSlow` 80 ms flat / 500 ms cumulative (2.00% / 12.50%); most flat cost was runtime/GC (`tryDeferToSpanScan` 270 ms, `scanSpanPackedAVX512` 150 ms, `memclrNoHeapPointers` 150 ms).

Ten-invocation alloc_space profile (1.97 GB total): `active` 0.62 GB flat / 1.09 GB cumulative; `verifyInertActive` 0.45 GB flat / 1.07 GB cumulative; `matchesWithCharsPTSlow` 0.07 GB flat / 0.07 GB cumulative. Corpus load contributes as well (`saferio.ReadData` 0.11 GB).

### N=100

Verbose timing: **49.021 ms/iteration**.

CPU profile (21.54 s CPU samples):

| Function | Flat | Cumulative |
|---|---:|---:|
| `rules.(*Engine).matchesWithCharsPTSlow` | 2.36 s (10.96%) | 13.48 s (62.58%) |
| `rules.(*Engine).matchesWithCharsPT` | 0.34 s (1.58%) | 13.94 s (64.72%) |
| `rules.(*Engine).abilityDependencyOrder` | 1.23 s (5.71%) | 1.23 s (5.71%) |
| `effects.compiledPositive` | 1.15 s (5.34%) | 2.05 s (9.52%) |
| `effects.compiledMatch` | 0.38 s (1.76%) | 2.76 s (12.81%) |

alloc_space profile (5.73 GB total):

| Function | Flat | Cumulative |
|---|---:|---:|
| `rules.(*Engine).matchesWithCharsPTSlow` | 2,003.03 MB (34.96%) | 2,003.03 MB (34.96%) |
| `rules.(*Engine).active` | 1,727.13 MB (30.14%) | 3,009.32 MB (52.52%) |
| `rules.(*Engine).verifyInertActive` | 1,212.97 MB (21.17%) | 2,943.54 MB (51.37%) |
| `rules.(*Engine).legalActionsWalkWithWindow` | 13.78 MB (0.24%) | 1,992.89 MB (34.78%) |
| `rules.(*Engine).snapshotTriggerBoard` | 20.38 MB (0.36%) | 20.38 MB (0.36%) |

## Growth mechanism and code references

The prototype submits an ordinary activation and drains its resulting actions each iteration (`rules/loops_prototype_test.go:638-675`). These actions derive characteristics and evaluate legal options against the growing game state. The key CPU path is `rules/layers.go:2835`, `matchesWithCharsPTSlow`, reached through `matchesWithCharsPT`; the N=100 profile shows that effect/filter matching dominates CPU. The same walk obtains the continuous effects through `active()` (`rules/layers.go:2303`), which scans the active continuous effects, refreshes static effects and sorts the combined list.

Additionally, rules tests globally turn on `layerInertVerify` (`rules/layercache_verify_test.go:7`) and `layer4PrecheckVerify` (`rules/layer4types_verify_test.go:9`). When `active()` reuses its cached list on a layer-inert event, it invokes `verifyInertActive` (`rules/layers.go:2322`; implementation `rules/layercache.go:179`), which deliberately invalidates the cache and rebuilds it for comparison. This accounts for the large `verifyInertActive` and additional `active` allocations. Separately, `layer4PrecheckVerify` runs `matchesWithCharsPTSlow` to verify the `Card.Self` early rejection (`rules/layers.go:2809-2816`); its implementation documents that this slow path is production-unused (`rules/layers.go:2832-2835`). These are test verification overheads, not evidence production performs those extra rebuilds/full match checks. Profile attribution here describes instrumented tests only.

The measured growth is consistent with repeated characteristics/effect evaluation as iterations add game objects and events: N=100 takes 5.9x the per-iteration time of N=20, and its profile has substantial cost in effect matching and ability dependency ordering. These data do not isolate board-size work from event-history work, nor production execution from test verification overhead. A separate production-mode profile is needed to measure those contributions. The current profiles identify where the instrumented test spends time, not a specific fix.

## Proposed follow-up ticket

**Title:** Profile and reduce loop decision cost from repeated characteristics matching

**Done means:**
- Capture comparable N=20 and N=100 profiles without the rules-test-only `layerInertVerify` and `layer4PrecheckVerify` instrumentation, while preserving ordinary engine behavior.
- Attribute the remaining growth to specific repeated object/effect walks with flat/cumulative CPU and allocation numbers; distinguish board-size work from accumulated-log work.
- Implement only the measured redundant work reduction, with no change to event streams, legality, deterministic replay, or `TestHeads`.
- Add a focused test or benchmark that exercises the Miner line at both N values and asserts deterministic event heads; demonstrate lower N=100 per-iteration cost and no regression at N=20.

**Test:** targeted Miner line at N=20 and N=100 (including deterministic rerun/head equality), followed by the applicable rules behavioral goldens.

## Commands and gate output

The required prototype gate passed:

```text
$ GOMAXPROCS=3 GOMEMLIMIT=1GiB go test -p 1 -count=1 -run '^TestLoopPrototype' ./rules/
EXIT=0
ok   github.com/adams-shaun/gorge/rules 20.879s
```

Verbose timing/profile confirmation passed:

```text
=== RUN   TestLoopPrototypeMiner/20
    loops_prototype_test.go:660: ms/iteration=2.653
=== RUN   TestLoopPrototypeMiner/100
    loops_prototype_test.go:660: ms/iteration=49.021
PASS
ok   github.com/adams-shaun/gorge/rules 17.256s
```

Required behavior goldens passed:

```text
$ go test ./internal/archtest/
(ok; cached)

$ go test -run TestConstructedDefaultIsByteIdentical ./cmd/botbench/
(ok; cached)

$ go test -run TestConstructedDefaultIsByteIdentical ./cmd/botbench/ 2>&1 | tail -5
ok   github.com/adams-shaun/gorge/cmd/botbench 0.824s
```

## Issues

No separate unfixed engine defect was discovered. The principal caveat is that the rules test binary enables `layerInertVerify` and `layer4PrecheckVerify`: these respectively inflate `active()` rebuild allocations and cause `matchesWithCharsPTSlow` to perform verification work that production skips. The proposed follow-up profiles without both test-only flags before deciding on a fix.
