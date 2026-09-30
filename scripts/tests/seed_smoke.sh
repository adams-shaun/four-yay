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
# __init__.py makes this a REGULAR package so it shadows the installed agentctl
# pin, which is also on PYTHONPATH. Without it both are namespace portions and
# `-m agentctl` resolves the pin's __main__ instead of this stub.
: >"$STUB/agentctl/__init__.py"
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

# --- 6. a storm whose endpoint answers NOW does not suppress work
# A models.json whose provider points at a URL that answers 200 makes the probe
# succeed; the storm rows are still in the journal from step 4.
printf '{"providers":{"test-tier":{"baseUrl":"http://stubbed.invalid/v1"}}}\n' >"$TMP/models.json"
mkdir -p "$TMP/curl-stub"
# A curl stub, so the probe's own logic is under test rather than the network.
stub_curl() {
	printf '#!/usr/bin/env bash\necho %s\n' "$1" >"$TMP/curl-stub/curl"
	chmod +x "$TMP/curl-stub/curl"
}
stub_curl 200
rm -rf "$GORGE_REWARD_DIR/markers"
before=$(wc -l <"$STUB/filed.txt")
PATH="$TMP/curl-stub:$PATH" PI_MODELS_JSON="$TMP/models.json" \
	"$ROOT/scripts/seed-agent.sh" --no-probe --cap 2 >"$TMP/cycle6.log" 2>&1
after=$(wc -l <"$STUB/filed.txt")
grep -q 'is historical' "$TMP/cycle6.log"
check "a storm whose endpoint answers now is called historical" $? "$(tail -6 "$TMP/cycle6.log")"
# Every candidate has already been filed by the earlier cycles (and marked
# queued), so the assertion is that the cycle does not WITHHOLD for the storm,
# not that it finds fresh work to file.
if grep -q 'no new tickets: .* provider failures' "$TMP/cycle6.log"; then
	check "the historical storm does not withhold work" 1 "$(grep 'no new tickets' "$TMP/cycle6.log")"
else
	check "the historical storm does not withhold work" 0
fi

# --- 7. tier-probe itself: healthy, unreachable, unknown provider
PATH="$TMP/curl-stub:$PATH" PI_MODELS_JSON="$TMP/models.json" "$ROOT/scripts/tier-probe.sh" test-tier >/dev/null 2>&1
check "tier-probe reports a 200 endpoint healthy" $?
stub_curl 000
if PATH="$TMP/curl-stub:$PATH" PI_MODELS_JSON="$TMP/models.json" "$ROOT/scripts/tier-probe.sh" test-tier >/dev/null 2>&1; then
	check "tier-probe reports an unreachable endpoint" 1 "it claimed healthy"
else
	check "tier-probe reports an unreachable endpoint" 0
fi
PI_MODELS_JSON="$TMP/models.json" "$ROOT/scripts/tier-probe.sh" no-such-tier >/dev/null 2>&1
[ $? = 2 ]
check "tier-probe exits 2 on an unknown provider" $?

# A 401 means the endpoint is UP and the probe lacked a key the server accepts.
# Calling that an outage would withhold work for a probe limitation.
stub_curl 401
PATH="$TMP/curl-stub:$PATH" PI_MODELS_JSON="$TMP/models.json" GORGE_TARGET_REPO="$TMP/target" \
	"$ROOT/scripts/tier-probe.sh" test-tier >"$TMP/probe401.txt" 2>&1
check "tier-probe treats 401 as reachable, not an outage" $? "$(cat "$TMP/probe401.txt")"
grep -q 'reachable' "$TMP/probe401.txt"
check "tier-probe says the endpoint was reachable on 401" $?
stub_curl 503
if PATH="$TMP/curl-stub:$PATH" PI_MODELS_JSON="$TMP/models.json" "$ROOT/scripts/tier-probe.sh" test-tier >/dev/null 2>&1; then
	check "tier-probe still fails a 503" 1 "it claimed healthy"
else
	check "tier-probe still fails a 503" 0
fi

# --- 8. the parked-ticket hold follows the REPO's policy, not the seed's taste
mkdir -p "$TARGET/.agentctl"
cat >"$STUB/agentctl/__main__.py" <<'PY'
import sys, pathlib
args = sys.argv[1:]
rec = pathlib.Path(__file__).parent.parent / "filed.txt"
if args and args[0] == "status":
    print("daemon: running  paused: no  fleet stop: no  paid seats: on")
    print("queue: new=0 briefed=0 merged=10  (depth 0, human_needed 2)")
elif args and args[0] == "issue" and args[1] == "add":
    title = args[args.index("--title") + 1]
    with rec.open("a") as f:
        f.write(title + "\n")
    print("filed stub")
else:
    print("")
PY
printf 'hold_new_while_parked = true\n' >"$TARGET/.agentctl/config.toml"
sed -i '1i [policy]' "$TARGET/.agentctl/config.toml"
rm -rf "$GORGE_REWARD_DIR/markers"
"$ROOT/scripts/seed-agent.sh" --no-probe --cap 2 >"$TMP/cycle8.log" 2>&1
grep -q 'parked ticket(s) come first' "$TMP/cycle8.log"
check "hold_new_while_parked=true holds new work" $? "$(tail -4 "$TMP/cycle8.log")"
printf '[policy]\nhold_new_while_parked = false\n' >"$TARGET/.agentctl/config.toml"
"$ROOT/scripts/seed-agent.sh" --no-probe --cap 2 >"$TMP/cycle9.log" 2>&1
grep -q 'NOT holding new work' "$TMP/cycle9.log"
check "hold_new_while_parked=false does not hold new work" $? "$(tail -4 "$TMP/cycle9.log")"

# --- 9. a candidate the generators no longer produce is retired, not filed
# Inject a candidate whose id no generator will emit; the next cycle must mark
# it stale and must not file it.
printf '%s\n' '{"id":"flow-mergefix-rate-99","axis":"flow","title":"stale: rate is 99%","body":"# stale\n\n## Done means\n\nnothing\n","est_delta":99,"est_cost":0.1,"evidence":"old","status":"open"}' \
	>>"$GORGE_REWARD_DIR/candidates.jsonl"
rm -rf "$GORGE_REWARD_DIR/markers"
before=$(wc -l <"$STUB/filed.txt")
"$ROOT/scripts/seed-agent.sh" --no-probe --cap 2 >"$TMP/cycle10.log" 2>&1
grep -q 'retired as stale' "$TMP/cycle10.log"
check "the cycle reports retiring stale candidates" $? "$(grep 'candidate backlog' "$TMP/cycle10.log")"
python3 -c "
import json,sys
rows=[json.loads(l) for l in open('$GORGE_REWARD_DIR/candidates.jsonl') if l.strip()]
c=[r for r in rows if r['id']=='flow-mergefix-rate-99']
assert c and c[0]['status']=='stale', c
" 2>"$TMP/stale.err"
check "the stale candidate is marked stale" $? "$(cat "$TMP/stale.err")"
! grep -q 'stale: rate is 99' "$STUB/filed.txt"
check "the stale candidate was never filed" $? "$(cat "$STUB/filed.txt" | tr '\n' '|')"

# --- 10. failure rows with NO provider probe every configured tier instead of
#         filing a ticket that names no tier at all.
printf '[policy]\nhold_new_while_parked = false\n\n[[tiers]]\nname = "t"\nprovider = "test-tier"\nmodel = "m"\n' \
	>"$TARGET/.agentctl/config.toml"
# A journal holding ONLY unattributed failures, so the fallback is what runs.
{
	for _ in $(seq 4); do
		printf '{"ts":"%s","kind":"transition","issue_id":"m1","evidence":{"to":"merged"}}\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
	done
	for _ in $(seq 12); do
		printf '{"ts":"%s","kind":"provider_failure","evidence":{}}\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
	done
} >"$TARGET/.ds4/orchestrator/journal.jsonl"
rm -rf "$GORGE_REWARD_DIR/markers"
stub_curl 200
before=$(wc -l <"$STUB/filed.txt")
PATH="$TMP/curl-stub:$PATH" PI_MODELS_JSON="$TMP/models.json" \
	"$ROOT/scripts/seed-agent.sh" --no-probe --cap 2 >"$TMP/cycle11.log" 2>&1
grep -q 'name no provider' "$TMP/cycle11.log"
check "unattributed failures fall back to probing every tier" $? "$(tail -6 "$TMP/cycle11.log")"
! grep -q 'Seat tier  is failing' "$STUB/filed.txt"
check "no ticket is filed naming an empty tier" $? "$(cat "$STUB/filed.txt" | tr '\n' '|')"
stub_curl 503
rm -rf "$GORGE_REWARD_DIR/markers"
PATH="$TMP/curl-stub:$PATH" PI_MODELS_JSON="$TMP/models.json" \
	"$ROOT/scripts/seed-agent.sh" --no-probe --cap 2 >"$TMP/cycle12.log" 2>&1
grep -q 'Seat tier test-tier' "$STUB/filed.txt"
check "the down tier found by the fallback probe IS named" $? "$(tail -6 "$TMP/cycle12.log")"

printf '\n%s failure(s)\n' "$fails"
[ "$fails" = 0 ]
