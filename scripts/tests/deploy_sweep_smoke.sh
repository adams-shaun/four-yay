#!/usr/bin/env bash
# deploy_sweep_smoke.sh — the demo port sweep must cover RETIRED ports.
#
# The regression this pins (2026-09-30): deploy-demo.sh retires a listener by
# emptying its *_PORT variable, and the sweep used to be built only from the
# CURRENT *_PORT values. So retiring the :8081 ManaBrew wire (MB_PORT=8081 ->
# MB_PORT=) also removed 8081 from the sweep, the server a previous layout had
# left there was never stopped, and it stood as a second standing gorged
# process -- standing_gorged_excess=1, the 100x stability veto.
#
#   scripts/tests/deploy_sweep_smoke.sh
set -uo pipefail

ROOT=$(git rev-parse --show-toplevel)
# shellcheck source=scripts/demo-ports.sh
. "$ROOT/scripts/demo-ports.sh"

fails=0
check() {
	if [ "$2" = 0 ]; then
		printf 'ok   %s\n' "$1"
	else
		printf 'FAIL %s %s\n' "$1" "${3:-}"
		fails=$((fails + 1))
	fi
}
contains() { demo_sweep_ports "$@" | grep -qx "$PORT"; }

# The precondition the real assertion depends on: the helper must actually see
# 8081 in its append-only history, or every assertion below passes for the
# wrong reason.
PORT=8081
contains
check "8081 is in the demo port history (precondition)" $? "$(demo_sweep_ports | tr '\n' ' ')"

# The bug: both second listeners retired (empty), which is today's default.
PORT=8081
contains "" "" # only the append-only history
check "a retired :8081 is still swept with OMNI_PORT/MB_PORT empty" $?
contains 8080
check "the current public port 8080 is swept" $?

# Adding a NEW listener on a new port must sweep it alongside the retired ones,
# and -- the class -- must not drop any earlier port.
PORT=8082
contains 8080 "" "" 8082
check "a newly bound port 8082 is swept" $?
PORT=8081
contains 8080 "" "" 8082
check "retired 8081 is STILL swept when 8082 is added" $?

# Empty arguments must not poison the regex (an empty alternative matches every
# address:port, which would sweep an unrelated agent server).
if demo_sweep_ports 8080 "" "" | grep -qx ''; then
	check "empty *_PORT arguments contribute no empty port" 1 "empty line in set"
else
	check "empty *_PORT arguments contribute no empty port" 0
fi

printf '\n%d failure(s)\n' "$fails"
[ "$fails" = 0 ]
