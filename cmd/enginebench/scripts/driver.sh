#!/usr/bin/env bash
# driver.sh <rep> <group>: one repetition of one row group, builds interleaved
# (order rotated per rep and row). Output: raw/results.jsonl, raw/load.log.
set -uo pipefail
cd /mnt/sata/gorge-training/enginecmp
rep=$1; group=$2
BUILDS=(a4af596 e0496f062 dev 9803b0655 1e27720d0 churn-snap)
OUT=raw/results.jsonl
case $group in
  random) ROWS=("random -pair A -secs 10" "random -pair B -secs 10" "random -pair burn -secs 10");;
  bot) ROWS=("bot -pair A -secs 10" "bot -pair B -secs 10" "bot -pair A -autopay -secs 10" "bot -pair B -autopay -secs 10");;
  copy) ROWS=("clone -copies 5000" "step -copies 5000");;
  search) ROWS=("sampler -secs 15" "az -sims 100 -secs 15");;
  az1000) ROWS=("az -sims 1000 -secs 30");;
esac
ri=0
for row in "${ROWS[@]}"; do
  echo "$(date +%FT%T) rep=$rep row=[$row] $(uptime)" >> raw/load.log
  n=${#BUILDS[@]}; off=$(( (rep + ri) % n ))
  for i in $(seq 0 $((n-1))); do
    b=${BUILDS[$(( (i + off) % n ))]}
    if [ "$b" = a4af596 ] && [[ "$row" == az* ]]; then continue; fi
    ./run1.sh "$b" "$OUT" -rep "$rep" -row $row > /dev/null 2>> raw/errors.log || echo "FAIL $b rep=$rep $row" >> raw/errors.log
  done
  ri=$((ri+1))
done
echo "done rep=$rep group=$group $(date +%T)"
