# SpellBench FDN Limited in gorge — definition, coverage, backlog

Date: 2026-09-28. Branch `wt/sb-fdn-backlog` (base `spellbench-prep` @ a4898664a).
Corpus pin: Forge `95f04e8a04c8925fa97cb226fc3341cabcc90a53`.

## 1. What "FDN Limited" means

**SpellBench has not defined it yet.** It is one of two *proposed* benchmarks,
with a one-line summary and no deck, pool or rules definition:

- `benchmarks/proposed.json` (spellbench and spellbench-v2 clones, identical):
  `{"title": "FDN Limited", "summary": "Foundations limited games, the format
  several community draft models target.", "needs": "an engine"}`.
- The live site (jackmaiorino.github.io/spellbench) shows the same card:
  "Proposed · needs an engine".
- `docs/design/2026-09-27-everyone-on-the-board.md` ties it to **DraftZero**
  ("FDN Limited models on XMage") and plans it as sub-project X (XMage for FDN
  Limited and Standard 2022-25). It notes "FDN mirror matches work on v2.0"
  and that rating DraftZero fairly needs v2.1 "fixed-deck benchmarks, rotating
  pairs with hidden lists, legality lists".
- `spec/SPELLBENCH_PROTOCOL_V2.md` §12.1-12.2: decks are `{name, count}` main
  decks, best-of-one; a *rotating pool* benchmark has both seats play the same
  list (`opponent_decklist: visible`); §15 reserves *fixed-deck* benchmarks
  (entry = pilot + deck, lists hidden by default).

The "community draft models" are DraftZero's
([github.com/danieljbrooks/draft-zero](https://github.com/danieljbrooks/draft-zero),
model [danbrooks/draftzero-fdn-exp1](https://huggingface.co/danbrooks/draftzero-fdn-exp1);
the same author asked mtg-kernel for FDN support in
[mtg-kernel#110](https://github.com/jackmaiorino/mtg-kernel/issues/110)).
DraftZero's FDN pool is the concrete definition in use:

> every deck in the FDN Premier Draft game data whose player sits in the
> ≥60% win-rate bucket: 31,516 decks, split by draft into 28,366 train and
> 3,150 eval — built by `tools/extract_decks.py --set FDN --format
> PremierDraft --min-winrate 0.60` from 17lands public data (CC BY 4.0);
> every game draws both decks at random from the pool.

So FDN Limited is **fixed 40-card decklists from real drafts**, not a sealed
or draft simulation. gorge adopts that definition:

- **Deck source:** 17lands FDN PremierDraft replay data, main deck of each
  `(draft_id, build_index)`, player in the ≥0.60 game-win-rate bucket, 40+
  cards. (We measured over the local 30,000-game sample
  `/mnt/sata/gorge-training/17lands-spike/fdn30k.csv`: 6,966 distinct decks,
  1,418 in the ≥60% bucket. DraftZero's 31.5k is over the full file.)
- **Benchmark pool:** 16 decks chosen deterministically (ascending
  `sha256(draft_id:build_index)`), as a SpellBench rotating pool — both seats
  play the same deck, seat-swapped pairs, bo1, visible lists. That is the
  shape v2.0 can run today ("FDN mirror matches"). A DraftZero-style run
  (random deck pairs from the full pool, hidden lists) needs v2.1 and a
  larger embedded pool; the generator takes `-n`.

## 2. What was built (this branch)

- `scripts/spellbench-fdn-catalog.py <replay.csv[.gz]> [-n 16]` regenerates
  both artifacts below from any 17lands FDN PremierDraft replay file.
- `internal/spellbench/fdn-cards.tsv`: the FDN Limited card universe (the
  286 `deck_*` columns of the replay data: 281 non-basics + 5 basics) with
  play weights (copies over all decks, copies/decks over the ≥60% decks,
  copies×games).
- `internal/spellbench/decks/fdn-limited/*.json`: the 16-deck pool
  (`FDN01-UBG` … `FDN16-UG`), `format: "limited"`.
- `internal/spellbench/fdn.go`: `FDNLimited`, `FDNPool`, `FDNCards()`, and a
  `Catalog` registry (`Catalogs`, `CatalogByID`; `pauper-kernel` and
  `fdn-limited`, alias `fdn`).
- `botbench -spellbench … -spellbench-catalog fdn` plays the FDN pool
  (ledger `format: "fdn-limited-bo1"`, card pool identity
  `spellbench-fdn-limited-catalog/forge@<pin>`); default stays
  `pauper-kernel`, so existing runs are unchanged.
- Tests: `TestFDNCatalogDecks` (corpus-free catalog checks),
  `TestFDNCoverage` (weighted coverage + one-way ratchet table
  `knownUnsupportedFDN`; `FDN_COVERAGE_OUT=<file>` writes the gap TSV),
  `TestFDNCatalogCoverage` (per pool deck), `rules/TestFDNParamCensusReport`
  (the parameter census over the FDN universe, report only), and
  `cmd/botbench/TestSpellbenchFDNCatalogMirrors` (two FDN mirrors play to
  completion).

## 3. Coverage

Two layers, both weighted by main-deck copies in the 1,418 ≥60% decks
("wr60 copies"; ~56.7k copies in all, 60% of them basics).

**Primitive coverage** (`reg.Unsupported` against `effects.Supported()` with
rules' registrations):

| | before this branch | after |
|---|---|---|
| non-basic FDN cards fully supported | 277 / 281 (98.6%) | 279 / 281 (99.3%) |
| weighted by wr60 copies (all / non-basic only) | 99.66% / 99.45% | 99.90% / 99.84% |
| catalog pool decks fully supported | 14 / 16 | 15 / 16 |

The 95% "of card appearances" figure from the 17lands spike measured
something else (whether the replay could *stage and offer* each action), not
primitive support.

Registration-only false gaps fixed here (memory: check registrations first):

| card | wr60 copies | missing | fix |
|---|---|---|---|
| Hare Apparent | 139 | `kw:A deck can have any number of cards named CARDNAME.` | registered as a deck-construction keyword (the Partner/Companion class in `rules/trigger_match.go`); `deck.AnyNumberAllowed` exempts it from the Commander singleton check. The keyword has 10 corpus carriers (Relentless Rats, Persistent Petitioners, ...). |
| Progenitus | 0 | `kw:Protection from everything` | `rules/protection.go` already answered the "everything" quality; only the registration was missing. Test pins damage prevention against coloured and colourless sources. |

Remaining primitive gaps (ratchet table `knownUnsupportedFDN`):

| card | wr60 copies (decks) | missing | ticket |
|---|---|---|---|
| Time Stop | 38 (35) | `api:EndTurn` | `fdn-api-endturn` |
| Herald of Eternal Dawn | 18 (18) | `repl:GameLoss`, `repl:GameWin` | `fdn-repl-cant-lose` |

**Parameter census** (registered primitives that leave a parameter unread —
the card "plays" but not as printed). 11 FDN cards, 1.9% of wr60 copies:

| label | wr60 copies | cards | ticket |
|---|---|---|---|
| `api:ChangeZone.AlternativeDecider` | 399 | Uncharted Voyage | `fdn-changezone-alternative-decider` |
| `trig:SpellCast.OpponentTurn` | 172 | Brineborn Cutthroat | `fdn-trigger-gates` |
| `api:Dig.RestRandomOrder` | 163 | Squad Rallier, Loot | `fdn-dig-rest-random-order` |
| `trig:Attacks.Threshold` | 116 | Kiora, Crypt Feaster | `fdn-trigger-gates` |
| `api:GenericChoice.TempRemember`, `.FallbackAbility` | 96 | Perforating Artist | `fdn-genericchoice-remember` |
| `api:CopyPermanent.AtEOTTrig` | 52 | Chandra, Flameshaper; Electroduplicate | `fdn-copy-ateottrig` |
| `api:Effect.PumpZone` | 46 | Zul Ashur, Lich Lord | `fdn-graveyard-mayplay` |
| `stat:Continuous.MayPlay.MayPlayText` | 23 | Muldrotha, the Gravetide (fails closed: grants nothing) | `fdn-graveyard-mayplay` |

Several of these are real play bugs, not fidelity nits: Brineborn Cutthroat
grows on its controller's own turn, Crypt Feaster/Kiora ignore threshold,
Chandra's and Electroduplicate's copies are never sacrificed, Muldrotha does
nothing.

**Play smoke:** `botbench -spellbench sb-heuristic,bot -spellbench-catalog fdn
-spellbench-pairs 1` — 32 games (16 decks × 1 seat-swapped pair): 0 halts, 0
truncations, 0 refused-answer fallbacks, mean 18.1 turns (bot 25, sb-heuristic 7).
Benchmark-size runs are for after merge (one heavy job).

**Behaviour (unmeasured here):** the 17lands replay spike's "not offered"
list (Eaten Alive 2,851, Tolarian Terror 1,716, Luminous Rebuke 1,157, ...)
is confounded by lossy staging, so it is filed as an audit, not as bugs:
`fdn-castability-audit`.

## 4. Tickets filed (main pipeline, `agentctl issue add`)

| id | priority | scope |
|---|---|---|
| `fdn-api-endturn` | P2 | api:EndTurn (CR 723), Time Stop + 8 corpus cards |
| `fdn-repl-cant-lose` | P2 | repl:GameLoss/GameWin, Herald + Platinum Angel family (28/12 corpus) |
| `fdn-changezone-alternative-decider` | P2 | owner's top/bottom ask, Uncharted Voyage (35 corpus) |
| `fdn-trigger-gates` | P2 | OpponentTurn$ / Threshold$ trigger gates |
| `fdn-copy-ateottrig` | P2 | CopyPermanent AtEOTTrig$ |
| `fdn-genericchoice-remember` | P2 | GenericChoice TempRemember$/FallbackAbility$ |
| `fdn-dig-rest-random-order` | P3 | Dig RestRandomOrder$ (209 corpus) |
| `fdn-graveyard-mayplay` | P3 | Muldrotha MayPlayText$, Zul Ashur PumpZone$ |
| `fdn-castability-audit` | P2 | scenario tests for the 14 "not offered" cards |

No open ticket overlapped (queue checked 2026-09-28).

## 5. Plan to full support

1. Land the nine tickets; each removes its row from `knownUnsupportedFDN`
   or its census label. After them the primitive layer is 281/281 and the
   census is empty for FDN.
2. Run the FDN pool through `botbench -spellbench -spellbench-catalog fdn`
   at benchmark size (post-merge, one heavy job) and through cardfuzz with
   FDN decks; triage halts/stalls/refusals into tickets.
3. For a DraftZero-comparable benchmark: regenerate with the full 17lands
   file and a larger `-n`, and add a random-pair schedule once SpellBench
   v2.1 fixed-deck/hidden-list benchmarks exist.
4. Propose the definition upstream (SpellBench `benchmarks/fdn-limited`) with
   the 16-deck pool and gorge as the engine.

## First FDN baseline (2026-09-28, spellbench-prep 49544624c)

This is a gorge-native round robin on the 16-deck `fdn-limited` pool, played as seat-swapped mirrors: 4 pairs per deck, 128 games per bot pair, 384 games in total. It was rated with SpellBench's own leaderboard code, anchored on sb-uniform.

| Bot | Elo | CI95 | W-L |
|---|---|---|---|
| bot | 1327 | [1267, 1395] | 212-44 |
| sb-heuristic | 1103 | [1058, 1150] | 110-146 |
| sb-uniform (anchor) | 1000 | anchor | 62-194 |

- Every game finished normally: 0 truncated, 0 halted, 0 draws.
- One sb-uniform answer was refused by the engine, and the fallback handled it.
- sb-heuristic's pursuits (multi-step plays) failed 216 of 743 times, and sb-uniform's 266 of 789. That is the lost-action signal the sb-actions work targets.
- The ordering and the gaps are close to pauper-kernel's (bot 1238 / heuristic 1080).

Rerun with this sequence:

1. `botbench -spellbench sb-uniform,sb-heuristic,bot -spellbench-catalog fdn -spellbench-pairs 4 -spellbench-out <dir> -workers 8`
2. `scripts/spellbench-rate.py --anchor sb-uniform --format fdn-limited-bo1 --out <dir>/rate <dir>` (with `/mnt/sata/gorge-training/sbvenv/bin` on PATH)

The data is under `/mnt/sata/gorge-training/spellbench-work/fdn/base4/`.
