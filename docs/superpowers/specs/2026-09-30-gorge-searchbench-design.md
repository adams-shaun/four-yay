# Gorge-native FDN search benchmark — design (2026-09-30)

## Purpose

This is a native Gorge replication of DraftZero experiment #3, not a claim to
reproduce its exact XMage numbers. It asks the same questions on the same kind
of public FDN Limited data:

- do honest PIMC and IS-MCTS agree with expert choices as well as an oracle
  MCTS control;
- does more simulation budget improve that agreement;
- do a learned value and policy prior improve the result; and
- do honest methods make the same choice under observably identical but
  hidden-information-different positions.

Gorge is the only rules engine and search implementation. `clairvoyant` is a
benchmark control, structurally unavailable to hosted/rated play.

## Scope and measures

The v1 item set contains the four labels the original study could rebuild:
spell, hold, attack and block. Its intended complete split is 300 development
and 1,000 test items, selected from FDN Premier Draft 17lands replay data
using the documented top-player filter (game win rate at least 0.60 and at
least 100 games), one or two items per game, turns 3–12.

The headline metric is the mean of balanced accuracies for cast-or-hold,
attack-or-not, and block-or-not. Plain macro agreement is reported only for
comparison. Every result also reports action/pass rates, conditional
which-action agreement, depth, turns crossed, refusal/fallback counts and
paired bootstrap 95% intervals. Constant passive, constant active, random
legal and ordinary `botpolicy` are required baselines.

## Immutable input boundary

Raw 17lands data, reconstructed roots, model checkpoints and run outputs live
under `/mnt/sata/gorge-training`; none is committed. A source file's licence,
URI and SHA-256 are recorded in a sealed manifest, as are the Forge pin and
compiler fingerprint. The manifest contains no Forge or token script text.

`internal/searchbench.Manifest` is the v1 contract. Each item records:

- source game/draft identity and a deterministic dev/test assignment;
- decision type, actor, turn and sequence;
- T0/T1 reconstruction classification;
- SHA-256 digests of the replay prefix, redacted public state and canonical
  legal options;
- the human action as canonical option positions and an act/wait bit; and
- eight fixed, distinct world seeds.

The manifest is sorted by item ID, seals every field with SHA-256, forbids a
draft from crossing dev/test, and enforces the declared item counts and
per-game maximum. Consumers use `searchbench.Read`, never a loose JSON
decoder. The current command proves an artifact is usable:

```sh
go run ./cmd/searchbench manifest validate -in /mnt/sata/gorge-training/searchbench/sb-v1/manifest.json
```

The future item builder must replay all prior human actions through
`rules.Engine`, never mutate `state.Game`, and retain only items whose public
projection and legal-option fingerprint match the source reconstruction.
Failures remain in a rejection ledger with a reason code; they must not be
silently dropped.

## Search arms

All arms share a candidate vocabulary, opponent policy, depth/submit cap,
semantic action identity, tie break and leaf evaluator. This prevents an
action-generation change from masquerading as a search-method result.

| arm | world treatment |
|---|---|
| clairvoyant MCTS | benchmark-only clone of the recorded full world |
| PIMC-1 | one honest sampled world and one tree |
| PIMC-4 | four sampled worlds, even budget split, root visits merged by semantic action |
| IS-MCTS | one information-set tree, redeal each simulation, availability counts |

The initial matrix uses 100, 300, 1,000 and 3,000 simulations for all arms;
PIMC-1 and IS-MCTS also run at 10,000. It first uses
`searchprobe.LeafValue`, then repeats with a redacted `policynet` value
checkpoint. Policy-only and dev-selected policy-prior arms are reported after
the heuristic matrix is sound.

`internal/searchprobe` owns public observations and world sampling;
`internal/azmcts` owns PUCT, cloning and diagnostic counters; `internal/policynet`
owns redacted policy/value evaluation. Wall-clock measurement stays at the
command boundary. The engine and search core remain deterministic and
clock-free.

## Fairness probes and acceptance

Before the main matrix, paired fixtures must establish the information
boundary. In each pair the searching seat's observable state, option list and
world seeds are identical; only an opponent hidden card or next hidden draw
changes. PIMC and IS-MCTS must choose identically. Clairvoyant MCTS must
change on enough pairs to prove that a probe can detect a leak.

Every run stores the manifest digest, code commit, Forge pin, checkpoint
digest, machine description, command line, worker/core allocation and the
per-item output. A run is publishable only when its items validate, every
recorded root replays deterministically, honest arms pass all leak probes, and
the analysis includes every exclusion and fallback.

## Delivery order

1. Manifest contract and validator (this change).
2. 17lands-to-Gorge item reconstruction and rejection ledger; create and seal
   a small development manifest.
3. Heuristic PIMC-1 and clairvoyant controls over that manifest, including
   replay/fidelity checks and paired probes.
4. PIMC-4 and IS-MCTS under the same runner; execute the full heuristic
   matrix.
5. Add redacted learned value and policy-prior arms, dev tuning only, then
   publish the test report and artifacts.
