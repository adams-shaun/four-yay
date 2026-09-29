#!/usr/bin/env bash
# park_branch_smoke.sh — prove park-branch.sh's safety contract on a real repo.
#
# The flow axis counts every unmerged worktree idle over 12h, because each one
# keeps holding its files against every branch that lands after it. Parking
# releases those files by removing the worktree and keeping the branch. This is
# the test a reviewer runs to believe the removal is safe: it builds a throwaway
# repo with worktrees and asserts that a dry run touches nothing, a dirty
# worktree is refused (a seat often finishes without committing), a live process
# inside is refused, a clean idle worktree is parked with its branch kept, and a
# worktree whose branch is already an ancestor of main is left to cleanup.sh.
#
#   scripts/tests/park_branch_smoke.sh
set -uo pipefail

ROOT=$(git rev-parse --show-toplevel)
PARK=$ROOT/scripts/park-branch.sh
TMP=$(mktemp -d /tmp/park-smoke.XXXXXX)
fails=0

check() {
	if [ "$2" = 0 ]; then
		printf 'ok   %s\n' "$1"
	else
		printf 'FAIL %s %s\n' "$1" "${3:-}"
		fails=$((fails + 1))
	fi
}
cleanup() {
	pkill -P $$ 2>/dev/null || true
	rm -rf "$TMP"
}
trap cleanup EXIT

# A disposable repo: one commit on main, three branches checked out into three
# worktrees, one of them dirty, one of them already merged.
R=$TMP/repo
mkdir -p "$R"
git -C "$R" init -q -b main
git -C "$R" -c user.name=t -c user.email=t@t commit -q --allow-empty -m base
for b in clean dirty landed; do
	git -C "$R" worktree add -q -b "wt/$b" "$TMP/wt/$b" main
done
echo work >"$TMP/wt/clean/file"
git -C "$TMP/wt/clean" add file
git -C "$TMP/wt/clean" -c user.name=t -c user.email=t@t commit -q -m "clean work"
# dirty must be one commit AHEAD of main (unmerged) and then have an
# uncommitted change on top -- a worktree with no commits of its own is an
# ancestor of main and is cleanup.sh's business, not parking's.
git -C "$TMP/wt/dirty" -c user.name=t -c user.email=t@t commit -q --allow-empty -m "dirty work"
echo uncommitted >"$TMP/wt/dirty/file"
git -C "$TMP/wt/landed" -c user.name=t -c user.email=t@t commit -q --allow-empty -m "landed work"
git -C "$R" branch -f main "wt/landed" 2>/dev/null || git -C "$R" merge -q --ff-only "wt/landed"
# Age the non-dirty worktrees so --all-idle sees them.
touch -d '2 days ago' "$TMP/wt/clean" "$TMP/wt/landed" "$TMP/wt/dirty" 2>/dev/null || true

park() ( cd "$R" && APPLY=${APPLY:-0} bash "$PARK" "$@" )

# --- 1. dry run touches nothing
park wt/clean >"$TMP/dry.txt" 2>&1
grep -q 'would park' "$TMP/dry.txt"
check "dry run says would park" $? "$(cat "$TMP/dry.txt")"
git -C "$R" worktree list | grep -q '\[wt/clean\]'
check "dry run left the worktree registered" $?

# --- 2. a dirty worktree is refused before anything else
park wt/dirty >"$TMP/dirty.txt" 2>&1
[ $? -ne 0 ]
check "dirty worktree is refused (nonzero)" $? "$(cat "$TMP/dirty.txt")"
grep -qi 'WIP-commit' "$TMP/dirty.txt"
check "refusal says WIP-commit first" $?
git -C "$R" worktree list | grep -q '\[wt/dirty\]'
check "dirty worktree stays registered" $?

# --- 3. a live process cwd'd inside is refused
( cd "$TMP/wt/clean" && exec sleep 30 ) &
sleeper=$!
for _ in $(seq 20); do
	[ -e "/proc/$sleeper/cwd" ] && break
	sleep 0.1
done
park wt/clean >"$TMP/busy.txt" 2>&1
[ $? -ne 0 ]
check "worktree with a process inside is refused" $? "$(cat "$TMP/busy.txt")"
grep -qi 'process inside' "$TMP/busy.txt"
check "refusal says process inside" $?
kill "$sleeper" 2>/dev/null
wait "$sleeper" 2>/dev/null

# --- 4. a clean idle worktree is parked; the branch is kept
APPLY=1 park wt/clean >"$TMP/apply.txt" 2>&1
grep -q 'parked' "$TMP/apply.txt"
check "apply reports parked" $? "$(cat "$TMP/apply.txt")"
if git -C "$R" worktree list | grep -q '\[wt/clean\]'; then gone=1; else gone=0; fi
check "parked worktree is gone from git worktree list" "$gone"
git -C "$R" rev-parse --verify -q refs/heads/wt/clean >/dev/null
check "the branch is kept" $?

# --- 5. --all-idle leaves a landed branch to cleanup.sh
park --all-idle 1 >"$TMP/all.txt" 2>&1 || true
git -C "$R" worktree list | grep -q '\[wt/landed\]'
check "--all-idle does not park a branch already in main" $?
grep -q 'wt/dirty' "$TMP/all.txt"
check "--all-idle lists the dirty candidate but refuses it" $? "$(cat "$TMP/all.txt")"

# --- 6. main is never parked
park main >"$TMP/main.txt" 2>&1
grep -q 'skip (main)' "$TMP/main.txt"
check "main is refused" $?
git -C "$R" worktree list | grep -q '\[main\]'
check "main stays registered" $?

printf '\n%s failure(s)\n' "$fails"
[ "$fails" = 0 ]
