#!/usr/bin/env bash
# xmage-oracle-run.sh IN.jsonl OUT.jsonl
#
# Replays oracle scenarios (one JSON object per line, rules/testdata/oracle
# schema) in XMage through tools/xmageoracle's ScenarioReplay driver and
# writes one snapshot line per scenario. Needs the out-of-tree build that
# scripts/xmage-oracle-setup.sh makes. The driver is compiled against
# Mage.Tests on first use and whenever its source changes.
#
# XMAGE_ORACLE_MEM caps the JVM's systemd scope (default 12G) and
# XMAGE_ORACLE_HEAP its heap (default: 1G under the scope, so a smaller
# scope never carries a heap the kernel would OOM-kill instead of GC).
set -euo pipefail

in=${1:?usage: xmage-oracle-run.sh IN.jsonl OUT.jsonl}
out=${2:?usage: xmage-oracle-run.sh IN.jsonl OUT.jsonl}
in=$(realpath "$in")
out=$(realpath -m "$out")
root=${XMAGE_ORACLE_DIR:-/mnt/sata/gorge-training/xmageoracle}
here=$(cd "$(dirname "$0")/.." && pwd)
src="$here/tools/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java"

export JAVA_HOME="$root/jdk"
export PATH="$JAVA_HOME/bin:$PATH"
tests="$root/mage/Mage.Tests"
[ -d "$tests/target/test-classes" ] || { echo "xmage-oracle-run: no XMage build at $root; run make xmage-oracle-setup" >&2; exit 1; }

cpfile="$root/tests.cp"
if [ ! -s "$cpfile" ]; then
  ( cd "$root/mage" && "$root/maven/bin/mvn" -q -B -pl Mage.Tests dependency:build-classpath \
      -Dmdep.outputFile="$cpfile" -Dmdep.includeScope=test )
fi
cp="$tests/target/test-classes:$tests/target/classes:$(cat "$cpfile")"

classes="$root/driver"
stamp="$classes/.src.sha"
sum=$(sha256sum "$src" | cut -d' ' -f1)
if [ ! -f "$stamp" ] || [ "$(cat "$stamp")" != "$sum" ]; then
  rm -rf "$classes" && mkdir -p "$classes"
  javac -nowarn -d "$classes" -cp "$cp" "$src"
  echo "$sum" > "$stamp"
fi

mem=${XMAGE_ORACLE_MEM:-12G}; mem_g=${mem%[Gg]}
heap=${XMAGE_ORACLE_HEAP:-$(( mem_g > 2 ? mem_g - 1 : 1 ))g}
# Parallel replays (validation/oracle/full-replay.sh, FULL_REPLAY_JOBS) must
# not share XMage's H2 card DB at ./db/cards.h2: H2's auto-server handoff
# fails a JVM at start-up ("Locked by another process", or "Connection
# refused" once the owning JVM exits). XMAGE_ORACLE_PRIVATE_DB=1 runs the
# JVM in a scratch cwd beside OUT whose db/ is a private copy (~300 MB) and
# whose other entries link back to Mage.Tests.
run_dir=$tests
if [ "${XMAGE_ORACLE_PRIVATE_DB:-0}" = 1 ]; then
  run_dir="$out.cwd"; rm -rf "$run_dir"; mkdir -p "$run_dir/db"
  trap 'rm -rf "$run_dir"' EXIT
  for e in "$tests"/*; do [ "$(basename "$e")" = db ] || ln -s "$e" "$run_dir/"; done
  cp --reflink=auto "$tests/db/cards.h2.mv.db" "$run_dir/db/"
fi
cd "$run_dir"
systemd-run --user --scope --quiet -p MemoryMax="$mem" -- \
  java -Xmx"$heap" -Dlog4j.configuration=file:/dev/null -cp "$classes:$cp" \
  org.mage.test.oracle.ScenarioReplay "$in" "$out" >"$out.log" 2>&1
