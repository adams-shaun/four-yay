#!/usr/bin/env bash
# cleanup.sh — reclaim disk from finished agent work. DRY RUN unless APPLY=1.
#
#   scripts/cleanup.sh seat-cache   # ~/.cache/pi-agent/<name>/ per-seat ~/.pi copies
#   scripts/cleanup.sh worktrees    # .worktrees/* whose branch is merged into main
#
# seat-cache: ds4-harness's pi-agent launcher gives every run a private,
# writable copy of ~/.pi at ~/.cache/pi-agent/<name>/.pi (rm -rf + cp -a at
# launch), so a finished seat's directory is pure leftover (~24 MB each,
# thousands of them). Transcripts are NOT here -- they live in the worktree's
# .ds4/pi-sessions. Kept: any directory a running process names (in its
# cmdline or as its cwd), and anything modified in the last MIN_AGE minutes
# (default 30), which covers a launch between its rm and its bwrap bind.
#
# worktrees: removes .worktrees/<id> when its branch is an ancestor of main,
# it has no tracked changes and no untracked (non-ignored) files, and no
# running process has its cwd inside it; then deletes the merged branch with
# `git branch -d`. Ignored run output (.ds4/, bin/) goes with the worktree --
# copy anything you want to keep first. Never forces, never touches main's
# checkout, and never uses pkill/pgrep -f: processes are read from /proc.
set -euo pipefail

APPLY=${APPLY:-0}
MIN_AGE=${MIN_AGE:-30}

say() { if [ "$APPLY" = 1 ]; then echo "remove: $*"; else echo "would remove: $*"; fi; }

# busy_paths prints every running process's cmdline and cwd, one per line.
busy_paths() {
	local p
	for p in /proc/[0-9]*; do
		{ tr '\0' ' ' <"$p/cmdline"; } 2>/dev/null && echo
		readlink "$p/cwd" 2>/dev/null || true
	done
}

seat_cache() {
	local root=$HOME/.cache/pi-agent busy d name n=0 kb=0 s
	[ -d "$root" ] || { echo "no $root"; return 0; }
	busy=$(busy_paths)
	for d in "$root"/*/; do
		d=${d%/}
		name=$(basename "$d")
		if grep -qF -- "pi-agent/$name/" <<<"$busy" || grep -qE -- "--name $name( |\$)" <<<"$busy"; then
			echo "keep (running): $name"
			continue
		fi
		if [ -n "$(find "$d" -maxdepth 0 -mmin "-$MIN_AGE")" ]; then
			echo "keep (<${MIN_AGE}m old): $name"
			continue
		fi
		s=$(du -sk "$d" | cut -f1)
		n=$((n + 1)); kb=$((kb + s))
		[ "$APPLY" = 1 ] && rm -rf -- "$d"
	done
	say "$n seat dirs, $((kb / 1024)) MB, under $root"
}

worktrees() {
	local root wt branch busy n=0 kb=0 s orphans=0 orphan_kb=0 gd
	root=$(git rev-parse --path-format=absolute --git-common-dir)
	root=${root%/.git}
	busy=$(busy_paths)
	for wt in "$root"/.worktrees/*/; do
		wt=${wt%/}
		[ -e "$wt/.git" ] || continue
		# An orphan keeps its `.git` file, but that file's `gitdir:` target (the
		# metadata dir `git worktree remove` deregistered) is gone, so `git -C`
		# would fail and the dir is unreachable by every cleanup path. Reclaim it
		# directly: its registration is already gone, so nothing is de-registered
		# and no branch is deleted -- there is no checked-out branch here any more.
		gd=$(sed -n 's/^gitdir: //p' "$wt/.git")
		if [ -n "$gd" ] && [ ! -d "$gd" ]; then
			if grep -qF -- "$wt" <<<"$busy"; then
				echo "keep (process inside): $wt"
				continue
			fi
			s=$(du -sk "$wt" | cut -f1)
			orphans=$((orphans + 1)); orphan_kb=$((orphan_kb + s))
			say "$wt [orphaned dir]"
			if [ "$APPLY" = 1 ]; then
				if rm -rf -- "$wt" 2>/dev/null; then
					:
				else
					echo "keep (read-only, rm by hand): $wt" >&2
				fi
			fi
			continue
		fi
		branch=$(git -C "$wt" symbolic-ref --quiet --short HEAD) || { echo "keep (detached): $wt"; continue; }
		if ! git -C "$root" merge-base --is-ancestor "$branch" main; then
			continue # unmerged: live work, not reported
		fi
		if [ -n "$(git -C "$wt" status --porcelain)" ]; then
			echo "keep (merged but dirty): $wt"
			continue
		fi
		if grep -qF -- "$wt" <<<"$busy"; then
			echo "keep (process inside): $wt"
			continue
		fi
		s=$(du -sk "$wt" | cut -f1)
		n=$((n + 1)); kb=$((kb + s))
		say "$wt [$branch]"
		if [ "$APPLY" = 1 ]; then
			git -C "$root" worktree remove "$wt"
			git -C "$root" branch -d "$branch" >/dev/null
		fi
	done
	[ "$APPLY" = 1 ] && git -C "$root" worktree prune
	echo "$n merged worktrees, $((kb / 1024)) MB$([ "$APPLY" = 1 ] || echo ' (dry run; APPLY=1 to remove)')"
	echo "$orphans orphaned dirs, $((orphan_kb / 1024)) MB$([ "$APPLY" = 1 ] || echo ' (dry run; APPLY=1 to remove)')"
}

case ${1:-} in
seat-cache) seat_cache ;;
worktrees) worktrees ;;
*)
	echo "usage: [APPLY=1] $0 seat-cache|worktrees" >&2
	exit 2
	;;
esac
