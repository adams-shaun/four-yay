# traindash re-scope: measure TRAIN_ROOT, fix the empty-panel defect, decide the viewer question

**Date:** 2026-09-30 · **Ticket:** cli-20260930T025659Z-d2f55535 · **Worktree base:** `e784bd12b` · **Fix commit:** `1f8a9d70a`
**Kind:** research/measurement + one diagnosed defect fix (no engine change, zero games played).

The operator looked at the live traindash (127.0.0.1:8094, root `/mnt/sata/gorge-training`) and called it "a garbage can of non-visualized data". This report re-measures what is actually populated under the root, corrects two stale premises in the ask, fixes the one real code defect found, and answers the re-scope question with numbers.

**Bottom line:** keep traindash as the one viewer for everything under the root, and fix its *presentation* — the data is not missing, the page buries it. A separate perf-audit viewer would duplicate a reader the scanner already has. The Compare panel was genuinely blank at first paint; that is fixed in `1f8a9d70a` (it was a default-selection bug, not missing data).

---

## 1. The two stale premises, re-checked

Both were re-verified against the live server and this worktree's HEAD; both corrections stated in the triage held.

**Premise A — "the eval/per-deck winrate panels looked empty" was partially wrong, and the part that was right was a selection bug, not missing data.**

- The live server already runs current main's traindash: `curl -s http://127.0.0.1:8094/` is **byte-identical** to `cmd/traindash/index.html` at HEAD (`cmp` of the fetched page against the worktree file: no output, identical — verified 2026-09-30, and re-verified against my own instance before the fix landed).
- The per-deck panel is NOT empty. The live `/api/runs` snapshot carries **3 populated deck ledgers**: `ref/4bf6638277783ca4` (focus `bot`, 256 games, 8 deck rows), `ref/b862bb30d6201d0a` and `ref/c6d42369f8f636e0` (192 games, 8 rows each), all under the `defaultDeckRoot` `/mnt/sata/gorge-training/spellbench-work/gauntlet` (`main.go:54`). E.g. Affinity 14W/18L = 0.4375 [0.282, 0.607], Burn 21W/11L = 0.656 [0.483, 0.796] in `ref/4bf6638277783ca4`.
- The **Compare** panel *was* genuinely empty at first paint — but because `renderCompare` (`cmd/traindash/index.html:271`) initialised its arm selection to every run of the most-recently-modified experiment, and that experiment (`compact-test`, 1 adhoc run) contains no eval data. `index.html:293` then rendered `<div class="empty">Select arms above.</div>` even though 338 eval points exist one picker-click away. **Fixed in this ticket** (§4).

**Premise B — "the c05460d2/c9591724 plan" mis-cites the ticket.** `cli-20260930T022327Z-c05460d2` is the *effects/misc.go split* (merged, `6893cf24b`). The actual prior work is `cli-20260929T235706Z-c9591724` (merged; its report `docs/superpowers/reports/2026-09-29-adopt-a-deck-and-rl-dashboard.md` is the adopt-a-deck + RL-dashboard plan that produced the Decks panel) and `cli-20260930T022010Z-2c2950fe` (merged; the gauntlet expansion measured on that dashboard). All related tickets are merged — there is **no open dependency** on any of this.

**Also verified:** `TRAIN_ROOT` appears nowhere in the repo (`grep -rn "TRAIN_ROOT" cmd/traindash/ scripts/` → no hits). traindash takes `-root` (`main.go`); the live server's root is `/mnt/sata/gorge-training` (confirmed from `/api/runs` → `roots`). It is the operator's launcher env, not a traindash flag.

## 2. Re-measured: what is populated under TRAIN_ROOT

Fetched fresh from the **live operator server on 8094** (read-only; the server was not touched): `curl -s --max-time 60 'http://127.0.0.1:8094/api/runs'` → `.ds4/scratch/runs-fresh.json` (1,100,045 bytes), re-measured independently with a fresh Python reduction (`.ds4/scratch/measure.txt`), not copied from the brief. 321 runs across 13 experiments; root `/mnt/sata/gorge-training`.

| experiment | exitloop | adhoc | rounds | rounds w/ eval winrate | adhoc jsonl records | zero-data adhoc |
|---|---:|---:|---:|---:|---:|---:|
| autopay-audit | 1 | 47 | 0 | 0 | 6,878 | 14 |
| compact-test | 0 | 1 | 0 | 0 | 31 | 0 |
| converge-0927 | 1 | 0 | 20 | 19 | 0 | 0 |
| mtg-kernel | 1 | 0 | 0 | 0 | 0 | 0 |
| mz-gorge-compare-2026-09-24 | 13 | 0 | 67 | 67 | 0 | 0 |
| pn14 | 20 | 3 | 184 | 184 | 26,717 | 1 |
| pn15 | 7 | 6 | 68 | 68 | 212,355 | 1 |
| pn20 | 1 | 2 | 0 | 0 | 864 | 1 |
| pn21 | 0 | 2 | 0 | 0 | 4,344 | 0 |
| spellbench | 0 | 2 | 0 | 0 | 136 | 0 |
| spellbench-v2 | 0 | 1 | 0 | 0 | 40 | 0 |
| spellbench-v2-harness | 0 | 1 | 0 | 0 | 40 | 0 |
| spellbench-work | 0 | 212 | 0 | 0 | 838,968 | 1 |
| **total** | **44** | **277** | **339** | **338** | **1,090,373** | **18** |

This matches the brief's triage table exactly (the brief's "spellbench / -v2 / -v2-harness" split-out rows are shown here as separate lines; the totals agree).

What the numbers say:

- **277 of 321 runs are `kind: adhoc`**, and the mass is two experiments: `spellbench-work`'s 212 scratch `matches.jsonl` ledgers (838,968 jsonl records — 77% of all adhoc records) and `autopay-audit`'s 47 audit runs.
- **18 adhoc runs are pure zero-data** (no jsonl records anywhere in their run dir, no `report.md`): 14 in `autopay-audit` — including the `lazy-parity/*/pm` runs the ask named, whose `findings.jsonl` is 0 bytes — plus `pn14/seed`, `pn15/recollect/ent-t1`, `pn20/full`, `spellbench-work/search2`.
- **Only 4 experiments hold real training data** (eval rounds with a pooled win rate): `converge-0927` (19 eval rounds), `mz-gorge-compare-2026-09-24` (67), `pn14` (184), `pn15` (68) — **338 eval rounds total**, i.e. 99.7% of the eval data sits in 4 experiments while the Overview's default ordering leads with `compact-test`.
- **maxMod ordering is what the page leads with**, and it is noise-driven: by `maxMod` (`index.html`, `maxMod = e => Math.max(0, ...e.runs.map(r => +new Date(r.last_modified)))`) the order at fetch time was `compact-test` (1 run, no data) → `spellbench-work` (212 scratch ledgers) → `spellbench-v2-harness` → `spellbench-v2` → `spellbench` → … with the four eval-bearing experiments at positions 6+. One scratch ledger write anywhere under `spellbench-work` re-tops that sort.

Data sources per number: run kinds and counts → `/api/runs` `experiments[].runs[].kind`; rounds and eval win rates → `experiments[].runs[].rounds[].eval.win_rate` (scanner `evalFromDoc`, `cmd/traindash/scan.go:781`, reads `pooled.a_win_rate` / `pooled.a_win_rate_ci`); jsonl records → `experiments[].runs[].jsonl[].records` (scanner counts lines of `*.jsonl`); deck ledgers → `/api/runs` `deck_ledgers[]` (`findDeckLedgers`, `cmd/traindash/deckledger.go:228`, reduced from each ledger's `matches.jsonl`).

## 3. Answer: re-scope, or a separate viewer?

**Recommendation: do NOT build a separate perf-audit/fuzz viewer. Re-scope traindash's presentation, not its scope.** The measured reasons:

1. **The clutter is presentation, not data.** traindash already *distinguishes* the two populations — every run carries `kind: exitloop | adhoc` (`scan.go:62-63`, discovery in `findRuns`, `scan.go:422`) — it just doesn't *use* the distinction in the Overview tables, so 212 scratch-ledger adhoc rows interleave with the 44 training runs the operator actually wants to see. A second viewer would re-implement the scanner (`Scanner.Scan`, `scan.go:259`), the run discovery, and the report reader to show the same rows somewhere else.
2. **The zero-data runs are not traindash's job to alert on.** 18 of 277 adhoc runs have no content at all; that is a hygiene problem in whatever scripts wrote them (empty `findings.jsonl`, no report), and it is one `alerts` entry in the existing `/api/runs` payload away from visible — not a reason for a second tool.
3. **Deck-ledger discovery is already bounded.** The 2026-09-29 adopt-a-deck report's bounded-discovery recommendation landed as `-deck-root` / `-deck-focus` (`main.go:61-69`) with `findDeckLedgers` scanning only that root (`deckledger.go:228`, measured there: cold scan 10.45 s / 3 ledgers). The heavy root (`/mnt/sata/gorge-training`, 1.09 M jsonl records) is scanned for runs cheaply because run discovery is directory-shape based; nothing in the operator's complaint requires widening that.
4. **The eval data the operator wants charted exists and is already charted** — 338 eval rounds across 4 experiments; the panel was blank only because of the default-selection bug fixed in this ticket.

### Concrete proposal (what I would land next, and what this ticket landed)

**Landed here (trivially small):** the Compare default-selection fix (§4). Without it the page's one eval chart is blank at first paint — the single most damaging presentation defect, measured above.

**Next ticket (small, client-side only):** an Overview that leads with signal:

- **Per-experiment kind/data summary row**: each experiment row in the Overview gains two counts computed from `/api/runs` fields it already ships — `N training runs (exitloop)` / `M adhoc ledgers` / `K eval rounds` — so `spellbench-work` reads "0 training / 212 adhoc" at a glance instead of 212 same-looking rows. Data source: `experiments[].runs[].kind` and `rounds[].eval` — no scanner change.
- **Default-hide zero-data adhoc** in the Overview tables (the 18 runs of §2 with no `jsonl[].records` and no `report.md`), behind an "include empty" toggle. Data source: same fields; pure `index.html` filter.
- **Optional adhoc section**: collapse all `kind: adhoc` into its own collapsible panel below the exitloop tables, sorted by record count (source: `jsonl[].records`), so `spellbench-work`'s 838,968-record mass is one row until expanded.
- **Sort the experiment list by eval-round count first, maxMod second** in the Overview and Compare picker (source: `rounds[].eval`), so the four training experiments lead regardless of which scratch ledger was touched last.

Every number each of these shows is an existing `/api/runs` field; no placeholder charts, no scanner change, no new game.

## 4. Landed fix: Compare default selection (commit `1f8a9d70a`)

**Defect** (measured, not inferred): `renderCompare` (`cmd/traindash/index.html:271-276`) initialised `compareSel` to **every run of the maxMod-first experiment** and only afterwards filtered by `r.rounds.some(d => d.eval)`. At the live root the maxMod-first experiment is `compact-test` (1 adhoc run, `rounds: []`), so the default selection contained 1 run, 0 of which pass the eval filter → `<div class="empty">Select arms above.</div>` — behind 338 eval points. The picker fieldsets render only eval-bearing experiments, so nothing even looked selectable.

**Fix** (7 lines in `cmd/traindash/index.html`, no restructuring): the default is now the first maxMod-ordered experiment that contains at least one eval-bearing run, selecting only *its* eval-bearing runs; when no experiment has any, every eval-bearing run (an empty set) is selected and the empty hint still shows.

**Live before/after verification** against the real root (read-only; my own instance, never 8080/8081 or the operator's 8094):

```
scripts/fleet.sh port            # → 8090
timeout 90 go run ./cmd/traindash -addr 127.0.0.1:8090 -root /mnt/sata/gorge-training
curl -s http://127.0.0.1:8090/            | cmp - cmd/traindash/index.html   # identical → patched page served
curl -s http://127.0.0.1:8090/api/runs    → .ds4/scratch/runs-mine.json
```

Reduction over the fetched snapshot (Python replication of the exact JS logic, `.ds4/scratch/measure.txt`):

```
BEFORE (old default): compareSel = ['compact-test/s']        → runs passing eval filter: 0  → "Select arms above."
AFTER  (new default): compareSel experiment = converge-0927  → selection = ['converge-0927/run']
                      series count: 1  ·  eval points plotted: 19
```

`converge-0927` is the maxMod-most-recent of the four eval-bearing experiments (1790546055 vs pn15 1790282833, pn14 1790273892, mz 1790269702). The server is read-only against the root, exactly like the operator's; my instance was wrapped in `timeout 90` and port 8090 was confirmed free afterwards. The operator's 8094 server was not touched; the fix lands in code for the operator's next redeploy.

**Why no Go test can fail on this:** the change is embedded-page JS (`index.html`); the Go tests cover the scanner and HTTP handlers only, which are unchanged. Per the brief's "Done means", the equivalent failing-proof is the before/after above: the unpatched page's default selects `compact-test/s` → 0 eval-filtered runs → empty panel; the patched page selects `converge-0927/run` → 1 series, 19 eval points.

## 5. Win rate by game iteration (the ask's chart question)

Today the Decks panel aggregates `matches.jsonl` rows per deck — one categorical point per deck (8 rows × 3 ledgers), a Wilson interval per point. A **cumulative win-rate-vs-game-index series is derivable from the same rows at zero game cost.** The ledger rows already carry everything needed: `decks[].catalog_id` (deck label, indexed by seat), `seats[].name` (policy per seat), `winner`, `outcome` (`deckledger.go` `deckLedgerRow`, lines 52-67) — the parse just discards row order when it folds into per-deck counts.

**Precise proposal** (a follow-up ticket; two equally small shapes, either is fine):

- **Server-side (preferred, keeps the page dumb):** add `rows_by_game []LedgerGame` to `DeckLedger` — the natural win/loss rows in file order, one struct `{Game int, Deck string, Policy string, Won bool}` — filled inside `parseDeckLedger`'s existing row loop (~15 lines in `deckledger.go`, one new field on the existing JSON payload, no scanner traversal change; the parser already walks every row). Client: one cumulative `lineChart` per ledger (`x` = game index, `y` = running win rate of the focus policy, optional per-deck filter). Cost: a few hundred extra JSON points per ledger (256 + 192 + 192 games total — trivial).
- **Client-side (zero Go change):** `/api/runs` would need the raw rows shipped, which means either a new endpoint (`/api/ledger?id=…` re-reading the one `matches.jsonl`, ~40 lines in `main.go`) or inflating the existing snapshot payload with per-game rows — the endpoint is the cleaner of the two but is still more Go surface than the first shape.

**Verdict: belongs in a follow-up, not this ticket** — this ticket's mandate is the re-scope decision plus the diagnosed default-selection fix; the per-game series touches `deckledger.go`'s payload contract and deserves its own test (`deckledger_test.go` has the harness to extend: `TestParseDeckLedgerPerDeck`). I have not pre-implemented it.

## 6. Resource cost

- **Games played:** 0 (everything measured from files already on disk under `/mnt/sata/gorge-training`).
- **Wall time:** ~40 min seat time, dominated by the two snapshot fetches (~2 s each), the measurement reduction, and one 12 s server bring-up for live verification.
- **Files touched:** `cmd/traindash/index.html` (+7/−1, commit `1f8a9d70a`); this report. Nothing else.

## 7. Gates run (real output)

```
$ go test -run 'TestDiscovery|TestAdhocRuns|TestHTTP|TestReadOnly|TestScanDeckRootsAreSeparate|TestParseDeckLedger' ./cmd/traindash/
ok  	github.com/adams-shaun/gorge/cmd/traindash	0.007s

$ go test ./internal/archtest/
ok  	github.com/adams-shaun/gorge/internal/archtest	7.086s

$ go test -run TestConstructedDefaultIsByteIdentical ./cmd/botbench/
ok  	github.com/adams-shaun/gorge/cmd/botbench	0.650s

$ gofmt -l cmd/traindash/          # empty output → clean
$ go run ./cmd/gentypes -check     # exit 0
```

botbench did not move (expected — no engine change). No `.cards` dependency: `cmd/traindash` tests are fixture-based (`t.TempDir()`); the worktree's `.cards` symlink was present regardless.

## 8. Relationship to the prior report

This report extends, and does not re-derive, `docs/superpowers/reports/2026-09-29-adopt-a-deck-and-rl-dashboard.md` (`cli-20260929T235706Z-c9591724`), which recommended adopting `uw-tempo` as the first standing target, bounded deck-ledger discovery (its cold-scan 10.45 s / 3 ledgers measurement is what landed as `-deck-root`), and a pooled 79.7 % [68.3, 87.7] zero-new-games measurement for `sb-search-lite-atk` vs `bot`. Everything this report proposes keeps those invariants: discovery stays bounded, the Decks panel stays the per-deck surface, and every new number is an existing `/api/runs` field.

## Issues

- **18 zero-data adhoc runs under the live root** (list in §2): empty `findings.jsonl` / no `report.md`, written by the autopay-audit and pn* scratch scripts. Not fixed here (traindash does not write); they pollute the Overview row count and are the concrete target of the proposed default-hide in §3. Whoever owns those scripts should stop emitting empty run dirs.
- **`maxMod` ordering is scratch-write-sensitive** (§2, point 4): any write under `spellbench-work` re-tops the Overview/Compare ordering over the four eval-bearing experiments. Proposed fix is the §3 sort change; not landed here (layout change beyond this ticket's trivially-small mandate).
- No new defects found in the scanner or handlers; no CR-lane implication (nothing here touches the engine).
