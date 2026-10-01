"""Score a gorge searchbench fixture with upstream's own analysis code.

    SEARCHBENCH_FIXTURE_DIR=DIR go test ./internal/searchbench -run TestDumpUpstreamFixture
    python scripts/searchbench/crosscheck_upstream.py --upstream <draft-zero>/tools/search_bench DIR

Converts the gorge manifest (schema 2) and two result files (a.jsonl, b.jsonl)
into upstream's item and decision-row shapes, then calls draft-zero's
analyze.py (score_row, macro, bootstrap, paired, chance, balanced_score) and
diagnose.py (act_split) unchanged. The printed numbers are the goldens pinned
by internal/searchbench TestMatchesUpstreamAnalyzePy.
"""

import argparse
import json
import sys
from pathlib import Path

PASSIVE = {"spell": "Pass", "hold": "Pass", "attack": "no", "block": "Stop Choosing"}


def lab(t: str, o: int) -> str:
    if o == 0:
        return PASSIVE[t]
    if t == "attack" and o == 1:
        return "yes"
    return f"o{o}"


def multiset_balanced_ci(items: dict, R: dict, n: int = 1000, seed: int = 0) -> list:
    """balanced_score's resamples (same games list, same random.Random(seed)),
    but scored over the drawn multiset. balanced_score builds a dict of the
    drawn ids, so a game drawn twice counts once; this keeps the multiplicity,
    as bootstrap() does for A_set. The arithmetic is act_split's, unrounded."""
    import random
    from collections import defaultdict

    def acted(t, label):
        if t in ("spell", "hold"):
            return label != "Pass"
        if t == "attack":
            return label == "yes"
        return label != "Stop Choosing"

    def bal(ids):
        parts = []
        for types in (("spell", "hold"), ("attack",), ("block",)):
            tp = fn = fp = tn = 0
            for i in ids:
                it = items[i]
                if it["type"] not in types:
                    continue
                h = True if it["type"] == "spell" else acted(it["type"], it["label"][0])
                s = acted(it["type"], R[i]["best"])
                tp += h and s
                fn += h and not s
                fp += (not h) and s
                tn += (not h) and not s
            if tp + fn and tn + fp:
                parts.append((tp / (tp + fn) + tn / (tn + fp)) / 2)
        return sum(parts) / len(parts) if parts else None

    by = defaultdict(list)
    for i in R:
        by[items[i]["row"]].append(i)
    games = list(by)
    rng = random.Random(seed)
    vals = []
    for _ in range(n):
        v = bal([i for g in (rng.choice(games) for _ in games) for i in by[g]])
        if v is not None:
            vals.append(v)
    vals.sort()
    return [vals[int(0.025 * len(vals))], vals[int(0.975 * len(vals)) - 1]]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--upstream", required=True, help="draft-zero tools/search_bench")
    ap.add_argument("dir")
    a = ap.parse_args()
    sys.path.insert(0, a.upstream)
    import analyze as up  # noqa: E402

    d = Path(a.dir)
    m = json.loads((d / "manifest.json").read_text())
    items = {}
    for it in m["Items"]:
        if it["Split"] != "test":
            continue
        t = it["Type"]
        items[it["ID"]] = {"id": it["ID"], "type": t, "row": it["Row"], "split": "test",
                           "legal": [lab(t, o) for o in range(len(it["Options"]))],
                           "label": [lab(t, x[0]) for x in it["Label"]["Alternatives"]],
                           "label_strict": [lab(t, x[0]) for x in it["Label"]["Strict"]]}
    rows = []
    for arm in ("a", "b"):
        for line in open(d / f"{arm}.jsonl"):
            r = json.loads(line)
            t = items[r["ItemID"]]["type"]
            rows.append({"run_id": arm, "item_id": r["ItemID"], "method": "x", "best": lab(t, r["AgentChoices"][0]),
                         "children": [{"label": lab(t, c["Choice"]), "N": c["Visits"], "Q": c["Q"]} for c in r["Root"] or []]})
    A = [r for r in rows if r["run_id"] == "a"]
    recs = [{**up.score_row(r, items[r["item_id"]]), "type": items[r["item_id"]]["type"],
             "row": items[r["item_id"]]["row"]} for r in A]
    out = {}
    for key in ("set", "strict", "soft"):
        v = up.macro([(x["type"], x[key]) for x in recs])
        lo, hi = up.bootstrap(recs, key, 1000, 0)
        out[key] = [v, lo, hi]
    bal, ci, _ = up.balanced_score(items, {r["item_id"]: r for r in A}, n_boot=1000, seed=0)
    out["balanced"] = [bal, ci[0], ci[1]]
    p = up.paired(rows, items, "a", "b", "set", 1000)
    # paired() rounds; recompute its unrounded point and CI the same way.
    B = {r["item_id"]: r for r in rows if r["run_id"] == "b"}
    precs = [{"type": items[r["item_id"]]["type"], "row": items[r["item_id"]]["row"],
              "d": up.score_row(r, items[r["item_id"]])["set"] - up.score_row(B[r["item_id"]], items[r["item_id"]])["set"]} for r in A]
    lo, hi = up.bootstrap(precs, "d", 1000, 0)
    out["paired_set"] = [up.macro([(x["type"], x["d"]) for x in precs]), lo, hi]
    out["paired_rounded"] = p
    out["chance"] = up.macro([(it["type"], up.chance(it)) for it in items.values()])
    out["balanced_multiset_ci"] = multiset_balanced_ci(items, {r["item_id"]: r for r in A})
    print(json.dumps(out, indent=1))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
