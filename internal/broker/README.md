# internal/broker — the experimental decision broker

*Optional package. Build and run with `-tags broker`; `make dzbroker`,
`make test-broker`. The default build compiles `broker_stub.go` only, so the
scheduler, the seat wrapper and their dependencies stay out of the default
build, `go vet`, and the 32-bit gate.*

## What it is

A scheduler that batches learner decisions from many independent gorge games
into one inference call, **with no all-games barrier**. It is the gorge
counterpart of mtg-kernel's `src/async_rollout.rs`, which states the rule it
implements:

> The broker snapshots whichever lanes are ready instead of imposing an
> all-lane barrier.

That sentence is the whole design. A game waits only for the batch that formed
when its own decision arrived; it never waits for every other game to reach a
decision point.

## Shape

- **Workers own games.** Each worker runs the whole rules engine locally.
  Opponent moves never cross the broker boundary.
- **Only wrapped seats talk to the broker.** `Broker.Wrap(inner, seatIdx)`
  publishes a decision for scheduling and releases it with the intent `inner`
  chose, so wrapping changes only *when* a decision is served, never *what*.
  An unwrapped seat is the control.
- **The broker serves the ready set.** With `Wait = 0` it drains whatever is
  queued at that instant and serves it (the purest no-barrier behaviour). With
  `Wait > 0` it lets stragglers join the batch first.
- **The scorer is pluggable.** `PassThrough` is the control (no model cost);
  a GPU scorer replaces it without touching the scheduler.

## The game multiplier (`cmd/dzbroker`)

`-multiply N` expands one `(deckA, deckB, base seed)` matchup into N games with
distinct shuffles (`seed = base + i`), so the same decks and the same bots
produce N different outcomes — a win-rate sample from one matchup. Widening N
never renumbers an existing game.

## Measured envelope

`boros-moxite-burn` vs `cavalry-charge`, 64 games, real decks, `PassThrough`
scorer, RTX-class box, 2026-10-07:

| workers | broker wait | mean ready-set width | max width | games/s |
|---|---|---|---|---|
| 2  | 0 ms | 1.02 | 2  | 75 |
| 4  | 0 ms | 1.06 | 4  | 133 |
| 8  | 0 ms | 1.11 | 5  | 208 |
| 16 | 0 ms | 1.20 | 11 | 276 |
| 32 | 0 ms | 1.25 | 32 | 308 |
| 16 | 1 ms | 14.49 | 16 | 29 |
| 16 | 5 ms | 14.50 | 16 | 6 |

Two findings, both matching the reference project's own observation that *"a
fast broker normally observes only one ready learner decision at a time"*:

1. **Without a wait, the ready set is ~1 wide even at 32 workers.** Games
   drift out of lockstep, so batching many games' decisions into one launch
   does not happen by itself.
2. **A wait widens the batch and costs throughput linearly.** 1 ms buys a
   12× wider batch (1.2 → 14.5) and costs 10× the throughput (276 → 29
   games/s), because every decision pays the wait. 5 ms buys nothing more
   (14.50 vs 14.49) and costs 4× again.

The consequence for a GPU scorer: at `Wait = 0` you are launching a kernel per
~1 decision, which is pure overhead; a real GPU path needs either a small wait
(buying width at a known latency) or the fixed-stride rounds mtg-kernel's v2
scheduler uses for determinism. The `-broker-wait` flag is the knob to find
that point per workload.

## Not proven

- The scorer is a pass-through: no model, so the numbers are the scheduling
  envelope only, not end-to-end inference throughput.
- Both seats are wrapped in `cmd/dzbroker`; a real run wraps only the learner,
  which would *narrow* the observed width.
- Single deck matchup, one box. The width/worker curve will move with deck
  and turn distribution.
