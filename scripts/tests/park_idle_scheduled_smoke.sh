#!/usr/bin/env bash
# park_idle_scheduled_smoke.sh -- prove park-idle-scheduled.sh only parks
# worktrees the pipeline is done with.
#
# A throwaway repo gets three idle, clean, unmerged worktrees: one whose
# ticket is still live (briefed), one whose ticket is superseded, and one with
# no ticket file. The dry run must change nothing; the apply run must park the
# superseded and ticketless ones (branches kept), keep the live one, and append
# the decision log.
#
#   scripts/tests/park_idle_scheduled_smoke.sh
set -uo pipefail

ROOT=$(git rev-parse --show-toplevel)
SCHED=$ROOT/scripts/park-idle-scheduled.sh
TMP=$(mktemp -d /tmp/park-idle-smoke.XXXXXX)
fails=0
check() {
	if [ "$2" = 0 ]; then
		printf 'ok   %s\n' "$1"
	else
		printf 'FAIL %s %s\n' "$1" "${3:-}"
		fails=$((fails + 1))
	fi
}
trap 'rm -rf "$TMP"' EXIT

R=$TMP/repo
mkdir -p "$R/.ds4/issues"
git -C "$R" init -q -b main
git -C "$R" -c user.name=t -c user.email=t@t commit -q --allow-empty -m base
for id in live done orphan; do
	git -C "$R" worktree add -q -b "wt/$id" "$TMP/wt/$id" main
	git -C "$TMP/wt/$id" -c user.name=t -c user.email=t@t commit -q --allow-empty -m "$id work"
	touch -d '2 days ago' "$TMP/wt/$id"
done
printf -- '---\nid: live\nstatus: briefed\n---\n' >"$R/.ds4/issues/live.md"
printf -- '---\nid: done\nstatus: superseded\n---\n' >"$R/.ds4/issues/done.md"

run() ( cd "$R" && PARK=$ROOT/scripts/park-branch.sh APPLY=${APPLY:-0} bash "$SCHED" )

run >"$TMP/dry.txt" 2>&1
grep -q 'keep (ticket live is briefed): wt/live' "$TMP/dry.txt"
check "dry run keeps the live ticket's worktree" $? "$(cat "$TMP/dry.txt")"
grep -q 'would park: wt/done' "$TMP/dry.txt" && grep -q 'would park: wt/orphan' "$TMP/dry.txt"
check "dry run would park the superseded and ticketless worktrees" $? "$(cat "$TMP/dry.txt")"
[ "$(git -C "$R" worktree list | wc -l)" = 4 ]
check "dry run parked nothing" $?
[ ! -e "$R/.ds4/park-idle.log" ]
check "dry run writes no log" $?

APPLY=1 run >"$TMP/apply.txt" 2>&1
check "apply exits 0" $? "$(cat "$TMP/apply.txt")"
git -C "$R" worktree list | grep -q '\[wt/live\]'
check "live worktree kept" $?
if git -C "$R" worktree list | grep -qE '\[wt/(done|orphan)\]'; then x=1; else x=0; fi
check "superseded and ticketless worktrees parked" "$x" "$(git -C "$R" worktree list)"
git -C "$R" rev-parse -q --verify refs/heads/wt/done >/dev/null && git -C "$R" rev-parse -q --verify refs/heads/wt/orphan >/dev/null
check "parked branches kept" $?
grep -q 'park-idle-scheduled' "$R/.ds4/park-idle.log"
check "apply appends the decision log" $?

printf '\n%s failure(s)\n' "$fails"
[ "$fails" = 0 ]
