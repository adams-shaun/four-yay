#!/usr/bin/env bash
# seed-agent.sh — one cycle of the reward loop's caretaker.
#
# Rubric: docs/agents/seed-rubric.md (that file is the judgement; this file is
# the mechanism). Design:
# docs/superpowers/specs/2026-09-29-reward-loop-and-seed-agent-design.md §4.
#
#   scripts/seed-agent.sh [--dry-run] [--cap N] [--no-probe]
#
# It runs read-only against the main checkout: it never switches branches,
# never commits there, and writes only under .ds4/reward/ and through
# agentctl's CLI. Nothing in it waits on a model — a ticket that needs prose is
# filed with --triage so the pipeline's own triage seat writes the brief. That
# is deliberate: the loop has to survive its seat tier being down, which it was
# on the day this was written.
set -uo pipefail

ROOT=${GORGE_ROOT:-$(git rev-parse --show-toplevel)}
TARGET=${GORGE_TARGET_REPO:-/home/sadams/projects/gorge}
STATE=${GORGE_REWARD_DIR:-$TARGET/.ds4/reward}
AGENTCTL=${AGENTCTL_DIR:-/home/sadams/projects/agentctl}
JOURNAL=$STATE/seed-journal.jsonl
MARKERS=$STATE/markers
CAP=${SEED_TICKET_CAP:-3}
DRY=0
PROBE=1
while [ $# -gt 0 ]; do
	case $1 in
	--dry-run) DRY=1 ;;
	--no-probe) PROBE=0 ;;
	--cap)
		CAP=${2:?--cap needs a number}
		shift
		;;
	--cap=*) CAP=${1#--cap=} ;;
	*)
		printf 'seed-agent: unknown option %s\n' "$1" >&2
		exit 2
		;;
	esac
	shift
done
case $CAP in
'' | *[!0-9]*)
	printf 'seed-agent: --cap must be a number, got %s\n' "$CAP" >&2
	exit 2
	;;
esac

mkdir -p "$STATE" "$MARKERS"
now() { date -u +%Y-%m-%dT%H:%M:%SZ; }
# The script owns its own log file rather than the unit owning it: systemd does
# not create the parent of an `append:` target, and that failed the unit before
# it ever ran (209/STDOUT, 2026-09-29). Here the directory exists by the time
# anything is written.
say() {
	local line
	line="[seed $(date -u +%H:%M:%SZ)] $*"
	printf '%s\n' "$line"
	printf '%s\n' "$line" >>"$STATE/seed.log" 2>/dev/null || true
}

HEAD_SHA=$(git -C "$TARGET" rev-parse --short HEAD 2>/dev/null || echo unknown)
SAW=()   # facts, one string each
DID=()   # actions taken
SKIPPED=() # judgement calls NOT to act, with the reason

saw() { SAW+=("$1"); say "saw: $1"; }
did() { DID+=("$1"); say "did: $1"; }
skipped() { SKIPPED+=("$1"); say "skipped: $1"; }

json_array() { # json_array "${arr[@]}" -> ["a","b"]
	local first=1 x
	printf '['
	for x in "$@"; do
		[ $first = 1 ] || printf ','
		first=0
		printf '%s' "$(printf '%s' "$x" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))')"
	done
	printf ']'
}

# marker_new <key> is true the FIRST time a key is seen for this head, so one
# event files one ticket however many cycles observe it.
marker_new() {
	local f=$MARKERS/$HEAD_SHA-$1
	[ -e "$f" ] && return 1
	: >"$f"
	return 0
}

# file_ticket <priority> <title> <brief-body-file>.
#
# Deliberately NOT --triage. Every brief this script files is already complete
# (goal, out of scope, and a "Done means" naming a real command), so routing it
# through the triage tier would add a round and a dependency on a seat tier that
# can be down -- it was down, on bm-llms-glm, during the first live cycle. A
# ticket filed with a complete brief starts at `briefed` and goes straight to an
# implementer.
file_ticket() {
	local prio=$1 title=$2 body=$3
	if [ "$DRY" = 1 ]; then
		did "DRY-RUN would file P$prio: $title"
		return 0
	fi
	local out
	if out=$(cd "$AGENTCTL" && python3 -m agentctl issue add "$TARGET" \
		--title "$title" --brief-file "$body" --priority "$prio" 2>&1); then
		did "filed P$prio: $title ($(printf '%s' "$out" | tail -1))"
	else
		did "FAILED to file P$prio: $title -- $(printf '%s' "$out" | tail -1)"
	fi
}

brief_file() { mktemp "${TMPDIR:-/tmp}/seed-brief.XXXXXX.md"; }

# ---------------------------------------------------------------- 1. stability
"$ROOT/scripts/broker.sh" enforce >/dev/null 2>&1
BSTATUS=$("$ROOT/scripts/broker.sh" status 2>/dev/null | head -1)
saw "broker: $BSTATUS"

if [ "$PROBE" = 1 ]; then
	GORGE_REWARD_DIR=$STATE GORGE_TARGET_REPO=$TARGET "$ROOT/scripts/reward-probe.sh" free >/dev/null 2>&1 &&
		saw "free axes measured" || saw "free probe FAILED"
fi

SCORE_JSON=$(python3 "$ROOT/scripts/reward.py" --ledger "$STATE/scoreboard.jsonl" --json 2>/dev/null)
read_score() { printf '%s' "$SCORE_JSON" | python3 -c "
import json,sys
d=json.load(sys.stdin) if sys.stdin.isatty() is False else {}
k=sys.argv[1]
cur=d
for part in k.split('.'):
    cur=(cur or {}).get(part) if isinstance(cur,dict) else None
print('' if cur is None else cur)" "$1" 2>/dev/null; }

BLOCKED=$(read_score blocked_by_stability)
SCORE=$(read_score score)
saw "reward score ${SCORE:-n/a} (stability veto: ${BLOCKED:-n/a})"

if [ "$BLOCKED" = "True" ]; then
	"$ROOT/scripts/broker.sh" pause-all heavy >/dev/null 2>&1
	did "paused every heavy lease (stability veto active)"
	if marker_new stability; then
		b=$(brief_file)
		{
			printf '# A stability event is vetoing the reward score\n\n'
			printf 'The reward scorer reports a stability veto at head `%s`.\n\n' "$HEAD_SHA"
			printf 'Evidence (scripts/reward.py --md):\n\n```\n'
			python3 "$ROOT/scripts/reward.py" --ledger "$STATE/scoreboard.jsonl" --md 2>/dev/null
			printf '```\n\n## Goal\n\nFind the cause of the recorded stability event and remove it.\n'
			printf '\n## Out of scope\n\nAny strength, efficiency or coverage work.\n'
			printf '\n## Done means\n\n`scripts/reward.py --md` reports no stability veto on a later head, and\n'
			printf 'the cause is named in the commit message. Heavy leases must be pausable\n'
			printf 'throughout: `scripts/tests/broker_smoke.sh` passes.\n'
		} >"$b"
		file_ticket 1 "Stability veto active at $HEAD_SHA: OOM/gate-timeout in the reward window" "$b"
		rm -f "$b"
	else
		skipped "stability ticket already filed for $HEAD_SHA"
	fi
fi

# ------------------------------------------------------------------- 2. stalls
ST=$(cd "$AGENTCTL" && python3 -m agentctl status "$TARGET" 2>/dev/null)
saw "agentctl: $(printf '%s' "$ST" | sed -n 's/^queue: //p' | head -1)"
DAEMON=$(printf '%s' "$ST" | sed -n 's/^daemon: \([a-z]*\).*/\1/p' | head -1)
HUMAN=$(printf '%s' "$ST" | sed -n 's/.*human_needed \([0-9]*\)).*/\1/p' | head -1)
HUMAN=${HUMAN:-0}
[ "$DAEMON" != running ] && saw "DAEMON IS $DAEMON -- the pipeline is not dispatching"

# A paid-off marker outlives the outage that caused it: the window is hours long
# and nothing re-checks the endpoint. When the marker names a provider whose
# endpoint now answers, the fleet is holding a healthy tier idle. Clearing the
# marker is the operator's action (the permission classifier blocks an agent
# from moving it), so the seed's job is to say so every cycle until it is gone.
PAID_OFF=$(printf '%s' "$ST" | sed -n 's/.*paid seats: OFF for \([^ ]*\) until \([0-9:]*\).*/\1 \2/p' | head -1)
if [ -n "$PAID_OFF" ]; then
	PO_PROV=${PAID_OFF%% *}
	PO_UNTIL=${PAID_OFF##* }
	saw "paid-off marker: $PO_PROV held until $PO_UNTIL"
	if "$ROOT/scripts/tier-probe.sh" "$PO_PROV" >/dev/null 2>&1; then
		skipped "OPERATOR ACTION: $PO_PROV answers now but its paid-off marker holds it until $PO_UNTIL; clearing .ds4/orchestrator/paid-off is yours, not the seed's"
	fi
fi

PROVIDER_STORM=$(python3 - "$TARGET" <<'PY'
import json, sys
from datetime import datetime, timedelta, timezone
p = f"{sys.argv[1]}/.ds4/orchestrator/journal.jsonl"
cut = datetime.now(timezone.utc) - timedelta(minutes=30)
n, providers = 0, set()
try:
    with open(p) as f:
        for line in f:
            try:
                d = json.loads(line)
                ts = datetime.fromisoformat(str(d.get("ts", "")).replace("Z", "+00:00"))
            except Exception:
                continue
            if ts.tzinfo is None:
                ts = ts.replace(tzinfo=timezone.utc)
            if ts >= cut and d.get("kind") in ("provider_failure", "endpoint_down"):
                n += 1
                pr = (d.get("evidence") or {}).get("provider")
                if pr:
                    providers.add(pr)
except FileNotFoundError:
    pass
print(f"{n}\t{','.join(sorted(providers))}")
PY
)
STORM_N=$(printf '%s' "$PROVIDER_STORM" | cut -f1)
STORM_P=$(printf '%s' "$PROVIDER_STORM" | cut -f2)
[ "${STORM_N:-0}" -gt 0 ] && saw "provider failures in the last 30min: $STORM_N ($STORM_P)"

QUIET=0 # queue new work this cycle?
# A storm in the journal is HISTORY. Believe it only when a live probe agrees:
# on 2026-09-29 the free tier came back during a redeploy window and this cycle
# still read 87 failures from the previous half hour, suppressed new work and
# filed a P1 about a tier that was already answering. One curl settles it.
STORM_LIVE=1
if [ "${STORM_N:-0}" -ge 10 ]; then
	# Some failure rows carry no provider in their evidence, and the first
	# version filed a ticket titled "Seat tier is failing every launch" that
	# named no tier at all -- unactionable by construction. With no provider to
	# probe, probe every tier the TOML configures instead: if they all answer,
	# the storm is historical; if one is down, that is the tier to name.
	PROBES=${STORM_P//,/ }
	if [ -z "$PROBES" ]; then
		PROBES=$(python3 - "$TARGET/.agentctl/config.toml" <<'PY' 2>/dev/null
import sys, tomllib
try:
    cfg = tomllib.load(open(sys.argv[1], "rb"))
except (OSError, ValueError):
    sys.exit(0)
seen = []
for t in cfg.get("tiers") or []:
    p = t.get("provider")
    if p and p not in seen:
        seen.append(p)
print(" ".join(seen))
PY
		)
		saw "the failure rows name no provider; probing every configured tier instead"
	fi
	STORM_LIVE=0
	STORM_DOWN=""
	for prov in $PROBES; do
		PROBE_OUT=$("$ROOT/scripts/tier-probe.sh" "$prov" 2>&1)
		PROBE_RC=$?
		saw "tier probe: $PROBE_OUT"
		# rc 1 = the endpoint is down. rc 2 = the provider could not even be
		# resolved, so the probe knows NOTHING and the failure rows are the only
		# evidence there is: stay cautious and keep withholding.
		if [ "$PROBE_RC" != 0 ]; then
			STORM_LIVE=1
			STORM_DOWN=${STORM_DOWN:-$prov}
		fi
	done
	if [ "$STORM_LIVE" = 0 ]; then
		skipped "the storm on ${STORM_P:-an unnamed provider} is historical -- every probed tier answers now, so work is NOT withheld for it"
	else
		STORM_P=$STORM_DOWN
	fi
fi
if [ "${STORM_N:-0}" -ge 10 ] && [ "$STORM_LIVE" = 1 ]; then
	QUIET=1
	skipped "no new tickets: $STORM_N provider failures in 30min on $STORM_P -- queueing into a dead tier only grows the backlog"
	if marker_new "storm-$STORM_P"; then
		b=$(brief_file)
		{
			# The cause is NOT asserted here. Earlier this block named a
			# remembered cause ("a vLLM model-id rename"); on 2026-09-29 that
			# sent a whole round chasing a rename that did not exist while an
			# upstream 503 that had ALREADY recovered was the real story. A
			# storm is a symptom, and the seat's first job is to measure
			# which cause is present -- so the brief names the signals to
			# read, not a conclusion to assume.
			printf '# The %s seat tier is failing every launch\n\n' "$STORM_P"
			printf '%s provider_failure/endpoint_down entries in the last 30 minutes at head `%s`.\n\n' "$STORM_N" "$HEAD_SHA"
			printf 'Measure the cause before changing anything; do not assume one. In order:\n\n'
			printf '1. Is the endpoint answering NOW? `curl -s $endpoint/v1/models` (the baseUrl\n'
			printf '   is in pi\x27s models.json and the TOML tier; a 5xx/`000` means the storm\n'
			printf '   may already be over).\n'
			printf '2. Does the SERVED model id equal the tier model? A vLLM/SGLang rename makes\n'
			printf '   every launch return no output while auth still reads ready -- compare the\n'
			printf '   id from step 1 against the TOML tier `model` and `~/.ds4/local-seat.env`.\n'
			printf '3. What does the journal say the failures were, and when was the LAST one?\n'
			printf '   `endpoint_down` with a recent 5xx transcript is an upstream outage, not a\n'
			printf '   config error; a storm that stopped appending is HISTORY.\n'
			printf '\n## Done means\n\nA launched seat produces an assistant turn with output, and the journal\n'
			printf 'stops appending provider_failure for this provider.\n'
		} >"$b"
		file_ticket 1 "Seat tier $STORM_P is failing every launch ($STORM_N failures/30min)" "$b"
		rm -f "$b"
	fi
fi
if [ "${HUMAN:-0}" -gt 0 ]; then
	saw "$HUMAN ticket(s) in human_needed"
	# Whether a parked ticket holds new work is the REPO's decision, not the
	# seed's: gorge set policy.hold_new_while_parked = false deliberately after
	# measuring 686 of 1440 minutes on hold in a day. The seed shipped with a
	# stricter rule of its own and promptly held every reward ticket behind
	# three parked ones -- an orchestrator that overrides a measured operator
	# decision is not steering, it is guessing.
	HOLD_PARKED=$(python3 - "$TARGET/.agentctl/config.toml" <<'PY' 2>/dev/null
import sys, tomllib
try:
    cfg = tomllib.load(open(sys.argv[1], "rb"))
except (OSError, ValueError):
    print("true")  # no config to read: keep the cautious behaviour
else:
    print("true" if (cfg.get("policy") or {}).get("hold_new_while_parked", True) else "false")
PY
	)
	if [ "${HOLD_PARKED:-true}" = true ]; then
		QUIET=1
		skipped "no new tickets: $HUMAN parked ticket(s) come first (policy.hold_new_while_parked)"
	else
		skipped "$HUMAN parked ticket(s) noted but NOT holding new work (policy.hold_new_while_parked = false)"
	fi
fi

# --------------------------------------------------- 3 & 4. regressions + work
CANDS=$STATE/candidates.jsonl
python3 "$ROOT/scripts/seed_candidates.py" --repo "$TARGET" --state-dir "$STATE" \
	--ledger "$STATE/scoreboard.jsonl" >"$STATE/.candidates.new" 2>/dev/null
REFRESH=$(python3 - "$CANDS" "$STATE/.candidates.new" <<'PY' 2>/dev/null
import json, sys

cands_path, fresh_path = sys.argv[1], sys.argv[2]


def read(p):
    out = []
    try:
        for line in open(p):
            line = line.strip()
            if line:
                try:
                    out.append(json.loads(line))
                except ValueError:
                    pass
    except FileNotFoundError:
        pass
    return out


fresh = read(fresh_path)
fresh_by_id = {c["id"]: c for c in fresh if c.get("id")}
held = read(cands_path)

# A candidate's id encodes the measurement that produced it, so a candidate the
# current generation no longer produces is STALE: its evidence has moved. Filing
# it anyway sends a brief carrying a number that is no longer true -- which
# happened once, a ticket titled "merge_fix rate is 77%" filed minutes after the
# metric was corrected to 24.6%. A stale candidate is retired, not queued.
out, new, stale = [], 0, 0
for c in held:
    cid = c.get("id")
    if c.get("status") in ("open", None) and cid not in fresh_by_id:
        c["status"] = "stale"
        stale += 1
    elif c.get("status") in ("open", None) and cid in fresh_by_id:
        # The id is stable across branch-set churn, but the measurement under
        # it is not: an OPEN row whose id the fresh generation still produces
        # gets its title/body/est_delta/evidence refreshed so ranking and the
        # filed brief name the CURRENT holders, not the ones from the cycle
        # the candidate was first minted. queued/done/stale rows are never
        # rewritten -- their history is the point.
        f = fresh_by_id[cid]
        for k in ("title", "body", "est_delta", "est_cost", "evidence"):
            if k in f:
                c[k] = f[k]
    out.append(c)
have = {c.get("id") for c in out}
for cid, c in fresh_by_id.items():
    if cid not in have:
        out.append(c)
        new += 1
with open(cands_path, "w") as f:
    for c in out:
        f.write(json.dumps(c) + "\n")
print(f"{len(out)}\t{new}\t{stale}")
PY
)
TOTAL=$(printf '%s' "$REFRESH" | cut -f1)
NEW=$(printf '%s' "$REFRESH" | cut -f2)
STALE=$(printf '%s' "$REFRESH" | cut -f3)
rm -f "$STATE/.candidates.new"
saw "candidate backlog: ${TOTAL:-0} total, ${NEW:-0} new, ${STALE:-0} retired as stale this cycle"

if [ "$QUIET" = 1 ]; then
	skipped "opportunity step skipped this cycle (see the reason above)"
else
	filed=0
	while IFS=$'\t' read -r cid caxis ctitle cbody; do
		[ "$filed" -ge "$CAP" ] && {
			skipped "ticket cap $CAP reached; $cid and the rest stay in the backlog"
			break
		}
		marker_new "cand-$cid" || {
			skipped "$cid already filed"
			continue
		}
		b=$(brief_file)
		printf '%b' "$cbody" >"$b"
		# 1000x axes outrank the rest, which is the whole point of the weights.
		case $caxis in
		correct | flow) prio=1 ;;
		steward) prio=2 ;;
		eff) prio=3 ;;
		*) prio=4 ;;
		esac
		file_ticket "$prio" "$ctitle" "$b"
		rm -f "$b"
		filed=$((filed + 1))
		# Mark the candidate queued so it is never ranked again.
		python3 - "$CANDS" "$cid" <<'PY'
import json, sys
p, cid = sys.argv[1], sys.argv[2]
out = []
for line in open(p):
    line = line.strip()
    if not line:
        continue
    try:
        d = json.loads(line)
    except ValueError:
        continue
    if d.get("id") == cid:
        d["status"] = "queued"
    out.append(json.dumps(d))
open(p, "w").write("\n".join(out) + "\n")
PY
		# A generator that cannot produce ticket rows is a defect in the loop
		# itself, so its stderr is kept and reported rather than swallowed --
		# a silently empty candidate list looks exactly like "nothing to do"
		# and hid a real argument bug here once.
	done < <(python3 "$ROOT/scripts/seed_candidates.py" --repo "$TARGET" --state-dir "$STATE" \
		--ledger "$STATE/scoreboard.jsonl" --emit-tickets --top "$CAP" 2>"$STATE/.emit.err")
	if [ -s "$STATE/.emit.err" ]; then
		saw "candidate generator wrote errors: $(head -c 200 "$STATE/.emit.err" | tr '\n' ' ')"
	fi
	[ "$filed" = 0 ] && [ ! -s "$STATE/.emit.err" ] &&
		skipped "no candidate cleared the bar this cycle"
fi

# ------------------------------------------------------------------ 5. journal
{
	printf '{"ts":"%s","git_head":"%s","score":%s,"blocked_by_stability":%s,' \
		"$(now)" "$HEAD_SHA" "${SCORE:-0}" "$([ "$BLOCKED" = True ] && echo true || echo false)"
	printf '"saw":%s,"did":%s,"skipped":%s}\n' \
		"$(json_array "${SAW[@]+"${SAW[@]}"}")" \
		"$(json_array "${DID[@]+"${DID[@]}"}")" \
		"$(json_array "${SKIPPED[@]+"${SKIPPED[@]}"}")"
} >>"$JOURNAL"

{
	printf '# seed agent — last cycle %s (head %s)\n\n' "$(now)" "$HEAD_SHA"
	printf 'Rubric: docs/agents/seed-rubric.md\n\n'
	python3 "$ROOT/scripts/reward.py" --ledger "$STATE/scoreboard.jsonl" --md 2>/dev/null
	printf '\n## Saw\n\n'
	for x in "${SAW[@]+"${SAW[@]}"}"; do printf -- '- %s\n' "$x"; done
	printf '\n## Did\n\n'
	if [ ${#DID[@]} -eq 0 ]; then printf -- '- nothing: no action was warranted\n'; else
		for x in "${DID[@]}"; do printf -- '- %s\n' "$x"; done
	fi
	printf '\n## Decided not to\n\n'
	if [ ${#SKIPPED[@]} -eq 0 ]; then printf -- '- nothing withheld\n'; else
		for x in "${SKIPPED[@]}"; do printf -- '- %s\n' "$x"; done
	fi
	printf '\n## Top of the candidate backlog\n\n```\n'
	python3 "$ROOT/scripts/reward.py" rank --candidates "$STATE/candidates.jsonl" \
		--ledger "$STATE/scoreboard.jsonl" --top 10 2>/dev/null
	printf '```\n'
} >"$STATE/SEED.md"

say "cycle complete: ${#SAW[@]} fact(s), ${#DID[@]} action(s), ${#SKIPPED[@]} withheld -> $STATE/SEED.md"
