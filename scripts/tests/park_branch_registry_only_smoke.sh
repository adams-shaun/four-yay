#!/usr/bin/env bash
# park_branch_registry_only_smoke.sh — prove park-branch.sh --registry-only.
#
# A seat's bubblewrap jail mounts every sibling worktree read-only, so the
# default APPLY=1 park dies on `git worktree remove`'s final rm -rf (EROFS)
# even though the registry entry in the writable shared .git is already gone
# and the branch is kept. --registry-only codifies that outcome on purpose:
# it removes <git-common-dir>/worktrees/<id> directly, leaves the directory
# on disk for the host's `make clean-worktrees`, and refuses anything with an
# in-progress rebase/merge (whose state lives in that metadata dir).
#
# The bwrap case below binds the target directory read-only inside a real
# jail so the EROFS condition the flag exists for is actually exercised; the
# writable-repo cases assert the same registry-only semantics without it.
#
#   scripts/tests/park_branch_registry_only_smoke.sh
set -uo pipefail

ROOT=$(git rev-parse --show-toplevel)
PARK=$ROOT/scripts/park-branch.sh
TMP=$(mktemp -d /tmp/park-registry.XXXXXX)
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

# A disposable repo: one commit on main, worktrees on unmerged branches.
R=$TMP/repo
mkdir -p "$R"
git -C "$R" init -q -b main
git -C "$R" -c user.name=t -c user.email=t@t commit -q --allow-empty -m base

# wt/x: clean, unmerged, the registry-only target.
git -C "$R" worktree add -q -b wt/x "$R/.worktrees/x" main
echo work >"$R/.worktrees/x/file"
git -C "$R/.worktrees/x" add file
git -C "$R/.worktrees/x" -c user.name=t -c user.email=t@t commit -q -m "x work"

# wt/dirty: one commit of its own plus an uncommitted change.
git -C "$R" worktree add -q -b wt/dirty "$R/.worktrees/dirty" main
git -C "$R/.worktrees/dirty" -c user.name=t -c user.email=t@t commit -q --allow-empty -m "dirty work"
echo uncommitted >"$R/.worktrees/dirty/file"

# wt/rebase: a clean unmerged worktree made to look mid-rebase by planting
# the metadata state --registry-only would otherwise delete.
git -C "$R" worktree add -q -b wt/rebase "$R/.worktrees/rebase" main
git -C "$R/.worktrees/rebase" -c user.name=t -c user.email=t@t commit -q --allow-empty -m "rebase work"
rmd=$(sed -n 's/^gitdir: //p' "$R/.worktrees/rebase/.git")
[ -n "$rmd" ] && [ -d "$rmd" ]
check "test precondition: wt/rebase metadata dir resolves" $? "$rmd"
mkdir -p "$rmd/rebase-merge"

# wt/landed: committed and merged into main -- cleanup.sh's business.
git -C "$R" worktree add -q -b wt/landed "$R/.worktrees/landed" main
git -C "$R/.worktrees/landed" -c user.name=t -c user.email=t@t commit -q --allow-empty -m "landed work"
git -C "$R" merge -q --ff-only wt/landed

registered() { git -C "$R" worktree list | grep -q '\[wt/'"$1"'\]'; }
# registry_only_park <branch> — run the flag path against the writable repo.
registry_only_park() ( cd "$R" && APPLY=1 bash "$PARK" --registry-only "$1" )

# --- 1. registry-only parks cleanly: registry gone, branch kept, dir left
registry_only_park wt/x >"$TMP/x.txt" 2>&1
check "registry-only park exits 0" $? "$(cat "$TMP/x.txt")"
registered x
check "registry entry is gone from git worktree list" "$((1 - $?))"
git -C "$R" rev-parse --verify -q refs/heads/wt/x >/dev/null
check "the branch is kept" $?
[ -d "$R/.worktrees/x" ]
check "the directory is left on disk" $?
grep -q 'dir for host cleanup' "$TMP/x.txt"
check "output names the directory as left for host cleanup" $? "$(cat "$TMP/x.txt")"
grep -qF -- "$R/.worktrees/x" "$TMP/x.txt"
check "output names the exact directory path" $?

# --- 2. dirty target is refused and stays registered
registry_only_park wt/dirty >"$TMP/dirty.txt" 2>&1
[ $? -ne 0 ]
check "dirty target is refused (nonzero)" $? "$(cat "$TMP/dirty.txt")"
registered dirty
check "dirty target stays registered" $?
[ -d "$R/.worktrees/dirty" ]
check "dirty target directory untouched" $?

# --- 3. a live process cwd'd inside is refused and stays registered
git -C "$R" worktree add -q -b wt/busy "$R/.worktrees/busy" main
git -C "$R/.worktrees/busy" -c user.name=t -c user.email=t@t commit -q --allow-empty -m "busy work"
( cd "$R/.worktrees/busy" && exec sleep 30 ) &
sleeper=$!
for _ in $(seq 20); do
	[ -e "/proc/$sleeper/cwd" ] && break
	sleep 0.1
done
registry_only_park wt/busy >"$TMP/busy.txt" 2>&1
[ $? -ne 0 ]
check "process-inside target is refused (nonzero)" $? "$(cat "$TMP/busy.txt")"
registered busy
check "process-inside target stays registered" $?
kill "$sleeper" 2>/dev/null
wait "$sleeper" 2>/dev/null

# --- 4. an in-progress rebase is refused and stays registered (the state
# --registry-only would delete lives in the metadata dir it removes)
registry_only_park wt/rebase >"$TMP/rebase.txt" 2>&1
[ $? -ne 0 ]
check "in-progress rebase is refused (nonzero)" $? "$(cat "$TMP/rebase.txt")"
grep -qi 'rebase/merge in progress' "$TMP/rebase.txt"
check "refusal names the rebase/merge state" $?
registered rebase
check "rebase target stays registered" $?
[ -e "$rmd/rebase-merge" ]
check "the planted rebase state survives the refusal" $?

# --- 5. a branch already in main is refused in registry-only too
registry_only_park wt/landed >"$TMP/landed.txt" 2>&1
[ $? -ne 0 ]
check "already-landed branch is refused (nonzero)" $? "$(cat "$TMP/landed.txt")"
registered landed
check "already-landed branch stays registered" $?

# --- 6. main is never parked
registry_only_park main >"$TMP/main.txt" 2>&1
grep -q 'skip (main)' "$TMP/main.txt"
check "main is refused in registry-only" $?
git -C "$R" worktree list | grep -q '\[main\]'
check "main stays registered" $?

# --- 7. the EROFS case: the same park under a real read-only bind of the
# target directory, the exact condition that aborts the default path.
BWRAP=$(command -v bwrap || true)
if [ -z "$BWRAP" ]; then
	printf 'skip bwrap EROFS case: bwrap not available\n'
else
	git -C "$R" worktree add -q -b wt/ro "$R/.worktrees/ro" main
	git -C "$R/.worktrees/ro" -c user.name=t -c user.email=t@t commit -q --allow-empty -m "ro work"
	if bwrap --unshare-all --dev-bind / / --ro-bind "$R/.worktrees/ro" "$R/.worktrees/ro" true 2>"$TMP/bwrap-err.txt"; then
		bwrap_ok=1
	else
		bwrap_ok=0
		printf 'skip bwrap EROFS case: bwrap failed to start (%s)\n' "$(tr '\n' ' ' <"$TMP/bwrap-err.txt")"
	fi
	if [ "$bwrap_ok" = 1 ]; then
		bwrap --unshare-all --dev-bind / / --ro-bind "$R/.worktrees/ro" "$R/.worktrees/ro" \
			bash -c 'cd "$1" && APPLY=1 bash "$2" --registry-only wt/ro' _ "$R" "$PARK" \
			>"$TMP/ro.txt" 2>&1
		check "registry-only park exits 0 under a read-only mount" $? "$(cat "$TMP/ro.txt")"
		registered ro
		check "EROFS park: registry entry is gone" "$((1 - $?))"
		git -C "$R" rev-parse --verify -q refs/heads/wt/ro >/dev/null
		check "EROFS park: the branch is kept" $?
		[ -d "$R/.worktrees/ro" ]
		check "EROFS park: the directory is left on disk" $?
		grep -q 'dir for host cleanup' "$TMP/ro.txt"
		check "EROFS park: output names the directory for host cleanup" $? "$(cat "$TMP/ro.txt")"
		# and the host reclaim path sees it as the orphan it now is
		( cd "$R" && APPLY=0 bash "$ROOT/scripts/cleanup.sh" worktrees ) >"$TMP/cleanup.txt" 2>/dev/null || true
		grep -qF -- "$R/.worktrees/ro" "$TMP/cleanup.txt"
		check "host cleanup dry-run reports the leftover as an orphaned dir" $?
		[ -e "$(sed -n 's/^gitdir: //p' "$R/.worktrees/ro/.git")" ]
		check "EROFS park: the leftover's gitdir target is gone (true orphan shape)" "$((1 - $?))"
	else
		# Never silently drop the case: assert the registry-only semantics
		# on a writable repo at least.
		registry_only_park wt/busy >"$TMP/ro-fallback.txt" 2>&1
		check "fallback (writable) registry-only park exits 0" $? "$(cat "$TMP/ro-fallback.txt")"
		registered busy
		check "fallback: registry entry is gone" "$((1 - $?))"
		[ -d "$R/.worktrees/busy" ]
		check "fallback: the directory is left on disk" $?
	fi
fi

printf '\n%s failure(s)\n' "$fails"
[ "$fails" = 0 ]
