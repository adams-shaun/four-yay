#!/usr/bin/env bash
# readopt_worktree_smoke.sh — prove readopt-worktree.sh re-registers an orphan.
#
# A seat whose metadata dir was deleted by an over-eager park keeps its
# worktree directory and its branch, but every git command in it dies. The
# re-adopt path recreates .git/worktrees/<name> so the seat can commit again,
# WITHOUT touching the working tree (including uncommitted changes) or the
# branch. This smoke manufactures the orphan exactly as cleanup_orphan_smoke.sh
# does, then asserts: the dry run changes nothing; APPLY=1 makes `git status`
# work again, re-lists the branch in `git worktree list`, preserves an
# uncommitted file, and rebuilds the index (so a committed file is not reported
# as staged for deletion); a second run refuses (never clobbers); and a name
# with no branch ref is refused.
#
#   scripts/tests/readopt_worktree_smoke.sh
set -uo pipefail

ROOT=$(git rev-parse --show-toplevel)
READOPT=$ROOT/scripts/readopt-worktree.sh
TMP=$(mktemp -d /tmp/readopt-smoke.XXXXXX)
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

# A disposable repo: one commit on main, a worktree with one commit of its own
# plus an uncommitted file (must survive re-adoption).
R=$TMP/repo
mkdir -p "$R"
git -C "$R" init -q -b main
git -C "$R" -c user.name=t -c user.email=t@t commit -q --allow-empty -m base
git -C "$R" worktree add -q -b wt/n "$R/.worktrees/n" main
echo committed >"$R/.worktrees/n/f"
git -C "$R/.worktrees/n" add f
git -C "$R/.worktrees/n" -c user.name=t -c user.email=t@t commit -q -m "n work"
echo uncommitted >"$R/.worktrees/n/u"

# Manufacture the orphan exactly as a park leaves it: metadata dir deleted by
# hand, worktree directory and its .git file stay.
MD=$R/.git/worktrees/n
rm -rf "$MD"

# Preconditions the later assertions depend on.
[ -e "$R/.worktrees/n/.git" ]
check "orphan's .git file exists (precondition)" $?
[ ! -d "$MD" ]
check "orphan's metadata dir is really missing (precondition)" $?
git -C "$R" rev-parse --verify -q refs/heads/wt/n >/dev/null
check "branch wt/n exists (precondition)" $?
git -C "$R/.worktrees/n" status --porcelain >/dev/null 2>&1
[ $? -ne 0 ]
check "git status in the orphan fails before re-adoption (precondition)" $?

readopt() ( cd "$R" && APPLY=${APPLY:-0} bash "$READOPT" "$1" )

# --- 1. DRY RUN reports and changes nothing.
readopt n >"$TMP/dry.txt" 2>&1
check "dry run exits 0" $? "$(cat "$TMP/dry.txt")"
grep -q 'would readopt' "$TMP/dry.txt"
check "dry run says would readopt" $? "$(cat "$TMP/dry.txt")"
[ ! -d "$MD" ]
check "dry run created no metadata dir" $?

# --- 2. APPLY=1 re-registers: git status works, branch re-listed, working tree
# intact, index rebuilt from HEAD.
APPLY=1 readopt n >"$TMP/apply.txt" 2>&1
check "apply exits 0" $? "$(cat "$TMP/apply.txt")"
[ -d "$MD" ]
check "apply created the metadata dir" $?
for f in gitdir commondir HEAD; do
	[ -s "$MD/$f" ]
	check "apply wrote $f" $? "$MD/$f"
done
grep -qx "ref: refs/heads/wt/n" "$MD/HEAD"
check "HEAD names the branch" $?
git -C "$R/.worktrees/n" status --porcelain >"$TMP/status.txt" 2>&1
check "git status in the re-adopted worktree succeeds" $? "$(cat "$TMP/status.txt")"
grep -qx '?? u' "$TMP/status.txt"
check "the uncommitted file survives and is the only change" $? "$(cat "$TMP/status.txt")"
[ -f "$R/.worktrees/n/f" ]
check "the committed working-tree file is intact" $?
git -C "$R" worktree list | grep -q '\[wt/n\]'
check "git worktree list re-lists wt/n" $?

# --- 3. A second run refuses and never clobbers.
head_before=$(cat "$MD/HEAD")
APPLY=1 readopt n >"$TMP/second.txt" 2>&1
[ $? -ne 0 ]
check "second run is refused (nonzero)" $? "$(cat "$TMP/second.txt")"
grep -qi 'already registered' "$TMP/second.txt"
check "second-run refusal says already registered" $?
[ "$(cat "$MD/HEAD")" = "$head_before" ]
check "second run did not clobber HEAD" $?

# --- 4. A name with no branch ref is refused and no metadata dir is created.
mkdir -p "$R/.worktrees/ghost"
printf 'gitdir: %s\n' "$R/.git/worktrees/ghost" >"$R/.worktrees/ghost/.git"
git -C "$R" rev-parse --verify -q refs/heads/wt/ghost >/dev/null
[ $? -ne 0 ]
check "precondition: no branch wt/ghost exists" $?
APPLY=1 readopt ghost >"$TMP/ghost.txt" 2>&1
[ $? -ne 0 ]
check "branchless orphan is refused (nonzero)" $? "$(cat "$TMP/ghost.txt")"
grep -qi 'no branch' "$TMP/ghost.txt"
check "branchless refusal names the missing branch" $?
[ ! -d "$R/.git/worktrees/ghost" ]
check "branchless refusal created no metadata dir" $?

printf '\n%s failure(s)\n' "$fails"
[ "$fails" = 0 ]
