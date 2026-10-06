#!/usr/bin/env bash
# Check the driver's setup-counter P/T: a setup +1/+1 counter must reach the
# snapshot's P/T (layer 7d re-applied), without H2 or a game replay. Requires
# the read-only XMage build, just like the driver's javac check.
set -euo pipefail
root=${XMAGE_ORACLE_DIR:-/mnt/sata/gorge-training/xmageoracle}
out=${1:-.ds4/scratch/setup-counter-out}
here=$(cd "$(dirname "$0")/.." && pwd)
export JAVA_HOME="$root/jdk"
export PATH="$JAVA_HOME/bin:$PATH"
tests="$root/mage/Mage.Tests"
cp="$tests/target/test-classes:$tests/target/classes:$(< "$root/tests.cp")"
mkdir -p "$out"
javac -J-Xmx512m -nowarn -d "$out" -cp "$cp" \
  "$here/tools/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java" \
  "$here/tools/xmageoracle/test/org/mage/test/oracle/ScenarioReplaySetupCounterTest.java"
java -Xmx512m -XX:ActiveProcessorCount=2 -Dlog4j.configuration=file:/dev/null -cp "$out:$cp" \
  org.mage.test.oracle.ScenarioReplaySetupCounterTest
