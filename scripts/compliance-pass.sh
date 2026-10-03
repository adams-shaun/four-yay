#!/usr/bin/env bash
# compliance-pass.sh [SET...] -- one XMage compliance pass over the named sets
# (default: every set with a printed list under compliance/printed/), writing
# verdict rows into compliance/verdicts.
#
# docs/superpowers/specs/2026-10-03-rules-engine-lasagna-design.md section
# 11.3 C1; the oracle itself is
# docs/superpowers/specs/2026-10-02-xmage-compliance-oracle-design.md.
#
# Per set:
#   1. oraclediff gen   -- one level-A scenario per manifest card gorge supports
#   2. oraclediff plan  -- keep only the STALE scenarios: no verdict, another
#                          scenario or XMAGE_REF, not passing, or gorge no
#                          longer meets the frozen expectation
#   3. XMage replays the stale scenarios its result cache does not hold
#                          (scripts/xmage-oracle-run.sh)
#   4. oraclediff diff -write -- recompute the stale verdicts, merging them
#                          into compliance/verdicts; the cache keeps XMage's
#                          results, so a later pass replays nothing it has
#                          already seen
# then oraclediff triage -apply (shape rulings, section 11.3 C2: rows a
# compliance/rulings/<id>.json ruling matches are classified, the rest are
# clustered by shape under compliance/triage/), a summary (scripts/compliance-summary.py) and `oraclediff status` per set.
#
# The pass runs under the heavy-job lock (scripts/heavy.sh heavy) unless it
# already holds it (GORGE_BROKER_CLASS is set) or COMPLIANCE_PASS_LOCK=none.
#
# Environment:
#   XMAGE_REF, XMAGE_ORACLE_DIR  default to the Makefile's values
#   COMPLIANCE_PASS_OUT          run directory (default $XMAGE_ORACLE_DIR/pass)
#   COMPLIANCE_PASS_CACHE        XMage result cache (default
#                                $XMAGE_ORACLE_DIR/cache/<ref12>-<driver12>)
#   COMPLIANCE_PASS_NO_WRITE=1   compute verdicts without merging them
#   COMPLIANCE_PASS_WAIT/_MEM    heavy.sh --wait / --mem (20000 s / 16G)
set -euo pipefail

repo=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo"

if [ -z "${GORGE_BROKER_CLASS:-}" ] && [ "${COMPLIANCE_PASS_LOCK:-heavy}" != none ]; then
	main=$(cd "$(git rev-parse --git-common-dir)/.." && pwd)
	exec env GORGE_ROOT="$main" "$repo/scripts/heavy.sh" heavy \
		--wait "${COMPLIANCE_PASS_WAIT:-20000}" --mem "${COMPLIANCE_PASS_MEM:-16G}" \
		--name compliance-pass -- "$repo/scripts/compliance-pass.sh" "$@"
fi

ref=${XMAGE_REF:-$(sed -n 's/^XMAGE_REF *?= *//p' Makefile)}
root=${XMAGE_ORACLE_DIR:-$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' Makefile)}
[ -n "$ref" ] && [ -n "$root" ] || { echo "compliance-pass: XMAGE_REF/XMAGE_ORACLE_DIR unset" >&2; exit 2; }
[ -d .cards ] || { echo "compliance-pass: no .cards corpus here (make fetch-cards compile-cards)" >&2; exit 2; }

if [ $# -eq 0 ]; then
	set -- $(for f in compliance/printed/*.json; do basename "$f" .json; done)
fi

driver=tools/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java
drv=$(sha256sum "$driver" | cut -c1-12)
out=${COMPLIANCE_PASS_OUT:-$root/pass}
cache=${COMPLIANCE_PASS_CACHE:-$root/cache/${ref:0:12}-$drv}
mkdir -p "$out" "$cache"
[ -d /mnt/sata/gorge-training/gotmp ] && export GOTMPDIR=/mnt/sata/gorge-training/gotmp
export GOMEMLIMIT=${GOMEMLIMIT:-8GiB}

go build -o "$out/oraclediff" ./cmd/oraclediff
od=$out/oraclediff
write=(-write compliance/verdicts -xmage-ref "$ref")
[ "${COMPLIANCE_PASS_NO_WRITE:-}" = 1 ] && write=()

echo "== compliance pass: ${#} set(s), XMAGE_REF ${ref:0:12}, driver $drv, cache $cache"
rc=0
for s in "$@"; do
	d=$out/$s
	rm -rf "$d" && mkdir -p "$d"
	if ! "$od" gen -manifest "compliance/manifests/$s.json" -out "$d/scen.jsonl" >"$d/gen.log" 2>&1; then
		echo "$s: gen FAILED"; tail -3 "$d/gen.log"; rc=1; continue
	fi
	if ! "$od" plan -scenarios "$d/scen.jsonl" -xmage-ref "$ref" -cache "$cache" \
		-replay "$d/replay.jsonl" -rediff "$d/rediff.jsonl" >"$d/plan.log" 2>&1; then
		echo "$s: plan FAILED"; tail -3 "$d/plan.log"; rc=1; continue
	fi
	xm=()
	if [ -s "$d/replay.jsonl" ]; then
		if ! scripts/xmage-oracle-run.sh "$d/replay.jsonl" "$d/xmage.jsonl"; then
			echo "$s: XMage FAILED"; tail -5 "$d/xmage.jsonl.log"; rc=1; continue
		fi
		xm=(-xmage "$d/xmage.jsonl")
	fi
	if [ -s "$d/rediff.jsonl" ]; then
		if ! "$od" diff -scenarios "$d/rediff.jsonl" "${xm[@]}" -cache "$cache" \
			-out "$d/verdicts.jsonl" "${write[@]}" >"$d/diff.log" 2>&1; then
			echo "$s: diff FAILED"; tail -3 "$d/diff.log"; rc=1; continue
		fi
	else
		: >"$d/diff.log"
	fi
	echo "$s: $(head -1 "$d/plan.log")"
done

if [ "${COMPLIANCE_PASS_NO_WRITE:-}" != 1 ]; then
	# Re-apply the shape rulings to every row and rewrite the clusters
	# (compliance/triage/): what remains is one triage item per shape.
	"$od" triage -apply >"$out/triage.txt" 2>&1 || rc=1
	sed -n '1,/^open:/p' "$out/triage.txt"
fi
python3 scripts/compliance-summary.py "$out" "$@"
for s in "$@"; do
	"$od" status -set "$s" >"$out/$s/status.txt" 2>&1 || rc=1
	tail -1 "$out/$s/status.txt"
done
exit $rc
