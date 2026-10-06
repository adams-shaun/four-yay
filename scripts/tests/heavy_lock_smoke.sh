#!/usr/bin/env bash
# heavy_lock_smoke.sh — every HEAVY runner contends on ONE lock, and a gate that
# takes it is not deadlocked by a heavy lease the gate bracket paused.
#
#   scripts/tests/heavy_lock_smoke.sh
#
# Part A resolves each script's DEFAULT lock path (no GORGE_HEAVY_LOCK / LOCK
# override) and requires them all to be the one repo path. Part B shows two
# different scripts really contend on it. Part C is the gate-bracket liveness
# check: a running lease, `broker.sh gate-begin`, then the gate's own command
# (`heavy_lock.sh run -w N`) must get the lock inside the wait, not hang.
set -uo pipefail

ROOT=$(git rev-parse --show-toplevel)
S=$ROOT/scripts
TMP=$(mktemp -d /tmp/heavy-lock.XXXXXX)
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
	[ -z "${HPID:-}" ] || kill -KILL "$HPID" 2>/dev/null
	[ -z "${JOBPID:-}" ] || kill -KILL -- "-$JOBPID" 2>/dev/null
	rm -rf "$TMP"
}
trap cleanup EXIT

# ---- A: one default path -------------------------------------------------
unset GORGE_HEAVY_LOCK LOCK
want=$("$S/heavy_lock.sh" path)
case $want in
*/.ds4/heavy.lock) check "helper path is <repo>/.ds4/heavy.lock" 0 ;;
*) check "helper path is <repo>/.ds4/heavy.lock" 1 "$want" ;;
esac
# The path is derived from the shared git common dir, so a sibling WORKTREE
# resolves to the SAME file (the operator decision: one repo-owned lock, not one
# per checkout). Assert the derivation explicitly: the lock is next to the
# common dir's parent. This fails if anyone switches to `git rev-parse
# --show-toplevel`, which would give each worktree its own lock.
common=$(git -C "$S" rev-parse --path-format=absolute --git-common-dir 2>/dev/null)
[ -n "$common" ]
check "git common dir resolves (precondition for the one-lock derivation)" $? "common=$common"
[ "$(dirname "$common")/.ds4/heavy.lock" = "$want" ]
check "lock is derived from the shared git common dir, not this checkout's toplevel" $? "common=$common want=$want"
paths=$(
	printf 'heavy.sh=%s\n' "$("$S/heavy.sh" --print-heavy-lock 2>&1)"
	printf 'broker.sh=%s\n' "$("$S/broker.sh" heavy-lock 2>&1)"
	printf 'postmerge_batch.sh=%s\n' "$("$S/postmerge_batch.sh" --print-heavy-lock 2>&1)"
	printf 'sb-gauntlet.sh=%s\n' "$(bash "$S/sb-gauntlet.sh" --print-heavy-lock 2>&1)"
	printf 'm1b-distill.sh=%s\n' "$(bash "$S/m1b-distill.sh" --print-heavy-lock 2>&1)"
)
bad=$(printf '%s\n' "$paths" | grep -v -F "=$want" || true)
[ -z "$bad" ]
check "heavy.sh, broker.sh, postmerge_batch.sh, sb-gauntlet.sh, m1b-distill.sh resolve the same default lock" $? "want $want; got: $paths"
# The two config.toml callers go through heavy_lock.sh, so they cannot drift:
# no literal lock path may appear in the config.
! grep -n 'heavy\.lock' "$ROOT/.agentctl/config.toml" | grep -v '^[0-9]*:#' | grep -q .
check "config.toml names no literal heavy lock path outside comments" $?
# The affected gate's wrapper must come from the BASE copy too: if it invoked
# the branch's scripts/heavy_lock.sh, a branch could edit the helper into a
# no-op and neuter its own gate's serialisation.
grep -qF 'git show {base}:scripts/heavy_lock.sh' "$ROOT/.agentctl/config.toml"
check "config.toml extracts the affected gate's heavy_lock.sh from {base}" $?

# ---- B: two different scripts contend on it ----------------------------------
export GORGE_ROOT=$ROOT GORGE_REWARD_DIR=$TMP/reward
export GORGE_HEAVY_LOCK=$TMP/lockfile
export HEAVY_START_FLOOR_MB=1 PROBE_START_FLOOR_MB=1 HEAVY_MAX_LEASES=5 LOAD_CEIL_FRAC=100
export PAUSE_GRACE_S=1 KILL_FLOOR_MB=1
cat >"$TMP/job.sh" <<'JOB'
#!/usr/bin/env bash
# fd check: the job must NOT hold the lock file open.
n=0
for l in /proc/$$/fd/*; do [ "$(readlink "$l")" = "$GORGE_HEAVY_LOCK" ] && n=$((n + 1)); done
echo "$n" >"$JOBFD"
echo "$$" >"$JOBPIDFILE"
while :; do echo tick >>"$TICKS"; sleep 0.2; done
JOB
chmod +x "$TMP/job.sh"
export TICKS=$TMP/ticks JOBFD=$TMP/jobfd JOBPIDFILE=$TMP/jobpid
: >"$TICKS"
"$S/heavy.sh" heavy --name locktest -- "$TMP/job.sh" >"$TMP/heavy.log" 2>&1 &
HPID=$!
for _ in $(seq 80); do [ -s "$JOBPIDFILE" ] && break; sleep 0.1; done
JOBPID=$(cat "$JOBPIDFILE" 2>/dev/null || true)
[ -n "$JOBPID" ]
check "precondition: heavy lease job started" $? "$(cat "$TMP/heavy.log")"

"$S/heavy_lock.sh" run -w 0 -- true
rc=$?
[ "$rc" = 75 ]
check "heavy_lock.sh (the gate/ledger wrapper) contends with a running heavy.sh lease" $? "rc=$rc"
"$S/heavy.sh" heavy --wait 0 -- true >"$TMP/second.log" 2>&1
rc=$?
[ "$rc" = 3 ] && grep -q 'another heavy job holds' "$TMP/second.log"
check "a second heavy.sh is refused with exit 3 and the 'holds' message" $? "rc=$rc $(cat "$TMP/second.log")"
[ "$(cat "$JOBFD")" = 0 ]
check "the heavy job does not inherit the lock fd" $? "fds on lock: $(cat "$JOBFD")"

# ---- C: gate bracket does not deadlock on a paused lease ----------------------
"$S/broker.sh" gate-begin smoke >/dev/null 2>&1
t0=$(date +%s)
"$S/heavy_lock.sh" run -w 20 -- bash -c 'echo gate-ran >"$1"' _ "$TMP/gate.out"
rc=$?
el=$(($(date +%s) - t0))
[ "$rc" = 0 ] && [ "$(cat "$TMP/gate.out" 2>/dev/null)" = gate-ran ]
check "gate command got the lock after gate-begin (rc=$rc in ${el}s, bounded wait 20s)" $? "rc=$rc"
# The job really was frozen while the gate held the lock, so the lock was yielded
# by a PARKED lease, not by a job that happened to be idle.
p1=$(wc -l <"$TICKS")
sleep 0.8
p2=$(wc -l <"$TICKS")
[ "$p1" = "$p2" ]
check "lease is stopped while the gate bracket is open" $? "$p1 -> $p2"

# gate-end while the gate command still holds the lock: the lease must not run
# until the lock is its again (a job running beside a lock holder is the bug).
"$S/heavy_lock.sh" run -w 20 -- sleep 4 &
GPID=$!
sleep 0.5
"$S/broker.sh" gate-end smoke >/dev/null 2>&1
sleep 2.5
p3=$(wc -l <"$TICKS")
[ "$p3" = "$p2" ]
check "lease stays stopped after gate-end while the gate still holds the lock" $? "$p2 -> $p3"
wait "$GPID"
for _ in $(seq 60); do
	[ "$(wc -l <"$TICKS")" -gt "$p2" ] && break
	sleep 0.2
done
[ "$(wc -l <"$TICKS")" -gt "$p2" ]
check "lease resumes after gate-end" $? "ticks stuck at $p2"
"$S/heavy_lock.sh" run -w 0 -- true
rc=$?
[ "$rc" = 75 ]
check "resumed lease holds the lock again" $? "rc=$rc"

# A bounded wait on a lock that is genuinely held (no bracket) ends with 75 and
# -s turns it into a logged skip instead of a failure.
"$S/heavy_lock.sh" run -w 1 -s -- touch "$TMP/should-not-exist" 2>"$TMP/skip.err"
rc=$?
[ "$rc" = 0 ] && [ ! -e "$TMP/should-not-exist" ] && grep -q 'not run' "$TMP/skip.err"
check "heavy_lock.sh run -w 1 -s skips (exit 0, command not run) on a held lock" $? "rc=$rc $(cat "$TMP/skip.err")"

# ---- D: a broker-wait loop holds NO lock -------------------------------------
# The affected gate's own `gate-begin` is what keeps `may-i` refusing, so a
# heavy.sh parked in the broker wait must not hold the lock the gate is about to
# want; otherwise the gate waits out its whole budget on a holder its own bracket
# froze (the l0-CRITICAL class). Kill the Part B lease first: the tests below
# need a FREE lock.
[ -z "${HPID:-}" ] || kill -KILL "$HPID" 2>/dev/null
[ -z "${JOBPID:-}" ] || kill -KILL -- "-$JOBPID" 2>/dev/null
for _ in $(seq 100); do "$S/heavy_lock.sh" run -w 0 -- true 2>/dev/null && break; sleep 0.1; done
"$S/heavy_lock.sh" run -w 0 -- true
check "precondition: lock is free before the broker-wait test" $?

# A command that itself exits 75 must NOT be read as a lock timeout: without the
# start marker, `-s` would swallow it as a logged skip (exit 0) and a real
# failure would vanish. 75 is the one status the wrapper must pass through.
"$S/heavy_lock.sh" run -w 5 -s -- bash -c 'touch "$1"; exit 75' _ "$TMP/ran75" 2>"$TMP/ran75.err"
rc=$?
[ "$rc" = 75 ] && [ -e "$TMP/ran75" ] && ! grep -q 'still held' "$TMP/ran75.err"
check "a command that exits 75 after running is not misread as a lock timeout" $? "rc=$rc err=$(cat "$TMP/ran75.err")"

: >"$TMP/reward/gate-active"
"$S/heavy.sh" heavy --wait 30 -- true >"$TMP/brokerwait.log" 2>&1 &
BWID=$!
sleep 1
kill -0 "$BWID" 2>/dev/null
check "precondition: heavy.sh is parked in the broker wait (gate active)" $? "$(cat "$TMP/brokerwait.log")"
t0=$(date +%s)
"$S/heavy_lock.sh" run -w 3 -- bash -c 'echo bw-ran >"$1"' _ "$TMP/bw.out"
rc=$?
el=$(($(date +%s) - t0))
[ "$rc" = 0 ] && [ "$(cat "$TMP/bw.out" 2>/dev/null)" = bw-ran ]
check "a broker-waiting heavy.sh does not hold the lock (gate got it in ${el}s)" $? "rc=$rc"
kill -KILL "$BWID" 2>/dev/null
for _ in $(seq 100); do kill -0 "$BWID" 2>/dev/null || break; sleep 0.1; done
rm -f "$TMP/reward/gate-active"

printf '\n%s failure(s)\n' "$fails"
[ "$fails" = 0 ]
