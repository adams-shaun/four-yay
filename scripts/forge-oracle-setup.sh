#!/usr/bin/env bash
# forge-oracle-setup.sh -- build Forge out of tree for the Forge oracle
# (.ds4/forge-oracle/DESIGN.md §4.2, §7.3). Host only; never run in a seat.
#
# Idempotent. Re-run after a FORGE_ORACLE_REF bump. Everything lands under
# $FORGE_ORACLE_DIR (default /mnt/sata/gorge-training/forgeoracle); nothing in
# the gorge tree changes. Forge is GPL-3.0: its source, jars and output stay
# under that directory and are never committed.
#
#   scripts/forge-oracle-setup.sh                 # clone/fetch, pin check, build, cp.txt, measure
#   scripts/forge-oracle-setup.sh --rebuild       # as above, ignoring the per-ref build stamp
#   scripts/forge-oracle-setup.sh --check-pin REF CLONE
#                                                 # pin invariant only (forge-oracle-run.sh calls this)
#
# The pin invariant (§7.3): FORGE_REF must be an ancestor of the driver ref, and
# the cardsfolder and tokenscripts trees must be identical at both. Otherwise a
# script difference between gorge's corpus and Forge's runtime would look like
# an engine disagreement, so the scripts refuse to run.
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
mk() { sed -n "s/^$1 *?= *//p" "$here/Makefile"; }
die() { echo "forge-oracle-setup: $*" >&2; exit 2; }

forge_ref=${FORGE_REF:-$(mk FORGE_REF)}
oracle_ref=${FORGE_ORACLE_REF:-$(mk FORGE_ORACLE_REF)}
root=${FORGE_ORACLE_DIR:-$(mk FORGE_ORACLE_DIR)}
repo=${FORGE_REPO:-$(mk FORGE_REPO)}
clone=${FORGE_ORACLE_CLONE:-$root/forge}
[ -n "$forge_ref" ] && [ -n "$oracle_ref" ] && [ -n "$root" ] || die "FORGE_REF/FORGE_ORACLE_REF/FORGE_ORACLE_DIR unset"
root=$(realpath -m "$root")
clone=$(realpath -m "$clone")

# pin_check CLONE REF: the §7.3 invariant, or exit 2 naming which half failed.
pin_check() {
  local c=$1 ref=$2 rc=0 p a b
  git -C "$c" merge-base --is-ancestor "$forge_ref" "$ref" || rc=$?
  case $rc in
    0) ;;
    1) die "pin invariant: FORGE_REF $forge_ref is not an ancestor of $ref; merge the fork's gorge branch into the driver branch, bump FORGE_ORACLE_REF, rerun forge-oracle-setup.sh" ;;
    *) die "pin invariant: cannot compare FORGE_REF $forge_ref with $ref in $c (git exit $rc)" ;;
  esac
  for p in forge-gui/res/cardsfolder forge-gui/res/tokenscripts; do
    a=$(git -C "$c" rev-parse "$forge_ref:$p") || die "pin invariant: no $p at FORGE_REF $forge_ref"
    b=$(git -C "$c" rev-parse "$ref:$p") || die "pin invariant: no $p at $ref"
    [ "$a" = "$b" ] || die "pin invariant: $p differs between FORGE_REF $forge_ref ($a) and $ref ($b); a script difference would masquerade as an engine disagreement"
  done
}

if [ "${1:-}" = --check-pin ]; then
  [ $# -eq 3 ] || die "usage: --check-pin REF CLONE"
  pin_check "$3" "$2"
  echo "forge-oracle-setup: pin invariant holds for $2"
  exit 0
fi
rebuild=0
[ "${1:-}" = --rebuild ] && rebuild=1

# Clone (sparse, D3) or fetch, then check the pin before anything is built.
if [ ! -e "$clone/.git" ]; then
  mkdir -p "$(dirname "$clone")"
  git clone --filter=blob:none --no-checkout "$repo" "$clone"
  git -C "$clone" sparse-checkout init --no-cone
  git -C "$clone" sparse-checkout set '/*' '!/*/' '/*/pom.xml' '/forge-core/' '/forge-game/' '/forge-ai/' \
    '/forge-gui/src/' '/forge-gui/pom.xml' '/forge-gui/res/cardsfolder/' '/forge-gui/res/tokenscripts/' \
    '/forge-gui/res/editions/' '/forge-gui/res/blockdata/' '/forge-gui/res/languages/' '/forge-gui/res/ai/' \
    '/forge-gui/res/lists/' '/forge-gui-desktop/src/test/java/forge/ai/' '/forge-oracle/'
fi
git -C "$clone" fetch -q origin
oref=$(git -C "$clone" rev-parse --verify -q "$oracle_ref^{commit}") || die "FORGE_ORACLE_REF $oracle_ref is not in $clone after fetch"
pin_check "$clone" "$oref"

case "$root" in /tmp/*) die "$root is RAM-backed; use /mnt/sata" ;; esac
[ -z "$(git -C "$clone" status --porcelain --untracked-files=no)" ] || die "$clone has uncommitted changes; refusing to move HEAD"
[ "$(git -C "$clone" rev-parse HEAD)" = "$oref" ] || git -C "$clone" checkout -q --detach "$oref"

xmage=${XMAGE_ORACLE_DIR:-$(mk XMAGE_ORACLE_DIR)}
jdk=$(realpath -m "${FORGE_ORACLE_JDK:-$xmage/jdk}")
mvn=$(realpath -m "${FORGE_ORACLE_MVN:-$xmage/maven/bin/mvn}")
[ -x "$jdk/bin/javac" ] || die "no JDK at $jdk; run make xmage-oracle-setup first or set FORGE_ORACLE_JDK"
[ -x "$mvn" ] || die "no Maven at $mvn; run make xmage-oracle-setup first or set FORGE_ORACLE_MVN"
export JAVA_HOME="$jdk" PATH="$jdk/bin:$PATH" MAVEN_OPTS=-Xmx2g
mkdir -p "$root"
m2=$root/m2

# Upstream modules, installed once per pinned ref. Driver worktree edits alone
# only trigger javac in forge-oracle-run.sh, not a Maven build.
if [ "$rebuild" = 1 ] || [ "$(cat "$root/build.ref" 2>/dev/null)" != "$oref" ]; then
  start=$(date +%s)
  GORGE_ROOT=$here "$here/scripts/heavy.sh" heavy --mem 7G --wait 3600 --name forge-oracle-build -- \
    /usr/bin/time -v -o "$root/build.time" \
    "$mvn" -B -T 2 -f "$clone/pom.xml" -pl forge-core,forge-game,forge-ai -am install -DskipTests -Dmaven.repo.local="$m2"
  echo "build_seconds=$(( $(date +%s) - start ))" > "$root/build.env"
  # dependency:build-classpath only works through the reactor (the installed
  # POMs keep ${revision} unflattened), hence -am and the online repo.
  GORGE_ROOT=$here "$here/scripts/heavy.sh" heavy --mem 3G --wait 3600 --name forge-oracle-cp -- \
    "$mvn" -B -q -f "$clone/pom.xml" -pl forge-ai -am dependency:build-classpath \
      -Dmdep.outputFile=target/cp.deps.txt -Dmaven.repo.local="$m2"
  [ -s "$clone/forge-ai/target/cp.deps.txt" ] || die "no classpath written at $clone/forge-ai/target/cp.deps.txt"
  cp "$clone/forge-ai/target/cp.deps.txt" "$root/cp.deps.txt"
  echo "$oref" > "$root/build.ref"
fi
[ -s "$root/cp.deps.txt" ] || die "no $root/cp.deps.txt; rerun with --rebuild"

# gson is not a dependency of core, game or ai but the driver reads JSON with it.
gson=2.13.1
gjar=$root/lib/gson-$gson.jar
if [ ! -s "$gjar" ]; then
  mkdir -p "$root/lib"
  url=https://repo1.maven.org/maven2/com/google/code/gson/gson/$gson/gson-$gson.jar
  curl -fsSL -o "$gjar.part" "$url"
  want=$(curl -fsSL "$url.sha1")
  [ "$(sha1sum "$gjar.part" | cut -d' ' -f1)" = "${want%% *}" ] || { rm -f "$gjar.part"; die "gson sha1 mismatch"; }
  mv "$gjar.part" "$gjar"
fi

# cp.txt: the three module jars, the reactor classpath, gson.
# Select the version the pinned reactor actually resolved, not all versions
# left in m2 by earlier pins.
ver=$(tr ':' '\n' < "$root/cp.deps.txt" | sed -n 's|.*/forge/forge-core/\([^/]*\)/forge-core-[^/]*\.jar$|\1|p')
[ -n "$ver" ] && [ "$(printf '%s\n' "$ver" | wc -l)" = 1 ] || die "expected one forge-core version in cp.deps.txt, got: $ver"
mods=""
for m in forge-core forge-game forge-ai; do
  jar=$m2/forge/$m/$ver/$m-$ver.jar
  [ -s "$jar" ] || die "missing installed module $jar; rerun with --rebuild"
  mods="$mods${mods:+:}$jar"
done
echo "$mods:$(cat "$root/cp.deps.txt"):$gjar" > "$root/cp.txt"

# Measure: compile the driver through the same path a run uses.
mkdir -p "$root/tmp"
start=$(date +%s)
FORGE_ORACLE_DIR=$root FORGE_ORACLE_REF=$oref FORGE_ORACLE_CLONE=$clone FORGE_ORACLE_JDK=$jdk \
  "$here/scripts/forge-oracle-run.sh" --compile > "$root/compile.out"
compile_s=$(( $(date +%s) - start ))
rss() { sed -n 's/.*Maximum resident set size (kbytes): \([0-9]*\).*/\1/p' "$1" 2>/dev/null | awk '{printf "%d", $1/1024}'; }
. "$root/build.env"
nsrc=$(find "$clone/forge-oracle/src/main/java" -name '*.java' 2>/dev/null | wc -l)
{
  echo "# Forge oracle measurement ($oref)"
  echo
  echo "- driver ref: $oref; FORGE_REF: $forge_ref"
  echo "- cardsfolder tree: $(git -C "$clone" rev-parse "$oref:forge-gui/res/cardsfolder"), tokenscripts tree: $(git -C "$clone" rev-parse "$oref:forge-gui/res/tokenscripts")"
  echo "- upstream build (stamp for this ref): $build_seconds s, peak RSS $(rss "$root/build.time") MiB"
  echo "- classpath: $(tr ':' '\n' < "$root/cp.txt" | wc -l) entries ($(basename "$(cut -d: -f1 "$root/cp.txt")") first)"
  echo "- driver javac: $nsrc source files, $compile_s s wall; $(cat "$root/compile.out")"
  echo "- java: $("$jdk/bin/java" -version 2>&1 | head -1)"
  echo
  echo "P0 reference (2026-10-06): build 33.8 s / 2.17 GB peak RSS; driver javac 0.91 s; 60 warm replays median 14.7 ms, 199 MB RSS."
  echo "Design §10 targets: warm median at most 500 ms per scenario, JVM heap at most 2.5 GB; replay performance is not measured here."
} > "$root/MEASURE.md"
cat "$root/MEASURE.md"
