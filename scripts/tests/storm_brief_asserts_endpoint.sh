#!/usr/bin/env bash
# storm_brief_asserts_endpoint.sh — assert the provider-storm ticket BRIEF body.
#
# seed_smoke.sh only asserts the storm ticket's TITLE and that the storm
# suppresses opportunity tickets; the brief body itself went unasserted, and on
# 2026-09-29 it carried a false "Known cause class" telling the implementer to
# check a vLLM model-id rename first when the real failure was the serving
# Deployment at replicas: 0. This script drives one storm cycle against a
# throwaway target and a stubbed agentctl that CAPTURES the brief file, then
# asserts the body:
#
#   - tells the implementer to measure the endpoint state FIRST with an HTTP
#     code check (`curl -s -o /dev/null -w '%{http_code}' ...`),
#   - names BOTH classes: a 5xx (or no response) = the engine is not serving
#     (the 2026-09-29 replicas: 0 case as the example), and only a 2xx that
#     still fails points at a model-id rename,
#   - no longer presents the rename as an established "Known cause class".
#
#   scripts/tests/storm_brief_asserts_endpoint.sh
set -uo pipefail

ROOT=$(git rev-parse --show-toplevel)
TMP=$(mktemp -d ${STORM_BRIEF_TMP:-/tmp}/storm-brief.XXXXXX)
fails=0
check() {
	if [ "$2" = 0 ]; then printf 'ok   %s\n' "$1"; else
		printf 'FAIL %s %s\n' "$1" "${3:-}"
		fails=$((fails + 1))
	fi
}
trap 'rm -rf "$TMP"' EXIT

# --- a throwaway target repo whose journal carries a live provider storm
TARGET=$TMP/target
mkdir -p "$TARGET/.ds4/orchestrator" "$TARGET/rules" "$TARGET/internal/testutil/decks"
git -C "$TARGET" init -q 2>/dev/null
printf 'x\n' >"$TARGET/README.md"
git -C "$TARGET" add README.md >/dev/null 2>&1
git -C "$TARGET" -c user.email=t@t -c user.name=t commit -qm init >/dev/null 2>&1
printf '{"name":"D","cards":["Lightning Bolt","Mountain"]}\n' >"$TARGET/internal/testutil/decks/d.json"
: >"$TARGET/rules/acceptance_test.go"
: >"$TARGET/rules/paramcensus_test.go"
{
	for _ in $(seq 12); do
		printf '{"ts":"%s","kind":"provider_failure","evidence":{"provider":"test-tier"}}\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
	done
} >"$TARGET/.ds4/orchestrator/journal.jsonl"

# --- a stubbed agentctl whose `issue add` CAPTURES the brief body it is handed
STUB=$TMP/agentctl
mkdir -p "$STUB/agentctl"
: >"$STUB/agentctl/__init__.py" # python3 -m agentctl needs a real package
cat >"$STUB/agentctl/__main__.py" <<'PY'
import shutil, sys, pathlib
args = sys.argv[1:]
rec = pathlib.Path(__file__).parent.parent / "filed.txt"
if args and args[0] == "status":
    print("repo: t  config: .agentctl/config.toml")
    print("daemon: running  paused: no  fleet stop: no  paid seats: on")
    print("queue: new=0 briefed=0 merged=10  (depth 0, human_needed 0)")
elif args and args[0] == "issue" and args[1] == "add":
    title = args[args.index("--title") + 1]
    body = args[args.index("--brief-file") + 1]
    with rec.open("a") as f:
        f.write(title + "\n")
    shutil.copyfile(body, rec.parent / "captured-brief.md")
    print("filed stub-" + str(len(rec.read_text().splitlines())))
else:
    print("")
PY

export GORGE_ROOT=$ROOT
export GORGE_TARGET_REPO=$TARGET
export GORGE_REWARD_DIR=$TMP/reward
export AGENTCTL_DIR=$STUB
export PROBE_START_FLOOR_MB=1 HEAVY_START_FLOOR_MB=1

# --- one cycle: the storm marker is per-head, so this cycle files the ticket
"$ROOT/scripts/seed-agent.sh" --no-probe --cap 2 >"$TMP/cycle.log" 2>&1
check "the storm cycle exits cleanly" $? "$(tail -3 "$TMP/cycle.log")"
grep -q 'no new tickets' "$TMP/cycle.log"
check "the storm reached the branch (work was suppressed)" $? "$(tail -5 "$TMP/cycle.log")"

# Precondition: the brief under test is the storm brief, actually captured.
CAPTURED=$STUB/captured-brief.md
[ -s "$CAPTURED" ]
check "the stub captured a brief file" $? "captured-brief.md missing or empty"
head -1 "$CAPTURED" | grep -q '^# The test-tier seat tier is failing every launch$'
check "the captured brief is the storm brief (title intact)" $? "$(head -1 "$CAPTURED")"
grep -q 'Seat tier test-tier is failing every launch' "$STUB/filed.txt"
check "the storm ticket was filed" $? "$(cat "$STUB/filed.txt" | tr '\n' '|')"

# The body: measure the endpoint state FIRST, with an HTTP code check.
grep -qF "curl -s -o /dev/null -w '%{http_code}' \$endpoint/v1/models" "$CAPTURED"
check "the brief tells the implementer to check the HTTP code FIRST" $?
# Precondition for the class assertions: the two classes differ in the text.
grep -qi '5xx\|503' "$CAPTURED" && grep -q 'model-id rename' "$CAPTURED"
check "the brief names both classes (a 5xx case AND the rename case)" $?
grep -q 'replicas: 0' "$CAPTURED"
check "the brief cites the measured 2026-09-29 replicas: 0 example" $?
# The old, false framing must be gone entirely.
if grep -q 'Known cause class' "$CAPTURED"; then
	check "the brief no longer presents the rename as a 'Known cause class'" 1 \
		"old phrasing still present in the captured brief"
else
	check "the brief no longer presents the rename as a 'Known cause class'" 0
fi
# The '## Done means' tail of the template is unchanged.
grep -q '## Done means' "$CAPTURED" && grep -q 'A launched seat produces an assistant turn' "$CAPTURED"
check "the template's '## Done means' tail is unchanged" $?

printf '\n%s failure(s)\n' "$fails"
[ "$fails" = 0 ]
