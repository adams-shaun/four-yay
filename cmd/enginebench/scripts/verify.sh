#!/usr/bin/env bash
# verify.sh [REV]: memo-verification smoke over the enginebench rows.
#
# Builds REV (default ".") with rules.derivedMemoVerifyFlag=1, which makes the
# engine recompute every Derived/face-scan/walk-cache memo hit and panic on a
# stale one, then plays each row in ROWS and fails on any error, stall or
# recorded panic. Run it after a change that adds or widens a cache.
# Verify mode is slow (the sampler manages ~30 games in 90s).
#
#   ROWS  ';'-separated rows (default: sampler 90s, random A/B and bot A 20s)
set -euo pipefail
. "$(dirname "$0")/lib.sh"
rev=${1:-.}
bin=$("$(dirname "$0")/build.sh" "$rev" -ldflags "-X github.com/adams-shaun/gorge/rules.derivedMemoVerifyFlag=1")
out=$BENCH_DIR/results/verify-$(date +%Y%m%dT%H%M%S).jsonl
mkdir -p "$BENCH_DIR/results"
echo "verify binary $bin -> $out"
IFS=';' read -r -a rows <<<"${ROWS:--row sampler -secs 90;-row random -pair A -secs 20;-row random -pair B -secs 20;-row bot -pair A -secs 20}"
for row in "${rows[@]}"; do
	read -r -a args <<<"$row"
	# shellcheck disable=SC2046
	heavy "$bin" "${args[@]}" $(workload_args) -label verify -out "$out" >/dev/null 2>>"$out.stderr" || true
done
python3 - "$out" "$out.stderr" <<'EOF'
import json, sys
bad = 0
for line in open(sys.argv[1]):
    r = json.loads(line); d = r.get("detail") or {}
    games = d.get("Games") or d.get("GamesStarted")
    problems = [p for p in (r.get("err"), d.get("Stalls") and f"stalls={d['Stalls']}", d.get("Panics") and f"panics={d['Panics']}") if p]
    print(f"{r['row']:<12} {r.get('pair',''):<5} games={games} " + ("OK" if not problems else "FAIL " + "; ".join(map(str, problems))))
    bad += bool(problems)
for line in open(sys.argv[2]):
    if "panic" in line or "stale" in line:
        print("!!", line.strip()[:400]); bad += 1
sys.exit(1 if bad else 0)
EOF
