#!/usr/bin/env bash
# heavy_lock_smoke.sh — every HEAVY runner contends on the same lane pool, and
# a gate that takes a lane is not deadlocked by a heavy lease the gate bracket
# paused.
#
#   scripts/tests/heavy_lock_smoke.sh
#
# Part A resolves each script's DEFAULT pool base (no GORGE_HEAVY_LOCK / LOCK
# override) and requires them all to be the one repo path. Part B shows two
# different scripts really contend on a lane. Part C is the gate-bracket
# liveness check: a running lease, `broker.sh gate-begin`, then the gate's own
# command (`heavy_lock.sh run -w N`) must get a lane inside the wait, not hang.
# Parts B–D run the pool with GORGE_HEAVY_LANES=1 so the single-lane contention
# assertions stay exact; Part E is the 2-lane proof (2026-10-09, DRAM 120G),
# pinned to GORGE_HEAVY_LANES=2 so it survives the pool moving to 3 lanes
# later the same day; Part F is the 3-lane proof (cli-20261009T224425Z-
# a610bfaf): the DEFAULT pool runs three heavy leases at once and a fourth
# contender finds every lane held. Part G is the non-heavy.sh yield proof
# (agent-20261009T214406Z-777851fe): a `heavy_lock.sh run` job stops its child
# and releases its lane while a broker bracket is open, resumes and re-takes it
# at gate-end, and a fresh `run` defers while the flag is live; a job that ends
# during a bracket's grace gives its lane back at once, and a group signal to
# the wrapper (Ctrl-C, `timeout`) takes the job with it, parked or not.
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
	[ -z "${GYPID:-}" ] || kill -KILL "$GYPID" 2>/dev/null
	[ -z "${GYJOBPID:-}" ] || kill -KILL -- "-$GYJOBPID" 2>/dev/null
	[ -z "${GQPID:-}" ] || kill -KILL "$GQPID" 2>/dev/null
	[ -z "${GSJOBPID:-}" ] || kill -KILL -- "-$GSJOBPID" 2>/dev/null
	for v in LPIDA LPIDB LPIDC LPIDD LPIDE; do
		[ -z "${!v:-}" ] || kill -KILL "${!v}" 2>/dev/null
	done
	for v in PA PB PC PD PE; do
		[ -z "${!v:-}" ] || kill -KILL -- "-${!v}" 2>/dev/null
	done
	for v in FJ1 FJ2 FJ3; do
		[ -z "${!v:-}" ] || kill -KILL "${!v}" 2>/dev/null
	done
	rm -rf "$TMP"
}
trap cleanup EXIT

# ---- A: one default path -------------------------------------------------
unset GORGE_HEAVY_LOCK LOCK
want=$("$S/heavy_lock.sh" path)
case $want in
*/.ds4/heavy.lock) check "helper pool base is <repo>/.ds4/heavy.lock" 0 ;;
*) check "helper pool base is <repo>/.ds4/heavy.lock" 1 "$want" ;;
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
# the affected gate may name the lock ONLY as the inline-derived
# <git-common-dir>/../.ds4/heavy.lock; no other literal path may appear.
bad=$(/usr/bin/grep -n 'heavy\.lock' "$ROOT/.agentctl/config.toml" | /usr/bin/grep -v '^[0-9]*:#' | /usr/bin/grep -vF '.ds4/heavy.lock' || true)
[ -z "$bad" ]
check "config.toml names no heavy lock path other than the derived <common-dir>/.ds4/heavy.lock" $? "$bad"
# The affected gate must take the ONE lock without depending on a file that
# exists only in the branch. Extracting scripts/heavy_lock.sh from {base} fails
# on the very ticket that introduces the helper ({base} is an ancestor that has
# no such file), so the gate derives the lock INLINE. Assert both halves: no
# {base} helper extraction, and a merge-base really lacks the file (the reason
# the inline form is required).
! grep -qF 'git show {base}:scripts/heavy_lock.sh' "$ROOT/.agentctl/config.toml"
check "config.toml does not extract the branch-only heavy_lock.sh from {base}" $?
mb=$(git merge-base main HEAD 2>/dev/null || git rev-parse main)
git show "$mb":scripts/heavy_lock.sh >/dev/null 2>&1
check "precondition: the merge-base HAS scripts/heavy_lock.sh (the gate stays inline by policy — the gate reads nothing from the branch — not by necessity)" $? "mb=$mb"

# Run the REAL affected-gate command with {base}=merge-base, not HEAD. Only the
# inner `systemd-run ... gate_affected` body is stubbed (bwrap cannot run the
# 8G scope and the full gate is far over a seat's budget); every lock line is
# the gate's own. A free lock must run the body; a held lock must exit 75.
gatecmd=$(python3 - "$ROOT/.agentctl/config.toml" "$mb" <<'PY'
import re, sys, tomllib
cfg, mb = sys.argv[1], sys.argv[2]
for g in tomllib.load(open(cfg, 'rb'))['gates']:
    if g['name'] == 'go test (affected)':
        c = g['cmd'][2]
        # stub the systemd-run scope + the heavy gate body; keep every lock
        # line. The scope numbers move (caps x2, 2026-10-09), so strip by shape.
        c = re.sub(r'systemd-run --user --scope -q -p MemoryMax=\\d+G -p CPUQuota=\\d+% ', '', c)
        c = c.replace('git show {base}:scripts/gate_affected.sh | bash -s {base}', 'echo GATE-BODY-RAN')
        c = c.replace('{base}', mb)
        print(c)
PY
)
[ -n "$gatecmd" ]
check "read the affected gate command from config.toml" $?
stub=$gatecmd
printf '%s' "$stub" | grep -qF 'echo GATE-BODY-RAN'
check "stubbed the gate body with {base}=merge-base (precondition)" $?
GORGE_HEAVY_LOCK=$TMP/gate.lock bash -c "$stub" >"$TMP/gatebody.out" 2>&1
rc=$?
[ "$rc" = 0 ] && grep -qF 'GATE-BODY-RAN' "$TMP/gatebody.out"
check "real affected gate command (base=merge-base) takes the lock and runs its body" $? "rc=$rc $(cat "$TMP/gatebody.out")"
# The same command on a HELD lane 1 must not run the body and must exit 75.
# GORGE_HEAVY_LANES=1 keeps this a single-lane assertion (with 2 lanes the
# free lane 2 would take the command and the rc would be 0).
flock -o -E 0 "$TMP/gate.lock" -c 'sleep 3' &
HLPID=$!
sleep 0.3
GORGE_HEAVY_LANES=1 GORGE_HEAVY_LOCK=$TMP/gate.lock bash -c "${stub/1500/1}" >"$TMP/gateheld.out" 2>&1
rc=$?
wait "$HLPID" 2>/dev/null || true
[ "$rc" = 75 ] && ! grep -qF 'GATE-BODY-RAN' "$TMP/gateheld.out" && grep -qF 'still held' "$TMP/gateheld.out"
check "real affected gate command exits 75 without running on a held lock" $? "rc=$rc $(cat "$TMP/gateheld.out")"

# ---- B: two different scripts contend on one lane --------------------------------
export GORGE_ROOT=$ROOT GORGE_REWARD_DIR=$TMP/reward
export GORGE_HEAVY_LOCK=$TMP/lockfile
# Parts B-D assert SINGLE-lane semantics (a second lease is refused, run waits
# on the one held lane); Part E below restores the default 2-lane pool.
export GORGE_HEAVY_LANES=1
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
# The production gate's command is the INLINE acquisition in config.toml and
# does NOT consult the bracket (the bracket exists for it), so this stand-in
# ignores the flag too: point its own GORGE_HEAVY_GATE_FLAG at a path that is
# never set. Without this, gorge_heavy_acquire defers mid-bracket (the fix for
# agent-20261009T214406Z-777851fe) and the gate could never take the lane.
GORGE_HEAVY_GATE_FLAG=$TMP/no-gate-flag "$S/heavy_lock.sh" run -w 20 -- bash -c 'echo gate-ran >"$1"' _ "$TMP/gate.out"
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
GORGE_HEAVY_GATE_FLAG=$TMP/no-gate-flag "$S/heavy_lock.sh" run -w 20 -- sleep 4 &
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
GORGE_HEAVY_GATE_FLAG=$TMP/no-gate-flag "$S/heavy_lock.sh" run -w 3 -- bash -c 'echo bw-ran >"$1"' _ "$TMP/bw.out"
rc=$?
el=$(($(date +%s) - t0))
[ "$rc" = 0 ] && [ "$(cat "$TMP/bw.out" 2>/dev/null)" = bw-ran ]
check "a broker-waiting heavy.sh does not hold the lock (gate got it in ${el}s)" $? "rc=$rc"
kill -KILL "$BWID" 2>/dev/null
for _ in $(seq 100); do kill -0 "$BWID" 2>/dev/null || break; sleep 0.1; done
rm -f "$TMP/reward/gate-active"

# ---- E: two lanes, so two heavy jobs run at once (2026-10-09, DRAM 120G) ------
# Pinned to 2: the default pool moved to 3 lanes the same day
# (cli-20261009T224425Z-a610bfaf); Part F asserts the 3-lane default.
export GORGE_HEAVY_LANES=2
export GORGE_HEAVY_LOCK=$TMP/pool.lock
TICKS=$TMP/ticksA JOBPIDFILE=$TMP/jobpidA JOBFD=$TMP/jobfdA \
	"$S/heavy.sh" heavy --name laneA -- "$TMP/job.sh" >"$TMP/leaseA.log" 2>&1 &
LPIDA=$!
TICKS=$TMP/ticksB JOBPIDFILE=$TMP/jobpidB JOBFD=$TMP/jobfdB \
	"$S/heavy.sh" heavy --name laneB -- "$TMP/job.sh" >"$TMP/leaseB.log" 2>&1 &
LPIDB=$!
for _ in $(seq 80); do
	[ -s "$TMP/jobpidA" ] && [ -s "$TMP/jobpidB" ] && break
	sleep 0.1
done
PA=$(cat "$TMP/jobpidA" 2>/dev/null || true)
PB=$(cat "$TMP/jobpidB" 2>/dev/null || true)
[ -n "$PA" ] && [ -n "$PB" ]
check "precondition: two heavy leases started (lanes 1 and 2)" $? "A: $(cat "$TMP/leaseA.log") B: $(cat "$TMP/leaseB.log")"
a1=$(wc -l <"$TMP/ticksA")
b1=$(wc -l <"$TMP/ticksB")
sleep 1.2
a2=$(wc -l <"$TMP/ticksA")
b2=$(wc -l <"$TMP/ticksB")
[ "$a2" -gt "$a1" ] && [ "$b2" -gt "$b1" ]
check "two heavy leases tick at the same time (2 lanes)" $? "A $a1->$a2 B $b1->$b2"
"$S/heavy_lock.sh" run -w 0 -- true
rc=$?
[ "$rc" = 75 ]
check "a third contender finds every lane held (rc 75)" $? "rc=$rc"
"$S/heavy.sh" heavy --wait 0 -- true >"$TMP/third.log" 2>&1
rc=$?
[ "$rc" = 3 ] && grep -q 'another heavy job holds' "$TMP/third.log"
check "a third heavy.sh is refused with exit 3 while both lanes are held" $? "rc=$rc $(cat "$TMP/third.log")"
kill -KILL "$LPIDA" 2>/dev/null
kill -KILL -- "-$PA" 2>/dev/null
freed=0
for _ in $(seq 60); do
	if "$S/heavy_lock.sh" run -w 2 -- true 2>/dev/null; then freed=1; break; fi
	sleep 0.2
done
[ "$freed" = 1 ]
check "killing one lease frees its lane for the next contender" $?
kill -KILL "$LPIDB" 2>/dev/null
kill -KILL -- "-$PB" 2>/dev/null

# ---- F: the DEFAULT pool is 3 lanes (cli-20261009T224425Z-a610bfaf) -----------
# Three heavy leases tick at once on the pool as configured, and a fourth
# contender finds every lane held. The leases are the part-E shape (heavy.sh
# through the broker), so this also exercises the broker cap that must move
# with the lane count (broker.sh HEAVY_MAX_LEASES): the part-B exports above
# set HEAVY_MAX_LEASES=5, so the broker is not the binding constraint here --
# the pool width is.
unset GORGE_HEAVY_LANES # the default pool: 3 lanes since 2026-10-09
export GORGE_HEAVY_LOCK=$TMP/pool3.lock
LPIDC=; LPIDD=; LPIDE=; FJ1=; FJ2=; FJ3=
for n in 1 2 3; do
	TICKS=$TMP/ticksF$n JOBPIDFILE=$TMP/jpF$n JOBFD=$TMP/jfdF$n \
		"$S/heavy.sh" heavy --name laneF$n -- "$TMP/job.sh" >"$TMP/leaseF$n.log" 2>&1 &
	case $n in
	1) LPIDC=$! ;;
	2) LPIDD=$! ;;
	3) LPIDE=$! ;;
	esac
done
for _ in $(seq 80); do
	[ -s "$TMP/jpF1" ] && [ -s "$TMP/jpF2" ] && [ -s "$TMP/jpF3" ] && break
	sleep 0.1
done
FJ1=$(cat "$TMP/jpF1" 2>/dev/null || true)
FJ2=$(cat "$TMP/jpF2" 2>/dev/null || true)
FJ3=$(cat "$TMP/jpF3" 2>/dev/null || true)
[ -n "$FJ1" ] && [ -n "$FJ2" ] && [ -n "$FJ3" ]
check "precondition: three default-pool heavy leases started (lanes 1, 2 and 3)" $? \
	"1: $(tail -1 "$TMP/leaseF1.log") 2: $(tail -1 "$TMP/leaseF2.log") 3: $(tail -1 "$TMP/leaseF3.log")"
f1a=$(wc -l <"$TMP/ticksF1"); f2a=$(wc -l <"$TMP/ticksF2"); f3a=$(wc -l <"$TMP/ticksF3")
sleep 1.2
f1b=$(wc -l <"$TMP/ticksF1"); f2b=$(wc -l <"$TMP/ticksF2"); f3b=$(wc -l <"$TMP/ticksF3")
[ "$f1b" -gt "$f1a" ] && [ "$f2b" -gt "$f2a" ] && [ "$f3b" -gt "$f3a" ]
check "three heavy leases tick at the same time (3-lane default pool)" $? \
	"1 $f1a->$f1b 2 $f2a->$f2b 3 $f3a->$f3b"
"$S/heavy_lock.sh" run -w 0 -- true
rc=$?
[ "$rc" = 75 ]
check "a fourth contender finds every lane of the 3-lane pool held (rc 75)" $? "rc=$rc"
for v in LPIDC LPIDD LPIDE FJ1 FJ2 FJ3; do
	[ -z "${!v:-}" ] || kill -KILL "${!v}" 2>/dev/null
done
freed=0
for _ in $(seq 60); do
	if "$S/heavy_lock.sh" run -w 2 -- true 2>/dev/null; then freed=1; break; fi
	sleep 0.2
done
[ "$freed" = 1 ]
check "killing the leases frees a lane of the 3-lane pool" $?

# ---- G: a heavy_lock.sh run job yields its lane to a live gate bracket --------
# The core fix (agent-20261009T214406Z-777851fe): the pool's non-heavy.sh
# runners held fd 9 for their whole child run with no gate observation. Here a
# `run` job must, while the broker bracket is open, STOP its child, release the
# lane, and re-take it before continuing; and a fresh `run` STARTED while the
# bracket is live must not take a lane at all (gorge_heavy_acquire defers).
export GORGE_HEAVY_LANES=1
export GORGE_HEAVY_LOCK=$TMP/gateyield.lock
export GORGE_HEAVY_PAUSE_GRACE_S=1
rm -f "$GORGE_REWARD_DIR/gate-active"
: >"$TMP/gyticks"
TICKS=$TMP/gyticks JOBPIDFILE=$TMP/gyjobpid JOBFD=$TMP/gyjobfd \
	"$S/heavy_lock.sh" run -- "$TMP/job.sh" >"$TMP/gy.log" 2>&1 &
GYPID=$!
for _ in $(seq 80); do [ -s "$TMP/gyjobpid" ] && break; sleep 0.1; done
GYJOBPID=$(cat "$TMP/gyjobpid" 2>/dev/null || true)
g1=$(wc -l <"$TMP/gyticks")
sleep 0.6
g2=$(wc -l <"$TMP/gyticks")
[ -n "$GYJOBPID" ] && [ "$g2" -gt "$g1" ]
check "precondition: a heavy_lock.sh run job ticks (holds the only lane)" $? \
	"pid=$GYJOBPID $g1 -> $g2 $(cat "$TMP/gy.log")"
# The lane is held before the bracket: a DIRECT probe (not `run`, which now
# defers mid-bracket) must fail to lock it.
flock -n "$GORGE_HEAVY_LOCK" -c true 2>/dev/null
[ $? -ne 0 ]
check "precondition: the run job holds the only lane before the bracket" $?

# 1. bracket open -> child frozen AND lane released.
"$S/broker.sh" gate-begin smoke >/dev/null 2>&1
freed=0
for _ in $(seq 60); do
	if flock -n "$GORGE_HEAVY_LOCK" -c true 2>/dev/null; then freed=1; break; fi
	sleep 0.2
done
[ "$freed" = 1 ]
check "run job released the lane while the bracket is open (lane takeable)" $?
p1=$(wc -l <"$TMP/gyticks")
sleep 0.8
p2=$(wc -l <"$TMP/gyticks")
[ "$p1" = "$p2" ]
check "run job is STOPped while the gate bracket is open" $? "$p1 -> $p2"

# 2. gate-end -> job resumes and re-takes its lane.
"$S/broker.sh" gate-end smoke >/dev/null 2>&1
for _ in $(seq 80); do
	[ "$(wc -l <"$TMP/gyticks")" -gt "$p2" ] && break
	sleep 0.2
done
[ "$(wc -l <"$TMP/gyticks")" -gt "$p2" ]
check "run job resumes after gate-end" $? "ticks stuck at $p2"
flock -n "$GORGE_HEAVY_LOCK" -c true 2>/dev/null
[ $? -ne 0 ]
check "resumed run job re-took the lane" $?

# Free the lane (kill the job) for the deferral checks below.
kill -KILL "$GYPID" 2>/dev/null
kill -KILL -- "-$GYJOBPID" 2>/dev/null
freed=0
for _ in $(seq 80); do
	if flock -n "$GORGE_HEAVY_LOCK" -c true 2>/dev/null; then freed=1; break; fi
	sleep 0.2
done
[ "$freed" = 1 ]
check "lane is free after the run job is killed" $?

# 3. a fresh run STARTED while the bracket is live does NOT take a free lane.
# (Pre-fix this takes the lane and writes the marker; with the fix it defers.)
rm -f "$TMP/gy-marker"
"$S/broker.sh" gate-begin smoke >/dev/null 2>&1
"$S/heavy_lock.sh" run -w 2 -- touch "$TMP/gy-marker" 2>"$TMP/gy-defer.err"
rc=$?
[ "$rc" = 75 ] && [ ! -e "$TMP/gy-marker" ]
check "a fresh run defers while the bracket is live even with a free lane" $? \
	"rc=$rc $(cat "$TMP/gy-defer.err")"
"$S/broker.sh" gate-end smoke >/dev/null 2>&1
"$S/heavy_lock.sh" run -w 2 -- touch "$TMP/gy-marker"
rc=$?
[ "$rc" = 0 ] && [ -e "$TMP/gy-marker" ]
check "after the bracket the same command takes the lane and runs" $? "rc=$rc"

# 4. the re-entrant pass-through runs WITHOUT a lane while the flag is live.
: >"$GORGE_REWARD_DIR/gate-active"
GORGE_HEAVY_LOCK_HELD=$GORGE_HEAVY_LOCK "$S/heavy_lock.sh" run -- touch "$TMP/gy-nested"
rc=$?
[ "$rc" = 0 ] && [ -e "$TMP/gy-nested" ]
check "GORGE_HEAVY_LOCK_HELD pass-through runs while the flag is live" $? "rc=$rc"
rm -f "$GORGE_REWARD_DIR/gate-active"

# 5. a job that ENDS during a bracket's grace gives its lane back at once. The
# supervisor holds the lane's open file description, so a wrapper that waited
# out its in-flight grace sleep kept a FINISHED job's lane for the whole grace
# (here 20 s; production default 60 s).
export GORGE_HEAVY_PAUSE_GRACE_S=20
rm -f "$TMP/gq-started"
"$S/heavy_lock.sh" run -- bash -c ': >"$1"; sleep 2' _ "$TMP/gq-started" >"$TMP/gq.log" 2>&1 &
GQPID=$!
for _ in $(seq 50); do [ -e "$TMP/gq-started" ] && break; sleep 0.1; done
[ -e "$TMP/gq-started" ]
check "precondition: the short run job started" $? "$(cat "$TMP/gq.log")"
"$S/broker.sh" gate-begin smoke >/dev/null 2>&1
gq0=$(date +%s)
wait "$GQPID"
rc=$?
gqel=$(( $(date +%s) - gq0 ))
[ "$rc" = 0 ] && [ "$gqel" -lt 8 ]
check "run returns promptly when its job ends inside the bracket's grace" $? \
	"rc=$rc after ${gqel}s (grace 20s)"
flock -n "$GORGE_HEAVY_LOCK" -c true 2>/dev/null
check "the finished job's lane is free the moment run returns" $?
"$S/broker.sh" gate-end smoke >/dev/null 2>&1
export GORGE_HEAVY_PAUSE_GRACE_S=1

# 6. a group-directed signal to the wrapper (a terminal Ctrl-C, `timeout`) must
# take the job with it: the job runs in its OWN group (so it can be STOPped),
# which such a signal no longer reaches unless the wrapper forwards it. Python
# spawns the wrapper in a fresh session with default signal dispositions (a
# bash `&` would start it with SIGINT ignored). Case 1: running job, SIGINT.
# Case 2: job parked by a live bracket, SIGTERM -- it must not stay STOPped.
# Case 2's job ignores HUP: without that the kernel's orphaned-group SIGHUP
# (sent to a pgrp with stopped members) would kill it with no forwarding at all.
cat >"$TMP/sigtest.py" <<'PY'
import os, signal, subprocess, sys, time
wrapper, sig, bracket, broker = sys.argv[1:5]
p = subprocess.Popen([wrapper, "run", "--"] + sys.argv[5:], start_new_session=True,
                     stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
pf = os.environ["JOBPIDFILE"]
for _ in range(100):
    if os.path.exists(pf) and os.path.getsize(pf) > 0:
        break
    time.sleep(0.1)
if bracket == "1":
    subprocess.run([broker, "gate-begin", "smoke"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    time.sleep(3)
os.killpg(p.pid, getattr(signal, sig))
try:
    print(p.wait(timeout=15))
except subprocess.TimeoutExpired:
    os.killpg(p.pid, signal.SIGKILL)
    print("timeout")
PY
for case in "SIGINT 0 running" "SIGTERM 1 parked"; do
	set -- $case
	jobcmd=("$TMP/job.sh")
	[ "$3" = parked ] && jobcmd=(bash -c 'trap "" HUP; exec "$0"' "$TMP/job.sh")
	rm -f "$TMP/gsjobpid" "$TMP/gsjobfd"
	: >"$TMP/gsticks"
	prc=$(TICKS=$TMP/gsticks JOBPIDFILE=$TMP/gsjobpid JOBFD=$TMP/gsjobfd \
		python3 -I "$TMP/sigtest.py" "$S/heavy_lock.sh" "$1" "$2" "$S/broker.sh" "${jobcmd[@]}")
	GSJOBPID=$(cat "$TMP/gsjobpid" 2>/dev/null || true)
	[ -n "$GSJOBPID" ]
	check "precondition: the $3 job started under the signalled wrapper" $? "wrapper rc=$prc"
	dead=0
	for _ in $(seq 30); do
		kill -0 "$GSJOBPID" 2>/dev/null || { dead=1; break; }
		sleep 0.1
	done
	[ "$dead" = 1 ]
	check "$1 to the wrapper's group leaves no $3 job behind" $? "wrapper rc=$prc job=$GSJOBPID"
	[ "$dead" = 1 ] || kill -KILL -- "-$GSJOBPID" 2>/dev/null
	flock -n "$GORGE_HEAVY_LOCK" -c true 2>/dev/null
	check "the lane is free after the signalled $3 wrapper is gone" $?
	"$S/broker.sh" gate-end smoke >/dev/null 2>&1
	GSJOBPID=""
done
rm -f "$GORGE_REWARD_DIR/gate-active"

printf '\n%s failure(s)\n' "$fails"
[ "$fails" = 0 ]
