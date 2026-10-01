"""Regenerate the tables of docs/superpowers/reports/2026-10-01-searchbench-replication.md.

    python3 scripts/searchbench/report_tables.py \
        --bin /mnt/sata/gorge-training/searchbench/grid-bin \
        --data /mnt/sata/gorge-training/searchbench/sb-v1/gorge2 \
        --out /mnt/sata/gorge-training/searchbench/sb-v1/gorge2/report

Runs `searchbench analyze` over the E2 grid (discount 0.99 per ply), the E2b discount
arms and the undiscounted pilot, and `searchbench compare` over the report's paired
contrasts. It writes each command's markdown and JSON under --out and prints one
summary table of the contrasts (A_set, balanced, same choice; points, 95% game-cluster
bootstrap CI). Light: the analysis reads result files only, no engine, no build.

Runs that do not exist yet (the 10,000-simulation arms while the grid is running) are
skipped, so rerunning after they land fills their rows and contrasts.
"""

from __future__ import annotations

import argparse
import json
import subprocess
from pathlib import Path

METHODS = ["clairvoyant-mcts", "pimc-1", "pimc-4", "is-mcts"]
BUDGETS = [100, 300, 1000, 3000, 10000]
E2B = ["d1.0-ply", "d0.99", "d0.95-ply", "d0.9-ply", "action", "turn"]

# (a, b) run stems under grid/ ("runs/..." for the no-search and pilot files).
CONTRASTS = [
    # search against no search
    ("grid/pimc-1-b3000-d0.99", "runs/no-search"),
    ("grid/pimc-4-b100-d0.99", "runs/no-search"),
    ("grid/is-mcts-b3000-d0.99", "runs/no-search"),
    # budget
    ("grid/pimc-1-b1000-d0.99", "grid/pimc-1-b100-d0.99"),
    ("grid/pimc-1-b3000-d0.99", "grid/pimc-1-b100-d0.99"),
    ("grid/pimc-1-b3000-d0.99", "grid/pimc-1-b1000-d0.99"),
    ("grid/pimc-1-b10000-d0.99", "grid/pimc-1-b3000-d0.99"),
    ("grid/clairvoyant-mcts-b3000-d0.99", "grid/clairvoyant-mcts-b100-d0.99"),
    ("grid/is-mcts-b3000-d0.99", "grid/is-mcts-b1000-d0.99"),
    ("grid/is-mcts-b10000-d0.99", "grid/is-mcts-b3000-d0.99"),
    # method at equal budget
    ("grid/pimc-1-b3000-d0.99", "grid/clairvoyant-mcts-b3000-d0.99"),
    ("grid/is-mcts-b3000-d0.99", "grid/clairvoyant-mcts-b3000-d0.99"),
    ("grid/pimc-1-b1000-d0.99", "grid/clairvoyant-mcts-b1000-d0.99"),
    ("grid/pimc-4-b1000-d0.99", "grid/pimc-1-b1000-d0.99"),
    ("grid/pimc-4-b3000-d0.99", "grid/pimc-1-b3000-d0.99"),
    ("grid/is-mcts-b1000-d0.99", "grid/pimc-1-b1000-d0.99"),
    ("grid/is-mcts-b3000-d0.99", "grid/pimc-1-b3000-d0.99"),
    ("grid/is-mcts-b10000-d0.99", "grid/pimc-1-b10000-d0.99"),
] + [
    # E2b: each discount arm against the 0.99-per-ply default, 1,000 simulations
    (f"grid/{m}-b1000-{arm}", f"grid/{m}-b1000-d0.99")
    for m in ("clairvoyant-mcts", "pimc-4")
    for arm in E2B
    if arm != "d0.99"
] + [
    # rerun checks: the first wave's undiscounted runs against E2b's 1.0 arm (expect 100%
    # same choice), and the pilot's undiscounted PIMC-1 (earlier binary) against the grid
    ("grid/undiscounted/clairvoyant-mcts-b1000", "grid/clairvoyant-mcts-b1000-d1.0-ply"),
    ("grid/undiscounted/pimc-4-b1000", "grid/pimc-4-b1000-d1.0-ply"),
    ("runs/pimc-1-b1000", "grid/pimc-1-b1000-d0.99"),
]


def run(cmd: list[str], out_md: Path) -> None:
    res = subprocess.run(cmd, capture_output=True, text=True, check=True)
    out_md.write_text(res.stdout)


def existing(data: Path, stems: list[str]) -> list[str]:
    return [str(data / f"{s}.jsonl") for s in stems if (data / f"{s}.jsonl").exists()]


def fmt(d: dict, key: str) -> str:
    v = d["diff"].get(key)
    if v is None:
        return "—"
    lo, hi = v["ci"]
    star = "**" if (lo > 0 or hi < 0) and not key.startswith("same_choice") else ""
    if key.startswith("same_choice"):
        return f"{100 * v['value']:.1f}%"
    return f"{star}{100 * v['value']:+.1f} ({100 * lo:+.1f}, {100 * hi:+.1f}){star}"


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--bin", default="/mnt/sata/gorge-training/searchbench/grid-bin")
    ap.add_argument("--data", default="/mnt/sata/gorge-training/searchbench/sb-v1/gorge2")
    ap.add_argument("--out", required=True)
    args = ap.parse_args()
    data, out = Path(args.data), Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    manifest = str(data / "manifest.json")

    refs = ["runs/baselines/baseline-passive", "runs/baselines/baseline-active",
            "runs/baselines/baseline-random", "runs/no-search"]
    e2 = [f"grid/{m}-b{b}-d0.99" for m in METHODS for b in BUDGETS]
    e2b = [f"grid/{m}-b1000-{arm}" for m in ("clairvoyant-mcts", "pimc-4") for arm in E2B]
    pilot = [f"runs/{m}-b{b}" for m in METHODS for b in (100, 300, 1000)]

    for name, stems in (("e2", refs + e2), ("e2b", e2b), ("pilot-undiscounted", refs + pilot)):
        files = existing(data, stems)
        run([args.bin, "analyze", "-manifest", manifest, "-json", str(out / f"{name}.json"),
             "-results", *files], out / f"{name}.md")

    rows = []
    for a, b in CONTRASTS:
        fa, fb = data / f"{a}.jsonl", data / f"{b}.jsonl"
        if not (fa.exists() and fb.exists()):
            rows.append(f"| {Path(a).name} − {Path(b).name} | TODO: run missing | | |")
            continue
        tag = f"{Path(a).name}--vs--{Path(b).name}"
        js = out / f"compare-{tag}.json"
        run([args.bin, "compare", "-manifest", manifest, "-a", str(fa), "-b", str(fb),
             "-json", str(js)], out / f"compare-{tag}.md")
        d = json.loads(js.read_text())
        rows.append(f"| {d['a']} − {d['b']} | {fmt(d, 'a_set')} | {fmt(d, 'balanced')} "
                    f"| {fmt(d, 'same_choice')} |")
    table = ["| contrast (a − b) | A_set, points | balanced, ×100 | same choice |",
             "|---|---|---|---|", *rows]
    (out / "contrasts.md").write_text("\n".join(table) + "\n")
    print("\n".join(table))


if __name__ == "__main__":
    main()
