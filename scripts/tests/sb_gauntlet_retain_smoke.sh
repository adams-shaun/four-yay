#!/usr/bin/env bash
# sb_gauntlet_retain_smoke.sh — prove sb-gauntlet.sh KEEPS each candidate's
# SpellBench ledger under the gauntlet root.
#
# The defect this guards: the candidate output dir was created inside the
# script's mktemp scratch dir and removed by its EXIT trap, so a candidate's
# matches.jsonl/games.jsonl were thrown away and only the pooled rating row
# survived. cmd/traindash walks the gauntlet root for any matches.jsonl, so a
# kept candidate dir is what renders a per-deck chart. The second regression
# this guards: the inline Python head-to-head table globbed a hardcoded
# scratch path, so moving the dirs without updating the glob would leave the
# h2h column silently empty and the table still green.
#
# It stubs the four things that make a faithful run impossible in a seat --
# the go build, the flock/systemd-run resource wrapper, and the sbvenv python
# (via SB_GAUNTLET_SBPY) -- so no real games are played and the shared mount
# is never touched. The rate call is intercepted with a tiny leaderboard.json;
# the inline head-to-head Python is the REAL interpreter reading the heredoc,
# so the glob under test actually executes.
#
#   scripts/tests/sb_gauntlet_retain_smoke.sh
set -uo pipefail

ROOT=$(git rev-parse --show-toplevel)
GAUNTLET=$ROOT/scripts/sb-gauntlet.sh
SPEC=bot+passguard
REF=sb-uniform
TMP=$(mktemp -d /tmp/sb-gauntlet-smoke.XXXXXX)
fails=0

check() {
	if [ "$2" = 0 ]; then
		printf 'ok   %s\n' "$1"
	else
		printf 'FAIL %s %s\n' "$1" "${3:-}"
		fails=$((fails + 1))
	fi
}
trap 'rm -rf "$TMP"' EXIT

# --- stub PATH: go builds a fake botbench; flock/systemd-run are pass-throughs
BIN=$TMP/bin
mkdir -p "$BIN"

cat >"$BIN/go" <<EOF
#!/usr/bin/env bash
# stub go: only-'go build -o <path>' is used; write a botbench stub there.
out=""
while [ \$# -gt 0 ]; do
	case "\$1" in
	-o) out=\$2; shift 2 ;;
	*) shift ;;
	esac
done
[ -n "\$out" ] || { echo "stub go: no -o" >&2; exit 1; }
mkdir -p "\$(dirname "\$out")"
cat >"\$out" <<'BOTBENCH'
#!/usr/bin/env bash
# stub botbench: read -spellbench-out/-spellbench/-spellbench-with and write a
# tiny ledger that has the fields the rating and h2h readers consume.
out=""; with=""
while [ \$# -gt 0 ]; do
	case "\$1" in
	-spellbench-out) out=\$2; shift 2 ;;
	-spellbench-with) with=\$2; shift 2 ;;
	*) shift ;;
	esac
done
[ -n "\$out" ] || { echo "stub botbench: no -spellbench-out" >&2; exit 1; }
mkdir -p "\$out"
# A candidate run has a -with spec: the candidate beats a reference. A
# reference run has no -with: a plain ref-vs-ref game.
if [ -n "\$with" ]; then
	p0="\$with"; p1="sb-uniform"
else
	p0="sb-uniform"; p1="bot"
fi
printf '%s\n' "{\"schema\":\"spellbench-match-ledger/v1\",\"game_id\":\"m/0\",\"pair_index\":0,\"decks\":[{\"catalog_id\":\"Wildfire\"},{\"catalog_id\":\"Burn\"}],\"seats\":[{\"name\":\"\$p0\",\"seat\":\"p0\"},{\"name\":\"\$p1\",\"seat\":\"p1\"}],\"winner\":\"\$p0\",\"outcome\":\"win\"}" >"\$out/matches.jsonl"
printf '%s\n' "{\"game_id\":\"m/0\",\"p0\":\"\$p0\",\"p1\":\"\$p1\",\"result\":\"p0 wins\"}" >"\$out/games.jsonl"
printf '%s\n' '{"schema":"spellbench-run/v1"}' >"\$out/run.json"
BOTBENCH
chmod +x "\$out"
EOF
chmod +x "$BIN/go"

cat >"$BIN/flock" <<'EOF'
#!/usr/bin/env bash
# stub flock: consume -o <lockfile>, then run the rest.
[ "$1" = -o ] && shift 2
exec "$@"
EOF
chmod +x "$BIN/flock"

cat >"$BIN/systemd-run" <<'EOF'
#!/usr/bin/env bash
# stub systemd-run: drop the resource flags, hand the `env VAR=VAL cmd ...`
# tail to the real env.
while [ $# -gt 0 ] && [ "$1" != env ]; do shift; done
[ "$1" = env ] && shift
exec env "$@"
EOF
chmod +x "$BIN/systemd-run"

# --- stub sbvenv python: intercept the rate call, run the inline script for real
SBPY=$TMP/sbpy
mkdir -p "$SBPY"
cat >"$SBPY/python3" <<EOF
#!/usr/bin/env bash
for a in "\$@"; do
	if [ "\$a" = "$ROOT/scripts/spellbench-rate.py" ]; then
		out=""
		while [ \$# -gt 0 ]; do
			[ "\$1" = --out ] && out=\$2
			shift
		done
		mkdir -p "\$out"
		printf '%s\n' '{"anchor":"$REF","rows":[{"name":"$SPEC","elo_milli":1010,"ci95_elo_milli":[1000,1020],"wins":1,"losses":0},{"name":"$REF","elo_milli":990,"ci95_elo_milli":[980,1000],"wins":0,"losses":1}]}' >"\$out/leaderboard.json"
		exit 0
	fi
done
# Anything else (the inline head-to-head/append script on stdin) is the real
# interpreter, so the glob under test actually runs.
exec /usr/bin/python3 "\$@"
EOF
chmod +x "$SBPY/python3"

# --- a throwaway gauntlet root per run. HEAD is shared; the root is not.
HEAD=$(git -C "$ROOT" rev-parse HEAD)

# run_gauntlet <gdir> <catalog> [args...] -- one full gauntlet run against a
# fresh root. A nonempty catalog sets SB_GAUNTLET_CATALOG; an empty one runs
# under `env -u` so an inherited value cannot leak in and the pauper-kernel
# default is what actually executes. Returns the gauntlet's exit code.
run_gauntlet() {
	local gdir=$1 catalog=$2
	shift 2
	mkdir -p "$gdir"
	if [ -n "$catalog" ]; then
		PATH="$BIN:$PATH" SB_GAUNTLET_DIR="$gdir" SB_GAUNTLET_SBPY="$SBPY" \
			SB_GAUNTLET_CATALOG="$catalog" SB_GAUNTLET_WORKERS=2 \
			bash "$GAUNTLET" "$@" >"$gdir/run.log" 2>&1
	else
		env -u SB_GAUNTLET_CATALOG PATH="$BIN:$PATH" SB_GAUNTLET_DIR="$gdir" \
			SB_GAUNTLET_SBPY="$SBPY" SB_GAUNTLET_WORKERS=2 \
			bash "$GAUNTLET" "$@" >"$gdir/run.log" 2>&1
	fi
}

# --- run 0: an explicit decks argument, catalog unset (pauper-kernel default)
GDIR=$TMP/gauntlet
CAND_DIR=$GDIR/cand/$HEAD/$SPEC
[ ! -e "$CAND_DIR" ]
check "candidate dir is absent before the run" $? "$CAND_DIR"

run_gauntlet "$GDIR" "" "$SPEC" 1 Wildfire
rc=$?
check "gauntlet exits 0" "$rc" "$(tail -5 "$GDIR/run.log")"

# --- 1. the ledger is KEPT under the root (the defect)
[ -s "$CAND_DIR/matches.jsonl" ]
check "candidate matches.jsonl is kept under \$GDIR/cand/<head>/<spec>" $? "$(ls -la "$CAND_DIR" 2>&1)"
[ -s "$CAND_DIR/games.jsonl" ]
check "candidate games.jsonl is kept" $? "$(ls -la "$CAND_DIR" 2>&1)"

# --- 2. the kept ledger is READABLE (schema + >=1 row), not an empty corpse
python3 - "$CAND_DIR/matches.jsonl" <<'PY' >"$TMP/parse.err" 2>&1
import json
import sys

rows = [json.loads(l) for l in open(sys.argv[1]) if l.strip()]
assert rows, "no rows"
assert all(r.get("schema") == "spellbench-match-ledger/v1" for r in rows), rows
assert all(r.get("decks") and len(r.get("seats", [])) >= 2 for r in rows), rows
PY
check "kept matches.jsonl parses as a spellbench-match-ledger/v1 with a deck and two seats" \
	$? "$(cat "$TMP/parse.err")"

python3 - "$CAND_DIR/meta.json" "$HEAD" "$SPEC" <<'PY' >"$TMP/meta.err" 2>&1
import json
import sys

meta = json.load(open(sys.argv[1]))
assert set(meta) == {"ts", "git_head", "spec", "key"}, meta
assert meta["git_head"] == sys.argv[2] and meta["spec"] == sys.argv[3], meta
assert meta["key"] and meta["ts"].endswith("Z"), meta
PY
check "candidate meta.json carries UTC ts, head, spec and key" $? "$(cat "$TMP/meta.err")"

# --- 3. the h2h column still finds the candidate dir (regression guard)
grep -q "$REF 1-0" "$GDIR/run.log"
check "head-to-head column is non-empty ($REF 1-0)" $? "$(grep -A2 'spec' "$GDIR/run.log")"

# --- 4. the results row is still appended with its schema intact
ROWS=$(wc -l <"$GDIR/results.jsonl" 2>/dev/null || echo 0)
[ "$ROWS" = 1 ]
check "one results.jsonl row appended" $? "rows=$ROWS"
python3 - "$GDIR/results.jsonl" <<'PY' >"$TMP/row.err" 2>&1
import json
import sys

r = json.loads(open(sys.argv[1]).readline())
for k in ("spec", "elo", "ci_lo", "ci_hi", "wins", "losses", "pairs", "decks", "git_head", "key", "ts"):
    assert k in r, (k, r)
assert r["spec"] == "bot+passguard", r
# An explicit decks argument already names real deck ids, so it is recorded
# unchanged -- the catalog suffix is only for the ambiguous default-pool
# fallback.
assert r["decks"] == "Wildfire", ("explicit decks must be unchanged", r)
PY
check "results row carries the unchanged schema and an unchanged explicit decks label" \
	$? "$(cat "$TMP/row.err")"

# --- 5. the row's decks label names the catalog's pool for the default-pool
# fallback: bare for pauper-kernel (historical rows stay comparable), suffixed
# for any other catalog. A nonempty catalog must really be in force -- the
# second label only appears if SB_GAUNTLET_CATALOG reached the script.
assert_row_decks() { # <gdir> <expected-label>
	python3 - "$1/results.jsonl" "$2" <<'PY' >"$TMP/row-label.err" 2>&1
import json, sys

path, want = sys.argv[1], sys.argv[2]
rows = [json.loads(l) for l in open(path) if l.strip()]
assert rows, ("no results.jsonl row to read", path)
assert rows[-1]["decks"] == want, ("decks label", want, rows[-1])
PY
}

GDIR_PAU=$TMP/gauntlet-pauper
run_gauntlet "$GDIR_PAU" "" "$SPEC" 1
rc=$?
check "pauper-kernel run exits 0" "$rc" "$(tail -5 "$GDIR_PAU/run.log")"
assert_row_decks "$GDIR_PAU" "default-pool"
check "pauper-kernel default-pool label stays bare" $? "$(cat "$TMP/row-label.err")"

GDIR_REPO=$TMP/gauntlet-repo
run_gauntlet "$GDIR_REPO" repo-constructed "$SPEC" 1
rc=$?
check "repo-constructed run exits 0" "$rc" "$(tail -5 "$GDIR_REPO/run.log")"
assert_row_decks "$GDIR_REPO" "default-pool:repo-constructed"
check "repo-constructed default-pool label carries the catalog suffix" $? "$(cat "$TMP/row-label.err")"

# The two labels must differ, or the catalog never reached the row writer.
[ "$(python3 -c 'import json,sys; print(json.loads(open(sys.argv[1]).readline())["decks"])' "$GDIR_PAU/results.jsonl")" \
	!= "$(python3 -c 'import json,sys; print(json.loads(open(sys.argv[1]).readline())["decks"])' "$GDIR_REPO/results.jsonl")" ]
check "the two catalog settings really produce different decks labels" $? \
	"pauper=$(python3 -c 'import json,sys; print(json.loads(open(sys.argv[1]).readline())["decks"])' "$GDIR_PAU/results.jsonl") repo=$(python3 -c 'import json,sys; print(json.loads(open(sys.argv[1]).readline())["decks"])' "$GDIR_REPO/results.jsonl")"

printf '\n%s failure(s)\n' "$fails"
[ "$fails" = 0 ]
