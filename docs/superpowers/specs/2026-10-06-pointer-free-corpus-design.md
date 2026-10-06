# Pointer-free card corpus: design and implementation plan

- **Date:** 2026-10-06
- **Base:** `main` at `7b37cc0eb`. Every `path:line` below was read at that SHA. The worktree this was written in sits on `95965567d`, which differs from `7b37cc0eb` only in `internal/codeshape/ratchet_test.go` (+1 on two constants unrelated to this doc).
- **Status:** design + plan. No production code changed. Measurements in §1 were taken in a throwaway probe package and with throwaway `enginebench` build tags. Neither is committed. §9 has the commands.
- **Labels:** anything not read from code or measured is marked **INFERRED** or **ESTIMATE**.

## 0. Summary

1. **The problem is confirmed and sized.** After `cards.OpenCorpus` the live heap is **163.1 MB in 1,685,473 heap objects**. Of that, **102.2 MB is scannable** (`/gc/scan/heap:bytes`). A forced GC takes **52–58 ms wall at GOMAXPROCS=2**; on an empty heap it takes 0.2–0.3 ms. Every GC cycle re-marks the whole corpus.
2. **Shrinking the live heap is not enough. The scan set must shrink while the heap stays heavy.** The subset loader that is already on `main` cuts live heap to 3.9 MB but moves nothing (random ×1.01, bot ×0.94): a tiny heap makes the pacer collect constantly. A throwaway build that adds a **pointer-free 40 MiB `[]byte` ballast** to the subset measured **random ×1.43 (GC 30.3% → 5.2%) and bot ×1.28 (GC 27.1% → 4.3%)**, medians of 3 ABBA reps. A pointer-free corpus image is exactly "subset scan set + noscan weight", so this is the payoff model (§1.4).
3. **Design: a pointer-free resident corpus image plus lazily materialized cards** (§2). The whole corpus lives as one noscan image: a string blob and dense `uint32` row arrays, about 31 MB (**ESTIMATE**). It grows out of the `CompiledCatalog` that already exists (`cards/compiled_catalog.go:98`). The existing `*Card`/`*Face`/`*SA` API is kept unchanged for the engine, but a card becomes a Go tree only when something touches it (`Lookup`, the token table, a named-card mechanic). The pointer set the GC sees drops from about 100 MB to the decks in play plus the tokens: about 4 MB, measured by the subset build as 3.9 MB including every token.
4. **Flattening the `*SA`/`*Face` tree in place (the "convert every reader" route) is deferred and gated.** It would touch about 2,400 non-test reader sites (§1.3). Once only materialized cards are pointerful, it buys at most about 2% CPU (map reads and `HasKeywordID` are each ≤1.5% of CPU in the baseline profiles) and almost no GC. It stays in the plan as Phase II, behind a kill criterion.
5. **Steps S0–S8, each landable green with heads unchanged** (§3). Eight are scheduled (S0, S1, S2a, S2b, S3, S4, S5, then S8); S3b, S6 and S7 open only on measured evidence. The GC win lands at **S4**. S5 (the image as the on-disk cache format) also removes gob, the segment file and the all-corpus transient at load. Lazy tokens are dropped: the whole token table measures about 1.2 MB.

## 1. Census

### 1.1 Live heap after corpus load

**Probe:** a throwaway test package (§9.1) calls `cards.OpenCorpus(.cards)` (not `SharedCorpus`), runs `runtime.GC()` twice, then reads `runtime.MemStats` and `runtime/metrics`. It runs under the operator cap (`systemd-run … MemoryMax=2G CPUQuota=200% GOMAXPROCS=2 GOMEMLIMIT=1536MiB`).

| metric | before load | after load | after dropping the registry |
|---|---:|---:|---:|
| `HeapAlloc` | 0.5 MB | **163.1 MB** | 0.5 MB |
| `HeapObjects` | 2,688 | **1,685,473** | 3,645 |
| `/gc/scan/heap:bytes` (pointer-bearing heap) | 0.4 MB | **102.2 MB** | 0.4 MB |
| forced `runtime.GC()` wall, mean of 10 | 0.2–0.3 ms | **52–58 ms** | 0.9–1.7 ms |

`enginebench` reports the same figure: `heap_live_mb` = 163.1 in every row of `/mnt/sata/gorge-training/enginebench/prof/20261006T140106/results.jsonl`. Its `-memprofile` is switched on only *after* the corpus loads (`cmd/enginebench/main.go:97-105`), so the baseline `.mem` files do not show the corpus at all. That is why this census needed its own probe.

### 1.2 Where it comes from

**By allocation site.** `go tool pprof -sample_index=inuse_space -top` on a heap profile written right after load, with `GODEBUG=memprofilerate=4096`:

| site | inuse MB | what it is |
|---|---:|---|
| `reflect.unsafe_New` (gob `decAlloc`) | 30.55 | the decoded `Card`/`Face`/`SA` structs |
| `encoding/gob.decString` | 30.10 | one heap string per decoded string field, map key and map value |
| `reflect.mapassign_faststr0` + `reflect.makemap` | 22.54 + 6.02 | `Params` and `SVars` maps |
| `cards.newParamSet` | 21.77 | `ParamSet`, one per node (`cards/params.go:1597`) |
| `cards.(*catalogBuilder).stringID` | 16.77 | `CompiledCatalog.StringBlob` and its growth slack |
| `cards.parseParams` (+`SplitSeq`) | 8.93 | re-parse of SVar bodies during the relink at load |
| `compileFace` / `compileAbility` / `compileParams` | 5.45 / 3.76 / 3.29 | catalog row arrays |
| `cards.parseSA` | 2.88 | re-parsed SVar ability trees |
| `rerootPaths` | 3.35 (cum) | a re-allocated absolute `Path` string per card (`cards/open.go:236`) |
| `nameIndexOf` | 2.35 (cum) | `byName map[string]*Card` |

Cumulative: `LoadRegistry` 160.3 MB, of which the gob decode is 93.45 MB and `finishDecoded` 66.85 MB. Within `finishDecoded`, `Card.Link` (the relink) is 34.09 MB, `CompileMetadata` 29.29 MB and `deriveParamSets` 22.21 MB.

**By type.** A reflective walk from the `*Registry` root deduplicates pointers, slice backings and string data. Sizes are struct sizes; map cost is estimated as entries × (k+v), so it is a lower bound.

| type | count | bytes | pointer-bearing |
|---|---:|---:|:--:|
| `*cards.Face` | 35,915 | 25.0 MB (**696 B each**) | yes |
| `*cards.ParamSet` | 85,819 | 15.8 MB (184 B each; 192 B size class) | yes |
| `map[string]string` entries | 121,734 maps / 488,716 entries | ≥14.5 MB | yes |
| `[]string` backings (Types, Keywords, Aliases, `ParamSet.vals`) | 135,326 | 7.7 MB | yes |
| `*cards.SA` | 57,038 | 5.9 MB | yes |
| `*cards.Card` | 34,928 | 2.5 MB | yes |
| `*cards.ExtSlot` / `*cards.Slot` | 92,953 / 70,843 | 0.7 / 0.6 MB | yes |
| `[]cards.Trigger` / `[]cards.Static` / `[]cards.Repl` backings | 15,333 / 6,491 / 2,571 | 0.9 / 0.3 / 0.1 MB | yes |
| **catalog rows** (`FaceRow`, `ParamRow`, `AbilityRow`, `StringRef`, `SVarRow`, …) | 13 slices | **~11.4 MB** | **no** |
| catalog `StringBlob` | 1 | 13.2 MB cap (10.7 MB len) | **no** |

- **Strings:** 2,106,516 string headers (≈33.7 MB of headers inside the structs above), 32.5 MB of distinct string data, but only **237,849 unique contents = 19.6 MB**. Interning alone would drop 13 MB of data and about 2 M heap objects.
- **Scale:** 34,074 cards and 854 tokens.
- **Catalog row counts:**

  | faces | abilities | triggers | statics | replacements | params | keywords | SVars | type tokens | strings |
  |---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
  | 35,915 | 57,038 | 18,594 | 7,471 | 2,716 | 389,415 | 18,898 | 63,234 | 80,259 | 103,181 |

Three findings drive the design.

1. **A pointer-free form of the corpus already exists, and it is a pure cost today.** `CompileMetadata` (`cards/compiled_catalog.go:189`) builds `CompiledCatalog` (`:98-121`) at every load: a string blob, `StringRef` offsets, `ParamRow{Key,Value StringID}` spans, `AbilityRow.Sub` as an index and `FaceRow` masks. That is about 25 MB of noscan arrays. Nothing outside `cards` reads `Registry.Catalog()` in non-test code (0 sites, §1.3). Faces and SAs use only a few derived fields from it (`SpellAbility`, `ManaAbilities`, `CompiledAPI`, `TriggerInterests`, the type and keyword masks). The pointer tree stays the source of truth, so the catalog *adds* heap.
2. **The catalog pins the whole tree.** `facePointers`, `abilityPointers` and `manaAbilityPointers` (`cards/compiled_catalog.go:115-120`) back-reference every face and ability. Every materialized face points at the catalog (`Face.compiledCatalog`, `cards/ir.go:144`). A token walk that followed `compiledCatalog` therefore reached 93.7 MB. Any lazy design must give a materialized face no pointer path to the rest of the corpus.
3. **Parameters are stored twice.** Each node keeps both `Params map[string]string` and `ParamSet.vals []string` (`cards/params.go:1579-1591`), and `ParamSet.src` holds the map so the set can check its own binding (`:1733`).

**Token table alone.** Walking `reg.Tokens` without following `compiledCatalog` gives 856 faces, 190 SAs, 343 `ParamSet`s and 1,199 maps. That is 0.9 MB of objects plus 0.2 MB of string data, about **1.2 MB** with map overhead. Making tokens lazy is not worth a 472-test-site migration; see S6.

### 1.3 Consumer API census

Counts come from a throwaway `go/types` tool (§9.3). It type-checks every package with the source importer and counts each `SelectorExpr` whose object is a field or method of `cards.{Registry,Card,Face,SA,Trigger,Static,Repl}`. "nontest" means non-`_test.go` files.

| member | nontest | test | heaviest non-test packages |
|---|---:|---:|---|
| `SA.ParamStr` / `SA.Param` / `SA.HasParam` / `SA.ParamCode` | 1022 / 131 / 18 / — | 69 / 6 / 3 | effects 704, rules 172, effects/params 35 |
| `Trigger.ParamStr` / `Param` | 373 / 80 | 16 / 2 | rules/trigmatch 135+52, rules 85+27 |
| `Static.ParamStr` / `HasParam` | 154 / 76 | 18 / 4 | rules 64, compliance/levelb 26+27 |
| `Repl.ParamStr` / `Param` | 92 / 41 | 2 / 2 | rules 90+38 |
| **`SA.Params` (the map)** | **312** | **1340** | effects 134, cards 46, effects/params 20, events 16, rules 25, rules/pay 14 |
| `Static.Params` / `Trigger.Params` / `Repl.Params` | 136 / 55 / 20 | 152 / 250 / 80 | rules 30+12+1, compliance 75 |
| `Face.Name` | 500 | 1899 | rules 236, effects 82, rules/pay 20, view 17 |
| `Card.Faces` | 482 | 1989 | rules 109, compliance/oraclegen/templates 77, effects 41 |
| `Face.SVars` | 298 | 835 | rules 167, cards 36, rules/pay 12 |
| `SA.API` / `SA.Sub` / `SA.Line` / `SA.Kind` | 262 / 113 / 79 / 59 | 673 / 286 / 64 / 81 | rules 111/53/43, effects 49/11/22 |
| `Face.Abilities` / `Triggers` / `Statics` / `Repls` | 202 / 161 / 132 / 124 | 604 / 424 / 185 / 183 | rules 33/63/37/66 |
| `Repl.With` / `Trigger.Effect` | 169 / 98 | 80 / 187 | rules 144 / 50 |
| `Face.ManaCost` / `Types` / `Keywords` | 90 / 105 / 73 | 70 / 208 / 101 | rules 24/16/24, effects 7/36/3 |
| `Face.HasKeyword` | 48 | 157 | rules 30, effects 7. **29 calls in `rules/` (non-test) pass a string literal** (`git grep -c -F 'HasKeyword("'`) |
| `Face.KeywordParam` / `Face.Mentions` | 68 / 7 | 47 / 0 | rules 54 / rules 5 + rules/pay 2 |
| `Registry.Lookup` | 75 | 850 | compliance/oraclegen/templates 43 |
| `Registry.Cards` (whole-corpus slice) | **57** | **290** | cards 15, compliance 17, cmd/* 16, rules 2 |
| `Registry.Tokens` (map) | **50** | **472** | cards 11, cmd/* 17, internal/spellbench 15, rules 2 |
| `Registry.Catalog()` | **0** | 18 | — |

Other surfaces that matter to this design:

- **Literal-key `Params["X"]` is already gone from rules/ and effects/.** codeshape's `stringParamReads` ratchet is frozen at **0** (`internal/codeshape/ratchet_test.go:111`, metric at `internal/codeshape/codeshape.go:404-407`). The remaining `SA.Params` uses are dynamic-key reads, ranges, copy-on-write rewrites and views that carry the map onward (for example `staticView{Params: st.Params, PS: st.ParamSetOf()}`, `rules/statics.go:313`). `stringKeyedParamReads` = 167 (`ratchet_test.go:285`).
- **Runtime-built IR nodes (non-test):**

  | literal | sites |
  |---|---:|
  | `cards.SA{` | 52 |
  | `cards.{Static,Trigger,Repl}{` | 45 |
  | `cards.Face{` | 7 |
  | `cards.Card{` | 24 |

  On top of these, `cards.ResolveSVar` (`cards/link.go:181`) is called from 54 non-test files, 17 of them in effects/ (`git grep -l 'cards\.ResolveSVar('`). These nodes must stay ordinary mutable Go structs: `SA.SetParam` (`cards/params.go:1874`) and copy-on-write `Params`.
- **Whole-corpus inputs to the engine.**
  - `rules.Config.Tokens map[string]*cards.Card`, `NameUniverse []*cards.Card` and `NamedCorpus []*cards.Card` (`rules/engine.go:91-100`), mirrored on `state.Game` (`state/game.go:435-444`).
  - `NameUniverse = reg.Cards` is set by `rules/acceptance_game.go:50` (so by **TestHeads**), by `cmd/gorged/main.go:722` (the live server), and by botbench, cardfuzz, paymirror and spellbench. `enginebench` passes `Tokens` only (`cmd/enginebench/decks.go:211`).
- **Runtime scans of the whole universe:**
  - `effects/namecard.go:166`: a full object matcher over every universe card for a `ValidCards$` NameCard ask.
  - `effects/clone.go:100` and `events/apply_copy.go:377`: first card whose `Faces[0].Name` equals a name.
  - `state/game.go:709` (`NamedCard`): first `NormalizeName` match, across `NameUniverse` then `NamedCorpus`.
  - `rules/chars/types.go:240` (`CorpusLandTypeWords`, memoized on `&universe[0]`).
  - `effects/namecard_cache.go:99` (`NameUniverseNames`).

### 1.4 The GC model, and the experiment that sized the payoff

GC CPU per unit of work is roughly *(allocation rate ÷ heap goal) × mark cost per cycle*, where heap goal ≈ live × (1 + GOGC/100) and mark cost ∝ scannable bytes + objects + roots. **INFERRED** from the Go pacer design and consistent with every row below. Two consequences follow.

- **The subset loader misses the win.** It shrinks *both* terms: cycles become about 40× more frequent and each cycle is about 25× cheaper. The 2026-09-30 speed report saw no throughput change on the subset build either (random FDN 819 vs 818, `docs/superpowers/reports/2026-09-30-gorge-engine-speed.md`), even though its GC-paid clone cost fell 58 → 26 µs.
- **A pointer-free image keeps the heap goal and drops the mark cost.** A noscan `[]byte` is allocated in a noscan span. The GC sets one mark bit for it and never reads its contents, but its bytes still count toward the live heap and the heap goal.

**Experiment.** Throwaway build tag `enginebench_ballast`, §9.2.

- **Variants:**

  | variant | corpus | ballast | heap after load |
  |---|---|---|---:|
  | full | whole corpus | — | 163.1 MB |
  | subset | `-tags enginebench_subset` (`cmd/enginebench/corpus_subset.go`, `cards.OpenCorpusFor`) | — | 3.9 MB |
  | subset+b40 | subset | 40 MiB noscan `[]byte` | 43.9 MB |
  | subset+b160 | subset | 160 MiB noscan `[]byte` | 163.9 MB |

- **Run conditions:** `-secs 8` (CPU-seconds), reps in ABBA order, each run under `flock -o /home/sadams/projects/gorge/.ds4/heavy.lock` plus a systemd scope.
- **Data:** `/mnt/sata/gorge-training/enginebench/corpus-plan/ballast-20261006T145100.jsonl`, with CPU profiles beside it.

Results: medians of 3 ABBA reps, pair A, turns per CPU-second. GC share is `runtime.gcDrain` cum from each rep-1 CPU profile.

| variant | random (reps) | random median | ratio | random GC | bot (reps) | bot median | ratio | bot GC |
|---|---|---:|---:|---:|---|---:|---:|---:|
| full | 2901, 2849, 2924 | 2901 | 1.00 | 30.3% | 2210, 2012, 2058 | 2058 | 1.00 | 27.1% |
| subset | 2934, 2982, 2822 | 2934 | 1.01 | 21.8% | 2293, 1930, 1915 | 1930 | 0.94 | 14.3% |
| **subset+b40** | 4154, 4229, 4117 | **4154** | **1.43** | **5.2%** | 2762, 2641, 2587 | **2641** | **1.28** | **4.3%** |
| subset+b160 | 4574, 4165, 4191 | 4191 | 1.44 | 2.5% | 2858, 2956, 2592 | 2858 | 1.39 | 1.9% |

The effect is consistent across all reps, and on both rows the subset alone is flat or worse, so the model holds. The sampler row was not run; it is the heaviest row and its GC share is the smallest (~17%).

**What this means for the design.**

- **Keep the image on the Go heap.** An off-heap `mmap` arena would remove the image's weight from the pacer and land at "subset" (no gain) unless GOGC/GOMEMLIMIT were tuned by hand. The ballast effect comes free from a heap-resident image.
- **31 MB of image plus about 4 MB of residual behaves like subset+b40:** about ×1.4 random and ×1.28 bot (**ESTIMATE** by emulation, method above).
- **The GOGC lever composes with this, but adds little once the scan set is gone.** b160 ties b40 on random (×1.44 vs ×1.43) and beats it on bot (×1.39 vs ×1.28). Followup item 1 measured GOGC=400 at +10% on today's corpus. It is a separate, optional knob (§7).

## 2. Design

### 2.1 Options weighed

| option | GC scan set after | heap goal | reader churn | replay risk | verdict |
|---|---|---|---|---|---|
| **A. Pointer-free resident image + lazily materialized `*Card` view** | decks in play + tokens (~4 MB) | image ~31 MB keeps the pacer calm | `Registry.Cards` 57 sites, the universe handle ~15 sites | low: the materialized card is the same value today's loader builds; oracle-tested | **chosen** |
| B. Flatten `Face`/`SA`/`Static`/… in place: StrIDs, param spans, index-linked SA, no maps, every card resident | ~0 for printed nodes, plus runtime-built nodes | same as A | ~2,400 non-test + ~10,000 test reader sites; 52+45+7 runtime literals need a second mutable representation | high: every reader is rewritten | Phase II only, gated (§2.7) |
| C. Subset loading only (already on `main`) | ~4 MB | ~8 MB, pacer thrashes | none | none | measured as no gain (§1.4); and the server needs the whole universe |
| D. Off-heap `mmap` arena (`syscall.Mmap`, pure stdlib) | ~0 | pacer loses the weight | as A | as A, plus `unsafe` slice-over-mmap (GC-safe only if no Go pointer is ever stored inside it) | rejected: loses the ballast effect; `unsafe.Slice` over foreign memory is legal but adds a lifetime hazard for no gain |
| E. GOGC / ballast / GOMEMLIMIT only | unchanged 102 MB | raised | none | none | a +10% stopgap (followup.md); does not remove the per-cycle 1.7 M-object mark; composes with A |

A is the only option that removes the scan set without dropping cards or rewriting the readers. It keeps the brief's target representation: one string table, `uint32` IDs, flat param rows, index-linked SA nodes, bitsets and no per-card maps. That representation becomes the *resident* form of the corpus, while the engine keeps reading the Go tree it already reads, built only for cards in play. B's representation is the same one; Phase II (§2.7) can carry it into the materialized view later if measurement asks for it.

### 2.2 Target representation: `cards.Image`

New file `cards/image.go`. Every field below is a `uint32`, a struct of `uint32`s, or a `[]byte`, so each slice backing is allocated noscan. The ID types reuse `cards/compiled_codes.go:6-10` (`StringID`, `FaceID`, `AbilityID`); `Span` and `StringRef` are reused from `cards/compiled_catalog.go:19-27`.

```go
// Image is the resident, pointer-free corpus: the exact content of today's
// gob cache (the CompileDir output, "T0") flattened into dense rows over one
// string blob. Immutable after build. No field holds a Go pointer except the
// slice headers below, so the GC marks ~20 headers and never scans a row.
type Image struct {
	Hdr ImageHeader

	Blob []byte      // every distinct string's bytes, concatenated
	Strs []StringRef // StringID-1 -> Blob[Offset:Offset+Length]; StringID 0 = ""

	Cards  []CardRec  // ordinal i == today's Registry.Cards[i] (sorted script path)
	Tokens []TokenRec // sorted by key (file stem)

	Faces   []FaceRec
	FaceSAs []NodeID // Face.Abilities, in order (a face's Span indexes here)
	Nodes   []SARec  // every *SA the gob encoder would emit; NO sharing (gob flattens)
	Trigs   []TrigRec
	Stats   []StaticRec
	Repls   []ReplRec
	Params  []ParamRow // {Key, Value StringID}; a node's span is sorted by key text
	SVars   []SVarRow  // {Name, Body}; a face's span is sorted by name
	Words   []StringID // Types / Keywords / Aliases spans

	// Whole-corpus indexes, pointer-free (replace byName and the seg tail).
	NameKeys []StringID // sorted NormalizeName keys: the tiered nameIndexOf
	NameOrds []uint32   // parallel: card ordinal
	FirstOrd []NameOrd  // exact Faces[0].Name -> FIRST ordinal (NamedCard/clone semantics)
}

type ImageHeader struct {
	Magic        [8]byte // "gorgeimg"
	Schema       uint32  // image layout version
	CacheVersion uint32  // today's cacheVersion (cards/registry.go:237)
	Fingerprint  [16]byte
	CorpusHash   [32]byte // sha256 of the canonical rows (determinism check)
}

type NodeID uint32 // 1-based into Nodes; 0 = nil *SA

type CardRec struct {
	Path          StringID // RELATIVE to the corpus root: "cardsfolder/a/x.txt"
	AlternateMode StringID
	Faces         Span
	Flags         CardFlags // NamesACard, ChangesTypes, SetsName, MayCarryControlStatic
}

type TokenRec struct {
	Key  StringID
	Card CardRec
}

type FaceRec struct {
	Name, ManaCost, PT, Loyalty, Defense, Colors, Oracle StringID
	SpecializeColor, CopyFaceFrom                        StringID
	Types, Keywords, Aliases                             Span // into Words
	Abilities                                            Span // into FaceSAs
	Triggers, Statics, Repls, SVars                      Span
	TypeMask                                             TypeMask // printed type line, for whole-corpus queries
}

type SARec struct {
	Kind, API, Line StringID
	Params          Span
	Sub             NodeID
}

type TrigRec struct {
	Mode   StringID
	Params Span
	Effect NodeID
}

type StaticRec struct {
	Mode   StringID
	Params Span
}

type ReplRec struct {
	Event  StringID
	Params Span
	With   NodeID
}

type NameOrd struct {
	Name StringID
	Ord  uint32
}
```

**Why T0 and not the finished tree.** Today the gob cache holds the `CompileDir` output, and `finishDecoded` (`cards/registry.go:343-412`) re-derives and re-links every face at load. The image holds exactly the bytes gob would have carried. Materializing card *i* is then `rebuild(i)` followed by the same per-card half of `finishDecoded`. The subset loader already uses this per-card route (`subsetSource.lookup`, `cards/subset.go:437-471`). `TestSubsetRegistryMatchesFull` (`cards/subset_test.go:78`) checks it against the whole-registry route on a sample: every 97th card plus split, transforming, CopyFaceFrom and alias shapes. S1 extends that check to every card. No new equivalence argument is needed, and `finishDecoded∘finishDecoded` is never assumed idempotent.

**Size (ESTIMATE).** Built from the §1.2 counts:

| part | estimate |
|---|---:|
| Blob (all unique strings, Oracle text included) | ≤ 19.6 MB |
| `Strs` | 1.9 MB |
| `Params` (389k × 8 B) | 3.1 MB |
| `Faces` (36k × 76 B) | 2.7 MB |
| `Nodes` (57k × 20 B) | 1.1 MB |
| rest | ~2.5 MB |
| **total** | **~31 MB, all noscan** |

### 2.3 Registry modes and the GC-visible residual

`Registry` (`cards/registry.go:17-33`) gains a second mode.

- **eager (unchanged):** `NewRegistry` + `Add`, `CompileDir`. These are test-built registries: 69 `Add` sites and 2,080 `cards.Card{` literal sites in tests stay as they are.
- **imaged (new):** `OpenCorpus`/`SharedCorpus`/`LoadRegistry` return `{img *Image, mat []atomic.Pointer[Card], tokens map[string]*Card}`. `mat[i]` is nil until card *i* is first touched.

The pointer set of an imaged registry after load:

| object | pointers | scan bytes |
|---|---|---:|
| `Image` header + ~16 slice headers | 16 | <1 KB |
| `mat` (34,074 × 8 B, mostly nil) | 34k words | 272 KB |
| token map + 854 eager tokens | as today | ~1.2 MB (§1.2) |
| materialized deck cards (a 4-seat game ≈ 100–300 distinct cards) | as today per card | ~0.3–2 MB (**ESTIMATE** from 696 B/face + params; the subset build measured 3.9 MB total *including* every token) |
| per-card mini catalogs (see §2.5) | 3 pointer slices each | small |

The residual is about 2–4 MB against 102 MB today: a ≥25× smaller scan set. `/gc/heap/objects` falls from about 1.69 M to ~30–60 k (**ESTIMATE**).

### 2.4 API seams

New and unchanged surface, all in `cards`:

```go
func (r *Registry) Len() int                           // number of corpus cards
func (r *Registry) Card(i int) *Card                   // materialize-on-first-use
func (r *Registry) AllCards() []*Card                  // materializes EVERY card; tools/tests only (memoised)
func (r *Registry) Lookup(name string) (*Card, bool)   // unchanged signature; image name index + materialize
func (r *Registry) Token(key string) (*Card, bool)     // exists (test-only callers today)
func (r *Registry) Universe() *Universe                // pointer-free name universe (S3)
func (r *Registry) MaterializedCount() int             // ratchet/bench observability
```

The `Cards []*Card` field becomes unexported (S2b). It must be removed, not left nil: a test that ranges a nil `reg.Cards` would pass vacuously. `Tokens map[string]*Card` stays exported and eager.

`cards.Universe` (S3) replaces `NameUniverse []*cards.Card` and `NamedCorpus []*cards.Card` in `rules.Config` (`rules/engine.go:93,100`) and `state.Game` (`state/game.go:438,444`):

```go
type Universe struct { /* *Image + ordinal list, or a []*Card adapter for test-built slices */ }
func (u *Universe) Len() int
func (u *Universe) Name(i int) string              // Faces[0].Name, zero-copy from the blob
func (u *Universe) Names() []string                // == effects.NameUniverseNames(old slice), byte-identical
func (u *Universe) FirstByName(exact string) (int, bool)
func (u *Universe) FirstByNormalized(n string) (int, bool)   // state.Game.NamedCard semantics
func (u *Universe) Card(i int) *Card                 // materializes one
func (u *Universe) LandTypeWords() []string          // == chars.CorpusLandTypeWords(old slice)
func UniverseOf(cs []*Card) *Universe                // adapter: same answers over a plain slice
```

### 2.5 The materialization contract

1. **Value.** `r.Card(i)` returns exactly the value today's `LoadRegistry` holds at `Cards[i]`: exported fields, and every derived unexported field that `equal.go` and `subset_test.go`'s `derivedOf` compare. This is the oracle (S1/S4).
2. **Identity.** One `*Card` per ordinal per registry for the life of the process. It is published with `CompareAndSwap` on `mat[i]`; a goroutine that loses the race discards its build and returns the winner, so no second pointer ever escapes. Every pointer-keyed memo depends on this:
   - `rules/compiled_text.go:27` `saFacts map[*cards.SA]`
   - `rules/engine_scratch.go:257` `faceScans`
   - `rules/engine_trigger_batches.go:180-194`
   - `Face`/`SA` `ExtSlot`s (`cards/slot.go:63`)
   - `svarFront` keyed by body-string data address (`cards/link.go:243`)
   - `landTypeWordsKey` (`rules/chars/types.go:231`)
3. **No back-pointers.** A materialized face binds to a catalog covering *its own card* (the subset route's mini catalog, `cards/subset.go:460` → `finishDecoded` → `CompileMetadata`). It never binds to a global catalog that holds `facePointers` to the whole corpus (§1.2 finding 2). `Registry.Catalog()` on an imaged registry builds the global catalog over `AllCards()` on demand (tests/tools; 0 non-test callers).
4. **Order.** `AllCards()` returns ordinal order, which equals today's `Registry.Cards` order. NameUniverse order is load-bearing: `effects/namecard.go`, `state/game.go:709` ("first match is deterministic"), and **TestHeads** through `AcceptanceConfig`.
5. **Determinism.** Materialization is a pure function of image bytes. The only order-dependent side effect is the process-local word interner (`cards/words.go`: "ordinals … never reach an event"), which is already order-free by contract. The ParamCoder codes (`cards/params.go:1644-1665`) are computed at `newParamSet` time, as today. They depend on `effects`, which is outside the cards fingerprint, so they must **never** be persisted in the image.
6. **Concurrency.** Materialization can run concurrently from sampler and bench goroutines (they share `SharedCorpus`). `finishDecoded` on a private one-card registry touches only that card plus the global interners and `svarFront`, which are already mutex- or atomic-guarded (`cards/words.go:54-58`, `cards/link.go:223-241`). The race detector must pass a `-race` test that materializes a deck's cards concurrently (S4).

### 2.6 Cache format (S5)

- **Files:** `ir-<fingerprint>.img` replaces both `ir-<fingerprint>.gob.gz` and `.seg`. The fingerprint already covers every non-test file in `cards/` (`cards/fingerprint.go:20`, `cards/fingerprint_sources.go`), and `TestCompilerSourcesListEveryNonTestFile` forces new `image*.go` files into the embed list.
- **Encoding:** a header, a section table, then raw little-endian `uint32` arrays and the blob. The decoder uses `encoding/binary`, with no `unsafe` reinterpretation: one read, one allocation per section and no reflection. **ESTIMATE:** load time falls from ~1.8 s per process (§1.1 probe wall) to well under 0.1 s, and the transient load allocation from ~500 MB (`cards/subset.go:25` measured 525 MB) to ~35 MB. Every test binary that opens the corpus benefits.
- **Relocatable:** `CardRec.Path` is stored relative to the corpus root. The registry joins it with the directory it actually opened, which replaces `rerootPaths` (`cards/open.go:236`), the c09b82ba0 cache-poisoning fix. A test pins it: build in dir A, copy to dir B, open from B, and every `Path` is under B.
- **Staleness:** `cacheFresh`/`corpusInputNewerThan` (`cards/open.go:123-169`) are unchanged. `PruneCaches` learns `ir-*.img` and drops the `.seg` rule.
- **Self-check:** `ImageHeader.CorpusHash` = sha256 over the canonical rows. Two compiles of the same corpus must produce byte-identical `.img` files (the same reproducibility property `CompileDir` documents at `cards/registry.go:414-416`).

### 2.7 Phase II: the materialized-view diet (gated, optional)

Once only materialized cards are pointerful, these become CPU and per-card items rather than GC items. Each runs only if §7's evidence calls for it.

- **II-a. One param store per node.** `ParamSet` reads values as `StringID`s from the node's image span instead of holding `src map` + `vals []string`. The `Params` map is built lazily for the remaining map readers.
  - This needs the 312 non-test `SA.Params` sites (and the `Static`/`Trigger`/`Repl` ones) moved to accessors first (§4).
  - The downstream identity rule `cards.SameParamMap`/`ParamMapIdentity` (`effects/params/doc.go:52-58`, `effects/changezone_params.go:361-369`, `effects/attach_params.go:151`, `effects/typed_params.go:33`) must be re-keyed to the node (`*SA` + span) at the same time.
- **II-b. Zero-copy strings.** Materialized string fields point into `Image.Blob` through `unsafe.String(&Blob[off], n)`. The blob is immutable, and a string header pointing inside a live `[]byte` keeps it alive. Both the GC and the race detector are fine with this (no write after publish). It removes about 20 allocations per materialized face.
- **II-c. Keyword literals become IDs.** The 29 literal `HasKeyword("…")` calls in `rules/` become `HasKeywordID(name, KHFlying)`-style precompiled IDs (`cards/face.go:85`, `cards/words.go:31`). `HasKeywordID` is 1.53% cum of random-A CPU, and this is the operator's "strings compiled to masks" rule applied to hot paths.
- **II-d. Index-linked SA for printed nodes.** `SA.Sub` stays a pointer for runtime-built nodes, but a printed node's `Sub`/`Effect`/`With` could be resolved lazily from its `NodeID`. Gated on a measured win only.

## 3. Migration plan

Every step lands green with `rules/testdata/heads/*.txt` byte-identical and adds no `events.Kind`. Each step is one seat ticket. Test commands use the operator cap; `CAP` below stands for:

```sh
systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% env GOMAXPROCS=2 GOMEMLIMIT=1536MiB go test -timeout 2m
```

### S0. Measurement harness and heap-census ratchet

- **Touches (new files only, plus one small edit):**
  - `internal/corpusheap/corpusheap_test.go` (new test-only package, so the census runs in a fresh process).
  - `cmd/enginebench/main.go:143-144`: add `scan_heap_mb`, `heap_objects` and later `materialized` next to `heap_live_mb` in `result`.
- **What:**
  - `TestCorpusHeapCensus` opens `.cards` with `cards.OpenCorpus` (not `SharedCorpus`), runs GC twice, and reads `/gc/scan/heap:bytes`, `/gc/heap/objects:objects` and `HeapAlloc` as deltas over a pre-load baseline.
  - It asserts each against a ceiling constant in the same file, with ±3% tolerance. The ratchet is **bidirectional**, like Ruling R-20's coverage ratchet: a measurement more than 10% *below* its ceiling fails as "stale ceiling, lower it". The test logs the live numbers.
  - It skips (`t.Skip`) only when `.cards` is absent, using the same discovery as `internal/testutil/decks.go:211`.
- **Done means:** the test passes on `main` with ceilings of 102.2 MB scan, 1.69 M objects and 163 MB heap. The enginebench rows carry the new fields.
- **Test:**
  - `CAP -run TestCorpusHeapCensus -v ./internal/corpusheap/` (≈2 s; well under budget).
  - `go build ./cmd/enginebench`.
- **Measure:** none needed beyond the test log.
- **Payoff:** none. This step is the ratchet every later step lowers.

### S1. `cards.Image`: schema, builder, materializer and oracle

- **Touches (new files only):**
  - `cards/image.go` (types, §2.2)
  - `cards/image_build.go` (`buildImage(cards []*Card, tokens map[string]*Card) (*Image, error)` over T0: the `cacheFile` contents before `finishDecoded`)
  - `cards/image_mat.go` (`(*Image).rawCard(i) *Card`, gob-equivalent: empty slices and maps decode as **nil** exactly as gob does, and no node sharing)
  - `cards/image_test.go`
  - plus three lines in `cards/fingerprint_sources.go`
- **Oracle:**
  1. For every card and token, `rawCard(i)` deep-equals what gob decodes from the same cache. Use the per-card segment decode (`segFile.decodeCards`, `cards/subset.go:383`) as the reference, comparing the exported tree with `cleanCopy` (`cards/subset_test.go`).
  2. After the per-card `finishDecoded`, `sameCard` (`cards/subset_test.go:64`) holds against `LoadRegistry`'s card.
  3. Canonical bytes are stable across two builds.
  4. `Path` round-trips relative.
- **Budget:** the full sweep is ~34k cards; shard it by ordinal range into subtests if one exceeds 1 minute under `CAP`.
- **Done means:** the oracle passes over the whole corpus; nothing outside `cards/*_test.go` calls the new code; heads untouched (no runtime change).
- **Test:** `CAP -run 'TestImage' ./cards/`. Also `CAP -run 'TestCompilerSourcesListEveryNonTestFile|TestSubsetRegistryMatchesFull' ./cards/`.
- **Measure:** log the image size and the build time.
- **Payoff:** none. This proves the representation.

### S2a. Registry access seams (non-test callers)

- **Touches:**
  - `cards/registry.go`: add `Len`, `Card`, `AllCards`, `MaterializedCount` (eager mode: trivial).
  - The 57 non-test `Registry.Cards` sites move to `AllCards()`/`Card(i)`/`Len()`: cards 15, compliance 17, cmd/* 16, internal/* 7, rules 2. Of the rules sites, `rules/acceptance_game.go:50` becomes `NameUniverse: reg.AllCards()` until S3, and `rules/oracle_run.go:451` likewise.
- **Done means:** `git grep -n '\.Cards\b'` finds no `Registry.Cards` field read outside `cards/` and tests; behaviour identical.
- **Test:** `go build ./... && go vet ./cards ./rules ./compliance/... ./cmd/...`, then `CAP -run 'TestHeads' ./rules/`, then `go vet -tags manabrew ./internal/manabrew/... ./host/manabrewhttp/...` (`internal/manabrew/deckimport.go:85-86` ranges `reg.Cards`; its tests are behind `-tags manabrew`).
- **Parallel with:** S1, S0.

### S2b. Unexport `Registry.Cards` (test callers)

- **What:** rename the field to `cards`; fix the 290 test sites mechanically. The compiler lists them: `go vet ./... 2>&1`, package by package.
- **Done means:** builds and vets under both default and `-tags manabrew`; per-package focused tests green.
- **Test:** for each touched package, `CAP -run '<the touched tests>' ./<pkg>/`; `go vet -tags manabrew ./internal/manabrew/... ./host/manabrewhttp/...`.
- **Note:** mechanical but wide. It can be split by top-level directory into 2–3 tickets with no ordering between them.

### S3. `cards.Universe` replaces `NameUniverse`/`NamedCorpus` slices

- **Touches:**
  - **new:** `cards/universe.go`
  - **edits:**
    - `rules/engine.go:93,100` (Config field types)
    - `state/game.go:438,444,709-726` (fields + `NamedCard` → `FirstByNormalized`)
    - `rules/genesis.go:258,303-307`
    - `rules/chars/types.go:219-266,268` (`CorpusLandTypeWords(*Universe)`; the memo becomes a field on `Universe`)
    - `effects/namecard.go`, `effects/namecard_cache.go:99`
    - `effects/clone.go:100`, `events/apply_copy.go:377` (`FirstByName`)
    - `rules/cast_etbchoice.go:203,314` (`Len()`)
    - `effects/cardflow.go:2040`
    - `host/registry.go:66` and `host/match.go:180,302`
    - setters in `cmd/gorged/main.go:722`, `cmd/botbench/spellbench.go:321`, `cmd/cardfuzz/main.go:596`, `cmd/mtgsim/main.go:137`, `internal/paymirror/driver.go:150`, `internal/spellbench/{v2engine/game.go:100,v2shadow/stage.go:143}`, `rules/acceptance_game.go:50`, `rules/oracle_run.go:451`, `host/feedback.go:324`
- **What:**
  - Every whole-universe *name* query (names list, first-by-name, normalized lookup, land type words) answers from pointer-free rows.
  - The one query that needs objects is `nameChoicesFiltered`'s matcher walk (`effects/namecard.go:155-195`). In this step it keeps materializing each card it tests (`u.Card(i)`), so behaviour is identical.
  - An eager registry's `Universe()` is `UniverseOf(r.cards)`.
- **Done means:** TestHeads unchanged (`AcceptanceConfig` passes the universe); `rules/nameuniverse_readers_test.go` green; a new test proves `Names()`/`LandTypeWords()`/`FirstBy*` equal the old slice functions over the real corpus.
- **Test:** `CAP -run 'TestHeads|TestNameUniverse|NameCard' ./rules/`, `CAP -run 'NameCard|Clone' ./effects/`, `CAP -run 'Copy' ./events/`, `CAP -run 'NamedCard' ./state/`, `CAP -run 'Feedback|Match' ./host/`, `go build ./...`.
- **Collision:** `host/` is in the decision-arena ticket's scope (`rules/decision_arena.go`, `host/`, `view/`). Land S3 **after** decision-arena, or limit the host edit to the two field lines. `rules/engine.go` and `state/game.go` are long-lived hot files, so keep each edit to the field lines.
- **Parallel with:** S1, S2a. It does not depend on the image: in this step `Universe` wraps `[]*Card`.

### S3b. NameCard filter fast path (only if needed)

- **What:** census which `ValidCards$` specs NameCard asks carry across the corpus. For type-only specs (the common shape, **INFERRED**), answer from `FaceRec.TypeMask` without materializing. Otherwise materialize *transiently*, unpublished and discarded, and keep today's per-(universe, spec) memo (`effects/namecard.go:93`).
- **Gate:** run only if S4's live-server measurement shows the first `ValidCards$` NameCard ask re-materializing the corpus. S0's `MaterializedCount` field makes that visible.

### S4. Flip: imaged, lazy registry (the GC win)

- **Depends on:** S1, S2b; S3 for the server and TestHeads path (otherwise `AcceptanceConfig` would call `AllCards()`, which is correct but materializes everything in that test).
- **Touches:**
  - `cards/registry.go` (`LoadRegistry`/`finishDecoded` split: decode T0 → `buildImage` → **drop T0 cards** → finish tokens eagerly as today)
  - `cards/open.go` (`SharedCorpus` unchanged; `openCorpus` after `CompileDir` also returns an imaged registry)
  - new `cards/registry_lazy.go` (`Card(i)`: `rawCard` + per-card `finishDecoded` + mini catalog + CAS publish; `Lookup` via `NameKeys`/`NameOrds`)
  - `cards/subset.go`: `OpenCorpusFor`/`OpenCorpusSubset` become thin wrappers over `SharedCorpus` (keep the names; delete in S8)
- **What does not change:** the gob cache format (S5 does that).
- **Done means:**
  - TestHeads (4 seat counts), `TestRepoDeckGamesReplayExactly`, `TestRepoDecksPlayAtEverySeatCount`, the `cmd/repro` committed fixture gate and `TestSubsetRegistryMatchesFull` are all green.
  - **S0's census ceilings are lowered to the new measurement:** expected scan ≤ ~5 MB, objects ≤ ~60 k, heap ~35–40 MB (**ESTIMATE**).
  - A new `-race` test materializes one deck's cards from 8 goroutines and asserts one pointer per ordinal.
  - A new ratchet test plays one acceptance game and asserts `MaterializedCount()` ≤ distinct deck cards + named-card mechanics actually fired, so a runtime path that calls `AllCards()` fails loudly.
- **Test:**
  - `CAP -run 'TestImage|TestRegistryLazy|TestSubset' ./cards/`
  - `CAP -run '^TestHeads$/^2seats$' ./rules/`, then `4seats`, `6seats` and `8seats`, each its own run
  - `CAP -run 'TestRepoDeckGamesReplayExactly' ./rules/`
  - `CAP -run 'TestReproFixtureSummaryExitsZero|TestCommittedFixtureDir|TestFeedbackSnapshotReplaysAUniverseBackedMatch' ./cmd/repro/` (the last is the universe-backed capture, `cmd/repro/feedback_nameuniverse_replay_test.go:90`)
  - `CAP -race -run 'TestRegistryLazyConcurrent' ./cards/`
  - `CAP -run TestCorpusHeapCensus ./internal/corpusheap/`
- **Measure (§6):** `make enginebench-profile REV=.`, `make enginebench-pair BASE=main CAND=.`, `make enginebench-verify`.
- **Payoff (ESTIMATE, §1.4 emulation):** random +35–45% turns/CPU-s (emulated ×1.43), bot +20–30% (emulated ×1.28), GC share 27–30% → ~5–8%; sampler in proportion to its ~17% GC share.

### S5. The image becomes the cache format

- **Touches:**
  - `cards/registry.go` (`Save`/`LoadRegistry`/`cacheFile`; `cacheVersion` → 7)
  - new `cards/image_codec.go`
  - `cards/open.go` (`cachePathFor` → `.img`; `PruneCaches`; `rerootPaths` deleted: paths are relative)
  - `cards/subset.go` (segment writer and reader deleted; `refreshSegments` gone)
  - callers of `Save`/`LoadRegistry`/`CachePath`: `cmd/forgec`, `cmd/keywordbench`, `cmd/kshadowcheck`, `cmd/sbagent`
  - `cards/cache_fresh_test.go`, `cards/open_test.go`, `cards/subset_test.go`
- **Done means:**
  - The S4 gates hold.
  - `make compile-cards` writes `ir-<fp>.img`, and a second compile is byte-identical.
  - The relocation test passes: compile in `t.TempDir()`/a, copy to /b, open, and every `Path` is under /b.
  - Cache-load wall time is logged by S0's test.
  - `AGENTS.md`'s "IR cache" wording and `docs/agents/repo-map.md` are updated in this step's own commit. `repo-map.md` is a hot file, so keep that to a one-row edit.
- **Test:**
  - `CAP -run 'TestImage|Cache|Open|Prune|Fingerprint' ./cards/`
  - the S4 rules/repro gates
  - `CAP -run 'TestGenerateCommittedFixture' ./cmd/repro/` **without** `REPRO_REGEN_FIXTURE` (it must still replay)
- **Payoff:**
  - per-process corpus load ~1.8 s → <0.1 s (**ESTIMATE**)
  - peak RSS at load −~450 MB
  - no transient GC storm at start
  - every corpus-opening test binary gets faster. Measure the suite delta with `scripts/postmerge_batch.sh`'s timing, not `go test ./...`.

### S6. Lazy tokens: measured, not planned

The token table is about 1.2 MB (§1.2) and `tokenCompiledText` already compiles every token at the first configuration (`rules/compiled_text.go:31-38`). Making tokens lazy would touch:

- `rules.Config.Tokens`, `state.Game.Tokens`
- effects/{amass,empower,endure,incubate,investigate,recruit,token,venture}.go
- `events/apply_copy.go:332` and `events/apply_misc.go:299`
- host and cmd setters
- 472 test sites

**Decision:** do not schedule it. S0's census reports the token share. Reopen only if the materialized-plus-token residual exceeds ~25% of post-S4 mark work.

One side note for any later touch: `effects/venture.go:207` ranges `g.Tokens`, a map. Today that range only filters by name. A future edit must keep the range unable to reach an event, per the AGENTS.md nondeterminism rule.

### S7. Phase II items (gated)

II-a, II-b, II-c and II-d from §2.7, one ticket each, each opened only by §7's evidence.

- **II-c (keyword literals → IDs)** is cheap and independent. It can run any time after S0, in parallel with everything; it touches `rules/` call sites, which are hot, so keep the diff to the literal sites.
- **II-a** needs §4's accessor migration first.

### S8. Delete shims

- **Remove:**
  - `OpenCorpusSubset`, `OpenCorpusFor`, `IsSubset`
  - the `enginebench_subset` tag and `cmd/enginebench/corpus_subset.go`
  - the eager `Registry.Catalog()` global build if no test still needs it
  - `SameParamMap`/`ParamMapIdentity` (only if II-a landed)
  - the gob type registrations
- **Done means:** codeshape ratchets (`internal/codeshape/ratchet_test.go`) are updated in the same commit (engineSurface and friends move); `go build ./...` and `go build -tags manabrew ./...` pass; S4's gates pass.

### 3.1 Ordering and parallelism

```
S0 ──┬─────────────────────────────────────────────┐
S1 ──┤                                             │
S2a ─┼─ S2b ──┐                                    │
S3 ──┘        ├── S4 ── S5 ── S8                   │
              │    └── (S3b if measured) (S7 gated)┘
II-c (any time after S0)
```

S0, S1, S2a, S3 and II-c are mutually independent.

### 3.2 Collision map

| step | files (new = N) | hot-file / live-ticket contact |
|---|---|---|
| S0 | `internal/corpusheap/*_test.go` N; `cmd/enginebench/main.go` (result struct only) | bench-Spare ticket edits `cmd/enginebench/play.go` + `internal/bench/bench.go`: **different files**, merges clean |
| S1 | `cards/image*.go` N; `cards/fingerprint_sources.go` (+3 lines) | none |
| S2a | `cards/registry.go`; ~57 one-line call sites across cmd/, compliance/, internal/, rules/ (2) | `compliance/oraclegen/templates` is wide but mechanical |
| S2b | ~290 test lines | none hot; split by directory |
| S3 | `cards/universe.go` N; `rules/engine.go`, `state/game.go`, `rules/genesis.go`, `rules/chars/types.go`, `effects/namecard*.go`, `effects/clone.go`, `events/apply_copy.go`, `host/registry.go`, `host/match.go`, cmd setters | **decision-arena** (`host/`, `view/`, `rules/decision_arena.go`): sequence S3 after it or keep host edits to field lines. **event-log growth** (`events/log.go`): different file. `events/apply_copy.go` is a one-line edit |
| S4 | `cards/registry.go`, `cards/open.go`, `cards/subset.go`, `cards/registry_lazy.go` N | none outside cards |
| S5 | `cards/*`, `cmd/forgec`, `cmd/keywordbench`, `cmd/kshadowcheck`, `cmd/sbagent`, `AGENTS.md`, `docs/agents/repo-map.md` | `docs/agents/repo-map.md` is the #1 hot file: a one-row edit, land alone |
| S8 | `cards/*`, `cmd/enginebench/corpus_subset.go` (delete), `internal/codeshape/ratchet_test.go` | codeshape ratchet: rebase-trivial constants |

## 4. Compatibility strategy for parameter readers

The design keeps every reader working unchanged through S4/S5. A materialized node is today's node, with its `Params` map and its `ParamSet`. **No reader migration is needed for the GC payoff.** The hundreds of `ParamStr(cards.PK…)` readers (1022 + 373 + 154 + 92 non-test, §1.3) already go through the accessor layer (`cards/params.go:1772-1858`), and codeshape has frozen literal-key map reads at 0.

If Phase II-a is opened:

1. **Accessor shims first** (one ticket, `cards/` only):
   - `(*SA).EachParam(func(key ParamKey, raw StringID, val string))`, `(*SA).ParamByName(string) (string, bool)` for the dynamic-key reads, and `(*SA).CloneParams() map[string]string` for copy-on-write rewrites.
   - The same set for `Static`/`Trigger`/`Repl`.
   - A view type `ParamView{node, span}` that replaces the `(ps, map)` pairs carried by `staticView` (`rules/statics.go:313` and siblings: `ParamSetParam`, `ParamSetCode`, `ParamSetMayHaveAny`, `cards/params.go:1829-1842`).
2. **Ratchet:** add a codeshape metric `IRParamsMapReads`, counting `<x>.Params` selectors on cards IR types outside `cards/`, frozen at today's count: 312 + 136 + 55 + 20 non-test. Like `stringParamReads`, it only shrinks.
3. **Mechanical migration**, package by package. Each package is one ticket that lowers the ratchet; effects (134), effects/params (20), events (16), rules (25+30+12+1), rules/pay (14+1+1) and view (6) are the runtime set. compliance/, cmd/ and internal/ tools can keep `CloneParams()`.
4. **Flip:** once the ratchet reads 0 outside `cards/`, drop `Params` from printed nodes, keep it on runtime-built ones (`SetParam` path), and re-key `SameParamMap` users to node identity.

## 5. Risks

| risk | where | mitigation |
|---|---|---|
| **Replay divergence** from a materialized card differing from today's | any S1/S4 bug | S1 oracle over *every* card and token (exported tree + `derivedOf`); TestHeads ×4, `TestRepoDeckGamesReplayExactly`, `cmd/repro` fixture, `make enginebench-verify` (memo verify mode) at S4 and S5 |
| **NameUniverse order or semantics drift** (three different "first match" rules) | `state/game.go:709` (normalized, two corpora in order), `effects/clone.go:100` and `events/apply_copy.go:377` (exact `Faces[0].Name`), `nameIndexOf`'s tiered rule (`cards/registry.go:124`) | `Universe` carries `FirstOrd` (exact) and a separate normalized-first index; S3 equality test over the real corpus; TestHeads uses the universe |
| **IR cache invalidation and poisoning** | S5 format change; every `cards/` edit already changes the fingerprint | relative `Path` + relocation test (closes the c09b82ba0 class for good); `.img` pruned like `.gob.gz`; header magic, schema, cacheVersion and fingerprint checked; a mismatch recompiles, never misreads |
| **Pointer-identity memos** | `SA.compiledCatalog` compared by address (`cards/compiled_catalog.go:171,179`; `cards/equal.go` "compares by identity"); `cards/card_probes.go:29` (`p.card == c`); `rules/face_scan_memo.go:91-96`; `rules/compiled_text.go:27`; `rules/chars/types.go:231`; `cards/link.go:243` | one-pointer-per-ordinal CAS contract (§2.5.2); mini catalog per card so the identity compare stays within one card; `-race` test; `make enginebench-verify` panics on any stale memo |
| **A runtime path silently calls `AllCards()`** and restores the 160 MB heap | S2a migrated callers; future code | `MaterializedCount` ratchet test (S4) + enginebench `materialized` field; S0 census on the bench rows |
| **First NameCard `ValidCards$` ask materializes the corpus** on the live server | `effects/namecard.go:166` | correct either way; S3b fast path if the measurement shows it; the result is memoized per (universe, spec) as today |
| **clone_gen** | `rules/clone_gen.go` is generated from Engine field tags | this plan adds no Engine field; `state.Game` field *type* changes in S3 are shared-by-copy pointers. Run `CAP -run TestCloneGenIsUpToDate ./rules/` at S3 |
| **paymirror** | `internal/paymirror/driver.go:150` passes `NameUniverse: d.Reg.Cards` | S2a/S3 move it to `reg.Universe()`; paymirror smoke (20 games, 2 workers) at S3 and S4 |
| **compliance oracle tooling** (XMage, Forge oracle; `compliance/oraclegen`, `compliance/levelb`) | the widest `Registry.Cards`/`Lookup` users (§1.3); `rules/oracle_run.go:451` `NamedCorpus` | `AllCards()` keeps their semantics at today's cost; focused `CAP -run` on the touched compliance packages; oracle reruns are post-merge (operator) |
| **manabrew tag** | `internal/manabrew/deckimport.go:85-86` ranges `reg.Cards` (untagged); `internal/manabrew` and `host/manabrewhttp` tests build only with `-tags manabrew` | every step that renames API runs `go vet -tags manabrew` on those packages (`make test-manabrew` is the operator's) |
| **ParamCoder codes persisted by mistake** | `cards/params.go:1644` coders live in effects (outside the fingerprint) | the image never stores codes (§2.5.5); a test asserts the image schema has no code column |
| **gob nil-vs-empty quirks** | `rawCard` must reproduce gob's "empty slice or map decodes as nil" | S1 oracle compares against an actual gob decode, not a hand model |
| **Concurrent first materialization** (sampler goroutines share `SharedCorpus`) | `cards/words.go` interners, `svarFront` | already guarded (§2.5.6); `-race` test |
| **Test budget** | S1 oracle over 34k cards | shard by ordinal range into subtests; each ≤1 min under `CAP` |

## 6. Measurement gate per step

All benches run under the shared heavy lock. Wait for it, never bypass it, keep runs short:

```sh
export HEAVY_LOCK=/home/sadams/projects/gorge/.ds4/heavy.lock
make enginebench-profile REV=.                   # GC share (runtime.gcDrain cum) + alloc top per row
SECS=10 REPS=3 make enginebench-pair BASE=main CAND=.   # per-CPU-second medians, ABBA
make enginebench-verify REV=.                    # memo-verify smoke; must print OK for every row
```

| step | census test (S0) | enginebench |
|---|---|---|
| S0 | establishes ceilings | `heap_live_mb`, `scan_heap_mb`, `heap_objects` present in rows |
| S1, S2a, S2b, S3 | **unchanged** (± tolerance): these are no-ops for the heap | pair: ratio within noise (0.95–1.05) |
| S4 | scan ≤ ~5 MB, objects ≤ ~60 k: **lower the ceilings** | profile: gcDrain ≤ 10% on random A/B; pair: random ≥ +25% (kill line), expected +35–45%; verify OK |
| S5 | heap ≈ S4; log load wall ≤ 0.2 s | pair: ratio ≥ S4's (no regression); profile shows no gob frames |
| S6 / S7 | ceilings lowered by the step's own measured delta | pair on the row the gate named |

## 7. Expected payoff and kill criteria

**Method:** §1.4's paired emulation. The image (~31 MB **ESTIMATE**) plus residual (~4 MB, measured 3.9 MB by the subset build) puts the heap at about the subset+b40 point, which measured **random ×1.43, bot ×1.28**. The sampler (~17% GC) was not run. A first-order estimate is speedup ≈ 1/(1 − GC_share × 0.8): about +15% (**ESTIMATE**). S5 adds about 1.7 s less corpus load per process (**ESTIMATE**). That matters for tests and short benches, not for steady-state rows.

| step | expected |
|---|---|
| S0–S3 | 0 (seams) |
| S4 | random +35–45%, bot +20–30% (emulation measured +43% / +28%), sampler +10–20%; live heap 163 → ~35–40 MB; heap objects 1.69 M → ~50 k |
| S5 | load ~1.8 s → <0.1 s; transient load alloc ~500 MB → ~35 MB |
| S3b / S6 / S7 | ≤ a few % each; only on evidence |
| optional GOGC knob on bench and search binaries | b160 vs b40: +0% random, about +8% bot, at the cost of more heap headroom; out of scope, noted |

**Kill criteria:**

1. **Before S1:** this criterion was evaluated during planning and **passed** (random ×1.43, bot ×1.28; the line was +15% on both). Re-run `ballast.sh` (§9.2) if the engine's allocation profile changes materially before S4.
2. **At S4:**
   - If `make enginebench-pair BASE=main CAND=S4` shows random below **+15%** and GC share above **15%**, stop. Do not start S5–S8; profile what still marks with `go tool pprof -top -cum` on `runtime.scanobject` callers and re-plan.
   - If random gains but bot or sampler do not (each below +5%), proceed with S5 for its load-time value but open nothing in S7.
3. **Phase II:** open an item only if a post-S5 CPU profile shows its target (map reads, `HasKeywordID`, `newParamSet` at materialization) at ≥ **2%** cum on some row, or the census shows the materialized-plus-runtime pointer set at ≥ **25%** of mark work.

## 8. Out of scope

- The `GOGC` / `GOMEMLIMIT` policy for batch binaries (followup.md item 1's quick lever). It composes with this design and is a one-line operator decision.
- Live-play decision allocation (followup.md item 2, the decision-arena ticket), event-log growth (`events/log.go`) and bench Spare recycling (`cmd/enginebench/play.go`, `internal/bench/bench.go`). These are separate tickets with no file overlap, except as noted in §3.2.

## 9. Reproducing the census

The throwaway sources are archived, uncommitted, in `/mnt/sata/gorge-training/enginebench/corpus-plan/probe-src/`: `probe_test.go`, `zz_ballast_probe.go`, `fieldcensus_main.go`, and the census output `fieldcensus.txt`. To rerun one, copy it into a worktree at the path named below.

### 9.1 Heap probe

A throwaway test package, `internal/zzcorpusprobe/probe_test.go`. It was never committed and was removed after use. The S0 ticket rebuilds it as `internal/corpusheap`.

- It imports `_ ".../rules"`, so the `effects` ParamCoders register exactly as in a real process.
- It records `report()`: two GCs, then `MemStats.HeapAlloc`, `HeapObjects`, `/gc/scan/heap:bytes`, `/gc/heap/live:bytes`, and the mean wall time of 10 forced GCs.
- The steps are: report → `cards.OpenCorpus(dir)` → report → optional heap profile → optional reflective type walk → drop the registry → report.

```sh
systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% \
  env GOMAXPROCS=2 GOMEMLIMIT=1536MiB GODEBUG=memprofilerate=4096 \
  PROBE_CARDS=$PWD/.cards PROBE_HEAP=/tmp/load.heap [PROBE_WALK=1] [PROBE_TOKENS=1] \
  go test -timeout 2m -count=1 -run TestCorpusHeapCensus -v ./internal/zzcorpusprobe/
go test -c -o /tmp/probe.test ./internal/zzcorpusprobe/
go tool pprof -sample_index=inuse_space -top -nodecount=45 /tmp/probe.test /tmp/load.heap
```

The type walk is `reflect` over the `*Registry`. It dedups `Pointer()` for pointers, maps and slice backings, and `unsafe.StringData` for strings. It sums `Type.Size()` per pointee type and `cap × elem` per backing. A type counts as pointer-bearing when its kind or any field kind is a pointer, map, slice, string, interface or unsafe pointer. With `PROBE_TOKENS` set it walks `reg.Tokens` only and does not follow `*CompiledCatalog` (§1.2 finding 2).

### 9.2 Ballast experiment

- **Build tags:** a throwaway file, `cmd/enginebench/zz_ballast_probe.go` (`//go:build enginebench_ballast`; `init()` allocates `BALLAST_MB` MiB as a `[]byte` held in a package variable and touches each page). The variants:

  ```sh
  go build -tags 'enginebench_subset enginebench_ballast' ./cmd/enginebench
  go build -tags enginebench_subset ./cmd/enginebench
  go build ./cmd/enginebench
  ```

- **Run script:** `/mnt/sata/gorge-training/enginebench/corpus-plan/ballast.sh`. Each run is a `flock -o` on the shared heavy lock plus a systemd scope, `-secs 8`, ABBA order over the full, subset, subset+b40 and subset+b160 variants, on rows `random A` and `bot A`, with CPU profiles on rep 1.
- **Outputs and binaries:** all in the same directory.

### 9.3 Field-usage census

A throwaway module under the session scratchpad (`fieldcensus/main.go`).

1. Run `go list -json ./...`.
2. Parse each package's `GoFiles+TestGoFiles` (and `XTestGoFiles` separately) and type-check them with `importer.ForCompiler(fset, "source", nil)`.
3. For each `types.Selection` whose object's package is `github.com/adams-shaun/gorge/cards` and whose receiver (pointer-stripped) is `Registry|Card|Face|SA|Trigger|Static|Repl`, count by member, by package, and by test vs non-test position.

The run takes about 24 s at GOMAXPROCS=2.
