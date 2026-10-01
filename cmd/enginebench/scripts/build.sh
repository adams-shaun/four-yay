#!/usr/bin/env bash
# build.sh <label> <worktree> [tags]  -- copy the harness into <worktree> and build it.
set -euo pipefail
label=$1; wt=$2; tags=${3:-}
src=/home/sadams/projects/gorge/.worktrees/enginecmp/cmd/enginebench
if [ "$wt" != /home/sadams/projects/gorge/.worktrees/enginecmp ]; then
  rm -rf "$wt/cmd/enginebench"; mkdir -p "$wt/cmd/enginebench"; cp "$src"/*.go "$wt/cmd/enginebench/"
fi
cd "$wt"
go build ${tags:+-tags $tags} -o /mnt/sata/gorge-training/enginecmp/bin/enginebench-$label ./cmd/enginebench
echo "built $label @ $(git rev-parse --short=9 HEAD) tags=[$tags]"
