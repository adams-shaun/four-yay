#!/usr/bin/env bash
# broker_smoke.sh — prove the broker's lease and pause contract on real processes.
#
# This is the test a reviewer runs to believe the 100x stability axis: it starts
# fake jobs through heavy.sh and asserts the lease appears, that a cooperative
# job pauses at its own checkpoint, that a job ignoring its pause file is
# SIGSTOPped after the grace, and that leases are gone after exit.
#
#   scripts/tests/broker_smoke.sh
set -uo pipefail

ROOT=$(git rev-parse --show-toplevel)
TMP=$(mktemp -d /tmp/broker-smoke.XXXXXX)
export GORGE_ROOT=$ROOT
export GORGE_REWARD_DIR=$TMP/reward
export PROBE_START_FLOOR_MB=1 PROBE_MAX_LEASES=5 PAUSE_GRACE_S=2 KILL_FLOOR_MB=1
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
	pkill -P $$ 2>/dev/null || true
	rm -rf "$TMP"
}
trap cleanup EXIT

# A cooperative job: it appends a tick per second and stops ticking while its
# pause file exists, which is exactly the contract heavy.sh documents.
cat >"$TMP/coop.sh" <<'EOF'
#!/usr/bin/env bash
while :; do
	while [ -e "${GORGE_PAUSE_FILE:-/nonexistent}" ]; do sleep 0.2; done
	echo tick >>"$TICKS"
	sleep 0.5
done
EOF
# An uncooperative job: never reads its pause file.
cat >"$TMP/rude.sh" <<'EOF'
#!/usr/bin/env bash
while :; do echo tick >>"$TICKS"; sleep 0.5; done
EOF
chmod +x "$TMP/coop.sh" "$TMP/rude.sh"

# --- 1. a lease appears, and may-i agrees the class may run
TICKS=$TMP/coop.ticks
export TICKS
"$HEAVY" probe --name coop -- "$TMP/coop.sh" >"$TMP/coop.log" 2>&1 &
hpid=$!
for _ in $(seq 40); do
	[ -n "$(ls "$GORGE_REWARD_DIR"/leases/*.json 2>/dev/null)" ] && break
	sleep 0.25
done
ls "$GORGE_REWARD_DIR"/leases/*.json >/dev/null 2>&1
check "lease file created" $?
"$BROKER" status >"$TMP/status.txt" 2>&1
grep -q 'probe' "$TMP/status.txt"
check "status lists the lease" $? "$(cat "$TMP/status.txt")"

# --- 2. gate-begin pauses cooperatively: ticks stop without a signal
"$BROKER" gate-begin smoke >/dev/null
sleep 1.5
before=$(wc -l <"$TICKS")
sleep 1.5
after=$(wc -l <"$TICKS")
[ "$before" = "$after" ]
check "cooperative job stops ticking while paused" $? "before=$before after=$after"
if ls "$GORGE_REWARD_DIR"/leases/*.stopped >/dev/null 2>&1; then
	check "cooperative job was NOT SIGSTOPped" 1 "a .stopped marker exists"
else
	check "cooperative job was NOT SIGSTOPped" 0
fi

# --- 3. a gate in flight refuses new work
"$BROKER" may-i probe >"$TMP/mayi.txt" 2>&1
check "may-i refuses while a gate is in flight" $((1 - $?)) "$(cat "$TMP/mayi.txt")"
grep -q 'gate is in flight' "$TMP/mayi.txt"
check "may-i says why it refused" $?

# --- 4. gate-end resumes it
"$BROKER" gate-end >/dev/null
sleep 1.5
resumed=$(wc -l <"$TICKS")
[ "$resumed" -gt "$after" ]
check "job resumes after gate-end" $? "after=$after resumed=$resumed"

# --- 5. a job that ignores its pause file is SIGSTOPped after the grace
TICKS=$TMP/rude.ticks
export TICKS
"$HEAVY" probe --name rude -- "$TMP/rude.sh" >"$TMP/rude.log" 2>&1 &
rpid=$!
sleep 1.5
"$BROKER" pause-all probe >/dev/null
sleep 3
"$BROKER" enforce >"$TMP/enforce.txt" 2>&1
sleep 0.5
rb=$(wc -l <"$TICKS")
sleep 1.5
ra=$(wc -l <"$TICKS")
[ "$rb" = "$ra" ]
check "uncooperative job is halted by enforce" $? "before=$rb after=$ra; $(cat "$TMP/enforce.txt")"
ls "$GORGE_REWARD_DIR"/leases/*.stopped >/dev/null 2>&1
check "enforce recorded the SIGSTOP escalation" $?
grep -q '"metric":"gate_starved_minutes"' "$GORGE_REWARD_DIR/scoreboard.jsonl" 2>/dev/null
check "escalation is recorded on the stability axis" $?

# --- 6. leases are reaped when the jobs die
"$BROKER" resume-all probe >/dev/null
kill -KILL -"$(sed -n 's/.*"pid":\([0-9]*\).*/\1/p' "$GORGE_REWARD_DIR"/leases/*.json | head -1)" 2>/dev/null
pkill -P $hpid 2>/dev/null
pkill -P $rpid 2>/dev/null
kill -KILL "$hpid" "$rpid" 2>/dev/null
sleep 1
for _ in $(seq 20); do
	"$BROKER" reap >/dev/null 2>&1
	[ -z "$(ls "$GORGE_REWARD_DIR"/leases/*.json 2>/dev/null)" ] && break
	sleep 0.5
done
[ -z "$(ls "$GORGE_REWARD_DIR"/leases/*.json 2>/dev/null)" ]
check "leases reaped after the jobs exit" $? "$(ls "$GORGE_REWARD_DIR"/leases 2>/dev/null)"

# --- 7. an abandoned gate flag expires instead of stranding heavy work forever
GATE_FLAG_TTL_S=1 "$BROKER" gate-begin stale >/dev/null
sleep 2
if GATE_FLAG_TTL_S=1 "$BROKER" may-i probe >"$TMP/stale.txt" 2>&1; then
	check "an abandoned gate flag expires" 0
else
	check "an abandoned gate flag expires" 1 "$(cat "$TMP/stale.txt")"
fi
[ ! -e "$GORGE_REWARD_DIR/gate-active" ]
check "the stale flag is removed" $?
"$BROKER" gate-end >/dev/null 2>&1

printf '\n%s failure(s)\n' "$fails"
[ "$fails" = 0 ]
