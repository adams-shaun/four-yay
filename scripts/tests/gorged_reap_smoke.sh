#!/usr/bin/env bash
# gorged_reap_smoke.sh — prove the standing-gorged reaper's contract on real
# processes.
#
# The reaper is the removal tool for the class behind the 2026-10-01
# standing_gorged_excess stability veto (a finished seat's fixture gorged
# orphaned on :8095). This test drives a REAL orphan fixture (a sleeping
# python with gorged-shaped flags, spawned SUPERVISED — see the fixture
# contract at its spawn below), scopes the listing at a fake /proc tree the
# way reward_collect's own selftest does, and asserts:
#
#   1. the orphan is listed STANDING and nothing else is reapable;
#   2. the demo port entry is protected (and flipping DEMO_PORT_HISTORY to
#      the orphan's own port proves the protection is port-driven);
#   3. live-agent ownership signals (ancestor chain, dir-names-seat) keep
#      their entries out of the reap list;
#   4. --apply actually stops the real orphan and leaves nothing behind.
#
#   scripts/tests/gorged_reap_smoke.sh
set -uo pipefail

ROOT=$(git rev-parse --show-toplevel)
REAP="$ROOT/scripts/gorged_reap.py"
TMP=$(mktemp -d /tmp/gorged-reap-smoke.XXXXXX)
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

# The demo instance, on its documented port, parented to init: unowned by
# every agent signal, and still never reapable (the deploy sweep owns it).
fake_pid 7001 1 bin/gorged -addr 127.0.0.1:8080 -dir /tmp/gorge-demo-pub -tables 1
# Owned by the ancestor signal: its parent chain contains a live (fake)
# pi-agent. The agent's cwd also plants the seat id abcdef12.
fake_pid 8001 1 pi-agent --cwd /home/agent/agent-20261001T000000Z-abcdef12 --name impl
fake_pid 7002 8001 bin/gorged -addr 127.0.0.1:8092 -dir /tmp/gorge-dev-x -tables 1
# Owned by the dir-names-seat signal alone: parent is init, but the dir ends
# with a live seat's own id.
fake_pid 7003 1 bin/gorged -addr 127.0.0.1:8096 -dir /tmp/gorge-ui24-abcdef12 -tables 1

# A REAL orphan fixture: a sleeping python carrying gorged-shaped flags,
# spawned SUPERVISED — a plain `&` child of THIS script, so the collector's
# script-supervisor ownership signal owns it on the real table. This is the
# same conversion e2738cb74 gave the demo-port neighbour (ticket
# agent-20261009T175153Z-de58b6da): the old double-forked detached fixture
# had ppid 1 and was unowned by every ownership signal, so any free reward
# probe overlapping the smoke recorded it as a second standing gorged beside
# the real demo and vetoed the scoreboard for a test artifact. Residual
# (accepted, the same residual e2738cb74 accepted for the demo fixture): the
# reaper's classification of a genuinely unowned REAL process is no longer
# proven from the real table here — the STANDING classification and the kill
# are proven through the forged ppid=1 fake-tree carry below; the ownership
# signals themselves stay covered by reward_collect.py --selftest's
# fake-tree cases. The port is only ever a FLAG, never bound.
ORPHAN=""
python3 -c 'import time; time.sleep(300)' \
	-addr 127.0.0.1:8093 -tables 1 -dir /tmp/gorge-reap-orphan-smoke &
ORPHAN=$!
for _ in $(seq 1 50); do
	[ -e "/proc/$ORPHAN/stat" ] && break
	sleep 0.1
done
ppid=$(awk '{print $4}' "/proc/$ORPHAN/stat" 2>/dev/null)
[ "$ppid" = "$$" ]
check "a real orphan fixture is running, a direct child of this script" $? \
	"orphan=$ORPHAN ppid=${ppid:-}"

# Scope the listing at the fake tree, carrying the REAL orphan in: its
# cmdline/stat are copied under its real pid so classify() sees it and --apply
# signals the real process.
mkdir -p "$FAKEPROC/$ORPHAN"
cat "/proc/$ORPHAN/cmdline" >"$FAKEPROC/$ORPHAN/cmdline"
{ printf '%s (gorged) S 1 %s %s 0 -1\n' "$ORPHAN" "$ORPHAN" "$ORPHAN"; } \
	>"$FAKEPROC/$ORPHAN/stat"

out=$(GORGE_PROC_DIR="$FAKEPROC" python3 "$REAP")
printf '%s\n' "$out" >"$TMP/dryrun.log"
grep -q "pid=$ORPHAN .*STANDING" "$TMP/dryrun.log"
check "dry run lists the real orphan as STANDING" $? "$(cat "$TMP/dryrun.log")"
grep -q "pid=7001 .*demo port, protected" "$TMP/dryrun.log"
check "the demo-port entry is protected" $? "$(cat "$TMP/dryrun.log")"
grep -q "pid=7002 .*owned by a live agent" "$TMP/dryrun.log"
check "the ancestor-owned entry is not reapable" $? "$(cat "$TMP/dryrun.log")"
grep -q "pid=7003 .*owned by a live agent" "$TMP/dryrun.log"
check "the dir-names-seat-owned entry is not reapable" $? "$(cat "$TMP/dryrun.log")"
[ "$(grep -c STANDING "$TMP/dryrun.log")" = 1 ]
check "exactly one STANDING instance (the orphan)" $? "$(cat "$TMP/dryrun.log")"
kill -0 "$ORPHAN" 2>/dev/null
check "dry run did not signal anything" $?

# The protection is port-driven: adding the orphan's own port to the demo
# history makes it protected -- proof the demo guard is not a name list.
out=$(DEMO_PORT_HISTORY="8080 8093" GORGE_PROC_DIR="$FAKEPROC" python3 "$REAP")
printf '%s\n' "$out" >"$TMP/portflip.log"
grep -q "pid=$ORPHAN .*demo port, protected" "$TMP/portflip.log" &&
	grep -q "nothing to reap" "$TMP/portflip.log"
check "port flip protects the orphan (protection is port-driven)" $? \
	"$(cat "$TMP/portflip.log")"

out=$(GORGE_PROC_DIR="$FAKEPROC" python3 "$REAP" --apply)
printf '%s\n' "$out" >"$TMP/apply.log"
grep -q "reaped 1 standing instance(s)" "$TMP/apply.log"
check "apply reaps exactly the orphan" $? "$(cat "$TMP/apply.log")"
gone=1
for _ in $(seq 1 50); do
	kill -0 "$ORPHAN" 2>/dev/null || { gone=0; break; }
	sleep 0.1
done
check "the real orphan is gone after apply" "$gone"

printf '\n%d failure(s)\n' "$fails"
[ "$fails" = 0 ]
