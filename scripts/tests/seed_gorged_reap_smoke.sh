#!/usr/bin/env bash
# seed_gorged_reap_smoke.sh — prove the seed loop REMOVES the standing gorged
# it measures, instead of only reporting it.
#
# Why this test exists (2026-10-01): the reward loop's stability axis fired
# standing_gorged_excess=1 at head 7a9e516a1 on a leaked, agent-unowned
# gorged-shaped process (`-dir /tmp/gorge-reap-seed-orphan`). The loop already
# DETECTED that condition -- it paused every heavy lease and filed the stability
# ticket -- but nothing in the pipeline REMOVED the process, so the same leaked
# server kept vetoing every later head until a human ran the manual reaper
# (scripts/gorged_reap.py, cleanup.sh gorged). The cause was that the caretaker
# had no removal step. This test drives the loop's own stability section against
# a REAL detached orphan and asserts the orphan dies while the demo and a
# live-agent-owned dev server are left alone.
#
#   scripts/tests/seed_gorged_reap_smoke.sh
set -uo pipefail

ROOT=$(git rev-parse --show-toplevel)
TMP=$(mktemp -d /tmp/seed-gorged-reap.XXXXXX)
FAKEPROC=$TMP/proc
mkdir -p "$FAKEPROC"
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
	rm -rf "$TMP"
}
trap cleanup EXIT

fake_pid() { # <pid> <ppid> <arg>...
	local pid=$1 ppid=$2
	shift 2
	mkdir -p "$FAKEPROC/$pid"
	{ printf '%s\0' "$@"; printf '\0'; } >"$FAKEPROC/$pid/cmdline"
	printf '%s (gorged) S %s %s %s 0 -1\n' "$pid" "$ppid" "$pid" "$pid" \
		>"$FAKEPROC/$pid/stat"
}

# A throwaway target repo and a stubbed agentctl, so the loop's later sections
# (stalls/candidates) run without touching the real ledger. The reap under test
# is in section 1, before any of this is read, but a target and a stub keep the
# run honest rather than half-configured.
TARGET=$TMP/target
mkdir -p "$TARGET/.ds4/orchestrator" "$TARGET/rules" "$TARGET/internal/testutil/decks"
git -C "$TARGET" init -q 2>/dev/null
printf 'x\n' >"$TARGET/README.md"
git -C "$TARGET" add README.md >/dev/null 2>&1
git -C "$TARGET" -c user.email=t@t -c user.name=t commit -qm init >/dev/null 2>&1
STUB=$TMP/agentctl
mkdir -p "$STUB/agentctl"
: >"$STUB/agentctl/__init__.py"
cat >"$STUB/agentctl/__main__.py" <<'PY'
import sys
args = sys.argv[1:]
if args and args[0] == "status":
    print("repo: t  config: .agentctl/config.toml")
    print("daemon: running  paused: no  fleet stop: no  paid seats: on")
    print("queue: new=0 briefed=0 merged=10  (depth 0, human_needed 0)")
else:
    print("")
PY

# The demo instance on its documented port, parented to init: protected by the
# documented port contract, never reapable.
fake_pid 7001 1 bin/gorged -addr 127.0.0.1:8080 -dir /tmp/gorge-demo-pub -tables 1
# A dev server owned by a live (fake) pi-agent: must survive.
fake_pid 8001 1 pi-agent --cwd /home/agent/agent-20261001T000000Z-abcdef12 --name impl
fake_pid 7002 8001 bin/gorged -addr 127.0.0.1:8092 -dir /tmp/gorge-dev-x -tables 1

# A REAL orphan: a sleeping python carrying gorged-shaped flags, detached by a
# double fork so its parent exits and it is adopted at once -- the exact
# reparent-to-init shape of the 2026-10-01 leak. Its dir names no live seat.
ORPHAN=""
for port in 8093 8091 8097 8098 8099; do
	rm -f "$TMP/orphan.pid"
	python3 -c "import os
pid = os.fork()
if pid == 0:
    os.setsid()
    p2 = os.fork()
    if p2 == 0:
        open('$TMP/orphan.pid', 'w').write(str(os.getpid()))
        os.execvp('python3', ['python3', '-c', 'import time; time.sleep(300)',
                              '-addr', '127.0.0.1:$port', '-tables', '1',
                              '-dir', '/tmp/gorge-seed-reap-smoke-orphan'])
    os._exit(0)
os.waitpid(pid, 0)"
	adopted=1
	for _ in $(seq 1 50); do
		ORPHAN=$(cat "$TMP/orphan.pid" 2>/dev/null || true)
		[ -n "$ORPHAN" ] && [ -e "/proc/$ORPHAN/stat" ] || { sleep 0.1; continue; }
		ppid=$(awk '{print $4}' "/proc/$ORPHAN/stat")
		[ "$ppid" != "$$" ] && { adopted=0; break; }
		sleep 0.1
	done
	[ "$adopted" = 0 ] && break
	[ -n "$ORPHAN" ] && kill -9 "$ORPHAN" 2>/dev/null
	ORPHAN=""
done
check "a detached real orphan is running, adopted away from this script" "$adopted" \
	"orphan=$ORPHAN ppid=${ppid:-}"

# Carry the real orphan into the fake tree with ppid=1, so classify() sees it as
# STANDING while --apply signals its REAL pid (gorged_reap.py reads liveness
# from the real table, never the scoped fake tree).
mkdir -p "$FAKEPROC/$ORPHAN"
cat "/proc/$ORPHAN/cmdline" >"$FAKEPROC/$ORPHAN/cmdline"
printf '%s (gorged) S 1 %s %s 0 -1\n' "$ORPHAN" "$ORPHAN" "$ORPHAN" \
	>"$FAKEPROC/$ORPHAN/stat"

# Preconditions: the orphan is genuinely standing and the owned/demo entries are
# not, so the later assertions cannot pass for the wrong reason.
pre=$(GORGE_PROC_DIR="$FAKEPROC" python3 "$ROOT/scripts/gorged_reap.py" 2>&1)
grep -q "pid=$ORPHAN .*STANDING" <<<"$pre"
check "precondition: the orphan is classified STANDING" $? "$pre"
grep -q "pid=7001 .*demo port, protected" <<<"$pre"
check "precondition: the demo is port-protected" $? "$pre"
grep -q "pid=7002 .*owned by a live agent" <<<"$pre"
check "precondition: the dev server is agent-owned" $? "$pre"

# Drive the loop's own stability section. --no-probe is deliberate: the reap
# must NOT depend on the probe running, or a box whose probe is skipped would
# keep vetoing. --dry-run keeps the later sections from filing anything.
out=$(GORGE_ROOT="$ROOT" GORGE_TARGET_REPO="$TARGET" GORGE_REWARD_DIR="$TMP/reward" \
	AGENTCTL_DIR="$STUB" GORGE_PROC_DIR="$FAKEPROC" GORGED_REAP=1 \
	bash "$ROOT/scripts/seed-agent.sh" --dry-run --no-probe 2>&1)
printf '%s\n' "$out" >"$TMP/seed.log"
grep -q 'reaped standing gorged' "$TMP/seed.log"
check "the seed loop reports the reap" $? "$(grep -E 'gorged|reap' "$TMP/seed.log")"

gone=1
for _ in $(seq 1 50); do
	kill -0 "$ORPHAN" 2>/dev/null || { gone=0; break; }
	sleep 0.1
done
check "the real orphan is gone after a seed cycle" "$gone"

# The owned dev server and the demo were never signalled: classify() never
# called them reapable, so reap() never had their pids.
grep -q 'reaped standing gorged: reaped 1 standing instance(s)' "$TMP/seed.log"
check "the reap named exactly one instance" $? "$(grep 'reaped standing gorged' "$TMP/seed.log")"

printf '\n%s failure(s)\n' "$fails"
[ "$fails" = 0 ]
