# Corpus coverage census (2026-10-10)

Tool: `go run ./cmd/corpuscov -games 580 [-policy P] [-explore p] [-json out] [-record out.jsonl]`
(package `internal/corpuscov`). Data: `/mnt/sata/gorge-training/corpuscov/{bot,fuzzx,botx10}-580.{txt,json}`.
Each run is 580 2-seat games (each of the 29 repo decks seated 20 times; the commander decks play
FormatCommander), seed 1, autopay on. It takes about 55 s on 2 cores.

## Identity

Options already map back to (card, ability), so no engine change was needed. A priority
option carries `Obj` plus `Kind`/`Mode`/`AltCostIndex`/`Ability` (the flat
`Face().Abilities` index, rules/legal_walk_battlefield.go:482). A payment-plan cast is
read through `PaymentAction.Cast.Object`, and its mana steps through
`Plans[].Activations[].Source`. Alternative casts (`cast/alt1`, `cast/flashback`, ...)
fold onto the card's `cast` slot. Non-priority options become dynamic `(card, kind:optKind)`
rows that have no universe. The universe is 1636 slots: cast or land play, every
activated ability, and one mana slot per face.

Stages: IN_DECK means the card stood in the slot's zone at a decision (hand or
command for a cast, ActivationZone$ for an ability). POTENTIAL means the slot appeared
in the seat's `PotentialActions`, which is payable after tapping out; casts the planner
proved unpayable are excluded. OFFERED means the slot was a decision option. CHOSEN
means the policy picked it.

## Numbers (580 games)

| policy | IN_DECK | OFFERED | CHOSEN | policy-avoided | structural (pool-priced) | structural (other) |
|---|---|---|---|---|---|---|
| `bot` (production) | 1632 | 1583 | 1529 | 54 | 36 | 13 |
| `bot` + explore p=0.1 | 1632 | 1589 | 1535 (+38 explore-only) | 16 | 33 | 10 |
| cardfuzz-explore (diagnostic) | 1627 | 1589 | 1569 | 20 | 23 | 15 |

decision.Kind coverage for `bot`: every kind was asked except `mulligan` (0). The driver
does not set `Config.Mulligans`, matching mtgsim. The kinds asked were priority 253k,
choose 12.3k, target 5.3k, attackers 5.3k, blockers 1.6k, modes 1.2k, trigger_optional 877,
trigger_order 830, arrange 420, commander_zone 271, replacement 46 and starting_player 580.

## Structural gaps (IN_DECK, never OFFERED)

1. **Pool-priced activated abilities: 36 slots, the largest class.** The offer walk prices
   ability costs against the *floating* pool only: `offerCastable` reads `w.pricing()`
   (rules/legal_walk.go:81), which is nil (the real pool) on the priority walk, gated at
   rules/legal_walk_battlefield.go:436. Payment plans exist only for casts
   (`PaymentAction.Cast PlannedCast`, decision/payment_plan.go:211). The bot floats
   mana only toward a castable card (botpolicy/policy.go:449-451, X6). So an ability
   such as Spectral Sailor's `{3}{U}: draw` (potential in 47 games, offered in 0),
   Aftermath Analyst, Batterskull's bounce, Intelligence Bobblehead, Sea Gate Wreckage,
   Hoard-Smelter Dragon and Scavenging Ooze never reaches the policy. cardfuzz's
   speculative float cuts the class from 36 to 23 slots.
2. **Rules-correct.** Haakon (cast only from the graveyard, CantBeCast from hand),
   Sea Gate Wreckage (no cards in hand), Plasma Caster (needs a blocker of the equipped
   creature), Ojer Axonil's Temple (CheckSVar), Tezzeret and Wrenn and Six ultimates
   (loyalty never high enough).
3. **Unexplained, needs a probe:** Walking Ballista's `{4}: +1/+1 counter` (in deck in 15
   games, never potential); Mount Doom ability#2; Mirrorpool's copy-spell; Akroma's
   firebreathing (probably cast face-down via morph and never turned up);
   Squandered Resources (a ManaReflected mana ability with a Sac cost, never offered
   as a mana action).

## Policy avoidance (OFFERED, never CHOSEN): 54 slots, 48 of them abilities

The dominant cause is botpolicy's ability ranking. `abilityCost` reads the first digit
run of the label (botpolicy/ability.go:258). A loyalty label carries no cost digits, so
every loyalty ability scores 20 and the A3 tie-break takes the lowest index
(botpolicy/ability.go:302). Only the first `+N` ability of a planeswalker is ever chosen.
Jace TMS #1/#2/#3 (649/287/361 offers), Karn x2, Lord Windgrace, Ugin, Tezzeret and
Chandra are all never chosen. Exploration at p=0.1 reduces avoidance from 54 to 16 slots.

## Coverage-directed exploration

`corpuscov.ExploreSeat` wraps any seat. On a priority decision it forces the
least-chosen offered slot with probability p, using a seeded PCG and uniform tie-breaks;
mana taps are excluded. The answer is counted as `ExploreChosen`, apart from the
policy's choices. With `-record`, the decision line is written with `"explore": true` so
imitation can drop it. It is off by default (`-explore 0`). Determinism and flag/count
agreement are tested in cmd/corpuscov/main_test.go. At p=0.1 it forced 13,043 of 267k
priority decisions.

## Not measured

The search teacher (`bots/search` and other Env seats) needs a host-built Env, which
this driver refuses. The record format is the census driver's own; searchteacher/
policytrain labels do not carry the explore flag yet.
