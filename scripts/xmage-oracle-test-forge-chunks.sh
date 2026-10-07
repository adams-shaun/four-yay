#!/usr/bin/env bash
# Exercise Forge oracle chunking without a Forge checkout or JVM.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d "$root/.ds4/scratch/forge-chunks.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin" "$tmp/clone/forge-gui/res/cardsfolder" "$tmp/clone/forge-gui/res/tokenscripts" \
  "$tmp/clone/forge-oracle/src/main/java" "$tmp/root" "$tmp/jdk/bin" "$tmp/oracle-tmp"
echo '/stub/classpath.jar' > "$tmp/root/cp.txt"
: > "$tmp/clone/forge-oracle/src/main/java/Driver.java"

cat > "$tmp/bin/git" <<'STUB'
#!/usr/bin/env bash
args=("$@"); [ "${args[0]}" = -C ] && args=("${args[@]:2}")
case "${args[0]}" in
  merge-base|diff) exit 0 ;;
  ls-files) exit 0 ;;
  rev-parse)
    last=${args[${#args[@]}-1]}
    case "$last" in
      --show-toplevel) echo "$STUB_TOP" ;;
      HEAD|*'^{commit}') echo "$STUB_REF" ;;
      *:*) echo "tree-${last#*:}" ;;
      *) exit 2 ;;
    esac ;;
  *) exit 2 ;;
esac
STUB
cat > "$tmp/bin/systemd-run" <<'STUB'
#!/usr/bin/env bash
while [ "$1" != -- ]; do shift; done
shift
exec "$@"
STUB
cat > "$tmp/jdk/bin/javac" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
cat > "$tmp/jdk/bin/java" <<'STUB'
#!/usr/bin/env bash
args=("$@"); n=${#args[@]}; input=${args[n-2]}; output=${args[n-1]}
printf '%s %s\n' "$(basename "$input")" "$(wc -l < "$input")" >> "$JAVA_CALLS"
if grep -q FAIL_CHUNK "$input"; then echo 'deliberate chunk failure' >&2; exit 9; fi
cp "$input" "$output"
STUB
chmod +x "$tmp/bin/git" "$tmp/bin/systemd-run" "$tmp/jdk/bin/"*
export PATH="$tmp/bin:$PATH" STUB_TOP="$tmp/clone" STUB_REF=0123456789012345678901234567890123456789
export FORGE_REF="$STUB_REF" FORGE_ORACLE_REF="$STUB_REF" FORGE_ORACLE_SRC="$tmp/clone/forge-oracle"
export FORGE_ORACLE_DIR="$tmp/root" FORGE_ORACLE_TMP="$tmp/oracle-tmp" FORGE_ORACLE_JDK="$tmp/jdk"
export JAVA_CALLS="$tmp/java-calls" FORGE_ORACLE_CHUNK=1000

seq 1 2500 > "$tmp/input"
"$root/scripts/forge-oracle-run.sh" "$tmp/input" "$tmp/output"
[[ $(wc -l < "$JAVA_CALLS") -eq 3 ]] || { echo "expected 3 JVM calls" >&2; exit 1; }
[[ $(wc -l < "$tmp/output") -eq 2500 ]] || { echo "expected 2500 output rows" >&2; exit 1; }
cmp -s "$tmp/input" "$tmp/output" || { echo "output order/content differs from input" >&2; exit 1; }
[[ -z $(find "$tmp/oracle-tmp" -mindepth 1 -print -quit) ]] || { echo "run temporary files were not removed" >&2; exit 1; }
echo 'ok: 2500 rows ran as three ordered chunks and temporary files were removed'

: > "$JAVA_CALLS"
seq 1 1001 > "$tmp/failing-input"
sed -i '1001s/.*/FAIL_CHUNK/' "$tmp/failing-input"
if "$root/scripts/forge-oracle-run.sh" "$tmp/failing-input" "$tmp/failing-output" >"$tmp/failure.stdout" 2>"$tmp/failure.stderr"; then
  echo 'expected a failing chunk to return non-zero' >&2; exit 1
fi
grep -q 'chunk 2 failed' "$tmp/failure.stderr" || { echo 'failure did not name chunk 2' >&2; cat "$tmp/failure.stderr" >&2; exit 1; }
[[ $(wc -l < "$JAVA_CALLS") -eq 2 ]] || { echo 'expected failure on the second JVM call' >&2; exit 1; }
[[ -z $(find "$tmp/oracle-tmp" -mindepth 1 -print -quit) ]] || { echo "failed-run temporary files were not removed" >&2; exit 1; }
echo 'ok: failed chunk returns non-zero, names the chunk, and cleans temporary files'
