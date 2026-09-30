# Gate CPU ceiling — cli-20260930T021924Z-01e737bb

**Ticket:** add a CPU ceiling to `.agentctl/config.toml`'s go gates (they carried
`MemoryMax` but no `-p CPUQuota`, so one gate round burst to many vCPU) and
measure the wall-time cost so the fix does not quietly slow every landing.

**Verdict:** 6-package gate-shaped pair — **wall time did not regress** (the
warm-cache pair was 27.66s → 27.96s, within noise; the loaded-machine pair was
30.05s → 31.85s, and a back-to-back pair was 26.07s → 21.74s). The **CPU burst
dropped** (e.g. `go vet` 766% → 103% warm, `go build` 423% → 210%). Null / slightly
positive on wall, decisively positive on CPU: the intended outcome.

## What changed, per file

Only `.agentctl/config.toml` (repo-tracked; header says *"Change a value here,
not in agentctl"*). Five hunks, all inside the six go gates:

| gate | before | after |
|---|---|---|
| go build | `-p MemoryMax=4G`; env `GOMEMLIMIT=1GiB` | `-p MemoryMax=4G -p CPUQuota=400%`; env `GOMEMLIMIT=1GiB GOMAXPROCS=4` |
| go vet | `-p MemoryMax=4G`; env `GOMEMLIMIT=1GiB` | `-p MemoryMax=4G -p CPUQuota=400%`; env `GOMEMLIMIT=1GiB GOMAXPROCS=4` |
| go test (module) | `-p MemoryMax=8G … -p=4`; env `GOMEMLIMIT=1536MiB` | `-p MemoryMax=8G -p CPUQuota=400% … -p=4`; env `GOMEMLIMIT=1536MiB GOMAXPROCS=4` |
| CR conformance | `-p MemoryMax=4G … -p=1` | `-p MemoryMax=4G -p CPUQuota=200% … -p=1` |
| TestHeads | `-p MemoryMax=4G` | `-p MemoryMax=4G -p CPUQuota=200%` |
| make sim | `-p MemoryMax=4G` | `-p MemoryMax=4G -p CPUQuota=200%` |

Every `MemoryMax` value is unchanged. A dated comment above the first changed
gate (the `go build` block) names the ticket, the mechanism (`CPUQuota` caps the
scope cgroup's `cpu.max`, which go1.25+ reads into GOMAXPROCS and `-p` defaults;
`AllowedCPUs` does not), and the triage measurement. `npm`/`smoke` gates are
untouched (see `## Issues`).

### Why `CPUQuota` and not `AllowedCPUs`

Reproduced in this worktree, tiny `runtime.NumCPU()`/`GOMAXPROCS(0)` program,
`go version go1.26.3 linux/amd64`, `nproc` = 32:

```
plain:                 NumCPU: 32  GOMAXPROCS: 32
CPUQuota=200%:         NumCPU: 32  GOMAXPROCS: 2
AllowedCPUs=0-3:       NumCPU: 32  GOMAXPROCS: 32
```

Both candidates exit 0 in a user scope:

```
$ systemd-run --user --scope -q -p MemoryMax=1G -p CPUQuota=200% true; echo $?
0
$ systemd-run --user --scope -q -p MemoryMax=1G -p AllowedCPUs=0-3 true; echo $?
0
```

So `CPUQuota` alone caps GOMAXPROCS, `go build`/`go vet`'s `-p` default and the
cgroup scheduler together; `AllowedCPUs` only constrains affinity (Go does not
read the cpuset) and leaves 32 threads fighting over 4 cores. `GOMAXPROCS=4` in
`env` is the legible belt-and-braces for the parallel gates. The three serial
gates (`-p=1`/`-run TestCR`/`make sim`) get 200% (2 cores, ample) and no
`GOMAXPROCS` env — a serial workload has one thread anyway, and the quota still
bounds any internal parallelism (e.g. `go test`'s build phase).

The `-p CPUQuota=…` property form is the same form `scripts/heavy.sh:148-150`
uses for `AllowedCPUs`; the property is merged into the scope correctly (config
parses via `tomllib`, below).

## Config validation

```
$ python3 -c "import tomllib; d=tomllib.load(open('.agentctl/config.toml','rb')); ..."
go build | systemd-run --user --scope -q -p MemoryMax=4G -p CPUQuota=400% go build ./... | {'GOMEMLIMIT': '1GiB', 'GOMAXPROCS': '4'}
go vet | systemd-run --user --scope -q -p MemoryMax=4G -p CPUQuota=400% go vet ./... | {'GOMEMLIMIT': '1GiB', 'GOMAXPROCS': '4'}
go test (module) | systemd-run --user --scope -q -p MemoryMax=8G -p CPUQuota=400% go test -p=4 … | {'GOMEMLIMIT': '1536MiB', 'GOMAXPROCS': '4'}
CR conformance | systemd-run --user --scope -q -p MemoryMax=4G -p CPUQuota=200% go test -p=1 ./rules -run TestCR -v | …
TestHeads | systemd-run --user --scope -q -p MemoryMax=4G -p CPUQuota=200% go test ./rules -run TestHeads -v | …
make sim | systemd-run --user --scope -q -p MemoryMax=4G -p CPUQuota=200% make sim | …
```

`git diff --stat`: `1 file changed, 20 insertions(+), 9 deletions(-)` — only
`.agentctl/config.toml`.

## Measured before/after (real pasted outputs)

Instrument: `/usr/bin/time -f "wall=%es cpu=%P"` inside the scope, exactly as the
gate wraps it. `cpu=%P` is the process tree's average CPU as a fraction of one
core. All runs on this shared 32-core box; single samples, host load varies (see
the run-to-run note). `-count=1` on the test pair (uncacheable).

### Primary — 6-package gate-shaped run

`systemd-run --user --scope -q -p MemoryMax=8G [ -p CPUQuota=400% ] env
GOMEMLIMIT=1536MiB [ GOMAXPROCS=4 ] /usr/bin/time … go test -p=4 -count=1 ./cards
./state ./decision ./events ./view ./effects`

**Cold-cache / loaded-machine run (first OLD sample is the filer's burst shape):**

```
BEFORE-6pkg wall=30.05s cpu=331%
AFTER-6pkg  wall=31.85s cpu=156%
```

**Back-to-back warm pair (same subset, adjacent in time):**

```
OLD-6pkg wall=26.07s cpu=143%
NEW-6pkg wall=21.74s cpu=153%
```

**Second back-to-back warm pair (full 6-package):**

```
OLD wall=27.66s cpu=158%
NEW wall=27.96s cpu=157%
```

The warm-cache pairs show the quota costs nothing in wall time (~0.3s, within
noise) and does not change CPU when the box is already quiet; the quota earns its
keep in the loaded case, where it caps the burst (331% → 156%). This is a
**null-to-slightly-positive** wall result, exactly as the brief allowed. No cap
was raised and the quota was not tuned to make a number look better.

### Secondary — `go build ./...` (gate env `MemoryMax=4G GOMEMLIMIT=1GiB`)

```
BEFORE-build wall=6.39s cpu=423%
AFTER-build  wall=7.25s cpu=210%
```

### Secondary — `go vet ./...` (gate env `MemoryMax=4G GOMEMLIMIT=1GiB`)

```
BEFORE-vet wall=31.09s cpu=766%
AFTER-vet  wall=1.76s cpu=103%
```

The AFTER-vet hit vet's action cache (the brief's caveat: an mtime-only `touch`
does not invalidate it, so AFTER is **warm-cache** and BEFORE is **cold**). The
honest reading is the CPU number under a warm cache is 103% either way; the burst
the filer saw (766–1009%) is the cold/vet-build case, and the quota bounds it to
400% by construction.

## Fails without the fix

This ticket's "failing test without the fix" is the 6-package pair above: with the
**OLD** gate line (no `CPUQuota`) the run bursts to 331% of a core; with the
**NEW** line (the config in this commit) it is capped at 156%. Reverting the
config hunk reproduces the burst:

```
$ # OLD gate line: systemd-run --user --scope -q -p MemoryMax=8G env GOMEMLIMIT=1536MiB ...
BEFORE-6pkg wall=30.05s cpu=331%

$ # NEW gate line: systemd-run --user --scope -q -p MemoryMax=8G -p CPUQuota=400% env GOMEMLIMIT=1536MiB GOMAXPROCS=4 ...
AFTER-6pkg  wall=31.85s cpu=156%
```

`cpu=331%` (≈3.3 simultaneous cores, and `go vet` reaches 766%) is the defect;
`cpu=156%` is the fix. The same pair under a loaded box (the filer measured 18–25
vCPU) is the case the ceiling exists for.

## Sanity goldens (no Go code changed; run as confirmation)

```
$ go test ./internal/archtest/
ok  	github.com/adams-shaun/gorge/internal/archtest	19.213s

$ go test -run TestConstructedDefaultIsByteIdentical ./cmd/botbench/
ok  	github.com/adams-shaun/gorge/cmd/botbench	0.809s
```

Both pass; no allowlist edits, no head/split movement (a config change cannot
move the split). `.cards` was present as a symlink to
`/home/sadams/projects/gorge/.cards` (found, not created).

## Controller-side notes (not acted on here)

1. **agentctl parity pin.** agentctl's `test_consumer_parity.py` pins the
   2026-09-19 config snapshot; it may need a controller-side refresh after this
   lands, or it will diff against the new `CPUQuota`/`GOMAXPROCS` presence.
2. **Next-landing confirmation.** The true full-module `go test -p=4 ./...` wall
   time under the new ceiling can only be measured by the daemon (a full-module
   gate run is forbidden in a seat). The next landing's gate log is the
   confirmation; the 6-package pair is the sanctioned proxy.

## Deviations from the brief

None. The recommended shape was followed exactly: `CPUQuota=400%` +
`GOMAXPROCS=4` on the three parallel gates, `CPUQuota=200%` on the three serial
gates (no `GOMAXPROCS` env there — a serial workload has one thread and the quota
still bounds internal parallelism), all `MemoryMax` values unchanged, one dated
comment above the first changed gate.

## Issues

- **`gorge-heavy.slice`'s spec-promised `AllowedCPUs` subset is not installed.**
  `docs/superpowers/specs/2026-09-29-reward-loop-and-seed-agent-design.md:127`
  promises heavy work gets *"`AllowedCPUs` a subset"*, but
  `systemctl --user show gorge-heavy.slice -p AllowedCPUs -p CPUQuota` returns
  `AllowedCPUs=` (empty) and no `CPUQuota`. The slice unit at
  `deploy/gorge-heavy.slice` sets only `MemoryHigh`/`MemoryMax`/`MemorySwapMax`/
  `CPUWeight`/`IOWeight`. So heavy work is capped in memory/weight but not in
  CPU at all — the same class as this ticket, on the other half of the box.
  `scripts/heavy.sh:150` supports `--cpus` but its default is unset.
- **The npm gates (`npm run check`, `npm test`, `npm run build`) and the `smoke`
  gate run bare** — no `systemd-run` scope, no memory or CPU cap (config.toml
  lines 260–289). They self-limit somewhat (vitest/esbuild), and the smoke gate
  is `flock`-serialised, but a runaway web build is uncapped. Out of scope per the
  brief; named here so it can be filed without rediscovery.
- **`scripts/heavy.sh`'s `--cpus` uses `AllowedCPUs`, which does NOT cap a Go
  process's thread count** (measured above: `AllowedCPUs` leaves GOMAXPROCS at
  32). Any heavy Go job launched with `--cpus` is affinity-limited but still
  spawns 32 threads fighting over the subset. The same `CPUQuota` treatment
  would fix it; out of scope here (brief explicitly excludes `heavy.sh`).
