#!/usr/bin/env bash
# seed_smoke.sh — prove one seed cycle acts, journals, and respects its limits
# WITHOUT filing anything into the real ledger.
#
# The cycle runs against a throwaway target repo and a stubbed agentctl, so the
# assertions cover the parts that decide whether the loop is trustworthy: does it
# act with no model available, does it cap ticket filing, does it refuse to queue
# into a dead seat tier, and does it write down what it decided not to do.
#
#   scripts/tests/seed_smoke.sh
set -uo pipefail

ROOT=$(git rev-parse --show-toplevel)
TMP=$(mktemp -d ${SEED_SMOKE_TMP:-/tmp}/seed-smoke.XXXXXX)
fails=0
check() {
	if [ "$2" = 0 ]; then printf 'ok   %s\n' "$1"; else
		printf 'FAIL %s %s\n' "$1" "${3:-}"
		fails=$((fails + 1))
	fi
}
trap 'rm -rf "$TMP"' EXIT

# --- a throwaway target repo with a journal, decks and the ratchet files
TARGET=$TMP/target
mkdir -p "$TARGET/.ds4/orchestrator" "$TARGET/rules" "$TARGET/internal/testutil/decks"
git -C "$TARGET" init -q 2>/dev/null
printf 'x\n' >"$TARGET/README.md"
git -C "$TARGET" add README.md >/dev/null 2>&1
git -C "$TARGET" -c user.email=t@t -c user.name=t commit -qm init >/dev/null 2>&1
printf '{"name":"D","cards":["Lightning Bolt","Mountain"]}\n' >"$TARGET/internal/testutil/decks/d.json"
: >"$TARGET/rules/acceptance_test.go"
: >"$TARGET/rules/paramcensus_test.go"
# A journal with a healthy tier (no provider storm) and a merge_fix rate that
# will generate a flow candidate.
{
	for _ in 1 2 3 4 5 6; do
		printf '{"ts":"%s","kind":"transition","evidence":{"to":"merge_fix"}}\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
	done
	for _ in 1 2 3 4; do
		printf '{"ts":"%s","kind":"transition","evidence":{"to":"merged"}}\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
	done
} >"$TARGET/.ds4/orchestrator/journal.jsonl"

# --- a stubbed agentctl: `status` reports a healthy queue, `issue add` records
#     each call so the cap can be asserted.
STUB=$TMP/agentctl
mkdir -p "$STUB/agentctl"
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
    print("filed stub-" + str(len(rec.read_text().splitlines())))
else:
    print("")
PY

export GORGE_ROOT=$ROOT
export GORGE_TARGET_REPO=$TARGET
export GORGE_REWARD_DIR=$TMP/reward
export AGENTCTL_DIR=$STUB
export PROBE_START_FLOOR_MB=1 HEAVY_START_FLOOR_MB=1

# --- 1. a cycle with no model anywhere still measures, journals and files
"$ROOT/scripts/seed-agent.sh" --no-probe --cap 2 >"$TMP/cycle1.log" 2>&1
check "cycle exits cleanly with no model available" $? "$(tail -3 "$TMP/cycle1.log")"
# The free collectors are what fill the ledger; run them explicitly since
# --no-probe skipped them, then take a second cycle so candidates can rank.
python3 "$ROOT/scripts/reward_collect.py" all --repo "$TARGET" --state-dir "$GORGE_REWARD_DIR" \
	>>"$GORGE_REWARD_DIR/scoreboard.jsonl" 2>/dev/null
"$ROOT/scripts/seed-agent.sh" --no-probe --cap 2 >"$TMP/cycle2.log" 2>&1
check "second cycle exits cleanly" $? "$(tail -3 "$TMP/cycle2.log")"

[ -s "$GORGE_REWARD_DIR/seed-journal.jsonl" ]
check "the cycle journals" $?
python3 -c "
import json,sys
rows=[json.loads(l) for l in open('$GORGE_REWARD_DIR/seed-journal.jsonl')]
assert len(rows)==2, rows
for r in rows:
    assert r['saw'], 'no facts recorded'
    assert isinstance(r['skipped'], list)
" 2>"$TMP/journal.err"
check "journal records facts for every cycle" $? "$(cat "$TMP/journal.err")"
grep -q '## Decided not to' "$GORGE_REWARD_DIR/SEED.md"
check "SEED.md records what it decided NOT to do" $?

# --- 2. the ticket cap holds PER CYCLE (it is a rate limit, not a total)
filed=$(wc -l <"$STUB/filed.txt" 2>/dev/null || echo 0)
[ "$filed" -ge 1 ]
check "the loop files work when there is work to file" $? "filed=$filed"
c1=$(grep -c 'did: filed' "$TMP/cycle1.log" 2>/dev/null || true)
c2=$(grep -c 'did: filed' "$TMP/cycle2.log" 2>/dev/null || true)
[ "${c1:-0}" -le 2 ] && [ "${c2:-0}" -le 2 ]
check "no cycle exceeds the cap of 2" $? "cycle1=$c1 cycle2=$c2"

# --- 3. a candidate is never filed twice
before=$(wc -l <"$STUB/filed.txt")
"$ROOT/scripts/seed-agent.sh" --no-probe --cap 2 >"$TMP/cycle3.log" 2>&1
after=$(wc -l <"$STUB/filed.txt")
[ "$after" = "$before" ]
check "a third cycle re-files nothing" $? "before=$before after=$after"

# --- 4. a provider storm suppresses new work
{
	for _ in $(seq 12); do
		printf '{"ts":"%s","kind":"provider_failure","evidence":{"provider":"test-tier"}}\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
	done
} >>"$TARGET/.ds4/orchestrator/journal.jsonl"
rm -rf "$GORGE_REWARD_DIR/markers"
before=$(wc -l <"$STUB/filed.txt")
"$ROOT/scripts/seed-agent.sh" --no-probe --cap 2 >"$TMP/cycle4.log" 2>&1
after=$(wc -l <"$STUB/filed.txt")
grep -q 'no new tickets' "$TMP/cycle4.log"
check "a provider storm suppresses opportunity tickets" $? "$(tail -5 "$TMP/cycle4.log")"
# It still files the one ticket about the storm itself.
grep -q 'Seat tier test-tier' "$STUB/filed.txt"
check "the storm itself is reported as a ticket" $? "$(cat "$STUB/filed.txt" | tr '\n' '|')"
[ "$after" -le $((before + 1)) ]
check "no opportunity ticket was filed during the storm" $? "before=$before after=$after"

# --- 5. dry-run files nothing
before=$(wc -l <"$STUB/filed.txt")
rm -rf "$GORGE_REWARD_DIR/markers"
"$ROOT/scripts/seed-agent.sh" --no-probe --dry-run >"$TMP/cycle5.log" 2>&1
after=$(wc -l <"$STUB/filed.txt")
[ "$after" = "$before" ]
check "--dry-run files nothing" $? "before=$before after=$after"
grep -q 'DRY-RUN would file' "$TMP/cycle5.log"
check "--dry-run says what it would have filed" $?

printf '\n%s failure(s)\n' "$fails"
[ "$fails" = 0 ]
