# Adopt-a-deck: a standing RL target, and a real win-rate dashboard for it

Task `cli-20260929T235706Z-c9591724` (`kind: spec`). Plan plus the minimum
artifact that makes the plan verifiable: a per-deck ledger reader + a
`cmd/traindash` panel that plots it. No engine change, no new harness, no new
listener.

All file:line citations are against `main` `af75dd55d` plus this branch's
`cmd/traindash/` additions.

---

## 0. Summary, in one paragraph

The dashboard the ask describes ("SpellBench win rate %, by iteration, read
from existing ledgers") **already exists in shape** at `cmd/traindash`, and a
per-deck win-rate series can be charted **today, at zero game cost**, from the
cached reference ledgers `/mnt/sata/gorge-training/spellbench-work/gauntlet/ref/*/matches.jsonl`.
What is genuinely missing is (a) a *name* and a *standing target* for "the deck
we are improving against", and (b) a retained, deck-scoped series once a
candidate run is no longer a cached reference. This report recommends adopting
**`uw-tempo`** (a `internal/testutil/decks/uw-tempo.json` Legacy deck in the
validated 12) as the first standing target, extending **`cmd/traindash`** (not
a second dashboard) with a "Decks" panel, and keeping the gauntlet's
per-candidate `matches.jsonl` alive under the gauntlet root instead of the
`mktemp` scratch dir it is deleted from today. The measurement the ask cares
about — "is it getting better" — is answered by a seat-swapped Wilson CI, and a
real one is extracted below at **zero new games** (pooled 79.7% [68.3, 87.7]
for `sb-search-lite-atk` vs `bot`).

---

## 0b. Round-2 review findings, answered

A prior review of this branch raised one MAJOR and four MINORs. All are
addressed in this round:

- **MAJOR — unbounded deck-ledger discovery over the default `-root`.**
  `findDeckLedgers` walked every `-root`, and the default
  `/mnt/sata/gorge-training` holds 362 `matches.jsonl` / 145 MB, so a cold
  `/api/runs` took 15.5–16.5 s and rendered 199 charts from unrelated scratch
dirs. **Fixed**: deck ledgers now have their own `-deck-root` flag
  (`cmd/traindash/main.go`), defaulting to the narrow gauntlet tree
  `/mnt/sata/gorge-training/spellbench-work/gauntlet`; the training roots are
  walked only for runs. Measured: default cold scan 10.45 s, 3 ledgers
  (baseline was 10.1 s); warm 0.22 s. `TestScanDeckRootsAreSeparate` pins it.
- **MINOR — report §5 mislabeled the per-deck table's provenance.** §5 now
  states the 51-13 numbers come from an ad-hoc `opponent == bot` filter, not
  the landed reader, which pools all opponents (and reports 213-43 (83.2 %)
  for `sb-tactical` on the same file).
- **MINOR — `index.html` whitespace collapse.** `renderCompare()` is restored
  to its original two-line form.
- **MINOR — seat attribution by array index.** `parseDeckLedger` now reads
  `seats[].seat` and selects the focus seat's deck from the matching `decks[]`
  position, so a reordered `seats` array or a future non-mirror ledger is
  attributed correctly. `TestParseDeckLedgerSeatsNonMirror` and the reordered
  row in `TestParseDeckLedgerPerDeck` cover it.
- **MINOR — `TestParseDeckLedgerPerDeck` did not exercise the p0-win-in-p1
  branch.** The fixture now carries that row (and a reordered-seats row), so
  collapsing either win arm fails this test, not only
  `TestScanIncludesDeckLedgers`.

---

## 1. What exists today (file:line), and what is missing

### 1a. A live, read-only dashboard that already plots win rate by iteration

`cmd/traindash` is a read-only web dashboard over `cmd/exitloop` training runs:

- doc-comment contract: `cmd/traindash/main.go:1-42`;
- one embedded page (`//go:embed index.html`): `cmd/traindash/main.go:35`;
- `GET /api/runs` (the polled endpoint, 10s): `cmd/traindash/main.go:76`;
- scanner `Scan`/`parseRun`/`parseAdhoc`/`findRuns`/`cached`:
  `cmd/traindash/scan.go:236`, `:397`, `:532`, `:360`, `:190`;
- per-round reads (`plan.txt`, `roundN/eval.json`, `roundN/stats.json`,
  `control.json`): `cmd/traindash/scan.go:406-460`;
- `EvalResult`/`Stats`/`Round` shapes: `cmd/traindash/scan.go:84-140`;
- chart engine `lineChart`, win-rate panel, per-run detail, per-pair table:
  `cmd/traindash/index.html:140`, `:269`, `:331`, `:349`;
- tests + fixtures to imitate: `cmd/traindash/scan_test.go`
  (`TestDiscovery:43`, `TestAlerts:151`, `TestHTTP:264`, `TestReadOnly:303`).

So item (2) of the ask — "plots of win rate % by training iteration with a CI"
— is **already built**, for `exitloop` runs against the generic pool. The plan's
job is to say what is missing for a *deck-scoped* standing target, not to
propose a second dashboard. `cmd/traindash` is not on the hot-file list
(`system-t0.md`), and `Traindash` binds `127.0.0.1:8086` by default
(`main.go:53`) — outside the demo's 8080/8081.

### 1b. The append-only time series that already exists: the SpellBench gauntlet

`scripts/sb-gauntlet.sh` (`scripts/sb-gauntlet.sh:1-55` usage) rates candidate
policy specs with SpellBench's anchored Bradley–Terry fit and appends one row
per candidate run to
`/mnt/sata/gorge-training/spellbench-work/gauntlet/results.jsonl` (append block
`scripts/sb-gauntlet.sh:216-280`). Real rows read today:

```
{"spec":"sb-tactical","elo":1477.902,"ci_lo":1429.215,"ci_hi":1534.505,
 "wins":307,"losses":77,"pairs":4,"decks":"default-pool",
 "git_head":"ee7acf16...","key":"c6d42369f8f636e0","ts":"2026-09-28T09:02:40Z"}
```

(4 rows total: `bot+curve`, `sb-heuristic+curve`, `sb-tactical+curve`,
`sb-tactical-arch`.) This **is** an improvement-over-time series (keyed by `ts`
and `git_head`), but it is:

- **pooled over the default 8-deck SpellBench pool** — the `decks` field says
  `default-pool` (the pool is `Wildfire Rally Affinity Elves Spy Burn CawGates
  Faeries`, `scripts/sb-gauntlet.sh:56`);
- **Elo, not win rate**; and
- the **candidate's own anchored Elo** vs the whole reference set, not its
  win rate against a fixed opponent on one deck.

The reward loop already consumes exactly this file: `scripts/reward_collect.py`
`collect_win` (`reward_collect.py:292-310`) reads the max-`elo` row and emits
`champion_elo` / `champion_ci_lo`. **Schema is load-bearing; do not change it.**

### 1c. The raw per-deck material exists but is thrown away

`cmd/botbench -spellbench` writes `matches.jsonl` (schema
`spellbench-match-ledger/v1`, writer `cmd/botbench/spellbench.go:400-446`) and
`games.jsonl` (`spellbench.go:744-753`) into its `-spellbench-out` dir.

A real row (read today from `ref/4bf6638277783ca4/matches.jsonl`):

```json
{"schema":"spellbench-match-ledger/v1","game_id":"m0000p0000g0",
 "decks":[{"catalog_id":"Wildfire"},{"catalog_id":"Wildfire"}],
 "seats":[{"seat":"p0","name":"sb-uniform"},{"seat":"p1","name":"sb-heuristic"}],
 "winner":"p1","outcome":"p1_win","classification":"natural",
 "winner_bot_id":"...","reason":"game_over"}
```

Important schema detail verified against the writer and 1 408 real rows: the
natural-game marker is **`classification == "natural"`** (`spellbench.go:421-432`
sets `classification` to `natural`/`halted`/`truncated`) and the winner is
`outcome == "p<N>_win"` + `winner == "p<N>"` (`winner` is `null` for draws and
halts). `games.jsonl` has `"game_id","deck","seed","p0","p1","result"`.

**The gap.** `sb-gauntlet.sh` runs each candidate into a `mktemp -d` scratch dir
(`scripts/sb-gauntlet.sh:75`) removed by `trap ... EXIT` (`:76`). Only the
**pooled rating row** survives in `results.jsonl`; the per-candidate
`matches.jsonl` / `games.jsonl` are **deleted**. Cached *reference* games
survive only under `gauntlet/ref/<key>/` (`:161-178`). So there is **no
retained per-deck win-rate series for a candidate run today** — the concrete
thing this plan adds a home for.

### 1d. There is no "adopt a deck" concept anywhere

`/usr/bin/grep -rniE "adopt"` over the repo finds only
`internal/spellbench/v2agent/agent.go:229` (adopt a game id) and README's
*Adoption ladder* (`README.md:356`), which is about **policies**, not decks. No
standing-target config, no per-deck ledger, no deck-target vocabulary. This is
genuinely new; §4 names it.

### 1e. The repo decks are the candidate pool

`internal/testutil/decks/*.json` — 29 deck files (14 constructed 60-card, 15
100-card Commander). The 12 the acceptance suite pins its golden games to are
`internal/testutil/decks.go:66-79` (`legacyDeckNames`): `death-n-taxes`,
`dimir-tempo`, `eldrazi-stompy`, `mono-black-aggro`, `mono-blue-tempo`,
`mono-green-stompy`, `mono-red-goblins`, `the-epic-storm`, `tron`, `ur-delver`,
`uw-control`, `uw-tempo`. An adopted deck should be one of these: its primitive
coverage is maintained by the existing ratchet
(`rules/acceptance_test.go:186` `TestEveryRepoDeckIsFullySupported`), so
adopting it adds **zero new coverage obligation**.

---

## 2. The deck to adopt first: `uw-tempo`

**Adopt `internal/testutil/decks/uw-tempo.json`.** Reasons, each measured or
cited — not asserted:

1. **It sits in the validated set.** `uw-tempo` is one of the 12
   `legacyDeckNames` (`internal/testutil/decks.go:78`), so it is in the
   acceptance golden games and covered by the coverage ratchet. The ratchet's
   current table (`rules/acceptance_test.go:96-155`) names only three
   unsupported cards across all 29 decks — `Incinerate` (`stat:CantRegenerate`),
   `Vines of Vastwood` (`stat:CantTarget`), `Ojer Axonil, Deepest Might`
   (`count:NonCombatDamageThisTurn`) — and **none of them is a `uw-tempo`
   card**, so `uw-tempo` is fully supported by the ratchet's own measurement.

2. **The policy gap on it is already measured, with a CI, in three
   independent ways** (so "is the policy improving?" is a question with room
   for a signal, not a floor/noise read):
   - **Oracle teacher:** `docs/superpowers/reports/2026-09-24-training-approaches-summary.md:166`
     — `uw-tempo` seat win rate vs `bot`: `bot` baseline **14.3%**, sampled-PIMC
     seat **16.7%**, oracle attackers **25.6%**, oracle attackers+cast **33.6%**.
     A +11–19pp oracle ceiling on this exact deck.
   - **Deck-local SPSA fit:** same report, `:69-71` — the `uw-tempo` seat's win
     rate vs `bot` moved 14.3% → 17.2% and then plateaued, at 77 games/s on 2
     vCPU. A real mechanism moved a real number on this deck.
   - **Legacy matchup matrix:** `docs/superpowers/reports/2026-09-06-ds4-task-m1-botbench-matrix.md:120,130,...`
     — per-pair win rates with CIs for every `X:uw-tempo` matchup (e.g.
     `mono-blue-tempo:uw-tempo` 72.2% [70.9, 73.6], `mono-red-goblins:uw-tempo`
     48.1% [46.6, 49.7]).

3. **The search teacher's edge is measurable at a small games budget** on the
   SpellBench analog (not the repo deck), which is the closest existing harness:
   §5 below extracts `sb-search-lite-atk` vs `bot` at 64 games/pair and gets a
   CI 10pp wide — enough to see a +16pp effect, not a +1pp one. The
   L10 held-out gate itself is 53.7% [52.2, 55.2] (`README.md:254-256`), a CI
   that excludes 50%.

Why not a Commander deck: the 15 `foundations-*`/Commander decks are **not**
in `legacyDeckNames`, so seating golden games from them moves the chain heads
for a non-behavioural reason (`internal/testutil/decks.go:55-64` explains
exactly this); the ratchet would still cover them, but the seating contract
makes them a worse first target. Why not a mono deck: the matrix
(`...botbench-matrix.md:198-205`) names the two pairs the bot **loses**,
`mono-red-goblins:uw-tempo` and `the-epic-storm:uw-control`, and both losers are
"tempo/control" matchups — the type the research's #1 unfixed sensitivity (spell
selection, worth 16.8pp) is expected to struggle with. `uw-tempo` is the hero
of that finding.

**Evidence status.** The three cited measurements were produced on main before
this ticket and are not re-run here (they are the *reason* for the choice, and
re-deriving them is exactly what the brief forbids). The only fresh measurement
in this report is the zero-cost cached-ledger extraction in §5.

---

## 3. Every chart, its exact source file + JSON field + reader

The dashboard proposal is a **new "Decks" panel inside `cmd/traindash`**. Every
number it shows names a source:

| Chart / number | Exact source file | Exact JSON field(s) | Reader that produces it |
|---|---|---|---|
| **Per-deck win rate, focus policy, with 95% CI** (new, zero-cost) | `<gauntlet-root>/ref/<key>/matches.jsonl` | `decks[].catalog_id`, `seats[].name`, `winner`, `outcome` | `parseDeckLedger` in `cmd/traindash/deckledger.go` (new; Wilson 95 in `wilson95`, same file) |
| **Per-ledger game count** | same file | row count | `parseDeckLedger` (`DeckLedger.Games`) |
| **Pooled anchored Elo + CI by run/time** (existing series) | `/mnt/sata/gorge-training/spellbench-work/gauntlet/results.jsonl` | `spec`, `elo`, `ci_lo`, `ci_hi`, `ts`, `git_head`, `decks` | `scripts/spellbench-rate.py` writes `results.jsonl` (append `sb-gauntlet.sh:216-280`); **not yet read by traindash** — a small second reader would add this to the same panel |
| **Eval win rate by round + 95% CI** (already plotted) | `<run>/roundN/eval.json` (or `genN/`) | `a_win_rate`, `a_win_rate_ci` | `cmd/traindash/scan.go:84-90` (`EvalResult`), charted at `index.html:269` |
| **Per-pair eval win rate** (already plotted) | `<run>/roundN/eval.json` | per-pair fields of `EvalResult` | `cmd/traindash/scan.go`, table at `index.html:349` |

**No placeholder charts.** The per-deck chart (row 1) has a source today —
namely the cached `gauntlet/ref/<key>/matches.jsonl` files — and the reader is
the one landing with this ticket. Row 3 (the pooled Elo-by-time chart) has a
source today (`results.jsonl`) but **no reader yet**; if the follow-up wants it,
the same `deckledger.go` pattern (a file of JSONL rows → a series keyed by
`ts`) is the small writer/reader to add — it needs **no new games**.

For a **repo-deck** series (the adopted deck's `uw-tempo` number specifically),
the source is a `matches.jsonl` from a botbench run that seats `uw-tempo`; see
§4 for where it now needs to be *kept* rather than deleted.

---

## 4. What the dashboard *is*, and how it stays current

### Recommendation: extend `cmd/traindash` (do **not** stand up a second listener)

Chosen shape: a new **"Decks" panel in the existing `cmd/traindash` page**
(`cmd/traindash/index.html`, new `renderDecks` function), fed by a new
top-level snapshot field `deck_ledgers` produced by the existing scanner.
Reasons:

- `traindash` is already a read-only ledger reader with a chart engine
  (`lineChart`, `index.html:140`), CI bands, an embedded page, an mtime/size
  scan cache (`scan.go:190`) and `scan_test.go` fixtures. A second page would
  duplicate all of that.
- The plan must **not** stand up a second listener, and `traindash` defaults to
  `127.0.0.1:8086` (`main.go:53`), explicitly outside 8080/8081.
- A `host/` page would sit outside the training workstream, need the D8
  host-policy vocabulary and the `/api/games` surface, and would be the wrong
  lifecycle (the dashboard reads training artifacts, not live tables).

The only other shape the ticket offers — a static artifact reading a ledger
file — is strictly worse here: `traindash` already polls and re-renders, so a
static file would be a second, staler copy of the same read.

### How it stays current

- **Who appends:** (i) the **gauntlet run**, which already writes
  `matches.jsonl`/`games.jsonl` per candidate, and (ii) any **botbench
  `-spellbench`** run pointed at the gauntlet root.
- **The one change needed** is that `sb-gauntlet.sh` currently deletes the
  candidate dir (`scripts/sb-gauntlet.sh:75-76`). **Recommended follow-up (not
  landed here — see §6 for why it is a separate, reviewable change):** write the
  candidate `matches.jsonl` under the gauntlet root (e.g.
  `$GDIR/cand/<git_head>/<spec>/matches.jsonl`) instead of the scratch dir.
  That is the *only* writer change; `results.jsonl` keeps its schema and the
  reward loop is untouched.
- **How `traindash` discovers it:** `scanDeckLedgers(dir, multi)` walks each
  configured **`-deck-root`** (new flag, default
  `/mnt/sata/gorge-training/spellbench-work/gauntlet`) for `matches.jsonl`,
  parses each into a `*DeckLedger`, and attaches it to `Snapshot.DeckLedgers`.
  Deck roots are deliberately **separate from `-root`**: the default training
  root `/mnt/sata/gorge-training` holds 362 `matches.jsonl` files / 145 MB
  across unrelated scratch dirs (`spellbench-work/w1/`, `w4/`, `v1b/`, …), and
  walking all of them made a cold `/api/runs` take 15.5–16.5 s and render 199
  charts. With the bounded deck root the same cold scan takes ~10.5 s (the
  pre-panel baseline) and renders the 3 real gauntlet ledgers. Discovery still
  skips the dirs the scan already skips (`skipDirs`, `scan.go:28`). Because it
  re-scans on every `GET /api/runs` poll (10s, `main.go:76`) and caches on
  mtime+size (`scan.go:194-224`), a new or updated ledger appears on the next
  poll with no restart. The focus policy is set by `-deck-focus` (`main.go`,
  new flag); with no flag each ledger reports its dominant policy (the seat
  name appearing most often).

This is deliberately **new files next to the hot `scan.go`**, per the brief's
hot-file guidance: the reader lives in `cmd/traindash/deckledger.go` and its
tests in `cmd/traindash/deckledger_test.go`; `scan.go` gained only the
`DeckLedgers` field, a `cachedAs` discriminator (so the deck reader and the
existing JSONL line-count reader do not collide in the scan cache), and the
`scanDeckLedgers` call.

---

## 5. Measurement: a real seat-swapped CI, at zero new game cost

The ask's Done-means #3 says: measure with real games, seats swapped, enough
games for a real CI. The brief also says the ticket's ground rules want the
existing harness reused, not games replayed. Both are satisfied the same way:
**the cached reference ledgers already hold a full seat-swapped round-robin**,
so the CI is extracted, not re-run.

`ref/4bf6638277783ca4/matches.jsonl` contains 640 rows = 10 policy pairs × 8
decks × 8 seat-swapped games. Computing `sb-search-lite-atk` **vs `bot`** per
deck (an ad-hoc `opponent == bot` filter, **not** the landed reader — the
landed `parseDeckLedger` has no opponent filter and pools all opponents, so
against this same file it reports `sb-tactical` = 213-43 (83.2 %) including
non-`bot` matchups), with a Wilson 95 interval:

```
=== sb-search-lite-atk vs bot (seat-swapped, cached ref games, zero new cost) ===
  Affinity   8-0  100.0%  CI [67.6, 100.0]
  Burn       6-2   75.0%  CI [40.9, 92.9]
  CawGates   6-2   75.0%  CI [40.9, 92.9]
  Elves      6-2   75.0%  CI [40.9, 92.9]
  Faeries    6-2   75.0%  CI [40.9, 92.9]
  Rally      7-1   87.5%  CI [52.9, 97.8]
  Spy        4-4   50.0%  CI [21.5, 78.5]
  Wildfire   8-0  100.0%  CI [67.6, 100.0]
  POOLED     51-13   79.7%  CI [68.3, 87.7]
```

This is a genuine, seat-swapped, CI-bearing result: **the search policy beats
`bot` 79.7% [68.3, 87.7] pooled**, confirming README's "decision-time search
beats the bot" (`README.md:321`) on the SpellBench pool. It is **not** a
`uw-tempo` result — the cached reference pool is the SpellBench 8-deck pool,
not the repo decks (verified: the first row is a `Wildfire` mirror match; the
`ref/*/run.json` `decks` array is the 8-deck SpellBench pool). So it
demonstrates the *chart* and the *measurement convention*, while the
`uw-tempo` number itself is a **cited** prior result (§2), not a new one.

Per-deck focus, explicit (`-deck-focus sb-tactical`), same cached file, also
zero-cost:

```
ref/4bf6638277783ca4 focus=sb-tactical games=256
   Affinity   25-7  0.781 ci=[0.612, 0.890]
   Burn       22-10 0.688 ci=[0.514, 0.820]
   ...
   Wildfire   26-6  0.812 ci=[0.647, 0.911]
```

### Explicit statement on new sweeps

**No new `sb-gauntlet.sh` sweep and no new `botbench` run were executed in this
seat**, and none is required by the design: the deck choice rests on three
already-published, CI-bearing measurements (§2), and the only fresh number (the
79.7% [68.3, 87.7] extraction) reads games that were already played and kept.
This honours the in-seat one-smoke-sweep cap by spending **zero** sweeps. If a
reviewer wants a fresh `uw-tempo` number, the exact command is one smoke sweep
(not run here):

```
scripts/sb-gauntlet.sh bot 1 uw-tempo        # SB_GAUNTLET_WORKERS=2
```

or, on the repo deck directly:

```
timeout 300 go run ./cmd/botbench -a bot -b sb-search-lite-atk -pairs all \
  -decks uw-tempo -spellbench -spellbench-out .ds4/scratch/uw \
  -spellbench-decks uw-tempo
```

(botbench flag names per `cmd/botbench/spellbench.go:104-121`; the exact
`-decks` spelling for the repo deck must be read from that flag block before
running — it was **not** verified in this seat, so treat the line as a sketch,
not a paste-ready command).

---

## 6. Incremental cost / plan to build the rest

| Step | What | Cost | Status |
|---|---|---|---|
| 1 | Per-deck reader (`deckledger.go`) + tests | 0 games | **Landed this ticket** |
| 2 | `traindash` "Decks" panel + `-deck-focus` + `-deck-root` (bounded discovery) | 0 games | **Landed this ticket** |
| 3 | On the *existing* cached refs, the per-deck chart already renders real numbers | 0 games | **Landed, shown in §5** |
| 4 | Keep the gauntlet's per-candidate `matches.jsonl` (write under `$GDIR` instead of `mktemp`) | ~1 file, ~5 lines in `sb-gauntlet.sh` | **Follow-up** — see below |
| 5 | Optional: read `results.jsonl` (pooled Elo by `ts`) into the same panel | 0 games, ~1 reader | **Follow-up** |
| 6 | A first fresh `uw-tempo` smoke sweep (if a reviewer wants a current number) | 1 smoke sweep | Not run (§5) |

Step 4 is deliberately **not** landed here: `sb-gauntlet.sh` is shell under the
fleet's `flock`/`systemd-run` resource gate, and changing its output layout is
a behavioural change to a shared tool that the brief scopes out ("do not rewrite
`sb-gauntlet.sh`"). It is written as a follow-up ticket so it can be reviewed
on its own.

**Measured cost of what landed** (this ticket):

- games played by this ticket: **0** (the cached refs were played 2026-09-28 by
  the gauntlet; this ticket only read them);
- wall time: unit tests run in ~0.01 s; the manual live check served
  `/api/runs` in <2 s; total code-writing + verification well under a minute of
  compute;
- files touched: 5 changed/new —
  `cmd/traindash/deckledger.go` (new, ~250 lines),
  `cmd/traindash/deckledger_test.go` (new, ~230 lines),
  `cmd/traindash/index.html` (+~30 lines: nav, `renderDecks`, `xticks`),
  `cmd/traindash/main.go` (+~20: the `-deck-focus` and `-deck-root` flags),
  `cmd/traindash/scan.go` (+~70: `DeckLedgers`, `DeckRoots`, `cachedAs`,
  `scanDeckLedgers`).

---

## 7. Gates run (real output)

Card corpus present in this worktree (so the run is not vacuous):

```
$ [ -e .cards ] && echo present
present
```

Targeted traindash gate (Done-means command, round 2 with the bounded-root
and seat-attribution fixes):

```
$ go test -run 'TestDiscovery|TestHTTP|TestReadOnly|TestAlerts|TestParseDeckLedger|TestWilson95|TestFindDeckLedgers|TestScanIncludesDeckLedgers|TestScanDeckRootsAreSeparate' ./cmd/traindash/ 2>&1 | tail -30
ok  	github.com/adams-shaun/gorge/cmd/traindash	0.008s
```

Format + generated-types gates:

```
$ gofmt -l cmd/traindash/          # empty (rc=0)
$ go run ./cmd/gentypes -check     # (no output = clean, rc=0)
```

Live end-to-end check of the real reader against real cached games (`/api/runs`
payload), **with the default flags** (training root default + new deck-root
default):

```
$ .ds4/scratch/traindash-fixed -addr 127.0.0.1:8093 &   # no -root, no -deck-root
$ time curl -s http://127.0.0.1:8093/api/runs -o snap.json
cold: 10.45s
$ python3 -c "import json;d=json.load(open('snap.json'));print('deck_ledgers',len(d['deck_ledgers']))"
deck_ledgers 3
['ref/4bf6638277783ca4', 'ref/b862bb30d6201d0a', 'ref/c6d42369f8f636e0']
warm: 0.22s
```

This is the finding's measured regression, fixed: before the fix the same
default-root cold scan returned **199** deck ledgers in 15.5–16.5 s; after it
returns the **3** real gauntlet ledgers in 10.45 s (the pre-panel baseline was
10.1 s), warm 0.22 s.

The JS half of the embedded page parses:

```
$ sed -n '/<script>/,/<\/script>/p' cmd/traindash/index.html | sed '1d;$d' > .ds4/scratch/dash.js
$ node --check .ds4/scratch/dash.js && echo "JS OK"
JS OK
```

Behaviour goldens outside `rules/` (see `system-t0.md`):

```
$ go test -count=1 -p 1 ./internal/archtest/ 2>&1 | tail -15
ok  	github.com/adams-shaun/gorge/internal/archtest	4.077s

$ go test -count=1 -p 1 -run TestConstructedDefaultIsByteIdentical ./cmd/botbench/ 2>&1 | tail -5
ok  	github.com/adams-shaun/gorge/cmd/botbench	0.711s
```

Whole-tree build is clean (`go build ./...`, rc=0) and the botbench pinned
split did **not** move (this ticket changed no engine behaviour).

## Fails without the fix

Three distinct hunks are each reverted in a scratch copy of the real file,
the one covering test is run, and the file is restored byte-identically (cmp).

**(a) The bounded deck-root split** (`scan.go`): put `scanDeckLedgers` back
inside the training-root loop (the finding's regression) and
`TestScanDeckRootsAreSeparate` fails:

```
$ go test -run 'TestScanDeckRootsAreSeparate' ./cmd/traindash/
--- FAIL: TestScanDeckRootsAreSeparate (0.00s)
    deckledger_test.go:226: deck ledger id = "spellbench-work/w1", want ref/key (training-root ledger must not leak in)
FAIL
FAIL	github.com/adams-shaun/gorge/cmd/traindash	0.002s

$ cp .ds4/scratch/scan.go.bak cmd/traindash/scan.go
$ cmp cmd/traindash/scan.go .ds4/scratch/scan.go.bak && echo "restored byte-identical"
restored byte-identical
```

**(b) The seat-attribution fix** (`deckledger.go`): trust the seats array
position instead of the seat ordinal (collapse `focusSeat == "p0"/"p1"` to
`seat == 0/1`) and `TestParseDeckLedgerPerDeck` fails on its reordered-seat row:

```
$ go test -run 'TestParseDeckLedgerPerDeck|TestParseDeckLedgerSeatsNonMirror' ./cmd/traindash/
--- FAIL: TestParseDeckLedgerPerDeck (0.00s)
    deckledger_test.go:105: Spy rate = {Deck:Spy Wins:1 Losses:0 Draws:0 Games:1 WinRate:1 CI:[0.20654931437723745 1]}, want 0-1 (p0 won against the p1 focus; order must not matter)
FAIL
FAIL	github.com/adams-shaun/gorge/cmd/traindash	0.002s

$ cp .ds4/scratch/deckledger.go.bak cmd/traindash/deckledger.go
$ cmp cmd/traindash/deckledger.go .ds4/scratch/deckledger.go.bak && echo "restored byte-identical"
restored byte-identical
```

**(c) The non-mirror deck attribution**: revert `deckAt(r.Decks, seat)` to
"first non-empty deck" and `TestParseDeckLedgerSeatsNonMirror` fails:

```
$ go test -run 'TestParseDeckLedgerSeatsNonMirror|TestParseDeckLedgerPerDeck' ./cmd/traindash/
--- FAIL: TestParseDeckLedgerSeatsNonMirror (0.00s)
    deckledger_test.go:228: focus deck = "Burn", want Elves (the seat the focus policy sat in), not the first deck
FAIL
FAIL	github.com/adams-shaun/gorge/cmd/traindash	0.002s
```

All three tests assert their own preconditions: `TestParseDeckLedgerPerDeck`
carries at least one win *and* one loss per deck with the focus in **both** seat
ordinals (so a rate of exactly 0 or 1 fails); `TestParseDeckLedgerSeatsNonMirror`
uses a non-mirror row (different deck each seat) so the "first deck" shortcut
cannot pass; `TestScanDeckRootsAreSeparate` asserts that the training root
holds a real `matches.jsonl` before asserting it does not leak in.


## Issues

1. **`sb-gauntlet.sh` deletes the per-candidate `matches.jsonl`/`games.jsonl`**
   (`scripts/sb-gauntlet.sh:75-76`, `trap ... EXIT`), so a candidate run's
   per-deck series is lost forever. Fix: write the candidate dir under
   `$GDIR` (e.g. `$GDIR/cand/<git_head>/<spec>/`). File as a follow-up because
   the brief scopes out rewriting `sb-gauntlet.sh`. Symptom observable in the
   reward loop: only the pooled `elo` row survives, so no per-deck reward axis
   is possible today.
2. **`results.jsonl` has no `traindash` reader.** The pooled Elo-by-`ts`/
   `git_head` series is real (4 rows) and is the natural "is the *pooled*
   policy improving" chart, but `cmd/traindash` does not read it. A small
   reader (same `deckledger.go` shape, keyed by `ts`) would add it with **zero
   new games**. Marginal cost ~1 file; not in this ticket's minimal scope.
3. **The SpellBench reference pool and the repo decks are disjoint.** The
   cached refs give a zero-cost per-deck chart over the 8-deck SpellBench pool
   (`Wildfire`, `Affinity`, …), but an adopted **repo** deck (`uw-tempo`) has
   no cached per-deck ledger — its series needs the §6 step-4 writer plus at
   least one fresh run. The plan keeps the two universes distinct rather than
   conflating them; a reviewer should not read the §5 SpellBench numbers as
   `uw-tempo` numbers.
4. **`-deck-focus` auto-detection picks the seat name appearing most often**,
   which in the cached refs is `bot` (it appears in every round-robin row). For
   a candidate-scoped view the flag must be passed explicitly; the auto default
   is a convenience, not a statement about which policy "should" be focused.

## Deviations from the brief

- The brief's open question offered a `host/` page as an alternative dashboard
  shape; this report recommends extending `cmd/traindash` and says why (§4).
- No new sweep was run; §5 states this and cites the three already-published
  measurements that carry the deck choice, per the brief's explicit
  "no measurement needed, here is why" allowance.
