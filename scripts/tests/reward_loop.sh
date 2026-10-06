#!/usr/bin/env bash
# reward_loop.sh — every shell smoke, plus the reward-loop Python selftests.
#
# All scripts/tests/*.sh smokes run here; none are manual-only. Their measured
# combined wall time is ~63s, so this is called by the post-merge full suite,
# not the per-ticket gate. Keep this entry list in sync with the directory.
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
run "broker_heavy_pausable_smoke.sh" bash "$ROOT/scripts/tests/broker_heavy_pausable_smoke.sh"
run "deploy_sweep_smoke.sh" bash "$ROOT/scripts/tests/deploy_sweep_smoke.sh"
run "park_branch_smoke.sh" bash "$ROOT/scripts/tests/park_branch_smoke.sh"
run "cleanup_orphan_smoke.sh" bash "$ROOT/scripts/tests/cleanup_orphan_smoke.sh"
run "seed_gorged_reap_smoke.sh" bash "$ROOT/scripts/tests/seed_gorged_reap_smoke.sh"
run "seed_smoke.sh" bash "$ROOT/scripts/tests/seed_smoke.sh"
run "sb_gauntlet_retain_smoke.sh" bash "$ROOT/scripts/tests/sb_gauntlet_retain_smoke.sh"
run "gorged_reap_smoke.sh" bash "$ROOT/scripts/tests/gorged_reap_smoke.sh"
run "ledger_lane_smoke.sh" bash "$ROOT/scripts/tests/ledger_lane_smoke.sh"
run "park_branch_registry_only_smoke.sh" bash "$ROOT/scripts/tests/park_branch_registry_only_smoke.sh"
run "park_idle_scheduled_smoke.sh" bash "$ROOT/scripts/tests/park_idle_scheduled_smoke.sh"
run "seed_brief_premise.sh" bash "$ROOT/scripts/tests/seed_brief_premise.sh"
run "seed_reap_smoke.sh" bash "$ROOT/scripts/tests/seed_reap_smoke.sh"
run "storm_brief_asserts_endpoint.sh" bash "$ROOT/scripts/tests/storm_brief_asserts_endpoint.sh"
printf '\n=== reward loop: %s\n' "$([ $rc = 0 ] && echo ALL GREEN || echo FAILURES ABOVE)"
exit $rc
