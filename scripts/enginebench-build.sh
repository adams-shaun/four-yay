#!/usr/bin/env bash
# enginebench-build.sh REV [GO_BUILD_ARGS...]: build enginebench at REV and print the
# binary's path. REV "." is the working tree as it stands (uncommitted edits
# included); anything else is a git revision, exported with git archive into
# $BENCH_DIR/src/<sha> and built there, so no checkout or worktree is touched.
# Extra arguments go to go build (for example -tags enginebench_az, or
# -ldflags "-X github.com/adams-shaun/gorge/rules.derivedMemoVerifyFlag=1").
set -euo pipefail
. "$(dirname "$0")/enginebench-lib.sh"
rev=${1:?usage: enginebench-build.sh REV [GO_BUILD_ARGS...]}
shift
mkdir -p "$BENCH_DIR/bin"
suffix=""
if [ $# -gt 0 ]; then
	suffix=-$(printf '%s ' "$@" | sha1sum | cut -c1-8)
fi
if [ "$rev" = . ]; then
	label=wt-$(git -C "$REPO" rev-parse --short=12 HEAD)
	if [ -n "$(git -C "$REPO" status --porcelain -- '*.go' go.mod)" ]; then
		label=$label-dirty
	fi
	out=$BENCH_DIR/bin/enginebench-$label$suffix
	(cd "$REPO" && go build "$@" -o "$out" ./cmd/enginebench)
else
	sha=$(git -C "$REPO" rev-parse --verify "$rev^{commit}")
	short=${sha:0:12}
	out=$BENCH_DIR/bin/enginebench-$short$suffix
	if [ ! -x "$out" ]; then
		src=$BENCH_DIR/src/$short
		if [ ! -f "$src/go.mod" ]; then
			mkdir -p "$src.tmp"
			git -C "$REPO" archive "$sha" | tar -x -C "$src.tmp"
			mv "$src.tmp" "$src"
		fi
		(cd "$src" && go build "$@" -o "$out" ./cmd/enginebench)
	fi
fi
echo "$out"
