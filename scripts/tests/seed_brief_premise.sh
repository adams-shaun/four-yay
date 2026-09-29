#!/usr/bin/env bash
# seed_brief_premise.sh — the storm brief must MEASURE a cause, never assert one.
#
# Why this test exists (2026-09-29): the storm block in scripts/seed-agent.sh
# hard-coded a remembered cause -- "a vLLM model-id rename makes every launch
# return no output" -- into EVERY storm brief. A live storm that day was an
# upstream 503 that had already recovered; the served id matched the tier model
# in all four places (TOML, local-seat.env, models.json, /v1/models), so the
# brief sent a whole round chasing a rename that did not exist. A storm is a
# symptom; the seat's first job is to find which cause is present.
#
# This test pins the contract on the text the seed actually writes via
# --brief-file: it may name the signals to read (served id vs tier model, a
# live probe, the journal's last failure time) but must not assert a cause.
#
#   scripts/tests/seed_brief_premise.sh
set -uo pipefail

ROOT=$(git rev-parse --show-toplevel)
TMP=$(mktemp -d ${SEED_SMOKE_TMP:-/tmp}/seed-premise.XXXXXX)
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
{
	for _ in $(seq 12); do
		printf '{"ts":"%s","kind":"provider_failure","evidence":{"provider":"test-tier"}}\n' \
			"$(date -u +%Y-%m-%dT%H:%M:%SZ)"
	done
} >"$TARGET/.ds4/orchestrator/journal.jsonl"

# --- a stubbed agentctl that STORES the --brief-file it is handed, so the
#     brief's own text can be read back. `status` reports a healthy queue.
STUB=$TMP/agentctl
mkdir -p "$STUB/agentctl"
# __init__.py makes this a REGULAR package so it shadows the installed agentctl
# pin, which is also on PYTHONPATH. Without it both are namespace portions and
# `-m agentctl` resolves the pin's __main__ instead of this stub.
: >"$STUB/agentctl/__init__.py"
cat >"$STUB/agentctl/__main__.py" <<'PY'
import pathlib, shutil, sys
args = sys.argv[1:]
base = pathlib.Path(__file__).parent.parent
if args and args[0] == "status":
    print("repo: t  config: .agentctl/config.toml")
    print("daemon: running  paused: no  fleet stop: no  paid seats: on")
    print("queue: new=0 briefed=0 merged=0  (depth 0, human_needed 0)")
elif args and args[0] == "issue" and args[1] == "add":
    title = args[args.index("--title") + 1]
    (base / "titles.txt").open("a").write(title + "\n")
    body = args[args.index("--brief-file") + 1]
    # keep the LAST brief filed (the storm one, filed after any opportunity)
    shutil.copyfile(body, base / "last-brief.md")
    print("filed stub")
else:
    print("")
PY

export GORGE_ROOT=$ROOT
export GORGE_TARGET_REPO=$TARGET
export GORGE_REWARD_DIR=$TMP/reward
export AGENTCTL_DIR=$STUB
export PROBE_START_FLOOR_MB=1 HEAVY_START_FLOOR_MB=1
# `python3 -m agentctl` resolves modules on sys.path, and an installed agentctl
# pin (~/.agentctl/pins/current) shadows a bare `cd $STUB`. Prepend the stub so
# the stub actually runs -- seed_smoke.sh's own stub is shadowed this way today.
export PYTHONPATH="$STUB${PYTHONPATH:+:$PYTHONPATH}"

# The probe must agree the storm is live, or the block that writes the brief is
# skipped. A curl stub answering 000 (unreachable) makes --no-probe unnecessary
# and forces STORM_LIVE=1 regardless of the real network.
mkdir -p "$TMP/curl-stub"
printf '#!/usr/bin/env bash\necho 000\n' >"$TMP/curl-stub/curl"
chmod +x "$TMP/curl-stub/curl"
printf '{"providers":{"test-tier":{"baseUrl":"http://stubbed.invalid/v1"}}}\n' >"$TMP/models.json"
export PI_MODELS_JSON="$TMP/models.json"

PATH="$TMP/curl-stub:$PATH" "$ROOT/scripts/seed-agent.sh" --no-probe --cap 2 \
	>"$TMP/cycle.log" 2>&1

# PRECONDITION: the storm brief was actually written. Without this the whole
# test passes vacuously with the storm block unregistered or skipped.
[ -s "$STUB/last-brief.md" ]
check "the storm brief was written" $? "log: $(tail -6 "$TMP/cycle.log" | tr '\n' '|')"
[ -s "$STUB/titles.txt" ] && grep -q 'Seat tier test-tier' "$STUB/titles.txt"
check "the storm brief is the tier ticket" $? "$(cat "$STUB/titles.txt" 2>/dev/null | tr '\n' '|')"

B=$STUB/last-brief.md
# It must name the measurement signals.
grep -qi 'Measure the cause' "$B"
check "the brief says to measure the cause" $? "$(head -6 "$B" 2>/dev/null | tr '\n' '|')"
grep -q '/v1/models' "$B"
check "the brief names the live endpoint probe" $?
grep -qi 'SERVED model id' "$B"
check "the brief names the served-id-vs-tier check" $?
grep -qi 'HISTORY' "$B"
check "the brief warns a stopped storm is history" $?

# ...and must NOT assert the remembered cause as fact. The rename must appear
# only as a candidate to CHECK, never as the headline cause. The headline is
# "Measure the cause before changing anything; do not assume one."
grep -qi 'Known cause class' "$B" && \
	check "the brief does not assert a known cause class" 1 "still says 'Known cause class'" || \
	check "the brief does not assert a known cause class" 0
grep -qi 'a vLLM model-id rename makes every' "$B" && \
	check "the brief does not state the rename as fact" 1 "still asserts the rename" || \
	check "the brief does not state the rename as fact" 0

printf '\n%s failure(s)\n' "$fails"
[ "$fails" = 0 ]
