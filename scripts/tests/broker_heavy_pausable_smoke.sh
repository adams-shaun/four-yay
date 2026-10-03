#!/usr/bin/env bash
# broker_heavy_pausable_smoke.sh — prove a HEAVY lease is pausable throughout,
# and that pausing it is not recorded as starvation.
#
# Why this test exists (2026-10-03, head 2d4e01009): a long, CPU-bound heavy
# job (`heavy.sh heavy -- go test ./... -timeout 120m`, the shipgate loop) does
# not read GORGE_PAUSE_FILE. When a gate began, broker.sh's `enforce` SIGSTOPped
# it after the grace and recorded `gate_starved_minutes=1.63`, which is a full
# 100x stability veto -- for a pause the broker itself performed. The heavy
# class now runs through heavy.sh's OWN pause supervisor: heavy.sh stops the
# job's group (after the same grace, so a job with a checkpoint loop still parks
# first) and drops a `.supervised` marker, so broker.sh defers instead of
# escalating and no stability row is written.
#
# The broker's after-grace fallback is deliberately NOT removed: it still
# covers probe leases and any lease without the marker, and
# scripts/tests/broker_smoke.sh keeps asserting it through a probe lease.
#
#   scripts/tests/broker_heavy_pausable_smoke.sh
set -uo pipefail

ROOT=$(git rev-parse --show-toplevel)
TMP=$(mktemp -d /tmp/broker-heavy.XXXXXX)
export GORGE_ROOT=$ROOT
export GORGE_REWARD_DIR=$TMP/reward
export HEAVY_START_FLOOR_MB=1 PROBE_START_FLOOR_MB=1
export HEAVY_MAX_LEASES=5 PROBE_MAX_LEASES=5
export PAUSE_GRACE_S=2 KILL_FLOOR_MB=1 LOAD_CEIL_FRAC=100
export GORGE_HEAVY_LOCK=$TMP/heavy.lock
BROKER=$ROOT/scripts/broker.sh
HEAVY=$ROOT/scripts/heavy.sh
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
	[ -n "${HPID:-}" ] && kill -KILL "$HPID" 2>/dev/null
	[ -n "${LEASE:-}" ] && kill -KILL -"$(sed -n 's/.*"pid":\([0-9]*\).*/\1/p' "$LEASE" 2>/dev/null)" 2>/dev/null
	rm -rf "$TMP"
}
trap cleanup EXIT

# A HEAVY job that NEVER reads its pause file: the production shape (a long
# CPU-bound command the wrapper cannot make checkpoint itself). It ticks so the
# test can observe that a pause stopped it.
cat >"$TMP/rude.sh" <<'EOF'
#!/usr/bin/env bash
while :; do echo tick >>"$TICKS"; sleep 0.2; done
EOF
chmod +x "$TMP/rude.sh"

TICKS=$TMP/ticks
export TICKS
: >"$TICKS"
"$HEAVY" heavy --name rudeheavy -- "$TMP/rude.sh" >"$TMP/heavy.log" 2>&1 &
HPID=$!
for _ in $(seq 60); do
	[ -n "$(ls "$GORGE_REWARD_DIR"/leases/*.json 2>/dev/null)" ] && break
	sleep 0.25
done
LEASE=$(ls "$GORGE_REWARD_DIR"/leases/*.json 2>/dev/null | head -1)
[ -n "$LEASE" ]
check "heavy lease created" $? "$(cat "$TMP/heavy.log")"

# PRECONDITION: the job is really running and making progress before the pause,
# so "ticks stopped" below is caused by the pause and not by a dead job.
sleep 1.5
before=$(wc -l <"$TICKS")
sleep 1.0
grew=$(wc -l <"$TICKS")
[ "$grew" -gt "$before" ]
check "heavy job is running and ticking before the pause" $? "before=$before grew=$grew"

# The registration the fix relies on: heavy.sh marks the lease supervised so
# the broker defers. Assert it, or the whole test passes with the supervisor
# never started.
[ -e "$LEASE.supervised" ]
check "heavy lease carries the .supervised marker" $? "$(ls "$GORGE_REWARD_DIR"/leases 2>/dev/null)"

# Pause it and wait past the grace.
"$BROKER" pause-all heavy >"$TMP/pause.txt" 2>&1
sleep 4
# PRECONDITION for the veto assertion: no stability row existed before.
pre_rows=$(grep -c 'gate_starved_minutes' "$GORGE_REWARD_DIR/scoreboard.jsonl" 2>/dev/null || echo 0)
[ "$pre_rows" = 0 ]
check "no stability row before enforce" $? "rows=$pre_rows"

# The supervisor stops the group (or a checkpoint job parks itself), so ticks stop.
p1=$(wc -l <"$TICKS")
sleep 1.5
p2=$(wc -l <"$TICKS")
[ "$p1" = "$p2" ]
check "supervisor stops the heavy job while paused" $? "before=$p1 after=$p2; $(cat "$TMP/pause.txt")"

[ -e "$LEASE.parked" ]
check "supervisor recorded the park" $?

# The broker must DEFER, not escalate: an escalation here is the veto this
# ticket removed.
"$BROKER" enforce >"$TMP/enforce.txt" 2>&1
if [ -e "$LEASE.stopped" ]; then
	check "broker did not SIGSTOP the supervised heavy lease" 1 "a .stopped marker exists: $(cat "$TMP/enforce.txt")"
else
	check "broker did not SIGSTOP the supervised heavy lease" 0
fi
grep -q 'supervised by heavy.sh' "$TMP/enforce.txt"
check "broker says it deferred to the supervisor" $? "$(cat "$TMP/enforce.txt")"
grep -q 'gate_starved_minutes' "$GORGE_REWARD_DIR/scoreboard.jsonl" 2>/dev/null
# grep exits 0 when FOUND, so invert: 0 rows expected.
if grep -q 'gate_starved_minutes' "$GORGE_REWARD_DIR/scoreboard.jsonl" 2>/dev/null; then
	check "pausing a heavy lease writes no stability veto" 1 "gate_starved_minutes row present"
else
	check "pausing a heavy lease writes no stability veto" 0
fi

# And it resumes.
"$BROKER" resume-all heavy >/dev/null
sleep 1.5
r=$(wc -l <"$TICKS")
[ "$r" -gt "$p2" ]
check "heavy job resumes after resume-all" $? "parked=$p2 resumed=$r"

printf '\n%s failure(s)\n' "$fails"
[ "$fails" = 0 ]
