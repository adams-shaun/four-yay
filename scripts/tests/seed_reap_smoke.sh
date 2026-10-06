#!/usr/bin/env bash
# seed_reap_smoke.sh — prove the seed cycle's standing-gorged reaping trigger.
#
# The seed cycle is the tick that both MEASURES standing_gorged_excess (the
# free probe appends it) and ACTS: once the metric has been nonzero on two
# consecutive probes it runs `APPLY=1 scripts/cleanup.sh gorged` (the reaper
# from cli-20261001T052533Z-b6454a47), records an UNREGISTERED
# `gorged_reaped` stability row (visible on the scoreboard, scored nowhere),
# and journals the action through saw/did. This test drives that wiring end
# to end on real processes — the fixture pattern gorged_reap_smoke.sh
# established: a fake /proc tree scoping the reaper's LISTING, a REAL
# detached orphan (double-forked sleeping python carrying gorged-shaped
# flags) driving a REAL kill — with seed_smoke.sh's harness (throwaway
# target repo, stubbed agentctl, throwaway reward dir):
#
#   1. two nonzero probes + a real standing orphan -> the orphan is stopped,
#      exactly one gorged_reaped row is appended (value, shape), the veto
#      logic still runs this cycle;
#   2. one nonzero probe (one row zeroed) -> the unconditional early reap
#      still removes a real orphan, independent of probe history; it does not
#      append the late trigger's gorged_reaped action row;
#   3. two nonzero rows but an empty process tree (the instance already
#      exited between probes) -> no action row, one saw line, exit 0;
#   4. the demo-port neighbour is never signalled (still alive after case 1);
#   5. gorged_reaped is scored nowhere: reward.py's stability.detail holds
#      only standing_gorged_excess, so the cure does not veto like the
#      disease;
#   6. mutation probe: disable the unconditional early reap and remove the
#      two-probe trigger; the orphan survives and no action row is written.
#
#   scripts/tests/seed_reap_smoke.sh
set -uo pipefail

ROOT=$(git rev-parse --show-toplevel)
TMP=$(mktemp -d /tmp/seed-reap-smoke.XXXXXX)
FAKEPROC=$TMP/proc
EMPTYPROC=$TMP/proc-empty
mkdir -p "$FAKEPROC" "$EMPTYPROC"
fails=0

check() {
	if [ "$2" = 0 ]; then
		printf 'ok   %s\n' "$1"
	else
		printf 'FAIL %s %s\n' "$1" "${3:-}"
		fails=$((fails + 1))
	fi
}

cleanup() {
	[ -n "${ORPHAN:-}" ] && kill -9 "$ORPHAN" 2>/dev/null
	[ -n "${ORPHAN2:-}" ] && kill -9 "$ORPHAN2" 2>/dev/null
	[ -n "${ORPHAN3:-}" ] && kill -9 "$ORPHAN3" 2>/dev/null
	[ -n "${ORPHAN4:-}" ] && kill -9 "$ORPHAN4" 2>/dev/null
	[ -n "${DEMO:-}" ] && kill -9 "$DEMO" 2>/dev/null
	rm -rf "$TMP"
}
trap cleanup EXIT

# --- a throwaway target repo + stubbed agentctl (seed_smoke.sh's harness)
TARGET=$TMP/target
mkdir -p "$TARGET/.ds4/orchestrator" "$TARGET/rules" "$TARGET/internal/testutil/decks"
git -C "$TARGET" init -q 2>/dev/null
printf 'x\n' >"$TARGET/README.md"
git -C "$TARGET" add README.md >/dev/null 2>&1
git -C "$TARGET" -c user.email=t@t -c user.name=t commit -qm init >/dev/null 2>&1
printf '{"name":"D","cards":["Lightning Bolt","Mountain"]}\n' >"$TARGET/internal/testutil/decks/d.json"
: >"$TARGET/rules/acceptance_test.go"
: >"$TARGET/rules/paramcensus_test.go"
# One healthy transition: no provider storm, no candidate to file.
printf '{"ts":"%s","kind":"transition","evidence":{"to":"merged"}}\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
	>"$TARGET/.ds4/orchestrator/journal.jsonl"

STUB=$TMP/agentctl
mkdir -p "$STUB/agentctl"
# A REGULAR package so the stub shadows the installed agentctl pin (see the
# note in seed_smoke.sh: without __init__.py both are namespace portions and
# `-m agentctl` resolves the pin's __main__).
: >"$STUB/agentctl/__init__.py"
cat >"$STUB/agentctl/__main__.py" <<'PY'
import sys, pathlib
args = sys.argv[1:]
rec = pathlib.Path(__file__).parent.parent / "filed.txt"
if args and args[0] == "status":
    print("repo: t  config: .agentctl/config.toml")
    print("daemon: running  paused: no  fleet stop: no  paid seats: on")
    print("queue: new=0 briefed=0 merged=10  (depth 0, human_needed 0)")
elif args and args[0] == "issue" and args[1] == "add":
    title = args[args.index("--title") + 1]
    with rec.open("a") as f:
        f.write(title + "\n")
    print("filed stub")
else:
    print("")
PY

export GORGE_ROOT=$ROOT
export GORGE_TARGET_REPO=$TARGET
export AGENTCTL_DIR=$STUB
export PROBE_START_FLOOR_MB=1 HEAVY_START_FLOOR_MB=1

# probe_row <reward-dir> <value> — append one standing_gorged_excess row in
# the collector's exact row shape (scripts/reward_collect.py row()), at the
# target repo's head so reward.py's per-head veto logic sees it. One row per
# call, in file order, exactly as the collector appends one per free probe.
probe_row() {
	local d=$1 v=$2 head
	head=$(git -C "$TARGET" rev-parse --short HEAD)
	mkdir -p "$d"
	printf '{"ts":"%s","git_head":"%s","axis":"stability","metric":"standing_gorged_excess","value":%s,"cost_s":0,"cmd":"scripts/reward_collect.py","note":"seed_reap_smoke probe"}\n' \
		"$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$head" "$v" >>"$d/scoreboard.jsonl"
}

# spawn_detached <port> <dir> <pidfile> — a REAL orphan: a sleeping python
# carrying gorged-shaped flags, detached by a double fork (the intermediate
# exits, so the grandchild is adopted at once — the reparent shape the real
# 2026-10-01 event had). Prints the pid; the port is only ever a FLAG here,
# never bound.
spawn_detached() { # <port> <dir> <pidfile>
	local pidfile=$3 pid ppid
	rm -f "$pidfile"
	python3 -c "import os
pid = os.fork()
if pid == 0:
    os.setsid()
    p2 = os.fork()
    if p2 == 0:
        # The orphan must NOT hold this function's stdout: the caller captures
        # the pid with $(), and a grandchild that keeps the pipe open hangs it.
        devnull = os.open(os.devnull, os.O_RDWR)
        os.dup2(devnull, 1)
        os.dup2(devnull, 2)
        open('$pidfile', 'w').write(str(os.getpid()))
        os.execvp('python3', ['python3', '-c', 'import time; time.sleep(300)',
                              '-addr', '127.0.0.1:$1', '-tables', '1',
                              '-dir', '$2'])
    os._exit(0)
os.waitpid(pid, 0)"
	for _ in $(seq 1 50); do
		pid=$(cat "$pidfile" 2>/dev/null || true)
		[ -n "$pid" ] && [ -e "/proc/$pid/stat" ] || {
			sleep 0.1
			continue
		}
		ppid=$(awk '{print $4}' "/proc/$pid/stat")
		[ "$ppid" != "$$" ] && {
			echo "$pid"
			return 0
		}
		sleep 0.1
	done
	return 1
}

# carry_into_faketree <pid> — put a REAL process's cmdline/stat into the fake
# tree at its real pid, so classify() sees it and any --apply signals the
# real process (gorged_reap_smoke.sh's pattern).
carry_into_faketree() {
	mkdir -p "$FAKEPROC/$1"
	cat "/proc/$1/cmdline" >"$FAKEPROC/$1/cmdline"
	printf '%s (gorged) S 1 %s %s 0 -1\n' "$1" "$1" "$1" >"$FAKEPROC/$1/stat"
}

wait_gone() { # <pid>: 0 once the process is gone
	for _ in $(seq 1 50); do
		kill -0 "$1" 2>/dev/null || return 0
		sleep 0.1
	done
	return 1
}

count_metric_rows() { # <reward-dir> <metric> -> row count
	python3 -c "
import json, sys
print(sum(1 for l in open(f'{sys.argv[1]}/scoreboard.jsonl') if l.strip()
          and json.loads(l).get('metric') == sys.argv[2]))" "$1" "$2" 2>/dev/null
}

# ------------------------------------------------------------- case 1: reap
# Two nonzero probes, one real standing orphan, one real demo-port neighbour.
ORPHAN=$(spawn_detached 8093 /tmp/gorge-reap-seed-orphan "$TMP/orphan.pid")
[ -n "$ORPHAN" ]
check "a detached real orphan is running, adopted away from this script" $?
DEMO=$(spawn_detached 8080 /tmp/gorge-demo-seed-smoke "$TMP/demo.pid")
[ -n "$DEMO" ]
check "a real demo-port neighbour is running" $?
[ -n "$ORPHAN" ] && carry_into_faketree "$ORPHAN"
[ -n "$DEMO" ] && carry_into_faketree "$DEMO"

export GORGE_PROC_DIR=$FAKEPROC
RDIR1=$TMP/reward1
probe_row "$RDIR1" 1
probe_row "$RDIR1" 1
[ "$(count_metric_rows "$RDIR1" standing_gorged_excess)" = 2 ]
check "precondition: two standing_gorged_excess rows, both > 0" $?
export GORGE_REWARD_DIR=$RDIR1
"$ROOT/scripts/seed-agent.sh" --no-probe --cap 2 >"$TMP/case1.log" 2>&1
rc=$?
check "case 1: the seed cycle exits 0" "$rc" "$(tail -3 "$TMP/case1.log")"

wait_gone "$ORPHAN"
check "case 1: the real orphan was stopped by the cycle" $?
kill -0 "$DEMO" 2>/dev/null
check "case 4: the demo-port neighbour was never signalled" $?

python3 - "$RDIR1" "$ORPHAN" <<'PY'
import json, sys
rows = [json.loads(l) for l in open(f"{sys.argv[1]}/scoreboard.jsonl") if l.strip()]
g = [r for r in rows if r.get("metric") == "gorged_reaped"]
assert len(g) == 1, f"expected exactly one gorged_reaped row, got {g}"
r = g[0]
assert r["axis"] == "stability", r
assert r["value"] == 1, r
assert r["cost_s"] == 0, r
assert r["cmd"] == "seed-agent.sh", r
assert r["git_head"], r
assert r["ts"], r
assert f"pid={sys.argv[2]}" in r["note"], r
assert "addr=127.0.0.1:8093" in r["note"], r
PY
check "case 1: exactly one gorged_reaped row with the broker row shape" $? \
	"$(python3 -c "
import json
print([l for l in open('$RDIR1/scoreboard.jsonl') if 'gorged_reaped' in l])" 2>&1 | tail -2)"
grep -q 'did: reaped 1 standing gorged instance(s)' "$TMP/case1.log"
check "case 1: the cycle's journal records the action (did)" $? \
	"$(grep 'reaped' "$TMP/case1.log" | head -2)"
grep -q 'gorged_reaped' "$RDIR1/SEED.md"
check "case 1: SEED.md shows the action" $?
python3 -c "
import json, sys
rows = [json.loads(l) for l in open('$RDIR1/seed-journal.jsonl') if l.strip()]
assert any(any('gorged_reaped' in x or x.startswith('reaped') for x in r.get('did', []))
           for r in rows), rows" 2>"$TMP/j1.err"
check "case 1: seed-journal.jsonl records the action" $? "$(cat "$TMP/j1.err")"
grep -q 'did: paused every heavy lease (stability veto active)' "$TMP/case1.log"
check "case 1: the veto block still ran this cycle (no re-check, pause unchanged)" $? \
	"$(grep 'paused every heavy lease' "$TMP/case1.log")"

# ------------------------------------------------- case 5: gorged_reaped is
# visible but scored nowhere — the veto stays governed by the metric rows.
python3 "$ROOT/scripts/reward.py" --ledger "$RDIR1/scoreboard.jsonl" --json >"$TMP/reward1.json" 2>/dev/null
python3 - "$TMP/reward1.json" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
det = d["stability"]["detail"]
assert "standing_gorged_excess" in det, det
assert "gorged_reaped" not in det, f"the cure must not veto like the disease: {det}"
assert set(det) == {"standing_gorged_excess"}, det
assert d["blocked_by_stability"] is True, d["blocked_by_stability"]
PY
check "case 5: reward.py scores only standing_gorged_excess; gorged_reaped is unscored" $? \
	"$(cat "$TMP/reward1.json" | python3 -c 'import json,sys; print(json.load(sys.stdin)["stability"])' 2>&1 | head -2)"

# ------------------------------------------- case 2: early reap is unconditional
# One nonzero probe (second row zeroed): the early reap still removes the orphan.
RDIR2=$TMP/reward2
probe_row "$RDIR2" 1
probe_row "$RDIR2" 0
[ "$(count_metric_rows "$RDIR2" standing_gorged_excess)" = 2 ]
check "case 2 precondition: two rows, last one 0" $?
ORPHAN2=$(spawn_detached 8091 /tmp/gorge-reap-seed-orphan2 "$TMP/orphan2.pid")
[ -n "$ORPHAN2" ]
check "case 2 precondition: a fresh real orphan is running" $?
[ -n "$ORPHAN2" ] && carry_into_faketree "$ORPHAN2"
GORGE_PROC_DIR=$FAKEPROC GORGE_REWARD_DIR=$RDIR2 \
	"$ROOT/scripts/seed-agent.sh" --no-probe --cap 2 >"$TMP/case2.log" 2>&1
check "case 2: the cycle exits 0" $? "$(tail -3 "$TMP/case2.log")"
[ "$(count_metric_rows "$RDIR2" gorged_reaped)" = 0 ]
check "case 2: early reap does not append the late action row" $?
! kill -0 "$ORPHAN2" 2>/dev/null
check "case 2: early reap removes the orphan despite one nonzero probe" $?

# ------------------------------------------- case 3: nothing left to reap
# Two nonzero rows, but the process tree is empty (the instance exited
# between probes — the real 2026-10-01 shape): no action row, one saw line.
RDIR3=$TMP/reward3
probe_row "$RDIR3" 1
probe_row "$RDIR3" 1
ORPHAN3=$(spawn_detached 8097 /tmp/gorge-reap-seed-orphan3 "$TMP/orphan3.pid")
[ -n "$ORPHAN3" ]
check "case 3 precondition: an orphan the empty tree cannot see" $?
GORGE_PROC_DIR=$EMPTYPROC GORGE_REWARD_DIR=$RDIR3 \
	"$ROOT/scripts/seed-agent.sh" --no-probe --cap 2 >"$TMP/case3.log" 2>&1
check "case 3: the cycle exits 0" $? "$(tail -3 "$TMP/case3.log")"
[ "$(count_metric_rows "$RDIR3" gorged_reaped)" = 0 ]
check "case 3: no action row for a look that found nothing" $?
grep -q 'saw: gorged reaper found nothing reapable' "$TMP/case3.log"
check "case 3: a saw line records that the tick looked" $? \
	"$(grep 'gorged reaper' "$TMP/case3.log")"
kill -0 "$ORPHAN3" 2>/dev/null
check "case 3: the unseen orphan was not signalled" $?

# ------------------------------------------------------ case 6: mutation
# seed-agent.sh with the trigger check removed must write no row and reap
# nothing — proving case 1's assertions fail loudly on a cycle that skipped
# the action silently.
MUT=$TMP/seed-mutant.sh
sed 's/if \[ "\$GORGED_FIRE" = "fire" \]; then/if false; then # mutation: trigger removed/' \
	"$ROOT/scripts/seed-agent.sh" >"$MUT"
chmod +x "$MUT"
grep -q 'mutation: trigger removed' "$MUT"
check "case 6 precondition: the mutant really has the trigger removed" $?
RDIR6=$TMP/reward6
probe_row "$RDIR6" 1
probe_row "$RDIR6" 1
ORPHAN4=$(spawn_detached 8098 /tmp/gorge-reap-seed-orphan4 "$TMP/orphan4.pid")
[ -n "$ORPHAN4" ]
check "case 6 precondition: a reapable orphan is present" $?
[ -n "$ORPHAN4" ] && carry_into_faketree "$ORPHAN4"
GORGED_REAP=0 GORGE_PROC_DIR=$FAKEPROC GORGE_REWARD_DIR=$RDIR6 \
	"$MUT" --no-probe --cap 2 >"$TMP/case6.log" 2>&1
[ "$(count_metric_rows "$RDIR6" gorged_reaped)" = 0 ]
check "case 6: the mutation removes the row (the append is load-bearing)" $?
kill -0 "$ORPHAN4" 2>/dev/null
check "case 6: the mutation reaps nothing" $?

printf '\n%d failure(s)\n' "$fails"
[ "$fails" = 0 ]
