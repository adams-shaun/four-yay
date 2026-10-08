#!/usr/bin/env bash
# park-branch.sh — remove an idle branch's worktree while KEEPING its branch.
#
#   scripts/park-branch.sh <branch>...        # park these branches
#   scripts/park-branch.sh --all-idle [h]     # every worktree idle >= h hours (default 12)
#   scripts/park-branch.sh --registry-only <branch>...
#                                             # deregister the worktree, LEAVE its
#                                             # directory on disk for the host to
#                                             # reclaim (make clean-worktrees)
#   scripts/park-branch.sh --force <branch>...  # override the lock/live-seat guard
#
# A worktree that is LIVE is never deregistered, on any path: either it is
# `git worktree lock`ed (the documented "do not touch" marker), or its ticket
# in .ds4/issues/<id>.md is still open (any status other than merged/superseded
# or no ticket at all). A seat between shell commands is clean and holds no
# cwd, so the old dirty/busy checks could not see it; this guard can. --force
# is the explicit human override.

# --registry-only is the seat-safe path: a seat's jail mounts every sibling
# worktree read-only, so `git worktree remove` fails its final rm -rf on a
# read-only file system and aborts the script -- even though the registry
# entry (in the writable shared .git) is already gone and the branch is kept.
# --registry-only removes <git-common-dir>/worktrees/<id> directly (the same
# thing git does first) and prunes, leaving the directory for the host; it
# additionally refuses a worktree with an in-progress rebase/merge, because
# deregistering deletes the metadata dir where that state lives.
#
# DRY RUN unless APPLY=1. This is the unmerged counterpart to
# `cleanup.sh worktrees`: cleanup only removes a worktree whose branch is
# already an ancestor of main, so an idle branch nobody landed keeps holding
# its files against every branch that lands after it, and each of those pays a
# resolver round for work nobody is doing. Parking removes the worktree -- that
# is what releases the files -- and keeps the branch, so the work is not lost
# and can be picked up or landed later.
#
# Safety, in order:
#   * a dirty worktree is refused (a seat often finishes without committing;
#     WIP-commit by explicit path first, then park);
#   * a worktree with a running process whose cwd is inside it is refused;
#   * the removal is never forced and the branch is never deleted;
#   * main's checkout is never touched.
set -euo pipefail

APPLY=${APPLY:-0}
MIN_IDLE_H=${MIN_IDLE_H:-12}

say() { if [ "$APPLY" = 1 ]; then echo "parked: $*"; else echo "would park: $*"; fi; }

REGISTRY_ONLY=0
FORCE=0

root=$(git rev-parse --path-format=absolute --git-common-dir)
root=${root%/.git}

# ticket_status <id> prints the status: line of the ticket's front matter, or
# nothing when there is no ticket. park() reads it to refuse a live seat.
ticket_status() {
	local f="$root/.ds4/issues/$1.md"
	[ -f "$f" ] || return 0
	/usr/bin/awk '/^---$/ { n++; next } n == 1 && /^status:/ { print $2; exit }' "$f"
}

# branch_path <branch> prints the worktree path for a checked-out branch, or
# nothing. Read from git, never guessed, so a stale .worktrees/name directory
# is not mistaken for the live one.
branch_path() {
	git -C "$root" worktree list --porcelain | awk -v want="refs/heads/$1" '
		/^worktree / { wt = substr($0, 10) }
		/^branch /   { if (substr($0, 8) == want) { print wt; exit } }'
}

# busy_paths prints every running process's cwd, one per line.
busy_paths() {
	local p
	for p in /proc/[0-9]*; do
		readlink "$p/cwd" 2>/dev/null || true
	done
}

# park <branch-or-path>: the one place a worktree is removed.
park() {
	local branch=$1 wt ref dirty ahead
	wt=$(branch_path "$branch")
	if [ -z "$wt" ]; then
		echo "skip (no worktree): $branch"
		return 0
	fi
	ref=$(git -C "$root" rev-parse --symbolic-full-name "refs/heads/$branch")
	if [ "$ref" = "refs/heads/main" ]; then
		echo "skip (main): $branch"
		return 0
	fi
	dirty=$(git -C "$wt" status --porcelain)
	if [ -n "$dirty" ]; then
		echo "keep (dirty, WIP-commit it first): $wt" >&2
		echo "$dirty" | sed 's/^/    /' >&2
		return 1
	fi
	if grep -qxF -- "$wt" <<<"$(busy_paths)"; then
		echo "keep (process inside): $wt" >&2
		return 1
	fi
	# The metadata dir: computed before any deregistration because it is both
	# the lock marker's home and what --registry-only removes.
	md=$(sed -n 's/^gitdir: //p' "$wt/.git")
	# Live-seat guard. A running seat is often clean and holds no cwd between
	# tool calls, so the dirty/busy checks above cannot see it; these two can.
	if [ "$FORCE" != 1 ]; then
		if [ -n "$md" ] && [ -e "$md/locked" ]; then
			echo "keep (locked, unlock or --force): $wt" >&2
			return 1
		fi
		case "$branch" in
		wt/*)
			# Same rule park-idle-scheduled.sh enforces: only a merged or
			# superseded ticket (or no ticket at all) may be parked. Anything
			# else -- new/briefed/dispatched/waiting/landing/human_needed --
			# means the pipeline still owns this worktree.
			local id st
			id=${branch#wt/}
			st=$(ticket_status "$id")
			case "$st" in
			"" | merged | superseded) ;;
			*)
				echo "keep (ticket $id is $st): $branch"
				return 1
				;;
			esac
			;;
		esac
	fi
	if [ "$REGISTRY_ONLY" = 1 ]; then
		# A landed branch is cleanup.sh's business, never parking's.
		if git -C "$root" merge-base --is-ancestor "refs/heads/$branch" main; then
			echo "keep (already in main): $branch" >&2
			return 1
		fi
		# Deregistering deletes the metadata dir, which is where an
		# in-progress rebase/merge keeps its state (rebase-merge/,
		# rebase-apply/, MERGE_HEAD). Refuse rather than destroy it.
		if [ -z "$md" ]; then
			echo "keep (no worktree metadata dir): $wt" >&2
			return 1
		fi
		if [ -e "$md/rebase-merge" ] || [ -e "$md/rebase-apply" ] || [ -e "$md/MERGE_HEAD" ]; then
			echo "keep (rebase/merge in progress): $wt" >&2
			return 1
		fi
	fi
	ahead=$(git -C "$root" rev-list --count "main..refs/heads/$branch")
	if [ "$REGISTRY_ONLY" = 1 ]; then
		say "(registry only; dir for host cleanup) $branch [$wt] (+$ahead commits ahead, branch kept)"
	else
		say "$branch [$wt] (+$ahead commits ahead of main, branch kept)"
	fi
	if [ "$APPLY" = 1 ]; then
		if [ "$REGISTRY_ONLY" = 1 ]; then
			# Remove the registry metadata directly (what `git worktree
			# remove` does first anyway) and leave $wt on disk for the
			# host's `make clean-worktrees` to reclaim as an orphaned dir;
			# the prune below then drops any leftover entry. Deterministic
			# and independent of how git orders the two halves.
			rm -rf -- "$md"
		else
			git -C "$root" worktree remove "$wt"
		fi
	fi
}

targets=()
args=()
for a in "$@"; do
	if [ "$a" = "--registry-only" ]; then
		REGISTRY_ONLY=1
	elif [ "$a" = "--force" ]; then
		FORCE=1
	else
		args+=("$a")
	fi
done
if [ ${#args[@]} -gt 0 ]; then
	set -- "${args[@]}"
else
	set --
fi
if [ "${1:-}" = "--all-idle" ]; then
	hours=${2:-$MIN_IDLE_H}
	now=$(date +%s)
	while read -r wt; do
		[ -n "$wt" ] || continue
		[ -e "$wt/.git" ] || continue
		branch=$(git -C "$wt" symbolic-ref --quiet --short HEAD) || continue
		[ "$branch" != "main" ] || continue
		if git -C "$root" merge-base --is-ancestor "$branch" main; then
			continue # landed: cleanup.sh's job, not parking
		fi
		mtime=$(stat -c %Y "$wt")
		if [ $(( (now - mtime) / 3600 )) -lt "$hours" ]; then
			echo "skip (only $(( (now - mtime) / 3600 ))h idle < ${hours}h): $branch"
			continue
		fi
		targets+=("$branch")
	done < <(git -C "$root" worktree list --porcelain | awk '/^worktree / { print substr($0, 10) }')
else
	targets=("$@")
fi

if [ ${#targets[@]} -eq 0 ]; then
	echo "nothing to park"
	exit 0
fi

failed=0
for b in "${targets[@]}"; do
	park "$b" || failed=1
done
[ "$APPLY" = 1 ] && git -C "$root" worktree prune
[ "$failed" = 1 ] && exit 1
exit 0
