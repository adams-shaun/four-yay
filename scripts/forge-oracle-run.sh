#!/usr/bin/env bash
# forge-oracle-run.sh IN.jsonl OUT.jsonl
# forge-oracle-run.sh --compile
#
# Replays Forge-oracle requests (one JSON object per line, DESIGN.md §5.2) in
# Forge through the driver and writes one result row per request. Needs the
# out-of-tree build that scripts/forge-oracle-setup.sh makes. The driver is
# compiled with javac against $FORGE_ORACLE_DIR/cp.txt on first use and
# whenever its source (or the classpath) changes. The pass wrapper, not this
# script, takes the heavy lock (§7.2).
#
# Overrides:
#   FORGE_ORACLE_SRC      the forge-oracle project dir to compile and replay (a
#                         candidate worktree's forge-oracle/); default is the
#                         setup clone's. The pin invariant is then checked
#                         against that worktree's HEAD and its forge-gui/res is
#                         the runtime script root.
#   FORGE_ORACLE_CLASSES  compiled-driver root (default $FORGE_ORACLE_DIR/driver)
#   FORGE_ORACLE_TMP      java.io.tmpdir (default $FORGE_ORACLE_DIR/tmp); a seat
#                         jail with a read-only /mnt/sata points both at its worktree
#   FORGE_ORACLE_MAIN     driver main class (default forge.oracle.ScenarioReplay)
#   FORGE_ORACLE_MEM / FORGE_ORACLE_HEAP   scope MemoryMax / JVM heap (3G / 1536m)
set -euo pipefail

here=$(cd "$(dirname "$0")/.." && pwd)
mk() { sed -n "s/^$1 *?= *//p" "$here/Makefile"; }
die() { echo "forge-oracle-run: $*" >&2; exit 2; }

if [ "${1:-}" = --compile ]; then
  mode=compile
else
  in=${1:?usage: forge-oracle-run.sh IN.jsonl OUT.jsonl | --compile}
  out=${2:?usage: forge-oracle-run.sh IN.jsonl OUT.jsonl | --compile}
  mode=run
fi
root=${FORGE_ORACLE_DIR:-$(mk FORGE_ORACLE_DIR)}
clone=${FORGE_ORACLE_CLONE:-$root/forge}
xmage=${XMAGE_ORACLE_DIR:-$(mk XMAGE_ORACLE_DIR)}
jdk=${FORGE_ORACLE_JDK:-$xmage/jdk}
[ -n "$root" ] || die "FORGE_ORACLE_DIR unset"
root=$(realpath -m "$root")
clone=$(realpath -m "$clone")
jdk=$(realpath -m "$jdk")

# Which tree supplies the scripts, and at which commit: the setup clone at the
# pinned ref, or a candidate worktree at its own HEAD. Either way the checked
# commit must be the one on disk, and the script dirs must be unmodified.
if [ -n "${FORGE_ORACLE_SRC:-}" ]; then
  src=$(realpath "$FORGE_ORACLE_SRC")
  top=$(git -C "$src" rev-parse --show-toplevel) || die "FORGE_ORACLE_SRC $src is not in a git worktree"
  ref=$(git -C "$top" rev-parse HEAD)
else
  top=$clone
  want=${FORGE_ORACLE_REF:-$(mk FORGE_ORACLE_REF)}
  [ -n "$want" ] || die "FORGE_ORACLE_REF unset"
  ref=$(git -C "$top" rev-parse --verify -q "$want^{commit}") || die "FORGE_ORACLE_REF $want is not in $top; run make forge-oracle-setup"
  [ "$(git -C "$top" rev-parse HEAD)" = "$ref" ] || die "$top is not at FORGE_ORACLE_REF $ref; run make forge-oracle-setup"
  src=$top/forge-oracle
fi
"$here/scripts/forge-oracle-setup.sh" --check-pin "$ref" "$top" >&2
git -C "$top" diff --quiet HEAD -- forge-gui/res/cardsfolder forge-gui/res/tokenscripts || die "$top has uncommitted changes under forge-gui/res/{cardsfolder,tokenscripts}"
# Untracked (even ignored) scripts are loaded too, but git diff cannot see them.
[ -z "$(git -C "$top" ls-files --others -- forge-gui/res/cardsfolder forge-gui/res/tokenscripts)" ] || die "$top has untracked scripts under forge-gui/res/{cardsfolder,tokenscripts}"
[ -d "$top/forge-gui/res" ] || die "no $top/forge-gui/res"
[ -d "$src/src/main/java" ] || die "no driver source at $src/src/main/java"
[ -s "$root/cp.txt" ] || die "no $root/cp.txt; run make forge-oracle-setup"
[ -x "$jdk/bin/javac" ] || die "no JDK at $jdk"

# Compiled classes are keyed by the sha of the driver source plus the
# classpath, so a candidate worktree never clobbers the pinned build.
cp=$(cat "$root/cp.txt")
sum=$({ cat "$root/cp.txt"; find "$src/src/main/java" -name '*.java' -print0 | sort -z | xargs -0 sha256sum | sed 's|  .*/src/main/java/|  |'; } | sha256sum | cut -c1-12)
classes=$(realpath -m "${FORGE_ORACLE_CLASSES:-$root/driver}")/$sum
if [ ! -f "$classes/.done" ]; then
  tmp=$classes.tmp.$$
  rm -rf "$tmp" && mkdir -p "$tmp"
  mapfile -d '' srcs < <(find "$src/src/main/java" -name '*.java' -print0 | sort -z)
  systemd-run --user --scope --quiet -p MemoryMax=2G -p CPUQuota=200% -- \
    "$jdk/bin/javac" -J-Xmx1g --release 17 -encoding UTF-8 -nowarn -cp "$cp" -d "$tmp" "${srcs[@]}"
  touch "$tmp/.done"
  rm -rf "$classes" && mv "$tmp" "$classes"
fi
if [ "$mode" = compile ]; then
  echo "driver_sha=$sum ref=$ref"
  exit 0
fi

tmpdir=$(realpath -m "${FORGE_ORACLE_TMP:-$root/tmp}")
mkdir -p "$tmpdir"
out=$(realpath -m "$out")
in=$(realpath "$in")
chunk_size=${FORGE_ORACLE_CHUNK:-1000}
[[ "$chunk_size" =~ ^[1-9][0-9]*$ ]] || die "FORGE_ORACLE_CHUNK must be a positive integer (got: $chunk_size)"
run_tmp=$(mktemp -d "$tmpdir/forge-oracle-run.XXXXXX")
trap 'rm -rf "$run_tmp"' EXIT
mkdir -p "$(dirname "$out")"
: > "$run_tmp/combined.out"
: > "$out.log"
split -d -a 8 -l "$chunk_size" -- "$in" "$run_tmp/input."
chunk_number=0
cd "$tmpdir"
for chunk in "$run_tmp"/input.*; do
  [ -f "$chunk" ] || continue
  chunk_number=$((chunk_number + 1))
  chunk_name=$(printf 'chunk %d' "$chunk_number")
  chunk_out="$run_tmp/output.$(printf '%08d' "$chunk_number")"
  chunk_log="$run_tmp/log.$(printf '%08d' "$chunk_number")"
  if ! systemd-run --user --scope --quiet -p MemoryMax="${FORGE_ORACLE_MEM:-3G}" -p CPUQuota=200% -- \
    env FORGE_RES="$top/forge-gui/res" FORGE_ORACLE_REF="$ref" \
    "$jdk/bin/java" -Djava.awt.headless=true -Xmx"${FORGE_ORACLE_HEAP:-1536m}" -XX:+UseSerialGC \
      -Djava.io.tmpdir="$tmpdir" -cp "$classes:$cp" "${FORGE_ORACLE_MAIN:-forge.oracle.ScenarioReplay}" \
      "$chunk" "$chunk_out" >"$chunk_log" 2>&1; then
    cat "$chunk_log" >> "$out.log"
    echo "forge-oracle-run: $chunk_name failed (input $(basename "$chunk"))" >&2
    cat "$chunk_log" >&2
    exit 1
  fi
  cat "$chunk_log" >> "$out.log"
  cat "$chunk_out" >> "$run_tmp/combined.out"
done
cat "$run_tmp/combined.out" > "$out"
