#!/usr/bin/env bash
# forge_oracle_pin_smoke.sh — prove the Forge oracle scripts refuse to run when
# the pin invariant (DESIGN.md §7.3) fails, and that the javac-on-change stamp
# works, without Java, git history, systemd or a Forge checkout.
#
# git, java, javac and systemd-run are stubs on PATH. The invariant: FORGE_REF
# must be an ancestor of FORGE_ORACLE_REF and the cardsfolder and tokenscripts
# trees must be equal at both, otherwise forge-oracle-setup.sh and
# forge-oracle-run.sh exit 2 BEFORE any build, javac or JVM starts.
#
#   scripts/tests/forge_oracle_pin_smoke.sh
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
SETUP=$ROOT/scripts/forge-oracle-setup.sh
RUN=$ROOT/scripts/forge-oracle-run.sh
mkdir -p "$ROOT/.ds4/scratch"
TMP=$(mktemp -d "$ROOT/.ds4/scratch/forge-oracle-smoke.XXXXXX")
trap 'rm -rf "$TMP"' EXIT
fails=0
check() {
	if [ "$2" = 0 ]; then printf 'ok   %s\n' "$1"; else printf 'FAIL %s %s\n' "$1" "${3:-}"; fails=$((fails + 1)); fi
}

FREF=1111111111111111111111111111111111111111
OREF=2222222222222222222222222222222222222222
BIN=$TMP/bin
LOG=$TMP/calls.log
mkdir -p "$BIN" "$TMP/root" "$TMP/clone/.git" "$TMP/clone/forge-gui/res" "$TMP/clone/forge-oracle/src/main/java/forge/oracle" "$TMP/jdk/bin"
echo 'class A {}' > "$TMP/clone/forge-oracle/src/main/java/forge/oracle/A.java"
echo '/stub/forge-core.jar' > "$TMP/root/cp.txt"

# git: answers only what the scripts ask, driven by STUB_* variables.
cat > "$BIN/git" <<'STUB'
#!/usr/bin/env bash
echo "git $*" >> "$STUB_LOG"
args=("$@"); [ "${args[0]}" = -C ] && args=("${args[@]:2}")
case "${args[0]}" in
merge-base) exit "${STUB_ANCESTOR:-0}" ;;
diff) exit "${STUB_DIFF:-0}" ;;
ls-files) printf '%s' "${STUB_UNTRACKED:-}" ;;
status) exit 0 ;;
fetch | checkout) exit 0 ;;
rev-parse)
	last=${args[${#args[@]}-1]}
	case "$last" in
	--show-toplevel) echo "$STUB_TOP" ;;
	HEAD) echo "$STUB_HEAD" ;;
	*'^{commit}') last=${last%^\{commit\}}; echo "$last" ;;
	*:*) ref=${last%%:*}; p=${last#*:}
		if [ "$ref" = "$FORGE_REF" ] || [ -n "${STUB_TREE_PATH:-}" -a "$p" != "${STUB_TREE_PATH:-}" ]; then
			echo "tree-$p"
		else
			echo "tree-$p${STUB_TREE_SUFFIX:-}"
		fi ;;
	*) exit 2 ;;
	esac ;;
*) exit 2 ;;
esac
STUB
# java, javac: record the call; a refusal test fails if either is ever reached.
for t in java javac; do
	printf '#!/usr/bin/env bash\necho "%s $*" >> "$STUB_LOG"\n' "$t" > "$TMP/jdk/bin/$t"
done
printf 'echo "runtime res=$FORGE_RES ref=$FORGE_ORACLE_REF cwd=$PWD tmp=$*" >> "$STUB_LOG"\nprintf "stub output\\n" > "${@: -1}"\n' >> "$TMP/jdk/bin/java"
# systemd-run: drop its own flags up to `--`, run the rest.
cat > "$BIN/systemd-run" <<'STUB'
#!/usr/bin/env bash
echo "systemd-run $*" >> "$STUB_LOG"
while [ "$1" != -- ]; do shift; done
shift
exec "$@"
STUB
chmod +x "$BIN"/* "$TMP"/jdk/bin/*

export PATH="$BIN:$PATH" STUB_LOG=$LOG STUB_TOP=$TMP/clone STUB_HEAD=$OREF
export FORGE_REF=$FREF FORGE_ORACLE_REF=$OREF FORGE_ORACLE_DIR=$TMP/root FORGE_ORACLE_CLONE=$TMP/clone FORGE_ORACLE_JDK=$TMP/jdk
export FORGE_REPO=file:///nonexistent XMAGE_ORACLE_DIR=$TMP/none
unset FORGE_ORACLE_SRC FORGE_ORACLE_CLASSES FORGE_ORACLE_TMP FORGE_ORACLE_MAIN
unset STUB_ANCESTOR STUB_DIFF STUB_UNTRACKED STUB_TREE_SUFFIX STUB_TREE_PATH

has() { [[ $1 == *"$2"* ]]; }
reached() { /usr/bin/grep -qE "^(java|javac|systemd-run) |^git (-C [^ ]+ )?checkout" "$LOG" 2>/dev/null; }
count() { /usr/bin/grep -c "^$1 " "$LOG" 2>/dev/null || true; }

# 1. setup: FORGE_REF not an ancestor of the driver ref.
: > "$LOG"
out=$(STUB_ANCESTOR=1 "$SETUP" 2>&1); rc=$?
check "setup refuses a non-ancestor ref (exit 2)" $((rc == 2 ? 0 : 1)) "rc=$rc"
check "  ...naming the ancestry half" $([[ $out == *"not an ancestor"* ]] && echo 0 || echo 1) "$out"
reached; check "  ...before any checkout, javac, java or scope" $((! $? ))
check "  ...and no cp.txt rewritten" $([ "$(cat "$TMP/root/cp.txt")" = /stub/forge-core.jar ] && echo 0 || echo 1)
check "  ...and no build stamp" $([ ! -e "$TMP/root/build.ref" ] && echo 0 || echo 1)
check "  ...the ancestry handler ran" $(/usr/bin/grep -q "merge-base --is-ancestor $FREF $OREF" "$LOG" && echo 0 || echo 1)

# 2. setup: script trees differ at the two refs.
: > "$LOG"
out=$(STUB_TREE_SUFFIX=-moved "$SETUP" 2>&1); rc=$?
check "setup refuses a moved script tree (exit 2)" $((rc == 2 ? 0 : 1)) "rc=$rc"
check "  ...naming the tree half" $([[ $out == *"cardsfolder differs"* ]] && echo 0 || echo 1) "$out"
reached; check "  ...before any checkout, javac, java or scope" $((! $? ))

# Equal cardsfolder is not enough: tokenscripts must independently match.
for script in "$SETUP" "$RUN"; do
	: > "$LOG"
	if [ "$script" = "$SETUP" ]; then
		out=$(STUB_TREE_PATH=forge-gui/res/tokenscripts STUB_TREE_SUFFIX=-moved "$script" 2>&1); rc=$?
	else
		out=$(STUB_TREE_PATH=forge-gui/res/tokenscripts STUB_TREE_SUFFIX=-moved "$script" --compile 2>&1); rc=$?
	fi
	check "$(basename "$script") refuses a tokens-only mismatch" $([ "$rc" = 2 ] && has "$out" "tokenscripts differs" && echo 0 || echo 1) "rc=$rc $out"
	reached; check "  ...before javac, java or scope" $((! $? ))
done

# 3. setup --check-pin passes on a good pin (so the refusals above are the pin's doing).
: > "$LOG"
out=$("$SETUP" --check-pin "$OREF" "$TMP/clone" 2>&1); rc=$?
check "setup --check-pin accepts a good pin" $((rc == 0 ? 0 : 1)) "rc=$rc $out"

# 4. run: both refusals fire before javac/java, and a good pin proceeds past them.
: > "$LOG"; printf 'one input row\n' > "$TMP/in.jsonl"
out=$(STUB_ANCESTOR=1 "$RUN" "$TMP/in.jsonl" "$TMP/out.jsonl" 2>&1); rc=$?
check "run refuses a non-ancestor ref (exit 2, names the half)" $([ "$rc" = 2 ] && has "$out" "not an ancestor" && echo 0 || echo 1) "rc=$rc $out"
reached; check "  ...before javac, java or scope" $((! $? ))
out=$(STUB_TREE_SUFFIX=-moved "$RUN" "$TMP/in.jsonl" "$TMP/out.jsonl" 2>&1); rc=$?
check "run refuses a moved script tree (exit 2, names the half)" $([ "$rc" = 2 ] && has "$out" "cardsfolder differs" && echo 0 || echo 1) "rc=$rc $out"
reached; check "  ...before javac, java or scope" $((! $? ))
out=$(STUB_HEAD=3333333333333333333333333333333333333333 "$RUN" "$TMP/in.jsonl" "$TMP/out.jsonl" 2>&1); rc=$?
check "run refuses a clone that is not at FORGE_ORACLE_REF" $([ "$rc" = 2 ] && has "$out" "is not at FORGE_ORACLE_REF" && echo 0 || echo 1) "rc=$rc $out"
reached; check "  ...before javac, java or scope" $((! $? ))
out=$(STUB_DIFF=1 "$RUN" "$TMP/in.jsonl" "$TMP/out.jsonl" 2>&1); rc=$?
check "run refuses edited scripts under forge-gui/res (exit 2)" $([ "$rc" = 2 ] && has "$out" "uncommitted changes" && echo 0 || echo 1) "rc=$rc $out"
reached; check "  ...before javac, java or scope" $((! $? ))

: > "$LOG"
out=$(STUB_UNTRACKED=forge-gui/res/tokenscripts/extra.txt "$RUN" --compile 2>&1); rc=$?
check "run refuses untracked runtime scripts" $([ "$rc" = 2 ] && has "$out" "untracked scripts" && echo 0 || echo 1) "rc=$rc $out"
reached; check "  ...before javac, java or scope" $((! $? ))

# 5. run with a good pin: javac once per source sha, java with the pinned env.
: > "$LOG"
out=$("$RUN" --compile 2>&1); rc=$?
check "run --compile on a good pin succeeds" $((rc == 0 ? 0 : 1)) "rc=$rc $out"
check "  ...and compiled once" $([ "$(count javac)" = 1 ] && echo 0 || echo 1) "javac calls: $(count javac)"
"$RUN" --compile >/dev/null 2>&1
check "unchanged source reuses the stamp (no second javac)" $([ "$(count javac)" = 1 ] && echo 0 || echo 1) "javac calls: $(count javac)"
echo 'class B {}' > "$TMP/clone/forge-oracle/src/main/java/forge/oracle/B.java"
"$RUN" --compile >/dev/null 2>&1
check "a changed source recompiles" $([ "$(count javac)" = 2 ] && echo 0 || echo 1) "javac calls: $(count javac)"
"$RUN" "$TMP/in.jsonl" "$TMP/out.jsonl" 2>&1
check "run starts the JVM on the driver main class" \
	$(/usr/bin/grep -qE "^java .*forge.oracle.ScenarioReplay .*/input\\.00000000 .*/output\\.00000001" "$LOG" && echo 0 || echo 1) "$(/usr/bin/grep '^java' "$LOG")"
check "  ...with pinned runtime res and ref" \
	$(/usr/bin/grep -qF "runtime res=$TMP/clone/forge-gui/res ref=$OREF" "$LOG" && echo 0 || echo 1)

# 6. FORGE_ORACLE_SRC replays a candidate worktree under its own HEAD, in its own class dir.
mkdir -p "$TMP/cand/forge-gui/res" "$TMP/cand/forge-oracle/src/main/java/forge/oracle"
echo 'class C {}' > "$TMP/cand/forge-oracle/src/main/java/forge/oracle/C.java"
: > "$LOG"
CREF=3333333333333333333333333333333333333333
check "candidate HEAD differs from the pinned ref (precondition)" $([ "$CREF" != "$OREF" ] && echo 0 || echo 1)
rel=$(realpath --relative-to="$PWD" "$TMP")
out=$(FORGE_ORACLE_SRC=$rel/cand/forge-oracle FORGE_ORACLE_CLASSES=$rel/cand-classes FORGE_ORACLE_TMP=$rel/java-tmp STUB_TOP=$TMP/cand STUB_HEAD=$CREF "$RUN" "$TMP/in.jsonl" "$TMP/cand-out.jsonl" 2>&1); rc=$?
check "relative FORGE_ORACLE_SRC compiles and runs the candidate" $([ "$rc" = 0 ] && [ -n "$(ls "$TMP/cand-classes" 2>/dev/null)" ] && echo 0 || echo 1) "rc=$rc $out"
check "  ...using candidate res, HEAD and absolute tmpdir" \
	$(/usr/bin/grep -qF "runtime res=$TMP/cand/forge-gui/res ref=$CREF cwd=$TMP/java-tmp" "$LOG" && /usr/bin/grep -qF -- "-Djava.io.tmpdir=$TMP/java-tmp" "$LOG" && echo 0 || echo 1)
out=$(FORGE_ORACLE_SRC=$TMP/cand/forge-oracle STUB_TOP=$TMP/cand STUB_HEAD=$CREF STUB_ANCESTOR=1 "$RUN" --compile 2>&1); rc=$?
check "FORGE_ORACLE_SRC is still pin-checked (exit 2)" $((rc == 2 ? 0 : 1)) "rc=$rc $out"

[ "$fails" = 0 ] && echo "forge_oracle_pin_smoke: all passed" || echo "forge_oracle_pin_smoke: $fails FAILED"
exit $((fails != 0))
