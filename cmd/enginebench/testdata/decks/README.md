# enginebench workload decks

The decks behind enginebench's `-pair` rows, committed so `make enginebench-*`
runs from a fresh clone. Each file is a deck list: card names and counts, no
card scripts.

| Pair | Seat A | Seat B |
|---|---|---|
| `A` | `FDN_top_04956_UG.dck` | `FDN_top_20626_WG.dck` |
| `B` | `FDN_top_07961_WR.dck` | `FDN_top_02581_UR.dck` |
| `burn` | `burn.json` | `burn.json` (mirror) |

## Provenance and license

- **`FDN_top_*.dck`** come from the draft-zero sample pool
  (`assets/sample/decks`). That pool is derived from
  [17lands](https://www.17lands.com/) public FDN Premier Draft game data,
  keeping decks whose player sits in the 60%-or-better win-rate bucket.
  17lands publishes the data under
  [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/), and these files are
  redistributed under the same license. Credit: 17lands.
- **`burn.json`** is the SpellBench pauper-kernel catalog deck "Burn", from
  [jackmaiorino/mtg-kernel](https://github.com/jackmaiorino/mtg-kernel)
  `data/pauper_pool_v1.json` (MIT).
