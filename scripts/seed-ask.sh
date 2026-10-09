#!/usr/bin/env bash
# seed-ask.sh — queue a one-off research/exploration ask into the pipeline.
#
# The seed agent (scripts/seed-agent.sh) runs a fixed deterministic rubric on a
# timer; it has no model and cannot take an ad-hoc prompt. This is the other
# door: `make seed-ask PROMPT="..."` files a ticket through the same agentctl
# pipeline the seed and the operator both use, so an open research question
# gets a triage seat, a brief, an implementer and a reviewer like any other
# ticket -- it does not bypass the gates just because a human typed it.
#
#   scripts/seed-ask.sh "<prompt>" [--priority N] [--kind K] [--repo PATH]
#
# The raw prompt is wrapped with this repo's own evidence discipline (a
# measurable Done means, explicit scope) so a seat cannot answer with prose
# alone -- "show me, with data" is enforced by the brief, not left to the
# seat's judgement. Filed with --triage: an open research question is rarely a
# complete brief, and the triage seat's job is to turn it into one.
set -uo pipefail

ROOT=${GORGE_ROOT:-$(git -C "$(dirname "$0")/.." rev-parse --show-toplevel)}
AGENTCTL=${AGENTCTL_DIR:-/home/sadams/projects/agentctl}
PROMPT=${1:?usage: seed-ask.sh "<prompt>" [--priority N] [--kind K] [--repo PATH]}
shift || true

PRIORITY=2
KIND=research
REPO=$ROOT
while [ $# -gt 0 ]; do
	case $1 in
	--priority)
		PRIORITY=$2
		shift 2
		;;
	--kind)
		KIND=$2
		shift 2
		;;
	--repo)
		REPO=$2
		shift 2
		;;
	*)
		printf 'seed-ask: unknown option %s\n' "$1" >&2
		exit 2
		;;
	esac
done

TITLE=$(printf '%s' "$PROMPT" | tr '\n' ' ' | cut -c1-90)
[ "${#PROMPT}" -gt 90 ] && TITLE="$TITLE..."

BRIEF=$(mktemp "${TMPDIR:-/tmp}/seed-ask.XXXXXX.md")
trap 'rm -f "$BRIEF"' EXIT
{
	printf '# %s\n\n' "$TITLE"
	printf '## Ask (verbatim)\n\n%s\n\n' "$PROMPT"
	printf '## Ground rules for this class of ask\n\n'
	printf 'This is a research/planning/measurement ticket, not a feature ticket. It may\n'
	printf 'still need a small amount of code (a bot variant, a botbench flag, a scoring\n'
	printf 'script, a dashboard page) to produce the deliverable, but read the existing\n'
	printf 'implementation and infra first and reuse it -- this repo already has SpellBench\n'
	printf 'gauntlet tooling (`scripts/sb-gauntlet.sh`, `scripts/spellbench-rate.py`,\n'
	printf '`cmd/botbench`), the reward loop'"'"'s own scoreboard\n'
	printf '(`.ds4/reward/scoreboard.jsonl`, `scripts/reward.py`), and a CR 103 mulligan\n'
	printf 'flow (`rules/mulligan.go`, `botpolicy/policy.go` `case decision.KMulligan`).\n'
	printf 'Do not re-derive what is already there, and do not build a parallel harness\n'
	printf 'for something an existing tool already measures.\n\n'
	printf '## Out of scope\n\n'
	printf 'Training an RL model from scratch. Building a new bot policy tier. Any change\n'
	printf 'to `main` outside a reproducible measurement/plan and its written report, unless\n'
	printf 'the ask itself names a concrete artifact (e.g. a dashboard) to build.\n\n'
	printf '## Done means\n\n'
	printf 'A written report (docs/superpowers/reports/ or the ticket report) that:\n\n'
	printf '1. States what exists today precisely, with file:line citations.\n'
	printf '2. Proposes something concrete and nameable -- a rule, a plan, a dashboard\n'
	printf '   layout -- specific enough that a reviewer could implement or verify it from\n'
	printf '   the report alone.\n'
	printf '3. Where the ask calls for a measurement: MEASURE it with real games (seat-\n'
	printf '   swapped pairs, enough games for a real confidence interval) and report a\n'
	printf '   win rate / Elo delta with a CI, not a point estimate. A null or negative\n'
	printf '   result is a valid answer; an unsupported claim of +EV is not.\n'
	printf '4. Where the ask calls for a plan or a dashboard: the plan names the exact\n'
	printf '   data source for every number it shows (no placeholder charts), and a\n'
	printf '   dashboard proposal says which existing ledger/log it reads.\n'
	printf '5. Names the resource cost of the work (games played, wall time, files\n'
	printf '   touched) so this ask'"'"'s own cost is visible next to its payoff.\n\n'
	printf '## Brief context and test budget (operator, 2026-10-05)\n\n'
	printf 'The triage seat turns this ask into a brief that carries: measured facts with\n'
	printf 'the command and numbers behind them; the exact file:line of the mechanism; the\n'
	printf 'commands to run and their caps; what is already ruled out; and who is working\n'
	printf 'nearby (live tickets/branches on the same files). Unverified premises go under\n'
	printf 'a `Hypothesis:` label. Every test fits 4 GB RSS, 4 vCPU, 1 min wall and runs\n'
	printf 'focused and capped:\n'
	printf '`systemd-run --user --scope -q -p MemoryMax=4G -p CPUQuota=400%% env GOMAXPROCS=4 GOMEMLIMIT=3GiB go test -timeout 2m -run X ./pkg`.\n'
	printf 'Never `go test ./...`, `./compliance/...`, `-count=1`, full sweeps or long\n'
	printf 'seeded runs; a measurement that needs many games uses smoke-sized runs under\n'
	printf 'the same cap and says how the CI was obtained.\n'
} >"$BRIEF"

cd "$AGENTCTL" && python3 -m agentctl issue add "$REPO" \
	--title "$TITLE" --brief-file "$BRIEF" --kind "$KIND" --priority "$PRIORITY" --triage
