#!/usr/bin/env bash
# Check that one malformed scenario fails its own row and the batch goes on,
# without starting H2 or a game. Requires the read-only XMage build, just like
# the driver's javac check.
set -euo pipefail
root=${XMAGE_ORACLE_DIR:-/mnt/sata/gorge-training/xmageoracle}
out=${1:-.ds4/scratch/harness-row-driverout}
here=$(cd "$(dirname "$0")/.." && pwd)
export JAVA_HOME="$root/jdk"
export PATH="$JAVA_HOME/bin:$PATH"
tests="$root/mage/Mage.Tests"
cp="$tests/target/test-classes:$tests/target/classes:$(< "$root/tests.cp")"
mkdir -p "$out"
javac -J-Xmx512m -nowarn -d "$out" -cp "$cp" \
  "$here/tools/xmageoracle/src/org/mage/test/oracle/ScenarioReplay.java" \
  "$here/tools/xmageoracle/test/org/mage/test/oracle/ScenarioReplayHarnessRowTest.java"
java -Xmx512m -XX:ActiveProcessorCount=2 -Dlog4j.configuration=file:/dev/null -cp "$out:$cp" \
  org.mage.test.oracle.ScenarioReplayHarnessRowTest
