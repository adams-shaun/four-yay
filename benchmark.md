# dzgorge self-play benchmark

Wall-clock games/hour of the dzgorge push workload, used to measure the
heap-object perf cuts (see `docs/superpowers/reports/2026-10-07-heap-object-attack-poc.md`).

Contract: the perf changes must stay **bit-identical** — same events, RNG,
chain head and visit records. A run prints a corpus hash; it must match across
arms (`370fe249f618c93b…` for 34 pairs, `129c9238651fd55c…` for 70 pairs).

## Build

```sh
cd /home/sadams/projects/gorge/.worktrees/dzgorge-rebase
go build -o bin/dzgorge ./cmd/dzgorge
```

Build once per arm (baseline vs cut) and time each binary.

## Run

`GOMAXPROCS=5`; `-pprof` off (diagnostic only, adds overhead):

```sh
GOMAXPROCS=5 ./bin/dzgorge play \
  -cards .cards \
  -decks /mnt/sata/gorge-training/dzgorge-repro/inputs/decks \
  -pool  /mnt/sata/gorge-training/dzgorge-repro/inputs/pool.tsv \
  -split train \
  -a 'az:sims=100:autopay:oppnodes:explore' \
  -b 'az:sims=100:autopay:oppnodes:explore' \
  -seed 701 -mulligans 2 -mull-heuristic \
  -record-features entity \
  -pairs 34 -workers 5 \
  -out /tmp/ab-cut.jsonl
```

- `-a` and `-b` the same spec -> each pair played once (34 games). Different
  specs -> paired legs, policies swapped across seats.
- `-pairs 34` for repeatable A/B reps; `-pairs 70` for the directional (noisier)
  arm.
- `-out` is required. Add `-corpus /tmp/ab-cut.jsonl.corpus.gz` to also write the
  visit corpus.
- Baseline vs cut = `bin/dzgorge` from `wt/dzgorge-rebase` vs `wt/heapattack`
  (the perf branch).

## Read

Metrics land in `/tmp/ab-cut.jsonl.summary.json`:

- `games_per_hour` and `wall_s` — the timing signal.
- `errors`, `stalls` — must be 0.
- `az_decisions_searched`, `az_completed_sims_per_searched` — the search did the
  intended work.

GC knob: the thesis is alloc-rate-driven, so the GOGC knob alone moves the
number. Compare `GOGC=off`/`GOGC=800` with `GOMEMLIMIT` set as well as the
default. GC settings never change the output hash.

## Profile

Same command with `-pprof` (also enables block + mutex profiling), fetch while
it runs:

```sh
GOMAXPROCS=5 ./bin/dzgorge play ... -pprof 127.0.0.1:6095 &

go tool pprof -proto -output /tmp/cut9-cpu.pb.gz    'http://127.0.0.1:6095/debug/pprof/profile?seconds=30'
go tool pprof -proto -output /tmp/cut9-heap.pb.gz   'http://127.0.0.1:6095/debug/pprof/heap'
go tool pprof -proto -output /tmp/cut9-allocs.pb.gz 'http://127.0.0.1:6095/debug/pprof/allocs'
```

Ports/`seconds` vary per capture. Inspect allocations with:

```sh
go tool pprof -sample_index=alloc_space -top -nodecount=30 ./bin/dzgorge /tmp/cut9-allocs.pb.gz
```

## POC result (2026-10-07)

- Structural cuts (Engine recycle + `paymentIntent` scratch + mana-member-carry):
  ~**+3.8 %** games/hour at default GOGC, **bit-identical**.
- GOGC knob alone: ~**+8 %** (baseline default -> off/800).
- Combined: ~**+10.8 %** with `GOGC=off`.
- `alloc_objects` ~**-30 %**; heap inuse ~-10 %; GC proper ~2.9 % CPU.

Absolute games/hour are **not comparable across sessions** (shared box); only
paired, interleaved A/B deltas on a quiet box are.
