#!/bin/sh
# gate-ws.sh CMD [ARGS...] -- run a gate command against the calling
# worktree's HEAD in ONE fixed-path checkout, so the Go build and test caches
# hit across tickets.
#
# Why: Go puts a package's absolute directory into its compile cache key
# (unless -trimpath), and a test's cache key includes the absolute path of
# every repo file it opens. Every ticket gates in its own .worktrees/<id>, so
# before this script every gate recompiled and re-ran all 45 packages (0 cache
# hits, measured 2026-09-27). At one fixed path, a package whose sources,
# deps and opened files are unchanged is served from the cache.
#
# The gate checks out HEAD (commits only): the merge lands commits, so
# uncommitted edits in the seat's worktree are not what gets merged either.
# The workspace is this script's own detached worktree -- never a seat's and
# never the main checkout -- so the forced checkout/clean here is safe.
# Gates run serially under the daemon's tick; the flock covers a hand run.
set -eu

src=$(git rev-parse --show-toplevel)
rel=$(git rev-parse --show-prefix)
sha=$(git rev-parse HEAD)
root=$(cd "$(git rev-parse --git-common-dir)/.." && pwd)
ws=${GORGE_GATE_WS:-/mnt/sata/gorge-training/gate-ws}

exec 9>"$ws.lock"
flock 9

if [ ! -e "$ws/.git" ]; then
	git -C "$root" worktree add -q --detach "$ws" "$sha"
else
	git -C "$ws" checkout -q --detach --force "$sha"
	git -C "$ws" clean -qfd
fi

# The untracked inputs agent-worktree.sh and the pipeline's worktree copy
# list provide: the card corpus and the CR / ledger files under .ds4.
ln -sfn "$root/.cards" "$ws/.cards"
mkdir -p "$ws/.ds4"
for f in ledger.json MagicCompRules-20260807.txt; do
	if [ -e "$src/.ds4/$f" ]; then
		cp -p "$src/.ds4/$f" "$ws/.ds4/$f"
	elif [ -e "$root/.ds4/$f" ]; then
		cp -p "$root/.ds4/$f" "$ws/.ds4/$f"
	fi
done

# go test does not cache a result that opened a file modified in the last
# two seconds; files the checkout just rewrote would miss once for nothing.
sleep 2

cd "$ws/$rel"
exec "$@"
