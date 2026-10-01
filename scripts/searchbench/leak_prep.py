#!/usr/bin/env python3
# Copyright 2026 The gorge Authors. SPDX-License-Identifier: Apache-2.0
"""leak_prep.py -- DraftZero's E1 hidden-information probe pairs, as gorge `searchbench leak` input.

gorge replicates DraftZero experiment #3's E1 (docs/012 §2.5, docs/016 §3; danieljbrooks/draft-zero,
MIT, pinned at b1e0ba6863182a612a1a06fbac9754cbc979e018). Upstream's `tools/search_bench/leak.py`
builds six pairs of StateSpec positions (counterspell, counterspell_x, cantrip, cantrip_x, decklist,
canary) and, for the fair methods, eight belief worlds per seed drawn from each position's PUBLIC
view (`public_worlds(real, SEARCH_SEED + seed)`: B's hand as a count, B's decklist unknown, A's
library top dropped). This script imports that module from the pinned clone (never modified or
copied) and writes, per pair and member world X/Y, the real spec and the worlds of every seed. No
step here touches the XMage bridge: upstream's leak.py builds its pairs and worlds in Python, and
gorge materialises every spec itself.

    leak_prep.py --out probes.json.gz [--seeds 16]

It checks the property the fair methods' pass rests on: for every pair and seed, the X and Y
members' worlds are identical. A mismatch is a leak in the belief sampler, and is an error.
"""
from __future__ import annotations

import argparse
import gzip
import json
import os
import subprocess
import sys
from pathlib import Path

for _v in ("OMP_NUM_THREADS", "OPENBLAS_NUM_THREADS", "MKL_NUM_THREADS", "NUMEXPR_NUM_THREADS"):
    os.environ.setdefault(_v, "1")

PINNED = "b1e0ba6863182a612a1a06fbac9754cbc979e018"
DEFAULT_UPSTREAM = "/mnt/sata/gorge-training/searchbench/draft-zero"
FORMAT = "sbrep-leak/v1"


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--upstream", default=os.environ.get("SBREP_UPSTREAM", DEFAULT_UPSTREAM))
    ap.add_argument("--allow-unpinned", action="store_true")
    ap.add_argument("--out", required=True)
    ap.add_argument("--seeds", type=int, default=16)
    a = ap.parse_args(argv)
    up = Path(a.upstream).resolve()
    head = subprocess.run(["git", "-C", str(up), "rev-parse", "HEAD"], capture_output=True, text=True,
                          check=True).stdout.strip()
    if head != PINNED and not a.allow_unpinned:
        raise SystemExit(f"upstream {up} is at {head}, not the pinned {PINNED}")
    for p in (up / "tools" / "search_bench", up / "src"):
        sys.path.insert(0, str(p))
    import leak  # noqa: PLC0415  upstream tools/search_bench/leak.py

    pairs = []
    for name, pair in leak.probes().items():
        members = {}
        for w in ("X", "Y"):
            real = pair[w]
            members[w] = {"real": real,
                          "worlds": [leak.public_worlds(real, leak.SEARCH_SEED + s) for s in range(a.seeds)]}
        same = [members["X"]["worlds"][s] == members["Y"]["worlds"][s] for s in range(a.seeds)]
        if not all(same):
            raise SystemExit(f"{name}: X and Y belief worlds differ at seeds "
                             f"{[s for s, ok in enumerate(same) if not ok]}: the belief sampler reads hidden cards")
        pairs.append({"name": name, **members})
        print(f"{name}: {a.seeds} seeds x {len(members['X']['worlds'][0])} worlds, X/Y worlds identical",
              file=sys.stderr)
    doc = {"format": FORMAT, "upstream": {"repo": "danieljbrooks/draft-zero", "head": head},
           "search_seed": leak.SEARCH_SEED, "seeds": a.seeds, "pairs": pairs}
    with open(a.out, "wb") as raw, gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=0) as gz:
        gz.write(json.dumps(doc, separators=(",", ":")).encode())
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
