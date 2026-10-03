#!/bin/sh
# usage: run.sh BENCH_DIR LOGDIR   (runs an unrated bench run with sbagent stderr into LOGDIR)
set -e
mkdir -p "$2"
cd /mnt/sata/gorge-training/sb-bridge2/spellbench
SBAGENT_LOGDIR="$2" /mnt/sata/gorge-training/searchbench/heavy.sh env SBAGENT_LOGDIR="$2" PYTHONPATH=python python3 -m spellbench.arena.cli bench run "$1" --unrated --placement "$(cat ../placement.txt)"
