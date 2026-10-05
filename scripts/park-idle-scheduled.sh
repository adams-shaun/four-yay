#!/usr/bin/env bash
# park-idle-scheduled.sh -- the scheduled, host-side idle-worktree park.
#
#   scripts/park-idle-scheduled.sh            # dry run: print what would be parked
#   APPLY=1 scripts/park-idle-scheduled.sh    # park, and append the decision log
#
# Meant for a host timer (see the ticket agent-20261004T231016Z-c2e86e7f
# report for the suggested systemd user timer), so idle worktrees are parked
# on a schedule instead of by whichever seat happens to act, and the
# idle_unmerged_branches flow axis stops re-filing tickets about them.
#
# It narrows `park-branch.sh --all-idle` to branches the pipeline is done
# with: a `wt/<id>` branch is parked only when .ds4/issues/<id>.md says
# merged or superseded, or when no ticket file exists for it. A ticket that is
# still live (new, briefed, dispatched, waiting, landing, human_needed, ...)
# keeps its worktree: the daemon or the operator still works in it.
# Everything else (dirty, busy, landed, mid-rebase refusals; the branch is
# kept) is park-branch.sh's own safety, unchanged.
#
# MIN_IDLE_H (default 12) is the idle threshold; the log goes to
# .ds4/park-idle.log in the main checkout.
set -euo pipefail

APPLY=${APPLY:-0}
MIN_IDLE_H=${MIN_IDLE_H:-12}
root=$(git rev-parse --path-format=absolute --git-common-dir)
root=${root%/.git}
issues="$root/.ds4/issues"
log="$root/.ds4/park-idle.log"
park=${PARK:-$(cd "$(dirname "$0")" && pwd)/park-branch.sh}

# ticket_status <id> prints the status: line of the ticket's front matter.
ticket_status() {
	local f="$issues/$1.md"
	[ -f "$f" ] || return 0
	/usr/bin/awk '/^---$/ { n++; next } n == 1 && /^status:/ { print $2; exit }' "$f"
}

candidates=()
while read -r line; do
	case "$line" in
	"would park: "*) ;;
	*) continue ;;
	esac
	branch=${line#would park: }
	branch=${branch%% *}
	id=${branch#wt/}
	st=$(ticket_status "$id")
	case "$st" in
	"" | merged | superseded) candidates+=("$branch") ;;
	*) echo "keep (ticket $id is $st): $branch" ;;
	esac
done < <(APPLY=0 "$park" --all-idle "$MIN_IDLE_H" 2>/dev/null || true)

if [ ${#candidates[@]} -eq 0 ]; then
	echo "nothing to park"
	exit 0
fi

status=0
out=$(APPLY="$APPLY" "$park" "${candidates[@]}" 2>&1) || status=$?
echo "$out"
if [ "$APPLY" = 1 ]; then
	{
		echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) park-idle-scheduled (idle >= ${MIN_IDLE_H}h, exit $status)"
		echo "$out" | sed 's/^/  /'
	} >>"$log"
fi
exit "$status"
