# Bot policy consolidation plan

Status: analysis and plan. Nothing here is implemented. Base: `main` at
`3486783` (worktree `wt/bot-consolidation-plan`, created by
`scripts/agent-worktree.sh`). Every `path:line` below was read in this worktree.
Where I ran something, the command is given; where I did not, I say so.

## 1. Recommendation in one page

Operator intent: end with one uniform-random bot, one first-choice bot, one
basic heuristic bot and one tuned heuristic bot, with the AI bots (`search`,
`az-redeal`, `sb-search-*`, later `az-c3`) untouched.

| Tier | Keep (registry name) | Why |
|---|---|---|
| Random | `sb-uniform` | The SpellBench 1000-Elo anchor; name is baked into rating tooling. |
| First choice | `sb-first` | Same package family; no overlap. |
| Basic heuristic | `bot` | The only heuristic with a Commander/4-seat measurement, the default, the human-seat caretaker, and the base the search bots model. |
| Tuned heuristic | `sb-tactical` | +250 Elo over `bot` on every SpellBench pool (confounded, see 3.3); already the inner policy of `sb-search-*`. |

Retire as hosted entries: `lethal-pressure`, `cast-profile` (both become
aliases of `bot`), and `sb-heuristic` (recommended; see section 6 for the
alternative).

Four findings drive this, all verified here:

1. **`lethal-pressure` is already folded into `bot`.** `botpolicy.Decide` and
   `LethalPressureDecide` are the same call (`botpolicy/policy.go:391-403`). I
   played 24 full games (4 deck pairs x 6 seeds) of hosted `bot` against hosted
   `lethal-pressure` on manual mana: all 24 had identical outcome, turn count and
   intent count. On auto-pay tables `bot` is a strict superset: it is
   `lethal-pressure` plus the combat-simulation attacker (`bots/bot/bot.go:19-21`,
   `seat/bot.go:190-192`), promoted by the operator at +3.17pp constructed /
   +3.69pp Commander (commit `66aa546dd`). 10 of 24 games differed in that mode.
   So "fold lethal-pressure into bot" means deleting a weaker duplicate, not
   merging logic.
2. **`cast-profile` has no production consumer.** Nothing in `scripts/`,
   `Makefile`, `deploy/`, `.github/` or `web/` names it. It is intent-identical to
   manual `bot` by construction (`botpolicy/profile_test.go:19-30`), and its one
   fitted candidate failed the +3pp gate (50.92% [49.38, 52.47]).
3. **The demo's default is `lethal-pressure`** (`scripts/deploy-demo.sh:71`,
   `cmd/gorged/main.go:283`). That is the real blocker for retiring it, and it is
   the only reason the demo runs the weaker auto-pay bot.
4. **The common-sense guard must be a decorator, not a change inside `bot`.**
   `bot` is also the bench control arm and the SpellBench `bot` anchor
   (`cmd/botbench/spellbench_registry.go:23`), and `sb-heuristic`'s self-hit
   problem is mostly target choice, which the guard as designed does not touch.

Decisions for the operator are in section 11.

## 2. What I ran

All from `/home/sadams/projects/gorge/.worktrees/bot-consolidation-plan`.
Test runs used
`systemd-run --user --scope -q -p MemoryMax=4G -p CPUQuota=400% env GOMAXPROCS=4 GOMEMLIMIT=3GiB go test -timeout 2m -run <X> <pkg>`.

- `git grep -n -I` over the whole tree for every policy name, constructor and
  constant (excluding `docs/superpowers/plans` where noted). Results are
  summarised per policy in section 3.
- A throwaway test in `cmd/botbench` (deleted; not committed) that plays
  `bots.New("bot")` against `bots.New("lethal-pressure")` through `playMatch` on
  the same configs and compares winner, winner seat, turns and intents. Result:
  `autopay=false: identical-outcome games=24 different=0`;
  `autopay=true: identical-outcome games=14 different=10`. Pairs:
  death-n-taxes/dimir-tempo, mono-red-goblins/uw-control, tron/ur-delver,
  boros-moxite-burn/mono-green-stompy; seeds 0-5; `playMatch` caps 200 turns /
  20000 intents. This is a fingerprint (winner, turns, intents), not an event-log
  hash; the code-level identity in finding 1 is the stronger evidence.
- A throwaway test in `host` (deleted) that seats each of `bot`, `sb-uniform`,
  `sb-first`, `sb-heuristic`, `sb-tactical` on a 2-seat and a 4-seat
  **Commander** table (auto-pay on, the five `foundations-*` commander decks,
  seed 1001, `MaxIntents` 6000). Result, one seed only:

  | policy | 2 seats | 4 seats |
  |---|---|---|
  | `bot` | finished, 26 turns | finished, 36 turns |
  | `sb-uniform` | finished, 36 turns | finished, 42 turns |
  | `sb-first` | finished, 186 turns | **did not terminate within 6000 intents** (turn 200) |
  | `sb-heuristic` | finished, 39 turns | finished, 35 turns |
  | `sb-tactical` | finished, 23 turns | finished, 37 turns |

  This shows the `Formats: constructed` / `MaxSeats: 2` declarations on the
  `sb-*` entries are conservative metadata, not a crash. It says nothing about
  play quality in Commander. The `sb-first` row is an artefact of what that
  policy is: it answers option 0, which is pass (`internal/spellbench/builtins/builtins.go:21-22`),
  so a game ends only by decking, and a 4-seat pod takes longer than my cap.
- Read, did not re-measure: every strength number below is cited to its report.

## 3. Per-policy dossier

### 3.1 Registry facts common to all

- Registry: `bots/registry.go` (`Register` :82, `Lookup` :92, `Normalize` :94,
  `Validate` :135). Ten entries are linked by `bots/all/all.go`; the count is
  pinned at `bots/registry_test.go:35` (`len(entries) != 10`).
- `Info.Caretaker` is `"bot"` on every entry (`bots/*/…go`); `bots.Validate`
  fails if the caretaker is not registered (`bots/registry.go:140-145`). So
  `bot` cannot be removed while any entry names it. (At run time the human-seat
  timeout caretaker is built from the table's own policy name,
  `host/match.go:665-668`, not from this field.)
- Hosted construction: `host/bot_policy.go:53-55` -> `bots.New`. A table's
  policy is normalized at config validation (`host/table.go:206`), so
  `tables.json` with an unknown name fails the whole registry load
  (`host/restart.go:57-60`). On-demand (vs-bot) tables are dropped at load
  (`host/restart.go:62-70`).
- Offered vs registered: `-bot-policies` (default empty = every registered
  entry, `cmd/gorged/botpolicies.go:12-27`) gates `GET /api/bot-policies` and
  `POST /api/games`. `deploy-demo.sh` passes only `-bot-policy`
  (`scripts/deploy-demo.sh:266`), so all ten are offered on the demo today.
- Format/seat enforcement is thin: gorged requires the `-bot-policy` default to
  support both formats and `MaxSeats >= 2` (`cmd/gorged/botpolicies.go:84-91`);
  `POST /api/games` requires the requested format in `Info.Formats` and
  `MaxSeats >= 2`, and a vs-bot table is always two seats
  (`cmd/gorged/game.go:59-71`). Nothing checks a startup table's seat count
  against `MaxSeats` (no reference to `MaxSeats` outside those two files and the
  registry).
- Golden heads: `TestHeads` and the acceptance games run `botpolicy.GameBot`,
  which calls `botpolicy.Decide` directly (`botpolicy/gamebot.go:40`). **Hosted
  factories are not on that path**, so changing `bots/*` does not move
  `rules/testdata/heads/{2,4,6,8}.txt`. Changing `botpolicy.Decide` does
  (`docs/agents/invariants.md` section 9).
- archtest: `bots/...` must not depend on `host`, `internal/testutil` or
  `internal/azmcts/clairvoyant` (`internal/archtest/arch_test.go:233-238`);
  `protocol` must not depend on `bots` (:157). A new decorator package under
  `bots/` must respect both.

### 3.2 `bot` (the default)

- Entry: `bots/bot/bot.go`. Manual mana -> `seat.NewBot(seed)` (:22), i.e.
  `botpolicy.Decide`. Auto-pay -> `seat.NewAttackSimBot(...).EnableAutoPayMana()`
  (:20), i.e. the same policy with the combat-simulation attacker.
- Formats: constructed + commander (:14). `MaxSeats` unset (no cap). Tested at
  2 and 4 seats (section 2).
- Users: the vocabulary default (`bots/registry.go:19`); `host_test`, caretaker
  tests; the gorged default for the registry (`bots.Default`); web fixtures use
  the literal `'bot'` only in tests (`web/src/lib/*.test.ts`); SpellBench
  registry `"bot"` (`cmd/botbench/spellbench_registry.go:23`) which resolves to
  `hostedPolicy(bots.Default)` and therefore to **manual** mana
  (`cmd/botbench/main.go:385-401`: only `sb-uniform/first/heuristic` are forced to
  auto-pay in the bench).
- Dependencies in the other direction: the search bots do **not** use the
  hosted `bot` entry. They build `seat.NewBot` directly: `internal/azmcts/seat.go:157`
  (plus `.EnableAutoPayMana()` when the seat searches with auto-payment, :158-163),
  `internal/azmcts/env.go:236` and `nodecache.go:418`,
  `internal/searchseat/searchbot.go:129`, `internal/spellbench/kshadow/*`,
  `internal/searchbench/*`. `sb-search-*` falls back to `botpolicy.Decide`
  (`internal/spellbench/sbsearch/sbsearch.go:233,815`). The `az-c3` branch
  (`wt/az-c3-hosted`, not examined beyond its registry entry and diff stat) is
  declared `Caretaker: "bot"` and constructed/2-seat.
  **Consequence: any change to `seat.NewBot` or `botpolicy.Decide` reaches every
  search bot, the golden heads and the training corpora. A change confined to
  `bots/bot` does not.**
- Strength: 53.17% [52.68, 53.65] constructed and 53.69% [52.73, 54.64]
  Commander for the auto-pay combat-sim arm vs the plain auto-pay bot, held-out
  (commit `66aa546dd`; `bots/bot/bot.go:11`). SpellBench Elo 1242 (pauper),
  1277 (held-out seed), 1315 (FDN) in
  `docs/superpowers/specs/2026-09-28-spellbench-agent-design.md` section 3.4;
  1157 [1086, 1231] on the 13 repo-constructed decks
  (`docs/superpowers/reports/2026-09-30-repo-constructed-gauntlet.md`, "Anchored
  Bradley-Terry").

### 3.3 `sb-tactical` (the tuned candidate)

- Entry: `bots/sbtactical/sbtactical.go`. Auto-pay mode always
  (`New`, :80), needs `Deps.Cards` (refuses without it). `Env: true`: at
  priority it receives the host's honest-root planner
  (one redeal per priority decision, package doc :1-22).
- Formats `constructed`, `MaxSeats: 2` (:63-65), cost "~1 ms INFERRED" (`Info.Cost`).
- Why 1v1 only. Three separate reasons, none a crash:
  1. Declared, not proven: the packages spec recommends "constructed-only for
     every new entry until a Commander smoke" (`docs/superpowers/specs/2026-09-28-hosted-bot-packages.md`
     Q3, :1409-1412) and calls the new entries "2-seat only" (Q4, :1414).
  2. The code has a single-opponent model: `newState` picks the first
     non-lost other player as `oppP` (`internal/spellbench/builtins/tactical_state.go:124-137`),
     target direction compares `o.Player == s.opp`
     (`tactical_combat.go:29`), and the early-game window is computed from
     `(v.Turn+1)/2` ("the game's turn counter counts both players' turns")
     (`tactical_state.go:140`). Archetype classification is also per single
     opponent (`tactical.go:279`). In a pod it models one arbitrary opponent.
  3. It was only ever measured on two-player pools (pauper kernel, FDN limited,
     13 repo constructed decks).
  My Commander smoke (section 2) shows it plays 2- and 4-seat Commander tables
  to completion without refusal; quality in pods is unmeasured.
- Users: `sb-search-*` builds its own inner tactical with `sbtactical.Hosted()`
  weights (`bots/sbsearch/sbsearch.go:131`), so **tuning or changing sb-tactical
  weights moves `sb-search-lite-atk`**. botbench policy map
  (`cmd/botbench/main.go:452`), SpellBench registry (many `sb-tactical-*`
  ablation arms, `cmd/botbench/spellbench_registry.go:51-96`),
  `scripts/spellbench-rate.py:42-47`, `cmd/sbagent`/`cmd/sbv1agent` shadows.
- Strength: Elo 1507 [1465, 1555] vs `bot` 1242 (pauper), 1538 vs 1277
  (held-out seed), 1444 vs 1315 (FDN); head to head 110-18, 106-22, 87-41
  (spec section 3.4). Repo-constructed: 1438 [1362, 1532] vs `bot` 1157, tactical
  >=83% on 11 of 13 decks (gauntlet report).
  **Confound I could not remove:** in these runs `bot` is the manual-mana seat
  while `sb-tactical` is auto-pay, and the README records manual-tap bots losing
  about 190-230 Elo to plan-based payment (`README.md:225`). The hosted
  auto-pay `bot` additionally gains ~+3pp from combat simulation. I did not find
  a report of `sb-tactical` against the hosted auto-pay `bot`
  (`attack-sim-auto-pay`); the +250 Elo gap is therefore an upper bound on the
  hosted gap.

### 3.4 `sb-uniform`

- `bots/sbuniform/sbuniform.go`; wraps `builtins.New(builtins.Uniform, mode,
  seed ^ UniformSeed)` (:35-37). The XOR is load-bearing: it preserves the
  benchmark's stream. Formats `constructed`, `MaxSeats: 2` (:23). Mode is
  auto-pay when the table is, otherwise manual (:40-46).
- Users: botbench (bench forces auto-pay, `cmd/botbench/main.go:385-389`),
  SpellBench registry (`internal/spellbench/registry/sb.go:22`, plus `-manual` and
  `-planned` variants), `scripts/sb-gauntlet.sh:130,233` and
  `scripts/spellbench-rate.py:34` (anchor), `scripts/smoke-perf.sh:104,107`,
  `cmd/dzgorge/policy.go:17`, many `cmd/botbench` tests, `host/hosted_policies_test.go:420`.
- Strength: the Elo anchor, 1000. Commander 2/4-seat plays to completion (section 2).

### 3.5 `sb-first`

- `bots/sbfirst/sbfirst.go`, same shape, `builtins.First`. Elo 708 vs `bot` 1238
  on pauper (`bots/sbfirst/sbfirst.go:20`, spec section 3.1). Answers candidate 0;
  at priority that is pass, so it never casts or attacks (`builtins.go:21-22`).
  Games end by decking: 186 turns / 14,368 events in my 2-seat Commander smoke.
  Users as 3.4 (SpellBench `baseline` tag, `spellbench-rate.py:35`;
  `internal/spellbench/registry/*_test.go` decorator tests).

### 3.6 `sb-heuristic`

- `bots/sbheuristic/sbheuristic.go`, `builtins.Heuristic`: play land > cast >
  ability, attack with everything at the first defender, never block
  (`builtins.go:16-21,108,118`). Formats `constructed`, `MaxSeats: 2` (:23).
- Strength: Elo 1078 [1041, 1116] pauper, 1073 held-out, 1114 FDN
  (spec section 3.4); 1103 vs `bot` 1327 on FDN in the fdn-limited report
  (`bots/sbheuristic/sbheuristic.go:20`); 1046 [976, 1119] repo-constructed.
  These are two different FDN runs and I did not reconcile them.
- Users (broadest of the four): botbench tests that name it as the standard
  opposing reference (`cmd/botbench/corpus_test.go:31`, `pursuit_oracle_test.go:291`,
  `repoconstructed_test.go:144`, `spellbench_test.go:62-160`,
  `spellbench_digest_test.go:18-61` which pins a games.jsonl digest including
  it, `azcheckpoint_path_test.go:90-116`), `scripts/sb-gauntlet.sh:8,24,130`,
  `scripts/m1b-distill.sh:51-56`, `cmd/traindash` ledger tests, the SpellBench
  rating tags (`spellbench-rate.py:37`), the README (`README.md:362-363`), and
  the python-parity story in the agent spec (sbagent-heuristic 90.0% vs python
  heuristic 82.5%, spec section 3.3).
- Common-sense census (commit `4a5d4b63a`, branch `wt/bot-sanity`, **not on
  main**; file
  `docs/superpowers/reports/2026-10-09-common-sense-rules-evaluation.md` there):
  of 1,467 hostile target decisions, 787 (53.6%) aimed at its own permanent or
  player, and **488 of those had an opposing target available**. So only about
  299 of 787 are "the cast was the mistake"; the other 488 are "the target
  choice was the mistake".

### 3.7 `lethal-pressure`

- `bots/lethalpressure/lethalpressure.go`: `seat.NewLethalPressureBot` (:18),
  `+EnableAutoPayMana()` when the table is auto-pay. Constructed + commander.
- Behaviour vs `bot`: identical code path on manual mana (finding 1 and section 2:
  24/24 identical). On auto-pay it lacks only the combat-simulation attacker.
  The packages spec already says so: "`lethal-pressure` and `cast-profile` are
  labelled as what they now are: aliases of the manual bot, and weaker than the
  hosted auto-pay bot" (`docs/superpowers/specs/2026-09-28-hosted-bot-packages.md:81`,
  Q5 :1416-1418). There is no A/B between `lethal-pressure` and `bot` in the docs
  other than (a) AR7's own +1.65pp / 51.65% [50.10, 53.20] vs the *pre-AR7* bot
  (`bots/lethalpressure/lethalpressure.go:11`; training summary
  `docs/superpowers/reports/2026-09-24-training-approaches-summary.md:43`, which
  records AR7 as "Promoted into the default `bot` (9be52252)"), and (b) the
  attack-sim gate against the plain auto-pay bot (commit `66aa546dd`).
- **Is `lethal-pressure` a strict superset of `bot`? No: the reverse.** `bot` is
  a superset of `lethal-pressure` on auto-pay and equal on manual. Nothing in
  `lethal-pressure` is missing from `bot`.
- Vestigial plumbing: `seat.Bot.lethalPressure` (`seat/bot.go:32,435-437`) is set
  by every constructor except `NewBot`/`NewCastProfileBot*`, and the branch it
  selects is the same function as the fall-through.
- Users (every reference): constant `bots.LethalPressurePolicy`
  (`bots/registry.go:21`, used at :154,160), `host.LethalPressurePolicy`
  (`host/bot_policy.go:14`); **gorged flag default**
  `-bot-policy` (`cmd/gorged/main.go:283`); **demo default**
  `scripts/deploy-demo.sh:71` plus rationale comments :22-74 and `Makefile:154`;
  tests: `bots/registry_test.go:11,19`, `host/bot_policy_test.go:22,41,51`,
  `host/host_test.go:142,146,484,495,516`, `host/caretaker_test.go:26,74`,
  `host/caretaker_life_plan_test.go:26`, `host/bots_link_test.go:5` and
  `host/httpapi/bots_link_test.go:5` (blank imports of `bots/lethalpressure`,
  which break compilation if the package is deleted),
  `host/httpapi/game_test.go:131,139`, `cmd/gorged/{botpolicies,game,main,deploy}_test.go`;
  seat tests `seat/bot_test.go:308`, `seat/integration_test.go:112,170,333`,
  `cmd/botbench/board_test.go:62`; `internal/paymirror/driver.go:23,164`
  (the "lethal" driver policy); docs `README.md:377`,
  `docs/agents/repo-map-engine.md:21`. Persisted names: the two feedback
  fixtures `rules/testdata/feedback/20261006T100041Z-ec09a3af/match.json:2` and
  `…/20261006T100405Z-4be7e4f7/match.json:2` carry `"bot_policy":
  "lethal-pressure"`; I found no reader in `internal/testutil/feedback` or
  `cmd/repro` that consumes it (grep empty), and replay is by recorded intents,
  so they keep replaying. Archived match sidecars store the string
  (`host/restart.go:85-90` only fills an empty one) and are not re-normalized.
  The deploy script wipes the persistence dir on every deploy
  (`scripts/deploy-demo.sh:254`, `rm -rf "$dir"`), so deployed state does not
  pin the name; a gorged restarted outside a deploy would, via `tables.json`.
- Web client: no hard-coded policy names other than `'bot'` in tests; the picker
  is driven by `GET /api/bot-policies` (`cmd/gorged/botpolicies.go:94-111`). The
  rematch path POSTs the table's `bot_policy` back (`web/src/lib/playvsbot.ts:73-83`),
  so an in-flight table with a retired name needs the alias to rematch.

### 3.8 `cast-profile`

- `bots/castprofile/castprofile.go`: `seat.NewCastProfileBot(seed)` (embedded
  `botpolicy/profiles/default.json`), `+EnableAutoPayMana()`.
- Equality: the embedded default parses to exactly `DefaultCastWeights`
  (`botpolicy/profile_test.go:19-30`), and `host/host_test.go:94-205` asserts a
  whole hosted match is event-for-event identical to `bot` on manual mana. On
  auto-pay it is the plain auto-pay bot (no attack-sim), like `lethal-pressure`.
- **Production consumer: none found.** Not in `scripts/`, `Makefile`, `deploy/`,
  `.github/`, `web/`, `orchestrator/`, `tools/`, `validation/` (grep empty). Only
  tests, the gorged flag help text (`cmd/gorged/main.go:283`), the README
  vocabulary line, and the bench.
- Research tooling that must **not** be deleted with the hosted entry:
  `cmd/botbench` keeps `cast-profile` and `cast-profile-auto-pay` bench-native
  (`cmd/botbench/main.go:203-221`, and explicitly skips the hosted name in the
  registry loop at :432-436); `cmd/policytune` builds
  `seat.NewCastProfileBotWithWeights` directly (`cmd/policytune/main.go:99,141`);
  `botpolicy.ParseCastProfile`/`ParseDeckPolicy`/`CastWeights`
  (`botpolicy/deckpolicy.go`, `profile.go`; `deck.File.Policies`, no repo deck
  uses the `"policies"` key: `git grep -l '"policies"' internal/testutil/decks`
  is empty).
- Strength: fitted L4 profile 50.92% [49.38, 52.47], failed +3pp
  (training summary section 2). Deck-local rerun 14.3% -> 17.2% on uw-tempo, aborted.

### 3.9 Search and trained bots (untouched; dependency notes only)

| Policy | Entry | Depends on | Formats |
|---|---|---|---|
| `search` | `bots/search/search.go` | `searchseat.NewSearchBot` wraps `seat.NewBot` (`internal/searchseat/searchbot.go:129`) | constructed, 2 |
| `az-redeal` | `bots/azredeal/azredeal.go` | `seat.NewBot` (+auto-pay) as fallback and walk policy (`internal/azmcts/seat.go:157`, `env.go:236`) | constructed, 2 |
| `sb-search-lite-atk` | `bots/sbsearch/sbsearch.go` | its own `builtins.NewTactical` with `sbtactical.Hosted()` (:131); `botpolicy.Decide` fallback | constructed, 2 |
| `az-c3` (unmerged, `wt/az-c3-hosted`) | not on main | `Caretaker: "bot"`; trained net, opponent modelled as a searching player | constructed, 2 |

README's caveat applies: `bot` (the seat.NewBot policy) is both the opponent and
the search's environment model, "which flatters every search arm"
(`README.md`, Search section). Consolidation must leave `seat.NewBot` and
`botpolicy.Decide` byte-for-byte alone.

## 4. Option analysis: `sb-heuristic`

### Option A (recommended): drop `sb-heuristic` from the hosted registry; `bot` is the basic tier

- `bot` beats `sb-heuristic` on every pool measured (+110 to +210 Elo by the
  numbers above; confounded by payment mode in the bench, but the sign is
  consistent and large).
- `bot` is the only heuristic with Commander evidence and the demo runs 8
  Commander tables of 16 (`scripts/deploy-demo.sh:49-51`).
- The guard (section 7) lands on one policy that is already the product default,
  with +2.6pp measured on `bot` (52.6% [50.9, 54.3], 3,360 games).
- `sb-heuristic` remains available where it is actually needed: as an
  *opponent reference and rating tier* in botbench and the SpellBench registry.
  Those do not need the hosted entry if botbench re-adds it bench-natively
  (the same pattern as `sb-uniform-manual`, `cmd/botbench/main.go:286-299`).
- Cost: removes one row from the player-facing picker; re-homes
  `benchAutoPayHosted[sbheuristic.Policy]` (`cmd/botbench/main.go:385-389`); one
  hosted test list (`host/hosted_policies_test.go:420`).

### Option B: keep `sb-heuristic` as the basic tier and give it the guard

- It would be a stable, deliberately dumb tier with documented SpellBench
  lineage and a python-parity story.
- But then `bot` has no slot in the four-tier table. It cannot be removed (it is
  the default, the caretaker named by every entry, the demo's Commander-capable
  policy, and the base the AI bots model), so the table becomes five rows, or
  `bot` is registered but unoffered. That contradicts the stated goal.
- The guard as designed does not make `sb-heuristic` sane: the census shows 488
  of its 787 own-target decisions had an opposing target available, which a
  cast-level pre-filter ("drop the action if no legal target is the opponent's")
  does not touch. It needs a second rule (drop own-controlled target options when
  a hostile effect has an opposing one), i.e. a different policy from the
  SpellBench heuristic. In the SpellBench grammar that is a different name anyway
  (`sb-heuristic+sanity`), so "give sb-heuristic the guard" and "keep
  `sb-heuristic` pure for the anchor ladder" are only compatible as a decorator.
- It is declared 2-seat constructed-only, so it would not serve the Commander
  half of the demo.

### Option C (fallback): keep `sb-heuristic` registered but unoffered

Registered (so `bots.Lookup` and bench resolve it) but left out of
`-bot-policies`. Zero migration cost; keeps the player-facing set at four plus
AI. Costs one stale hosted entry and still leaves the guard question open for
it. Take this only if the operator wants the bench behaviour of
`bots.New("sb-heuristic")` preserved verbatim.

**Recommendation: A**, with the guard available as a decorator
(`sb-heuristic+sanity`) in the SpellBench/bench grammar for anyone who wants it,
not as a hosted tier.

## 5. Target end state

| Name | Tier | Label (wire) | Package | Formats | Seats | Notes |
|---|---|---|---|---|---|---|
| `sb-uniform` | Random | "Random" | `bots/sbuniform` | constructed (smoke-verified commander) | 2 declared | Name kept: SpellBench anchor, rating scripts. |
| `sb-first` | First choice | "First choice (passes)" | `bots/sbfirst` | constructed | 2 declared | Never plays; long games. Keep name. |
| `bot` | Basic heuristic | "Basic" | `bots/bot` | constructed + commander | any | Default. Guard applied here. |
| `sb-tactical` | Tuned heuristic | "Tuned" | `bots/sbtactical` | constructed | 2 | Stays 1v1 unless the operator funds the pod work (decision 6). |
| `search`, `az-redeal`, `sb-search-lite-atk` | AI | unchanged | unchanged | constructed | 2 | Untouched. |
| `az-c3` | AI (unmerged) | n/a | branch | constructed | 2 | Out of scope; rebase later. |
| `lethal-pressure`, `cast-profile` | retired | n/a | deleted | n/a | n/a | Aliases of `bot` (section 8). |
| `sb-heuristic` | retired from hosted | n/a | deleted (hosted only) | n/a | n/a | Stays bench-native + SpellBench registry. |

Registry goes 10 -> 7 entries. Internal SpellBench names do not change.
Renaming `sb-uniform`/`sb-first`/`sb-tactical` to friendlier ids is rejected:
the names are keys in rating ledgers and scripts (`scripts/spellbench-rate.py:33-47`,
`scripts/sb-gauntlet.sh`), and the label field already reaches the UI
(`protocol.BotPolicyInfo.Label`, `cmd/gorged/botpolicies.go:115-124`).

## 6. Migration order

Each step is one landable change with its own tests. "M" = mechanical (no
behaviour change a test cannot see); "A" = needs an operator call; "X" = needs
measurement. Test commands use the capped wrapper; abbreviate it `CAP` below:
`CAP = systemd-run --user --scope -q -p MemoryMax=4G -p CPUQuota=400% env GOMAXPROCS=4 GOMEMLIMIT=3GiB go test -timeout 2m`.
I did not run these post-change commands (nothing is changed yet); they are the
focused sets I would run, chosen from the tests that reference each name.

### Step 1 (M): alias mechanism, no entry removed

`bots/registry.go`: add a closed `aliases` map (`lethal-pressure`->`bot`,
`cast-profile`->`bot`; no entries removed yet, so the map is inert) and make
`Normalize` resolve it. Make `cmd/gorged/botpolicies.go:botPolicyOffered` and
`validateBotPolicyFlags` resolve aliases through the same function (today they
call `bots.Lookup`, :37 and :77, which would reject an alias). Lookups only; no
map iteration reaches an event.
Tests: `CAP -run 'TestNormalize|TestRegister|TestEntries' ./bots/`,
`CAP -run 'TestBotPolicyFlagsValidateAtStartup|TestBuildBotPolicyList' ./cmd/gorged`.

### Step 2 (A): retire `lethal-pressure` and `cast-profile` as hosted entries

- Delete `bots/lethalpressure`, `bots/castprofile`; edit `bots/all/all.go`; remove
  the two blank imports in `host/bots_link_test.go` and
  `host/httpapi/bots_link_test.go`; drop `LethalPressurePolicy` /
  `CastProfilePolicy` (`bots/registry.go:21-22,154,160`,
  `host/bot_policy.go:14-16`) or keep them as deprecated constants.
- `cmd/gorged/main.go:283`: default -> `bots.Default`; fix the help text.
- Rewrite tests that used `lethal-pressure` as "the policy that is not the
  default" to use a different surviving non-default policy (e.g. `sb-uniform` or
  `sb-tactical`): `cmd/gorged/botpolicies_test.go:177-197,208`,
  `cmd/gorged/game_test.go:92-99`, `cmd/gorged/main_test.go:1230-1261`,
  `host/host_test.go:142-146,484-516`, `host/caretaker_test.go:26,74`,
  `host/caretaker_life_plan_test.go:26`, `host/bot_policy_test.go:22,41,51`,
  `host/httpapi/game_test.go:131,139` (it should now assert that
  `lethal-pressure` normalizes to `bot`); `bots/registry_test.go:11,19,35`
  (count 10 -> 8; add alias assertions). The `host_test.go` cast-profile
  equality check (:186-205) becomes moot.
- `scripts/deploy-demo.sh:71` default -> `bot` and rewrite the comment block
  (:22-74); `Makefile:154` comment; `README.md:377`; `docs/agents/repo-map-engine.md:21`.
  The operator redeploys by hand; editing the script is not a deploy.
- Behaviour change to flag: the demo and any alias user move from the plain
  auto-pay bot to the combat-sim bot (already gated, `66aa546dd`). On manual
  mana nothing changes (24/24 identical).
Tests: `CAP -run 'TestRegister|TestNormalize|TestEntries|TestValidate' ./bots/...`;
`CAP -run 'TestNormalizeBotPolicy|TestNewBotPolicySeat|TestHostedAutoPayBotPlaysCombatSim|TestHostedPoliciesReplayDeterministically|TestEveryHostedPolicyIsDeterministic' ./host`
(the last is the heaviest; run it alone and watch the 1-minute budget);
`CAP -run 'TestCreateGameBotPolicy' ./host/httpapi`;
`CAP -run 'BotPolicy|ServeFlag|CreateGame' ./cmd/gorged`;
`CAP ./internal/archtest`;
`CAP -run 'CastProfile|AutoPay|BotsRegistry|Hosted' ./cmd/botbench` (`cast-profile`
must still resolve bench-natively, `cmd/botbench/main.go:432-436`).
Golden check: `CAP -run '^TestHeads$' ./rules` must pass unchanged (hosted
factories are off its path).

### Step 3 (M): delete the vestigial seat plumbing

Remove `seat.NewLethalPressureBot`, the `Bot.lethalPressure` field and its
branch (`seat/bot.go:32,150-154,435-437`); make `botpolicy.LethalPressureDecide`
a documented alias of `Decide` or delete it and its two tests
(`botpolicy/combat_test.go:144-170`); retarget `seat/bot_test.go:308`,
`seat/integration_test.go:112,170,333`, `cmd/botbench/board_test.go:62`,
`internal/paymirror/driver.go:23,164` (its `"lethal"` option) to `NewBot`.
`NewAttackSimBot`, `NewCombinedLethalBot`, `NewBlocksBot` and `NewExploreBot`
set `lethalPressure: true`, which is a no-op after removal because `Decide`
already passes `true` to `decide` (`botpolicy/policy.go:391-396`).
Tests: `CAP -run 'TestBotAdapters|TestBot' ./seat`;
`CAP -run 'LethalPressure|AttackSim' ./botpolicy`; `CAP -run '^TestHeads$' ./rules`
(must not move); `CAP -run 'TestPlayMatchUsesBoardSeat' ./cmd/botbench`.
Optional; can follow step 2 later.

### Step 4 (A): retire `sb-heuristic` from the hosted registry (Option A)

Delete `bots/sbheuristic` hosted package (keep `internal/spellbench/builtins`
and `internal/spellbench/registry/sb.go:25`); edit `bots/all/all.go`;
`cmd/botbench/main.go`: register `sb-heuristic` bench-natively (AutoPay mode,
the arm `benchAutoPayHosted` selected) and remove the `sbheuristic` import from
the map at :385-389; `host/hosted_policies_test.go:420` list;
`bots/registry_test.go` count 8 -> 7 and names. Do **not** alias it to `bot`:
an explicit request for the "dumb heuristic" silently becoming a stronger bot is
a worse failure than an unknown-policy error, and no persisted state names it
(it is not a gorged default and on-demand tables are dropped at restart).
Tests: `CAP -run 'TestRegister|TestEntries' ./bots`;
`CAP -run 'Hosted|Registry|SpellbenchBuiltins|AzCheckpoint' ./cmd/botbench`
(these include `spellbench_test.go:62`, `azcheckpoint_path_test.go:90`,
`spellbench_digest_test.go`, which must not change: the digest golden is the
proof the bench arm is byte-identical); `CAP -run 'TestHostedEnvSeatsIgnoreTheRealHiddenCardsSBPlainPolicies' ./host`.

### Step 5 (M): relabel the four tiers

Edit `Info.Label`/`Description` on `sb-uniform`, `sb-first`, `bot`,
`sb-tactical` to "Random", "First choice", "Basic", "Tuned", keeping
measurement claims and sources. Wire shape is unchanged
(`protocol.BotPolicyInfo`). Check `web/` renders arbitrary labels
(`git grep -n "label" web/src/lib/playvsbot.ts web/src/routes`), since I only
verified there are no hard-coded policy names.
Tests: `CAP -run 'TestBuildBotPolicyListFiltersAndOrders' ./cmd/gorged`; the `web`
tests are the web owner's.

### Step 6 (X, A): common-sense guard as a decorator

Design in `docs/superpowers/reports/2026-10-09-common-sense-rules-evaluation.md`
on `wt/bot-sanity` (commit `4a5d4b63a`; prototype numbers: `bot` 52.6%
[50.9, 54.3], `sb-tactical` 51.3% not significant, over 3,360 games each,
self-play of a policy against its own guarded copy on the 15 constructed decks;
Commander and 4-seat unmeasured). Additional constraints from this plan:

- Add `Options.CommonSense bool`, set **only by the host**
  (`host/bot_policy.go:53-55`, `host/match.go:329`). `botbench` and
  `bots.New(...)` callers leave it false, so `-a bot` stays the unguarded
  control that every historical comparison and the SpellBench `bot` anchor
  (`cmd/botbench/spellbench_registry.go:23`) assume (README: "Always run -a bot
  -b bot ... as the control"). The guarded arm is benched as `bot+sanity` in the
  existing decorator grammar (`internal/spellbench/registry`, `passguard` is the
  worked example). Without this, merging the guard silently changes the meaning
  of every future `vs bot` number.
- Apply it in the factories of `bot` and `sb-tactical` only. It must not touch
  `seat.NewBot`/`botpolicy.Decide` (heads, search bots, training corpora) or the
  inner tactical seat built by `bots/sbsearch` (which builds its own,
  `bots/sbsearch/sbsearch.go:131`).
- The README's rung-3 gate (+3pp pooled, no pair below -5pp, zero new stalls,
  seed 1,000,000 x 400 games/pair) is not met by the prototype's +2.6pp, and the
  prototype was not run under that protocol. Treat the guard as a
  correctness fix (self-hitting Wasteland/StP/Path is an objective error, 17% of
  hostile target decisions) and get the operator's explicit call rather than
  claiming a promotion (decision 4).
- Prototype cost is ~600 clones per game (~10x bench CPU); fine for a live
  table, not for sweeps. The `CommonSense=false` default keeps benches fast.
Measure: held-out guarded-vs-unguarded `bot` in constructed 2-seat and in
Commander 2- and 4-seat (`botbench -seats 4 -format commander` exists,
`cmd/botbench/main.go:2482-2485`) before enabling it for the demo.

### Step 7 (X, optional): make `sb-tactical` pod-aware

Replace the single `oppP` with a per-opponent view (`tactical_state.go:124-137`,
`tactical_combat.go:29`, archetype classification at `tactical.go:279`), measure
vs `bot` in 4-seat Commander pods, then widen `Formats`/`MaxSeats`. Not needed
for the consolidation; without it the picker already hides the tuned tier on
Commander tables via `Info.Formats`. Also changes `sb-search-*` (shares the
weights/code), so it needs its own gate.

## 7. Mechanical vs measured

| Step | Kind | Needs measurement | Needs operator |
|---|---|---|---|
| 1 alias plumbing | M | no | no |
| 2 retire lethal-pressure/cast-profile | M + behaviour note | no (gate already exists, `66aa546dd`) | yes: demo default and picker |
| 3 seat cleanup | M | `TestHeads` must not move | no |
| 4 retire sb-heuristic (hosted) | M | digest golden must not move | yes: Option A/B/C |
| 5 relabel | M | no | wording only |
| 6 guard | X | yes: constructed + Commander 2/4 held-out | yes: gate waiver or not |
| 7 pod-aware tactical | X | yes | yes |

## 8. Alias and deprecation handling

- `bots.Normalize` resolves a closed alias map; `bots.Lookup`, `Names()` and
  `Entries()` do not list aliases, so the picker (`GET /api/bot-policies`) and
  `-bot-policies` default show canonical names only.
- Aliases: `lethal-pressure` -> `bot`, `cast-profile` -> `bot`. Keep them
  indefinitely; two map entries are cheaper than the support cost of a table
  that will not load. Pin with a test so removal is deliberate.
- Where an alias is read: `tables.json` load (`host/table.go:206`), `POST
  /api/games` (`host/httpapi/rest.go:463`, `cmd/gorged/game.go:45`), the gorged
  `-bot-policy`/`-bot-policies` flags (after step 1's fix), the rematch POST
  (`web/src/lib/playvsbot.ts:73-83`).
  The response's `bot_policy` becomes the canonical `bot`; a client that
  compared it to what it sent will see a change. `web/` does not (verified by
  grep: it only forwards the table's own value).
- Where an alias is **not** read: archived sidecars and feedback captures keep the
  string they recorded (`host/restart.go:85-90`, `host/feedback.go:323`); they
  replay from intents. The two committed fixtures are therefore unaffected.
- `sb-heuristic`: no alias (step 4). `bots.Normalize` returns the existing
  "unknown bot policy ... (known: ...)" error (`bots/registry.go:94-101`).
- Bench names (`cast-profile`, `cast-profile-auto-pay`, `sb-heuristic`,
  `sb-*-manual`, `-planned`) are not hosted names and are untouched.

## 9. What blocks removal, by item

| Item | Blocker | Resolution |
|---|---|---|
| `lethal-pressure` | demo default (`deploy-demo.sh:71`), gorged flag default (`main.go:283`), ~12 test files, 2 blank-import link tests, `README`/repo-map text | steps 1-2 |
| `cast-profile` | tests, flag help, README; bench + `policytune` use their own constructors | step 2; keep research tooling |
| `sb-heuristic` | bench tests/scripts use the name; digest golden; `benchAutoPayHosted`; SpellBench rating tags | step 4, bench-native re-registration |
| `bot` | caretaker named by every entry (`bots/registry.go:140`); default; base of `seat.NewBot`/search bots; SpellBench `bot` anchor | not removable; do not edit `seat.NewBot`/`botpolicy.Decide` |
| `sb-tactical` | inner policy of `sb-search-*`; tuning weights shared | not removable |
| `sb-uniform`/`sb-first` | SpellBench anchor + scripts + names in ledgers | not removable; rename rejected |
| golden heads | none from hosted changes | `TestHeads` is the tripwire in steps 2-3 |
| archtest | `bots/...` import bans, `protocol` must not import `bots` | a new decorator package must respect `internal/archtest/arch_test.go:157,233` |
| `AGENTS.md` | Known-approximations table only shrinks; this plan adds no row | put deviations in commit messages |

## 10. Risks

1. **Control-arm drift.** Putting the guard inside `bots/bot` changes `-a bot`
   everywhere (botbench policy map, SpellBench `bot`). Mitigation: step 6's
   `Options.CommonSense` default-false.
2. **Hosted `bot` is not the benched `bot`.** Bench `bot` is manual mana; hosted
   `bot` on an auto-pay table is the combat-sim arm; the gating arm is
   `attack-sim-auto-pay`. The confound in 3.3 means `sb-tactical`'s headline
   margin over `bot` is not a hosted-vs-hosted number. Do not quote "+250 Elo"
   to players. Cheap fix: one botbench run of `sb-tactical` vs
   `attack-sim-auto-pay` before the labels in step 5 claim an ordering.
3. **Demo behaviour changes at the next redeploy** (weaker lethal-pressure ->
   `bot` combat-sim), and a Commander-capable default is required there; do not
   point the demo at `sb-tactical`.
4. **Alias semantics.** An alias that upgrades behaviour (auto-pay) is benign for
   `lethal-pressure`/`cast-profile`; it would be misleading for `sb-heuristic`
   (hence no alias).
5. **Removing a hosted package that bench imports.** `cmd/botbench/main.go:385-389`
   imports `sbheuristic`, `sbfirst`, `sbuniform` constants; deleting
   `bots/sbheuristic` without the step-4 bench edit breaks the build.
6. **Test-budget.** `TestEveryHostedPolicyIsDeterministic` iterates every entry
   and plays whole games (`host/hosted_policies_test.go:80+`); reducing entries
   makes it faster, but run it alone and confirm it stays under 1 minute.
7. **`sb-first` in a pod never ends** (section 2). If offered for 4-seat
   Commander tables it needs the host's stall guard or a note; today it is
   constructed-only in the picker.
8. **Hot files.** `docs/agents/repo-map-engine.md` (map table) and `bots/registry_test.go`
   (count pin) are touched by steps 2 and 4; sequence them, one branch at a time.
9. **I did not examine** `wt/az-c3-hosted` beyond its registry entry and diff
   stat; `az-c3` was said to use `bot` as fallback/opponent model, and I confirmed
   `Caretaker: "bot"` there but not its fallback construction.
10. **Smoke evidence is thin.** The Commander table is one seed; the `bot` vs
    `lethal-pressure` check is 24 games with a fingerprint comparison. The
    code-level identity (`botpolicy/policy.go:391-403`) is what makes
    finding 1 solid, not the games.

## 11. Decisions for the operator (with recommendation)

1. **Retire `lethal-pressure` and `cast-profile` as aliases of `bot`?**
   Recommend yes. Consequence: the demo's bots get stronger on auto-pay tables
   at the next hand redeploy.
2. **`sb-heuristic`: A (drop from hosted), B (keep as the basic tier), or C
   (registered, unoffered)?** Recommend A. B forces `bot` into an awkward
   fifth slot and the guard does not fix its 488 own-target-with-opponent
   decisions; C is the zero-cost fallback.
3. **Keep the internal ids and only change labels?** Recommend yes (anchor and
   ledger names).
4. **Common-sense guard: ship on `bot` and `sb-tactical` host-side only, without
   the +3pp rung-3 gate?** Recommend yes as a correctness fix, host-only
   (`Options.CommonSense`), pending a Commander/4-seat check; do not call it a
   promotion and do not apply it to `seat.NewBot`, `botpolicy.Decide`, or the AI
   bots. If the operator wants the gate applied literally, it fails on the
   prototype's +2.6pp.
5. **Keep `botpolicy.CastWeights`, `cmd/policytune` and the bench `cast-profile`
   arms** after the hosted entry goes? Recommend yes now; revisit when someone
   proposes retiring the L-series tooling (the L-series is closed per the
   training summary).
6. **Should the tuned tier be made pod-capable (step 7)?** Recommend no for this
   consolidation; the picker already filters by `Formats`. If the demo should
   offer a tuned Commander bot, that is a separate measured ticket.
7. **Delete the vestigial `seat.NewLethalPressureBot`/`lethalPressure` plumbing
   (step 3)?** Recommend yes, after step 2, with `TestHeads` as the guard.
8. **Demo default after step 2:** `bot`. Recommend yes; the operator redeploys.
