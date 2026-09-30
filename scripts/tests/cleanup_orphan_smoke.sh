#!/usr/bin/env bash
# cleanup_orphan_smoke.sh — prove cleanup.sh reclaims orphaned .worktrees dirs.
#
# Parking releases a worktree's branch but, inside a seat's bubblewrap jail,
# `git worktree remove` deregisters the worktree and then fails its own rm -rf
# on a peer dir's read-only mount. The directory is left on disk with a `.git`
# file naming a metadata dir that no longer exists. No git command reclaims it
# (`worktree prune` prints nothing; the dir is gone from `worktree list`), and
# cleanup.sh's old `git -C` gate misread it as `keep (detached)` and skipped it.
#
# This smoke builds a throwaway repo, manufactures exactly that orphan, and
# asserts: a dry run reports it without deleting, APPLY=1 reclaims it when the
# mount is writable (the throwaway repo is) without aborting, the orphan's
# branch ref survives (cleanup never deletes a branch for an orphan), and the
# three existing classifications -- merged+clean, unmerged, detached -- are
# unchanged.
#
#   scripts/tests/cleanup_orphan_smoke.sh
set -uo pipefail

ROOT=$(git rev-parse --show-toplevel)
CLEANUP=$ROOT/scripts/cleanup.sh
TMP=$(mktemp -d /tmp/cleanup-orphan-smoke.XXXXXX)
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

# A disposable repo: one commit on main, then four worktrees.
R=$TMP/repo
mkdir -p "$R"
git -C "$R" init -q -b main
git -C "$R" -c user.name=t -c user.email=t@t commit -q --allow-empty -m base
for b in orphan merged unmerged detached; do
	git -C "$R" worktree add -q -b "wt/$b" "$R/.worktrees/$b" main
done

# merged: one commit, fast-forwarded into main.
git -C "$R/.worktrees/merged" -c user.name=t -c user.email=t@t commit -q --allow-empty -m "merged work"
git -C "$R" merge -q --ff-only wt/merged

# unmerged: one commit of its own, not in main.
git -C "$R/.worktrees/unmerged" -c user.name=t -c user.email=t@t commit -q --allow-empty -m "unmerged work"

# detached: a valid metadata dir, just no branch -- must stay `keep (detached)`.
git -C "$R/.worktrees/detached" checkout -q --detach HEAD

# Manufacture the orphan exactly as a failed `git worktree remove` leaves it:
# the metadata dir is deleted by hand, the worktree dir and its `.git` file stay.
rm -rf "$R/.git/worktrees/orphan"

# Precondition checks -- every later assertion depends on these being true.
gd=$(sed -n 's/^gitdir: //p' "$R/.worktrees/orphan/.git")
[ -n "$gd" ]
check "orphan .git file names a gitdir: target" $? "gd='$gd'"
[ ! -d "$gd" ]
check "orphan's named metadata dir is missing" $?
[ -w "$R" ] && [ -w "$R/.worktrees/orphan" ]
check "throwaway repo and orphan dir are writable" $?
git -C "$R" merge-base --is-ancestor wt/merged main
check "merged worktree's branch is an ancestor of main (precondition)" $?
[ -z "$(git -C "$R/.worktrees/merged" status --porcelain)" ]
check "merged worktree is clean (precondition)" $?
git -C "$R" rev-parse --verify -q refs/heads/wt/orphan >/dev/null
check "orphan's branch ref exists before cleanup" $?

cleanup_run() ( cd "$R" && APPLY=${APPLY:-0} bash "$CLEANUP" worktrees )

# --- 1. DRY RUN reports the orphan and deletes nothing.
cleanup_run >"$TMP/dry.txt" 2>&1
grep -q 'would remove: .*/.worktrees/orphan \[orphaned dir\]' "$TMP/dry.txt"
check "dry run reports the orphan as would remove" $? "$(cat "$TMP/dry.txt")"
[ -d "$R/.worktrees/orphan" ]
check "dry run did not delete the orphan dir" $?

# --- 2/3. APPLY=1 reclaims the orphan, does not abort, keeps the branch ref.
APPLY=1 cleanup_run >"$TMP/apply.txt" 2>&1
rc=$?
[ $rc = 0 ]
check "APPLY=1 exits 0 (no abort)" $rc "$(cat "$TMP/apply.txt")"
[ ! -e "$R/.worktrees/orphan" ]
check "APPLY=1 removed the orphan dir" $? "$(cat "$TMP/apply.txt")"
git -C "$R" rev-parse --verify -q refs/heads/wt/orphan >/dev/null
check "orphan's branch ref survives cleanup" $?

# --- 4. a normal merged+clean worktree is still removed, branch deleted.
[ ! -e "$R/.worktrees/merged" ]
check "merged worktree dir removed" $?
git -C "$R" rev-parse --verify -q refs/heads/wt/merged >/dev/null
if [ $? -eq 0 ]; then gone=1; else gone=0; fi
check "merged worktree's branch deleted" "$gone"

# --- 5. a normal unmerged worktree is skipped silently.
[ -d "$R/.worktrees/unmerged" ]
check "unmerged worktree dir untouched" $?
grep -q 'wt/unmerged' "$TMP/apply.txt"
if [ $? -eq 0 ]; then named=1; else named=0; fi
check "unmerged worktree not named in output" "$named"

# --- 6. detached-HEAD worktree with a valid metadata dir stays keep (detached).
[ -d "$R/.worktrees/detached" ]
check "detached worktree dir untouched" $?
grep -q 'keep (detached): .*/.worktrees/detached' "$TMP/apply.txt"
check "detached worktree reported as keep (detached)" $? "$(cat "$TMP/apply.txt")"

printf '\n%s failure(s)\n' "$fails"
[ "$fails" = 0 ]
