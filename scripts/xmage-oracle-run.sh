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
# XMAGE_ORACLE_HEAP its heap (default 10g).
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

cd "$tests"
exec systemd-run --user --scope --quiet -p MemoryMax="${XMAGE_ORACLE_MEM:-12G}" -- \
  java -Xmx"${XMAGE_ORACLE_HEAP:-10g}" -Dlog4j.configuration=file:/dev/null -cp "$classes:$cp" \
  org.mage.test.oracle.ScenarioReplay "$in" "$out" >"$out.log" 2>&1
