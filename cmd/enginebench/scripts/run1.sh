#!/usr/bin/env bash
# run1.sh <label> <out.jsonl> <enginebench args...>: one pinned single-core run.
set -euo pipefail
label=$1; out=$2; shift 2
CPU=${CPU:-9}
exec /mnt/sata/gorge-training/searchbench/heavy.sh taskset -c "$CPU" env GOMAXPROCS=1 \
  /mnt/sata/gorge-training/enginecmp/bin/enginebench-$label -label "$label" -out "$out" \
  -cards /home/sadams/projects/gorge/.cards "$@"
