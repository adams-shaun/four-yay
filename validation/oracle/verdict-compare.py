#!/usr/bin/env python3
"""verdict-compare.py BASE_TREE NEW_TREE -- compare compliance/verdicts row
statuses between two checkouts. A regression is a row that is `agree` in
BASE and not `agree` in NEW; run it before trusting any replay
(validation/oracle/README.md, step 3)."""
import glob
import json
import sys


def load(tree):
    rows = {}
    for f in glob.glob(tree + "/compliance/verdicts/*.jsonl"):
        for line in open(f):
            if line.strip():
                r = json.loads(line)
                rows[r["id"]] = r
    return rows


a, b = load(sys.argv[1]), load(sys.argv[2])
reg, imp = [], []
for k in sorted(set(a) | set(b)):
    sa, sb = a.get(k, {}).get("status"), b.get(k, {}).get("status")
    if sa == sb:
        continue
    if sa == "agree":
        reg.append((k, sb, b.get(k, {}).get("detail", "")))
    elif sb == "agree":
        imp.append((k, sa))
print("regressed", len(reg))
for k, s, det in reg:
    print("  R", k, s, det[:160])
print("improved", len(imp))
for k, s in imp:
    print("  I", k, s)
sys.exit(1 if reg else 0)
