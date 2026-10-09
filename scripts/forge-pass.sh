#!/usr/bin/env bash
# forge-pass.sh [SET...] -- one incremental Forge oracle pass over the named
# sets (default: every set with a printed list under compliance/printed/).
#
# The Forge oracle is the third reference beside gorge and XMage (design:
# .ds4/forge-oracle/DESIGN.md sections 7-9). This is the standing
# second-reference pass: run it AFTER scripts/compliance-pass.sh, so the XMage
# result cache it adjudicates against is current. It ends by writing the
# compact three-way ledger under compliance/adjudication/ (operator decision
# D1=C, section 9.1). Forge is GPL-3.0 and its output stays off-repo; only the
# ledger -- gorge's own pattern and field vocabulary -- is committed.
#
# Per set, under the heavy lock:
#   1. oraclediff gen          -- one level-B scenario per manifest card
#   2. oraclediff forge-export -- the sidecar request (Item + gorge decisions
#                                 + ability lines), one line per scenario,
#                                 keyed by the request sha (gate.Hash of the
#                                 line without its newline)
#   3. filter the request to the shas the Forge result cache does not hold and
#      replay only those (scripts/forge-oracle-run.sh, which chunks JVMs)
#   4. oraclediff forge-diff   -- compare gorge with Forge through the
#                                 unchanged comparator, caching Forge's rows
# then, once over every set:
#   5. oraclediff adjudicate   -- join the committed verdicts, the XMage cache
#                                 and the Forge cache into the D1=C ledger
#
# A warm cache replays nothing; a cold cache replays every request. A new
# driver starts cold: the cache directory is named by the driver source sha.
# After a driver change, or a FORGE_ORACLE_REF bump (the analogue of an XMage
# driver change), force a full replay with --force or FORGE_PASS_FORCE=1.
# See validation/oracle/README.md.
#
# The pass takes the heavy-job lock (scripts/heavy.sh heavy) unless it already
# holds it (GORGE_BROKER_CLASS is set) or FORGE_PASS_LOCK=none. It never runs
# concurrently with an XMage pass.
#
# Environment:
#   FORGE_ORACLE_REF, FORGE_ORACLE_DIR  default to the Makefile's values
#   XMAGE_REF, XMAGE_ORACLE_DIR         default to the Makefile's values
#   LEVEL                               compliance level, B (default) or A
#   FORGE_PASS_OUT                      run directory (default
#                                       $FORGE_ORACLE_DIR/runs/forge-pass)
#   FORGE_PASS_CACHE                    Forge result cache (default
#                                       $FORGE_ORACLE_DIR/cache/<oracleref12>-<driver12>)
#   FORGE_PASS_XMAGE_CACHE              XMage result cache (default
#                                       $XMAGE_ORACLE_DIR/cache/<xmref12>-<xdriver12>)
#   FORGE_PASS_FORCE=1                  replay every request (after a driver change)
#   FORGE_PASS_LEDGER                   D1=C ledger directory (default
#                                       compliance/adjudication)
#   FORGE_PASS_ORACLEDIFF               oraclediff binary (default: built here)
#   FORGE_PASS_RUNNER                   Forge runner (default
#                                       scripts/forge-oracle-run.sh)
#   FORGE_PASS_WAIT / FORGE_PASS_MEM    heavy.sh --wait / --mem (20000 s / 8G)
set -euo pipefail

repo=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo"

if [ -z "${GORGE_BROKER_CLASS:-}" ] && [ "${FORGE_PASS_LOCK:-heavy}" != none ]; then
	main=$(cd "$(git rev-parse --git-common-dir)/.." && pwd)
	exec env GORGE_ROOT="$main" "$repo/scripts/heavy.sh" heavy \
		--wait "${FORGE_PASS_WAIT:-20000}" --mem "${FORGE_PASS_MEM:-8G}" \
		--name forge-pass -- "$repo/scripts/forge-pass.sh" "$@"
fi

force=${FORGE_PASS_FORCE:-0}
case "$force" in "" | 0) force=0 ;; *) force=1 ;; esac
sets=()
for a in "$@"; do
	case "$a" in
	--force) force=1 ;;
	*) sets+=("$a") ;;
	esac
done
if [ ${#sets[@]} -gt 0 ]; then set -- "${sets[@]}"; else set --; fi

oref=${FORGE_ORACLE_REF:-$(sed -n 's/^FORGE_ORACLE_REF *?= *//p' Makefile)}
root=${FORGE_ORACLE_DIR:-$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' Makefile)}
xref=${XMAGE_REF:-$(sed -n 's/^XMAGE_REF *?= *//p' Makefile)}
xroot=${XMAGE_ORACLE_DIR:-$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' Makefile)}
[ -n "$oref" ] && [ -n "$root" ] || { echo "forge-pass: FORGE_ORACLE_REF/FORGE_ORACLE_DIR unset" >&2; exit 2; }
[ -d .cards ] || { echo "forge-pass: no .cards corpus here (make fetch-cards compile-cards)" >&2; exit 2; }

if [ $# -eq 0 ]; then
	sets=()
	for f in compliance/printed/*.json; do
		[ -e "$f" ] || continue
		sets+=("$(basename "$f" .json)")
	done
	[ ${#sets[@]} -gt 0 ] || { echo "forge-pass: no printed sets under compliance/printed/" >&2; exit 2; }
	set -- "${sets[@]}"
fi

# Sub-scripts resolve their own pins from the same Makefile; export what this
# pass resolved so the adjudicate Forge cache matches the one built here.
export FORGE_ORACLE_DIR="$root" FORGE_ORACLE_REF="$oref"

runner=${FORGE_PASS_RUNNER:-$repo/scripts/forge-oracle-run.sh}
# The driver sha names the cache: Forge's side of a request is a function of
# the request, FORGE_ORACLE_REF and the driver source. --compile checks the pin
# invariant and (re)builds the driver when its source changed, so the replay
# below never rebuilds mid-pass.
if ! compile_out=$("$runner" --compile 2>&1); then
	echo "forge-pass: driver compile failed:" >&2
	printf '%s\n' "$compile_out" >&2
	exit 1
fi
drv=$(printf '%s\n' "$compile_out" | sed -n 's/^driver_sha=\([0-9a-f]\{1,\}\).*/\1/p' | tail -1)
[ -n "$drv" ] || { echo "forge-pass: no driver_sha from $runner --compile" >&2; exit 2; }

xdriver=tools/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java
xdrv=$(sha256sum "$xdriver" | cut -c1-12)

out=${FORGE_PASS_OUT:-$root/runs/forge-pass}
cache=${FORGE_PASS_CACHE:-$root/cache/${oref:0:12}-$drv}
xcache=${FORGE_PASS_XMAGE_CACHE:-$xroot/cache/${xref:0:12}-$xdrv}
ledger=${FORGE_PASS_LEDGER:-compliance/adjudication}
mkdir -p "$out" "$cache"

od=${FORGE_PASS_ORACLEDIFF:-}
if [ -z "$od" ]; then
	mkdir -p "$root/bin"
	go build -o "$root/bin/oraclediff" ./cmd/oraclediff
	od=$root/bin/oraclediff
fi

[ -d "$xcache" ] || echo "forge-pass: warning: no XMage cache at $xcache; the F/X leg will be skipped (run scripts/compliance-pass.sh first)" >&2

echo "== forge pass: ${#} set(s), FORGE_ORACLE_REF ${oref:0:12}, driver $drv, level ${LEVEL:-B}, cache $cache"
rc=0
allscen=$out/adjudicate-scen.jsonl
allfd=$out/adjudicate-forge-diff.jsonl
: > "$allscen"
: > "$allfd"
for s in "$@"; do
	d=$out/$s
	rm -rf "$d" && mkdir -p "$d"
	if ! "$od" gen -manifest "compliance/manifests/$s.json" -level "${LEVEL:-B}" -out "$d/scen.jsonl" >"$d/gen.log" 2>&1; then
		echo "$s: gen FAILED"; tail -3 "$d/gen.log"; rc=1; continue
	fi
	if ! "$od" forge-export -scenarios "$d/scen.jsonl" -out "$d/forge-req.jsonl" >"$d/export.log" 2>&1; then
		echo "$s: forge-export FAILED"; tail -3 "$d/export.log"; rc=1; continue
	fi
	# Incremental replay: keep only the request lines whose sha the Forge
	# cache does not hold. The cache path mirrors oraclediff.Cache.path.
	total=$(/usr/bin/grep -c . "$d/forge-req.jsonl" || true)
	: > "$d/forge-missing.jsonl"
	if [ "$force" = 1 ]; then
		cp "$d/forge-req.jsonl" "$d/forge-missing.jsonl"
	else
		while IFS= read -r line || [ -n "$line" ]; do
			[ -n "$line" ] || continue
			sha=$(printf '%s' "$line" | sha256sum | cut -d' ' -f1)
			[ -f "$cache/${sha:0:2}/$sha.json" ] || printf '%s\n' "$line" >> "$d/forge-missing.jsonl"
		done < "$d/forge-req.jsonl"
	fi
	missing=$(/usr/bin/grep -c . "$d/forge-missing.jsonl" || true)
	: > "$d/forge.jsonl"
	if [ "$missing" -gt 0 ]; then
		if ! "$runner" "$d/forge-missing.jsonl" "$d/forge.jsonl"; then
			echo "$s: Forge replay FAILED"; tail -5 "$d/forge.jsonl.log" 2>/dev/null || true; rc=1; continue
		fi
	fi
	if ! "$od" forge-diff -req "$d/forge-req.jsonl" -forge "$d/forge.jsonl" \
		-cache "$cache" -out "$d/forge-diff.jsonl" >"$d/diff.log" 2>&1; then
		echo "$s: forge-diff FAILED"; tail -3 "$d/diff.log"; rc=1; continue
	fi
	cat "$d/scen.jsonl" >> "$allscen"
	cat "$d/forge-diff.jsonl" >> "$allfd"
	echo "$s: replayed $missing/$total"
done

if [ "$rc" != 0 ]; then
	echo "forge-pass: skipping adjudicate after a failed set" >&2
	exit "$rc"
fi

# One adjudicate over every set's scenarios: its ledger writer replaces whole
# shard files, so a per-set run would drop the other sets' rows.
if ! "$od" adjudicate -scenarios "$allscen" -forge-diff "$allfd" \
	-xmage-cache "$xcache" -forge-cache "$cache" -oracle-ref "$oref" -driver "$drv" -ledger "$ledger" \
	> "$out/adjudicate.log" 2>&1; then
	echo "adjudicate FAILED"; tail -5 "$out/adjudicate.log"; exit 1
fi
cat "$out/adjudicate.log"
exit 0
