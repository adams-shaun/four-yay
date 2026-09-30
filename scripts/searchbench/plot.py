"""Draw docs/016-style figures from `searchbench analyze -json` reports.

    /mnt/sata/gorge-training/searchbench/venv/bin/python scripts/searchbench/plot.py \
        --out /mnt/sata/gorge-training/searchbench/plots/frontier report.json [more.json ...]

Writes <out>-frontier-{light,dark}.png (balanced score and A_set against core-seconds
per decision, log x, one line per method, 95% CI bars) and <out>-by-type-{light,dark}.png
(A_set per decision type against simulations per decision).

A line is every run whose arm is the same once its budget token (-b<N>) is removed, so
"pimc-1-b100" ... "pimc-1-b3000" form one line and a discount variant forms another. Its
colour is the method's (upstream's assignment: PIMC-4 blue, PIMC-1 orange, IS-MCTS aqua;
clairvoyant gray and hollow, since it reads hidden cards); further variants of a method are
dashed. Baseline arms (baseline-*) and chance are horizontal reference lines. matplotlib is
not a repository dependency: install it into the searchbench venv.
"""

from __future__ import annotations

import argparse
import json
import logging
import re
from pathlib import Path

import matplotlib

matplotlib.use("Agg")
logging.getLogger("matplotlib.font_manager").setLevel(logging.ERROR)
import matplotlib.pyplot as plt  # noqa: E402
from matplotlib.lines import Line2D  # noqa: E402

TYPES = ("spell", "hold", "attack", "block")
METHODS = ("clairvoyant-mcts", "pimc-1", "pimc-4", "is-mcts")
LABEL = {"clairvoyant-mcts": "Clairvoyant MCTS", "pimc-1": "PIMC, 1 world", "pimc-4": "PIMC, 4 worlds",
         "is-mcts": "IS-MCTS"}
BASELINE = {"baseline-passive": "always passive", "baseline-active": "always active",
            "baseline-random": "uniform random"}
# Validated with the dataviz skill's validate_palette.js (light and dark pass; aqua is
# below 3:1 on the light surface, so every line is direct-labelled at its end).
THEMES = {
    "light": dict(surface="#fcfcfb", ink="#0b0b0b", ink2="#52514e", muted="#898781", grid="#e1e0d9",
                  axis="#c3c2b7", series={"pimc-4": "#2a78d6", "pimc-1": "#eb6834", "is-mcts": "#1baf7a"}),
    "dark": dict(surface="#1a1a19", ink="#ffffff", ink2="#c3c2b7", muted="#898781", grid="#2c2c2a",
                 axis="#383835", series={"pimc-4": "#3987e5", "pimc-1": "#d95926", "is-mcts": "#199e70"}),
}
DASHES = ["-", (0, (5, 2)), (0, (1, 2)), (0, (3, 1, 1, 1))]


def method_of(arm: str) -> str | None:
    return next((m for m in sorted(METHODS, key=len, reverse=True) if arm.startswith(m)), None)


def series_key(arm: str) -> str:
    return re.sub(r"-b\d+(?=-|$)", "", arm)


def load(paths: list[str]) -> tuple[list[dict], dict]:
    runs, refs = [], None
    for p in paths:
        rep = json.loads(Path(p).read_text())
        refs = refs or rep["references"]
        if rep["references"]["items"] != refs["items"]:
            raise SystemExit(f"{p}: a different item set from the first report")
        runs += rep["runs"]
    return runs, refs


def metric(run: dict, key: str):
    e = run["metrics"].get(key) or {}
    return e.get("value"), e.get("ci")


def style(ax, t):
    ax.set_facecolor(t["surface"])
    ax.grid(True, which="major", color=t["grid"], linewidth=0.8)
    ax.set_axisbelow(True)
    for side in ("top", "right"):
        ax.spines[side].set_visible(False)
    for side in ("left", "bottom"):
        ax.spines[side].set_color(t["axis"])
    ax.tick_params(which="minor", length=0, colors=t["muted"], labelcolor=t["ink2"])
    ax.tick_params(which="major", colors=t["muted"], labelcolor=t["ink2"])


def lines(runs: list[dict]) -> list[tuple[str, str, list[dict]]]:
    """(series key, method, runs sorted by budget) for every search line, in a fixed order."""
    by: dict[str, list[dict]] = {}
    for r in runs:
        m = method_of(r["arm"])
        if m is None:
            continue
        by.setdefault(series_key(r["arm"]), []).append(r)
    out = []
    for key in sorted(by, key=lambda k: (METHODS.index(method_of(k)), k)):
        out.append((key, method_of(key), sorted(by[key], key=lambda r: r["budget"])))
    return out


def colour(t, method):
    return t["series"].get(method, t["muted"])


def frontier(runs, refs, out: Path, title: str) -> list[Path]:
    search = [(k, m, [r for r in rs if r["core_seconds_per_decision"] > 0]) for k, m, rs in lines(runs)]
    search = [(k, m, rs) for k, m, rs in search if rs]
    if not search:
        return []
    xs = [r["core_seconds_per_decision"] for _, _, rs in search for r in rs]
    base = [r for r in runs if r["arm"] in BASELINE]
    panels = [("balanced", "Balanced agreement (0.50 = any constant answer)", 1.0),
              ("a_set", "Agreement with top players, A_set (macro over types)", 100.0)]
    paths = []
    for mode, t in THEMES.items():
        plt.rcParams.update({"font.family": ["Helvetica Neue", "Helvetica", "Arial", "DejaVu Sans"], "font.size": 10})
        fig, axes = plt.subplots(1, 2, figsize=(14, 6.2), dpi=150, sharex=True)
        fig.patch.set_facecolor(t["surface"])
        for ax, (key, ylabel, scale) in zip(axes, panels):
            style(ax, t)
            ax.set_xscale("log")
            ax.set_xlim(min(xs) / 3, max(xs) * 4)
            refl = [(0.5, "chance / any constant answer")] if key == "balanced" else [(refs["chance"] * 100, "chance")]
            if key == "a_set":
                refl += [(metric(r, key)[0] * scale, BASELINE[r["arm"]]) for r in base if metric(r, key)[0] is not None]
            for y, lab in refl:
                ax.axhline(y, color=t["muted"], linewidth=1, linestyle=(0, (4, 3)), zorder=1)
                ax.text(max(xs) * 3.6, y, lab, color=t["ink2"], fontsize=8, ha="right", va="bottom")
            dash_n: dict[str, int] = {}
            ends = []
            for skey, m, rs in search:
                pts = [(r["core_seconds_per_decision"], *metric(r, key), r["budget"]) for r in rs]
                pts = [p for p in pts if p[1] is not None]
                if not pts:
                    continue
                c = colour(t, m)
                fair = m != "clairvoyant-mcts"
                ls = DASHES[dash_n.get(m, 0) % len(DASHES)]
                dash_n[m] = dash_n.get(m, 0) + 1
                px = [p[0] for p in pts]
                py = [p[1] * scale for p in pts]
                lo = [(p[1] - p[2][0]) * scale if p[2] else 0 for p in pts]
                hi = [(p[2][1] - p[1]) * scale if p[2] else 0 for p in pts]
                ax.errorbar(px, py, yerr=[lo, hi], fmt="none", ecolor=c, elinewidth=1, alpha=0.45, capsize=0, zorder=2)
                ax.plot(px, py, color=c, linewidth=2, linestyle=ls, zorder=3)
                ax.plot(px, py, linestyle="none", marker="o", markersize=7,
                        markerfacecolor=c if fair else t["surface"], markeredgecolor=t["surface"] if fair else c,
                        markeredgewidth=1.5 if fair else 2, zorder=4)
                if m == "pimc-1" and dash_n[m] == 1:
                    for (x_, *_rest, b), y_ in zip(pts, py):
                        ax.annotate(f"{b // 1000}k" if b >= 1000 else str(b), (x_, y_), xytext=(0, 8),
                                    textcoords="offset points", color=t["ink2"], fontsize=7.5, ha="center")
                name = LABEL.get(m, m) + ("" if skey == m else f" ({skey[len(m):].lstrip('-')})")
                ends.append([py[-1], px[-1], name])
            # direct end labels, pushed apart so none overlap
            ends.sort()
            gap = 0.012 * scale if key == "balanced" else 1.2
            ys = [e[0] for e in ends]
            for i in range(1, len(ys)):
                ys[i] = max(ys[i], ys[i - 1] + gap)
            for e, y_ in zip(ends, ys):
                ax.annotate(e[2], (e[1], e[0]), xytext=(e[1] * 1.2, y_), textcoords="data", color=t["ink"],
                            fontsize=8.5, va="center")
            if key == "a_set":
                ax.yaxis.set_major_formatter(matplotlib.ticker.FuncFormatter(lambda v, _: f"{v:.0f}%"))
            ax.set_ylabel(ylabel, color=t["ink2"])
            ax.set_xlabel("Core-seconds per decision (log scale)", color=t["ink2"])
        handles = [Line2D([], [], color=t["muted"], marker="o", markerfacecolor=t["surface"], markeredgewidth=2, linewidth=2,
                          label="Clairvoyant MCTS (reads hidden cards)")]
        handles += [Line2D([], [], color=t["series"][m], marker="o", linewidth=2, label=LABEL[m]) for m in ("pimc-1", "pimc-4", "is-mcts")]
        leg = fig.legend(handles=handles, loc="lower center", ncol=4, frameon=False, fontsize=9.5)
        for tx in leg.get_texts():
            tx.set_color(t["ink2"])
        fig.text(0.05, 0.955, title, color=t["ink"], fontsize=13, fontweight="bold", ha="left")
        fig.text(0.05, 0.925, "Each point is one budget (simulations per decision, labelled on the PIMC-1 line); "
                 "bars are 95% game-cluster bootstrap CIs.", color=t["ink2"], fontsize=10, ha="left")
        fig.subplots_adjust(left=0.06, right=0.97, top=0.86, bottom=0.17, wspace=0.18)
        p = Path(f"{out}-frontier-{mode}.png")
        p.parent.mkdir(parents=True, exist_ok=True)
        fig.savefig(p, facecolor=t["surface"])
        plt.close(fig)
        paths.append(p)
    return paths


def by_type(runs, refs, out: Path) -> list[Path]:
    search = [(k, m, [r for r in rs if r["budget"] > 0]) for k, m, rs in lines(runs)]
    search = [(k, m, rs) for k, m, rs in search if rs]
    if not search:
        return []
    budgets = sorted({r["budget"] for _, _, rs in search for r in rs})
    paths = []
    for mode, t in THEMES.items():
        fig, axes = plt.subplots(1, 4, figsize=(15, 4.6), dpi=150, sharey=True)
        fig.patch.set_facecolor(t["surface"])
        for ax, ty in zip(axes, TYPES):
            style(ax, t)
            ax.set_xscale("log")
            ax.set_title(f"{ty} (n = {refs['per_type'].get(ty, 0)})", color=t["ink"], fontsize=11, loc="left")
            ax.axhline(refs["chance_per_type"].get(ty, 0) * 100, color=t["muted"], linewidth=1, linestyle=(0, (4, 3)))
            dash_n: dict[str, int] = {}
            for skey, m, rs in search:
                pts = [(r["budget"], *metric(r, f"a_set_{ty}")) for r in rs]
                pts = [p for p in pts if p[1] is not None]
                if not pts:
                    continue
                ls = DASHES[dash_n.get(m, 0) % len(DASHES)]
                dash_n[m] = dash_n.get(m, 0) + 1
                name = LABEL.get(m, m) + ("" if skey == m else f" ({skey[len(m):].lstrip('-')})")
                c = colour(t, m)
                fair = m != "clairvoyant-mcts"
                ax.plot([p[0] for p in pts], [p[1] * 100 for p in pts], color=c, linestyle=ls, marker="o", markersize=5,
                        markerfacecolor=c if fair else t["surface"], markeredgecolor=c, linewidth=1.6, label=name)
            ax.set_xticks(budgets)
            ax.set_xticklabels([f"{b // 1000}k" if b >= 1000 else str(b) for b in budgets])
            ax.set_xlabel("Simulations per decision", color=t["ink2"])
        axes[0].set_ylabel("A_set (%)", color=t["ink2"])
        h, lab = axes[0].get_legend_handles_labels()
        leg = fig.legend(h, lab, loc="lower center", ncol=4, frameon=False, fontsize=8.5)
        for tx in leg.get_texts():
            tx.set_color(t["ink2"])
        fig.text(0.05, 0.94, "Agreement by decision type (gray dashes: chance)", color=t["ink"], fontsize=12, fontweight="bold")
        fig.subplots_adjust(left=0.05, right=0.98, top=0.84, bottom=0.27, wspace=0.08)
        p = Path(f"{out}-by-type-{mode}.png")
        p.parent.mkdir(parents=True, exist_ok=True)
        fig.savefig(p, facecolor=t["surface"])
        plt.close(fig)
        paths.append(p)
    return paths


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("reports", nargs="+", help="searchbench analyze -json outputs over one item set")
    ap.add_argument("--out", required=True, help="output path prefix")
    ap.add_argument("--title", default="Agreement with top 17lands players against compute (gorge)")
    a = ap.parse_args(argv)
    runs, refs = load(a.reports)
    for p in frontier(runs, refs, Path(a.out), a.title) + by_type(runs, refs, Path(a.out)):
        print("plot", p)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
