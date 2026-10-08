#!/usr/bin/env bash
# readopt-worktree.sh — re-register an orphaned worktree that still has a branch.
#
#   scripts/readopt-worktree.sh <name>          # dry run
#   APPLY=1 scripts/readopt-worktree.sh <name>  # recreate the metadata dir
#
# A worktree whose gitdir target (<git-common-dir>/worktrees/<name>/) was
# deleted by an over-eager park or prune keeps its directory and its `.git`
# file, but every git command in it dies with `fatal: not a git repository`.
# `cleanup.sh worktrees` classifies that shape as `[orphaned dir]` and, under
# APPLY=1, deletes the directory -- which is right for a dead branch and wrong
# for a live seat whose uncommitted tree is still wanted.
#
# This script is the safe counterpart: it recreates the metadata dir
# (gitdir -> the worktree's `.git` file, commondir -> ../..,
# HEAD -> refs/heads/wt/<name>) and rebuilds the index from HEAD, leaving the
# working tree (committed and uncommitted changes alike) untouched. It never
# clobbers an existing registration and never touches the branch.
#
# Refuses when: the worktree directory or its `.git` file is missing, the
# metadata dir already exists, the worktree's `.git` file points somewhere
# other than this name's metadata dir, or refs/heads/wt/<name> does not exist.
set -euo pipefail

APPLY=${APPLY:-0}
root=$(git rev-parse --path-format=absolute --git-common-dir)
root=${root%/.git}
name=${1:?usage: readopt-worktree.sh <name>}
wt="$root/.worktrees/$name"
md="$root/.git/worktrees/$name"
branch="wt/$name"

die() { printf 'readopt-worktree: %s\n' "$*" >&2; exit 1; }

[ -d "$wt" ] || die "no worktree directory: $wt"
[ -e "$wt/.git" ] || die "no .git file in $wt (not a worktree)"
# Never clobber: an existing metadata dir is a registered, live worktree.
[ ! -e "$md" ] || die "already registered (metadata dir exists): $md"
# The worktree's .git file must name exactly the metadata dir we are about to
# recreate; anything else is a shape this script does not own.
named=$(sed -n 's/^gitdir: //p' "$wt/.git")
[ "$named" = "$md" ] || die "$wt/.git names '$named', expected '$md'"
git -C "$root" rev-parse --verify -q "refs/heads/$branch" >/dev/null ||
	die "no branch refs/heads/$branch to re-adopt"

if [ "$APPLY" != 1 ]; then
	echo "would readopt: $wt -> $md (branch $branch, working tree untouched)"
	exit 0
fi

mkdir -p "$md"
printf '%s\n' "$wt/.git" >"$md/gitdir"
printf '../..\n' >"$md/commondir"
printf 'ref: refs/heads/%s\n' "$branch" >"$md/HEAD"
# Rebuild the index from HEAD without touching the working tree. `git status`
# before this reads an empty index and reports every tracked file as staged for
# deletion; a mixed reset restores the true committed state.
git -C "$wt" reset -q
echo "readopted: $md (branch $branch, working tree untouched)"
