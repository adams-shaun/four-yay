# Repo-constructed gauntlet: every supported constructed deck through SpellBench, and what the RL dashboard can see

Ticket `cli-20260930T022010Z-2c2950fe` (research/measurement). Worktree at merge
`d28579b30` (one merge past the brief's `bf90e1f34`); all facts below re-measured
there. `.cards` symlink was present.

## What changed (per file)

| file | change |
|---|---|
| `internal/spellbench/decks/repo-constructed/*.json` (new, 14 files) | Copies of the 14 supported 60-card constructed repo decks (`internal/testutil/decks`, `format: custom`, exactly 60 cards), mono-green-stompy included. Byte-identical to their sources EXCEPT the top-level `name` field, which each copy replaces with its file stem (see the round-2 fix note below the table) |
| `internal/spellbench/decks.go` | embed gains `decks/repo-constructed/*.json`; `RepoConstructed` dir const; `RepoPool` = the **13 fully supported** decks (all but mono-green-stompy, whose single Vines of Vastwood runs under the recorded `stat:CantTarget` approximation — the Terror precedent: in the dir, out of the pool) |
| `internal/spellbench/fdn.go` | one `Catalogs` row `{ID: "repo-constructed", Pool: RepoPool, Format: "repo-constructed-bo1"}` — `CatalogByID` accepts the id through the existing loop, no special case |
| `internal/spellbench/repoconstructed_test.go` (new) | `TestRepoConstructedCatalogDecks`: dir↔pool correspondence, 14 decks, 60 cards each, `custom` format, `CatalogByID` row (corpus-free) |
| `cmd/botbench/repoconstructed_test.go` (new) | `TestSpellbenchRepoConstructedCatalogCopiesMatchSource` (payload drift guard: format/archetype/commanders/main/sideboard vs the source, both directions) and `TestSpellbenchRepoConstructedCatalogMirrors` (real round-robin on the catalog, FDNCatalogMirrors shape) |
| `scripts/sb-gauntlet.sh` | 4th positional arg / `SB_GAUNTLET_CATALOG` (default `pauper-kernel`, today's behaviour unchanged); `-spellbench-catalog` in `run_bench`; `$CATALOG` in the cache key; POOL normalization only on pauper-kernel |

Byte-equality-guard placement deviation, deliberate: the brief suggested
`cmd/botbench` (which imports both packages); the copies guard lives there, but it
reads both sides from disk (`../../internal/...`) because `spellbench.decksFS` and
`testutil.decksFS` are both unexported and no raw-bytes accessor was worth adding.
The in-package `CatalogIDs`/decksFS enumeration stayed in `internal/spellbench`'s
own test (its `_test` file already imports `testutil` — `fdn_test.go` — so no new
import edge either). `internal/archtest` passes unchanged.

### Round-2 fix: the copies' `name` field is the file stem, not the display name

Review finding cli-20260930T022010Z-2c2950fe r2: `CatalogIDs`
(`internal/spellbench/decks.go:49-64`) returns each deck's `name` field as its
id, while `Deck` resolves ids as lowercased FILE STEMS
(`decks.go:68-74`). The display names ("Death & Taxes", "UW Tempo", …) are
not case-variants of their stems, so the ids `CatalogIDs` returned for this
catalog could not round-trip: `internal/spellbench/builtins`'s committed
`TestReanimateValueTerminates` (which iterates ALL of `Catalogs`) failed with
`deck "Death & Taxes": open decks/repo-constructed/death & taxes.json: file does
not exist`.

Fix (finding direction (b), the narrowest that makes the ids round-trip without
touching pauper-kernel's capitalized id contract — its consumers at
`kshadow/setup.go:158`, `v2engine/server.go:117`, `v2shadow/setup.go:218` and the
derived cardfacts all key on display names like "Burn"):

- each copy's top-level `name` is now its file stem; the card payload is
  untouched;
- the drift guard narrowed to the load-bearing payload (format, archetype,
  commander, main deck, sideboard) and now also asserts `name == stem`, so it
  still fails loudly on card drift — and on a re-copy that restores a display
  name;
- `TestSpellbenchRepoConstructedSourceNamesAreDisplayNames` pins the narrowing
  itself: if a source deck's name ever becomes its stem, the test says the
  copies can go back to byte-identical;
- `internal/spellbench/repoconstructed_test.go` pins the round-trip in-package:
  `CatalogIDs(RepoConstructed)` returns exactly the 14 stems and each resolves
  through `File`.

Probes (output under `## Fails without the fix` in the round report): with a
display-name copy restored, `TestReanimateValueTerminates` and
`TestRepoConstructedCatalogDecks` fail exactly as the finding measured; with a
copy's first main entry set 4→5, the drift guard fails naming
`(main entry 0: "Volcanic Island" copy 5, source 4)`.

## Ask 1 — the catalog (done)

`cmd/botbench -spellbench-catalog repo-constructed` plays the catalog. Proven by a
real game (mirrors test, real output):

```
ok github.com/adams-shaun/gorge/internal/spellbench 0.028s
ok github.com/adams-shaun/gorge/cmd/botbench 1.926s   (mirrors: m0000p0000g0 death-n-taxes + dimir-tempo, format repo-constructed-bo1, no halt)
```

The ledger's own `run.json` records `"catalog": "repo-constructed"` and
`"card_pool_identity": "spellbench-repo-constructed-catalog/forge@95f04e8a…"`,
so `spellbench-rate.py`'s provenance chain names the new catalog untouched.

## Ask 2 — training distribution, per deck (measured)

Sources, measured at this worktree:

- **Training/eval loop** (`cmd/exitloop/main.go:50-56`, `:184`): `defaultPairs` =
  uw-tempo vs {mono-white-equipment, mono-blue-tempo, mono-black-aggro,
  mono-red-prowess, mono-green-stompy}; `-pairs` defaults to exactly that for both
  `-mode exit` and `-mode ppo` (ppo collects with the same `cfg.pairs`).
- **SpellBench policy nets** (`docs/superpowers/reports/2026-09-28-spellbench-policy-networks.md:113-116`):
  g115 "trained on 6 of the 8 benchmark decks plus Terror, and never saw Spy or
  CawGates" — i.e. **zero repo decks**; likewise c12/a48 (pauper pool only).
- **Search (PIMC/L10)**: deck-agnostic at decision time; measured numbers exist
  for the pauper pool and the L10 held-out gate (`README.md:252-253`, 53.7%
  [52.2, 55.2]) — not per-repo-deck.
- **bot-vs-bot strength matrix** (`docs/superpowers/reports/2026-09-06-ds4-task-m1-botbench-matrix.md`):
  repo decks appear there, but that is bot-vs-bot deck STRENGTH, not policy
  training — labelled as such in the table.
- **botbench default** (`cmd/botbench/main.go:44-48`): death-n-taxes vs
  dimir-tempo only (the "always and only the first two sorted names" note).

| deck | in exitloop defaultPairs (teacher+eval+ppo collect) | in any SpellBench catalog/net | other measured coverage | verdict |
|---|---|---|---|---|
| uw-tempo | **yes** (anchor seat of all 5 pairs) | no | acceptance goldens; bot-vs-bot matrix | **trained** |
| mono-white-equipment | **yes** | no | acceptance goldens; matrix | **trained** |
| mono-blue-tempo | **yes** | no | acceptance goldens; matrix | **trained** |
| mono-black-aggro | **yes** | no | acceptance goldens; matrix | **trained** |
| mono-red-prowess | **yes** | no | acceptance goldens; matrix | **trained** |
| mono-green-stompy | **yes** (in pairs; 1 unsupported card, recorded approximation) | no (dir, not pool) | acceptance goldens; matrix | **trained** |
| death-n-taxes | no | no | botbench default seat; acceptance goldens; matrix | **outside distribution** |
| dimir-tempo | no | no | botbench default seat; goldens; matrix | **outside distribution** |
| eldrazi-stompy | no | no | goldens; matrix | **outside distribution** |
| mono-red-goblins | no | no | goldens; matrix | **outside distribution** |
| the-epic-storm | no | no | goldens; matrix | **outside distribution** |
| tron | no | no | goldens; matrix | **outside distribution** |
| ur-delver | no | no | goldens; matrix | **outside distribution** |
| uw-control | no | no | goldens; matrix | **outside distribution** |

**6 of 14 constructed decks are inside the RL distribution; 8 are exercised only
by deterministic goldens and bot-vs-bot strength runs.** No repo deck has ever
been in a SpellBench catalog, so the SpellBench-side nets never saw a repo-deck
card list. (Incinerate / Ojer Axonil, the other two `knownUnsupported` cards, are
commander-only — out of scope.)

## Ask 3 — the smoke measurement (the one sweep)

```
go build -o .ds4/scratch/botbench ./cmd/botbench
timeout 300 env GOMEMLIMIT=2GiB .ds4/scratch/botbench \
  -spellbench sb-uniform,sb-heuristic,bot,sb-tactical \
  -spellbench-catalog repo-constructed -spellbench-pairs 2 \
  -spellbench-out .ds4/scratch/sb-repo -workers 2
```

**Wall time 1m10.8s.** Count correction to the brief: the schedule is **312
games, not 156** — `sbSchedule` (cmd/botbench/spellbench.go:155) plays 2
seat-swapped games per pair slot: 6 matchups × 2 pairs × 13 decks × 2 = 312.
Ledger: 304 natural wins, **8 truncated** (max-turn/intents caps, excluded from
all rates below), **0 halted**.

Rates are per-deck for the focus policy across its games on that deck (opponents
pooled; the `deckledger.go` reader does not split by opponent). Extracted by
driving `cmd/traindash` — the existing reader, no parallel rater:

```
.ds4/scratch/traindash -root .ds4/scratch/dashroot -deck-root .ds4/scratch/sb-repo \
  -deck-focus sb-uniform,sb-heuristic,bot,sb-tactical -addr 127.0.0.1:8090
curl -s http://127.0.0.1:8090/api/runs   # snap.deck_ledgers, 4 ledgers × 13 rows
```

| deck | sb-uniform | sb-heuristic | bot | sb-tactical |
|---|---|---|---|---|
| death-n-taxes | 1-11 8.3% [1.5,35.4] | 4-8 33.3% [13.8,60.9] | 9-3 75.0% [46.8,91.1] | 10-2 83.3% [55.2,95.3] |
| dimir-tempo | 6-6 50.0% [25.4,74.6] | 1-11 8.3% [1.5,35.4] | 7-5 58.3% [32.0,80.7] | 10-2 83.3% [55.2,95.3] |
| eldrazi-stompy | 2-10 16.7% [4.7,44.8] | 7-5 58.3% [32.0,80.7] | 6-6 50.0% [25.4,74.6] | 9-3 75.0% [46.8,91.1] |
| mono-black-aggro | 3-9 25.0% [8.9,53.2] | 6-6 50.0% [25.4,74.6] | 6-6 50.0% [25.4,74.6] | 9-3 75.0% [46.8,91.1] |
| mono-blue-tempo | 1-11 8.3% [1.5,35.4] | 5-7 41.7% [19.3,68.0] | 6-6 50.0% [25.4,74.6] | 12-0 100.0% [75.8,100.0] |
| mono-red-goblins | 3-9 25.0% [8.9,53.2] | 4-8 33.3% [13.8,60.9] | 6-6 50.0% [25.4,74.6] | 11-1 91.7% [64.6,98.5] |
| mono-red-prowess | 3-9 25.0% [8.9,53.2] | 3-9 25.0% [8.9,53.2] | 7-5 58.3% [32.0,80.7] | 11-1 91.7% [64.6,98.5] |
| mono-white-equipment | 0-8 0.0% [0.0,32.4] | 0-4 0.0% [0.0,49.0] | 6-3 66.7% [35.4,87.9] | 10-1 90.9% [62.3,98.4] |
| the-epic-storm | 7-5 58.3% [32.0,80.7] | 3-9 25.0% [8.9,53.2] | 3-9 25.0% [8.9,53.2] | 11-1 91.7% [64.6,98.5] |
| tron | 2-10 16.7% [4.7,44.8] | 3-9 25.0% [8.9,53.2] | 9-3 75.0% [46.8,91.1] | 10-2 83.3% [55.2,95.3] |
| ur-delver | 4-8 33.3% [13.8,60.9] | 3-9 25.0% [8.9,53.2] | 5-7 41.7% [19.3,68.0] | 12-0 100.0% [75.8,100.0] |
| uw-control | 4-8 33.3% [13.8,60.9] | 6-6 50.0% [25.4,74.6] | **2-10 16.7% [4.7,44.8]** | 12-0 100.0% [75.8,100.0] |
| uw-tempo | 3-9 25.0% [8.9,53.2] | 3-9 25.0% [8.9,53.2] | 6-6 50.0% [25.4,74.6] | 12-0 100.0% [75.8,100.0] |

Anchored Bradley-Terry over the same ledger (sbvenv reachable on the mount):

```
sb-tactical  1438.4 [1361.9, 1531.5]  139-16
bot          1156.7 [1086.4, 1231.3]   78-75
sb-heuristic 1046.0 [ 976.0, 1118.6]   48-100
sb-uniform   1000.0 (anchor)           39-113
```

Reading notes: matchups are cross-policy (i<j pairs), so the Elo is a real policy
ordering; the per-deck rate for a policy pools its games vs all three opponents.
The strongest signal: **the trained `bot` policy is significantly below 50% on
uw-control (16.7%, CI [4.7,44.8] — excludes 50%) and above it on tron/death-n-taxes**,
while sb-tactical is ≥83% with the CI above 50% on 11 of 13 decks. That is exactly
the per-deck shape an undertrained deck exposes. No search arm was run (664
ms/decision; the pauper-side search numbers are already recorded in the ref
ledgers and README).

## Ask 3 — dashboard live test

```
.ds4/scratch/traindash -root .ds4/scratch/dashroot -deck-root .ds4/scratch/sb-repo \
  -deck-focus sb-uniform,sb-heuristic,bot,sb-tactical -addr 127.0.0.1:8090
curl -s http://127.0.0.1:8090/api/runs | jq '.deck_ledgers | map({focus, games, rows: (.rows|length)})'
→ [{"focus":"sb-uniform","games":156,"rows":13}, … ×4]   (156 scored games per focus; 8 truncations shared out)
```

The Decks panel (`index.html:304-321`) renders the repo-constructed ledger
**unmodified** — `deckLedgerRow` keys by `decks[].catalog_id`
(`cmd/traindash/deckledger.go`), so a repo-constructed `matches.jsonl` needs no
schema change. (First attempt bound 127.0.0.1:8094, already served by another
session — moved to 8090, per the port allocation table.)

**Is an undertrained deck visible today?** Yes, in the per-deck table — with
evidence pasted above: `bot` on uw-control 2-10, 16.7%, Wilson 95% CI
[4.7, 44.8] excludes 0.50; sb-uniform on mono-white-equipment 0-8 [0.0, 32.4].
Two conditions make it visible: (a) the ledger must contain a policy whose
games-per-deck reach ~12 (at 2-4 games/deck no CI excludes 50%), and (b) a
candidate-vs-reference ledger (the gauntlet's real shape), not an all-mirror
sweep.

**Does a per-deck win-rate-by-iteration view exist? No.** Measured:

- The by-round chart plots the **pooled** `eval.json` `win_rate`/`ci` series
  (`index.html:196`, `:287`; source `cmd/traindash/scan.go` `EvalResult`
  `WinRate`/`CI`).
- `EvalResult.Pairs` (`scan.go:104-111`, per deck-PAIR win rate per round)
  exists per round but is rendered only as a per-round **table** (rows = rounds,
  cells = pair win rates, no CI — `index.html:378-390`). So for the 6
  in-distribution decks, per-iteration numbers exist in table form, for their
  eval PAIR only.
- The Decks panel is a per-ledger **snapshot** (`deck_ledgers` has no round
  axis; `deckledger.go` reads one `matches.jsonl`), and candidate ledgers
  accumulate under `$GDIR/cand/<git_head>/<spec>/` — different heads land in
  different directories with no recorded ordering.

**The missing series, named** (same discipline as the previous ticket's §3
table): *per-deck win rate by candidate run*. Source file/field: each
`cand/<git_head>/<spec>/matches.jsonl` row's `decks[].catalog_id` +
`seats[].name` + `winner`/`outcome` (writer `cmd/botbench/spellbench.go:436-470`);
reader to extend: `cmd/traindash/deckledger.go` `parseDeckLedger` (per-file) —
the gap is an ordering axis across the `cand/<git_head>/…` directories, which
today nothing records. Cheapest honest fix: a new `deckhistory.go` next to
`deckledger.go` that walks the cand tree, reduces per-dir rates with the existing
`parseDeckLedger`, and orders directories by `ts` — which requires propagating
the gauntlet's `results.jsonl` `ts`/`git_head` (it already carries both) into the
cand dir (e.g. one small `meta.json` written by `sb-gauntlet.sh` alongside each
candidate ledger). Cost: ~120-180 LOC + one test, no schema change to
`matches.jsonl`, no new games. **Not built this ticket** — it needs a writer-side
decision (meta.json vs reading `results.jsonl`) that deserves its own ticket; the
plan is the deliverable. For the 6 in-distribution decks the existing per-round
per-pair TABLE already shows trajectory, so the ticket's proposal is: wire
`exitloop -pairs`/teacher onto the repo-constructed catalog (follow-up ticket),
at which point `EvalResult.Pairs` covers the new decks for free.

## sb-gauntlet.sh passthrough (diff summary)

- `CATALOG=${SB_GAUNTLET_CATALOG:-${4:-pauper-kernel}}`.
- `run_bench` passes `-spellbench-catalog "$CATALOG"` (today's explicit default =
  botbench's default → behaviour byte-identical).
- Cache key gains `$CATALOG` (`printf '%s|%s|%s|%s' … "$CATALOG"`) — a repo-deck
  run can never replay a pauper-kernel cache. Cost: **existing ref caches
  invalidate once** (one ref-vs-ref replay on the next wrapper run).
- `normalize_decks` applies the POOL normalization only when
  `CATALOG = pauper-kernel`; on other catalogs tokens pass through unspaced and
  non-empty (botbench validates against the catalog dir, case-insensitive file
  stem). Verified by a function-level harness: repo input
  `Death-N-Taxes, UW-Tempo ,mono-green-stompy` →
  `Death-N-Taxes,UW-Tempo,mono-green-stompy` (unchanged, botbench lowercases);
  pauper input ` affinity, elves, terror` → `Affinity,Elves,terror` (unchanged).
- `results.jsonl` schema and all field values untouched (the reward loop's
  `collect_win` reads only spec/elo/ci/git_head — `scripts/reward_collect.py:518-532`).
  Known wrinkle, not fixed: a repo-constructed wrapper run with the default pool
  records `"decks": "default-pool"` in the results row, which reads pauper-ish;
  follow-up if it ever matters.
- No shellcheck binary exists in this seat and the repo has no
  `.pre-commit-config.yaml`; verified with `bash -n` (clean) and the diff above.

## Resource cost

- Games: **312** in the single sweep (1m10.8s wall at `-workers 2`, GOMEMLIMIT
  2GiB). No search arm. No second sweep.
- Anchored Elo: seconds (sbvenv, cached).
- Files touched: 14 new deck JSONs (copies, one `name` line each), 2 one-line-ish edits in
  `internal/spellbench`, 2 new test files, one 5-hunk diff in
  `scripts/sb-gauntlet.sh`, this report. No engine file touched; no hot file
  touched; no Known-approximations row touched (count unchanged).
- Scratch: `.ds4/scratch/{sb-repo,sb-rating,traindash,dashroot}` — never the
  shared gauntlet root; `results.jsonl` untouched.

## Reproduce

```sh
go build -o .ds4/scratch/botbench ./cmd/botbench
timeout 300 env GOMEMLIMIT=2GiB .ds4/scratch/botbench -spellbench \
  sb-uniform,sb-heuristic,bot,sb-tactical -spellbench-catalog repo-constructed \
  -spellbench-pairs 2 -spellbench-out .ds4/scratch/sb-repo -workers 2
go build -o .ds4/scratch/traindash ./cmd/traindash
.ds4/scratch/traindash -root <empty-dir> -deck-root .ds4/scratch/sb-repo \
  -deck-focus sb-uniform,sb-heuristic,bot,sb-tactical -addr 127.0.0.1:8090 &
curl -s localhost:8090/api/runs | jq '.deck_ledgers'
/mnt/sata/gorge-training/sbvenv/bin/python3 scripts/spellbench-rate.py \
  --anchor sb-uniform --out .ds4/scratch/sb-rating .ds4/scratch/sb-repo
```

Gates run (real output in the round report): targeted
`TestFDNCatalogDecks|TestRepoConstructed|TestSpellbenchRepoConstructedCatalogMirrors|TestSpellbenchRepoConstructedCatalogCopies|TestParseDeckLedger`
→ ok ×3; `go test ./internal/archtest/` → ok 12.6s;
`TestConstructedDefaultIsByteIdentical` → ok 1.594s (**split unmoved** — expected:
no policy or engine code changed); `go run ./cmd/gentypes -check` → silent pass;
`gofmt -l` → empty.

## Follow-up tickets this report proposes (not filed as code)

1. Wire the RL loop onto the repo-constructed catalog: extend `exitloop`'s
   `-pairs` vocabulary (or add `-catalog`) so teacher/eval/ppo collect on the 13
   pooled repo decks; the 8 outside-distribution decks are the point.
2. The per-deck win-rate-by-candidate-run series (`deckhistory.go` + a
   `meta.json`/results.jsonl-backed ordering axis), costed above.
3. Optionally mirror `spellbench-fdn-catalog.py`'s precedent with a generator
   instead of a copy+test if the deck list starts moving; the equality test is
   the cheaper guard today.

## Issues (defects found, not fixed)

- sb-tactical's planner reports cast-script aborts on repo decks
  (`pursuit failures by play/verdict: cast/ unsupported:census_incomplete=5`,
  "after the script: the play is neither offered nor priced") — all recovered
  in-game (0 halted games in 312), but the census-incomplete pursuit shape is the
  same class the pauper ref ledgers show; no engine defect demonstrated.
- sb-uniform recorded `LEGAL ACTIONS LOST 4 (recovered 0)` across the sweep —
  counter-only observation, no halt; parked here because no test failed.
- 8 of 312 games truncated (mono-white-equipment and the-epic-storm dominate);
  expected for mirror play at 60 turns/20k intents, excluded from all rates.
