#!/usr/bin/env bash
# xmage-oracle-setup.sh -- build XMage out of tree for the compliance oracle
# (docs/superpowers/specs/2026-10-02-xmage-compliance-oracle-design.md §4-§5).
#
# Idempotent. Re-run after an XMAGE_REF bump. Everything lands under
# $XMAGE_ORACLE_DIR (default /mnt/sata/gorge-training/xmageoracle); nothing in
# the gorge tree changes. The build and the smoke test each run inside a
# MemoryMax=7G user scope: this box allows 8 vCPU / 8 GB for the whole job.
#
#   scripts/xmage-oracle-setup.sh            # maven + clone + build + smoke + measure
#   scripts/xmage-oracle-setup.sh --smoke    # smoke + measure only (already built)
set -euo pipefail

repo=$(git rev-parse --show-toplevel)
ref=${XMAGE_REF:-$(sed -n 's/^XMAGE_REF *?= *//p' "$repo/Makefile")}
root=${XMAGE_ORACLE_DIR:-$(sed -n 's/^XMAGE_ORACLE_DIR *?= *//p' "$repo/Makefile")}
mvn_version=3.9.11
smoke_test=org.mage.test.cards.abilities.keywords.FlashbackTest
[ -n "$ref" ] && [ -n "$root" ] || { echo "xmage-oracle-setup: XMAGE_REF/XMAGE_ORACLE_DIR unset" >&2; exit 2; }
case "$root" in /tmp/*) echo "xmage-oracle-setup: $root is RAM-backed; use /mnt/sata" >&2; exit 2;; esac
mkdir -p "$root"
mvn="$root/maven/bin/mvn"
# A full JDK, user-local: the box's system Java is a runtime only (no javac,
# no ct.sym), and XMage compiles with --release 8, which needs ct.sym.
jdk_major=17
export JAVA_HOME="$root/jdk"
export PATH="$JAVA_HOME/bin:$PATH"
scope=(systemd-run --user --scope --quiet -p MemoryMax=7G -p CPUQuota=800% --)

if [ "${1:-}" != "--smoke" ]; then
  if [ ! -x "$JAVA_HOME/bin/javac" ]; then
    jtgz="$root/temurin-$jdk_major-jdk.tar.gz"
    curl -fsSL -o "$jtgz" "https://api.adoptium.net/v3/binary/latest/$jdk_major/ga/linux/x64/jdk/hotspot/normal/eclipse"
    rm -rf "$JAVA_HOME" && mkdir -p "$JAVA_HOME"
    tar -xzf "$jtgz" -C "$JAVA_HOME" --strip-components=1
  fi
  if [ ! -x "$mvn" ]; then
    tgz="$root/apache-maven-$mvn_version-bin.tar.gz"
    curl -fsSL -o "$tgz" "https://archive.apache.org/dist/maven/maven-3/$mvn_version/binaries/apache-maven-$mvn_version-bin.tar.gz"
    rm -rf "$root/maven" && mkdir -p "$root/maven"
    tar -xzf "$tgz" -C "$root/maven" --strip-components=1
  fi
  if [ ! -d "$root/mage/.git" ]; then
    git clone --filter=blob:none https://github.com/magefree/mage.git "$root/mage"
  fi
  git -C "$root/mage" fetch -q origin
  git -C "$root/mage" checkout -q "$ref"
  start=$(date +%s)
  ( cd "$root/mage" && MAVEN_OPTS=-Xmx3g "${scope[@]}" /usr/bin/time -v -o "$root/build.time" \
      "$mvn" -B -q -T 2 -pl Mage.Tests -am install -DskipTests ) 2>&1 | tail -20
  echo "build_seconds=$(( $(date +%s) - start ))" > "$root/build.env"
fi

start=$(date +%s)
( cd "$root/mage" && "${scope[@]}" /usr/bin/time -v -o "$root/smoke.time" \
    "$mvn" -B -pl Mage.Tests surefire:test -Dtest="$smoke_test" -DfailIfNoTests=false ) 2>&1 | tail -30
echo "smoke_seconds=$(( $(date +%s) - start ))" > "$root/smoke.env"

report="$root/mage/Mage.Tests/target/surefire-reports/TEST-$smoke_test.xml"
[ -f "$report" ] || { echo "xmage-oracle-setup: no surefire report at $report" >&2; exit 1; }
python3 - "$report" "$root" "$ref" <<'EOF'
import sys, re, xml.etree.ElementTree as ET
report, root, ref = sys.argv[1:4]
t = ET.parse(report).getroot()
cases = sorted(float(c.get("time", 0)) for c in t.iter("testcase"))
def rss(p):
    m = re.search(r"Maximum resident set size \(kbytes\): (\d+)", open(p).read())
    return int(m.group(1)) // 1024 if m else -1
env = {}
for f in ("build.env", "smoke.env"):
    try:
        for line in open(f"{root}/{f}"):
            k, v = line.strip().split("=")
            env[k] = v
    except FileNotFoundError:
        pass
warm = cases[:-1] or cases
med = warm[len(warm) // 2] if warm else -1
lines = [
    f"# XMage oracle measurement ({ref})", "",
    f"- tests: {t.get('tests')} failures: {t.get('failures')} errors: {t.get('errors')}",
    f"- build: {env.get('build_seconds', 'n/a')} s, peak RSS {rss(root + '/build.time') if 'build_seconds' in env else 'n/a'} MiB",
    f"- smoke run: {env.get('smoke_seconds')} s wall, peak RSS {rss(root + '/smoke.time')} MiB",
    f"- per-test seconds (warm, excluding the slowest): median {med:.3f}, max {max(warm) if warm else -1:.3f}",
    f"- slowest test (includes card DB load): {cases[-1] if cases else -1:.3f} s",
    "",
    "Spec §10 revisit trigger: warm median above 0.200 s or smoke peak RSS above 2560 MiB.",
]
open(f"{root}/MEASURE.md", "w").write("\n".join(lines) + "\n")
print("\n".join(lines))
EOF
