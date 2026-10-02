# Checklist: MageZero-style training on gorge

*2026-10-02. Status: checklist only, nothing here is scheduled.*

DraftZero trains its FDN Limited agents on XMage through MageZero
(`java/mzbridge`): tree search guided by a PyTorch network, an inference
server, a trainer and a self-play loop. Its own engine comparison
([docs/015](https://github.com/danieljbrooks/draft-zero/blob/main/docs/015-rules-engine-comparison.md),
gorge at `a4af596`, 2026-09-27) calls gorge "the only one of the four that is
both fast and has the FDN cards today" and lists what the gorge route would
need. This file is that list, with where each item stands on `main` and how
we know.

"Done" means measured on `main` at `4fcbb61fb` unless a different commit is
named. Re-measure before quoting; these go stale with every merge.

## Already in place

- [x] **Stepping a game from outside, in-process, reproducible from a seed.**
  `rules.Engine` (`Submit`, `Clone`, `CloneHypothetical`).
- [x] **Per-player redacted views.** `view/visibility.go`.
- [x] **Tree search over the engine.** `internal/azmcts`; PIMC with 1 or 4
  worlds and IS-MCTS run upstream's sb-v1 protocol in `internal/searchbench`
  at about a tenth of upstream's worker-seconds per decision
  (`docs/superpowers/reports/2026-10-01-searchbench-replication.md`).
- [x] **17lands positions rebuilt on gorge.** `searchbench build`: of 11,449
  rejected candidates in the sb-v1 build, about 590 are gorge-side; the rest
  are upstream's own prep rejecting
  (`/mnt/sata/gorge-training/searchbench/sb-v1/gorge2/rejections.jsonl`).
- [x] **FDN primitives registered.** `TestFDNCoverage`: 281/281 non-basic
  cards, 16/16 pool decks. `TestFDNParamCensusReport`: 0 cards with an unread
  parameter. `TestFDNCastability`: 15/15.
- [x] **Training records from human games.** `searchbench train-build`
  (branch `wt/il-enc`, not merged): 10,000 games give 54,210 records.
- [x] **A scorable encoding for payment and macro candidates.**
  `internal/azmcts/scoreopts.go` (branch `wt/il-enc`, not merged). Without it
  any learned prior is inert at priority roots under auto-payment.

## Missing

- [ ] **Batched network bridge.** gorge's net (`internal/policynet`) is a
  small CPU-only Go model. MageZero's are PyTorch behind an inference server.
  Needs a process boundary outside the rules core (no cgo there): a step
  server speaking reset / observe / options / step / clone, or a batched
  inference client the Go search calls. Upstream's own finding: on a fast
  engine, inference becomes the bottleneck, so batching across many games is
  the point of the bridge, not an optimisation.
- [ ] **Observation and action mapping.** MageZero hashes actions into a
  set-wide vocabulary; gorge offers positional options. Their trained nets
  cannot be plugged in. Either map gorge options onto their vocabulary, or
  retrain on gorge's features.
- [ ] **Opponent-pool sampler for Limited.** `internal/searchprobe/redeal.go`
  refuses unless the hidden pool is derivable from a public decklist. Limited
  has no public list; the dealer must guess the opponent's 40 from what has
  been seen.
- [ ] **A self-play loop that improves.** Records (`azvisits-v1`) and a
  trainer (`cmd/policytrain`) exist. No loop here has produced a gain:
  `az-redeal` loses to `sb-tactical`, PPO went flat, and the trainer has no
  best-epoch checkpoint or learning-rate decay.
- [ ] **A leak-free FDN value network.** The replication ran heuristic leaves
  only for this reason.
- [ ] **Behavioural card audit for FDN.** Registration coverage is not
  behaviour. The Oracle-text audit covers 18 of 281 FDN cards (5.2% of deck
  copies). The parameter census cannot see a value that was read and then
  defaulted: Nine-Lives Familiar returned with the wrong counter count while
  the census read 0 (fixed on `wt/wca-x`). Cheapest next measurement: play
  the FDN pool and count `Note` events by text.
- [ ] **A pinned engine commit per experiment.** Merging 198 engine commits
  changed 4–6% of benchmark rows for PIMC and 25% for IS-MCTS. A training run
  and its evaluation must name one commit.

## Evidence to weigh before building any of it

Upstream's case for a faster engine is that more search for the same money
should improve play. Our measurements do not support that yet:

- Agreement with top players was flat from 100 to 10,000 simulations for
  every method on gorge (replication report, finding 2).
- In play, 400 against 100 simulations gained +4.4 points at 4.5× wall time
  (`/mnt/sata/gorge-training/killtests-1001/RESULTS.md`; measured on a binary
  that predates the perf merge).
- An imitation net trained on 17lands data agreed with humans about 7 points
  better than the bot and lost games when used as a cast veto
  (`/mnt/sata/gorge-training/imitation/play/RESULTS.md`).

## Open question

Who uses the result: DraftZero training on gorge as its backend, or gorge
running a MageZero-style stack itself? The bridge and the sampler serve both;
the vocabulary mapping matters only for the first.
