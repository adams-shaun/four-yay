#!/usr/bin/env bash
# reward_loop.sh — every test for the reward loop, in one command.
#
# The loop is shell and Python, so it is outside the Go gate list; this is the
# command a reviewer (or the seed's own steward ticket) runs to believe it.
#
#   scripts/tests/reward_loop.sh
set -uo pipefail
ROOT=$(git rev-parse --show-toplevel)
rc=0
run() {
	printf '\n=== %s\n' "$1"
	shift
	"$@" || rc=1
}
run "reward.py --selftest" python3 "$ROOT/scripts/reward.py" --selftest
run "reward_collect.py --selftest" python3 "$ROOT/scripts/reward_collect.py" --selftest
run "seed_candidates.py --selftest" python3 "$ROOT/scripts/seed_candidates.py" --selftest
run "broker_smoke.sh" bash "$ROOT/scripts/tests/broker_smoke.sh"
run "park_branch_smoke.sh" bash "$ROOT/scripts/tests/park_branch_smoke.sh"
run "cleanup_orphan_smoke.sh" bash "$ROOT/scripts/tests/cleanup_orphan_smoke.sh"
run "seed_smoke.sh" bash "$ROOT/scripts/tests/seed_smoke.sh"
printf '\n=== reward loop: %s\n' "$([ $rc = 0 ] && echo ALL GREEN || echo FAILURES ABOVE)"
exit $rc
