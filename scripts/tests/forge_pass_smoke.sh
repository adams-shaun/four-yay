#!/usr/bin/env bash
# forge_pass_smoke.sh — prove scripts/forge-pass.sh replays only the request
# shas the Forge result cache does not hold, without Java, a Forge checkout,
# systemd or a real corpus.
#
# The Forge runner and oraclediff are stubs on the pass's own override hooks
# (FORGE_PASS_RUNNER / FORGE_PASS_ORACLEDIFF). The cache is a t.TempDir-style
# scratch tree. The assertions:
#   - a cold cache replays every request and populates the cache;
#   - a warm cache replays 0 and does not start the runner;
#   - --force replays every request even when warm;
#   - every run reaches adjudicate and writes the D1=C ledger.
#
#   scripts/tests/forge_pass_smoke.sh
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
PASS=$ROOT/scripts/forge-pass.sh
mkdir -p "$ROOT/.ds4/scratch"
TMP=$(mktemp -d "$ROOT/.ds4/scratch/forge-pass-smoke.XXXXXX")
trap 'rm -rf "$TMP"' EXIT
fails=0
check() { if [ "$2" = 0 ]; then printf 'ok   %s\n' "$1"; else printf 'FAIL %s %s\n' "$1" "${3:-}"; fails=$((fails + 1)); fi; }
has() { [[ $1 == *"$2"* ]]; }
cached_count() { find "$TMP/cache" -name '*.json' 2>/dev/null | wc -l | tr -d ' '; }

BIN=$TMP/bin
mkdir -p "$BIN" "$TMP/root"
REPLAYS=$TMP/replays
: > "$REPLAYS"

# runner stub: --compile answers the driver sha forge-pass keys the cache by; a
# replay records how many request lines it was handed and copies them through.
cat > "$BIN/runner" <<'STUB'
#!/usr/bin/env bash
if [ "${1:-}" = --compile ]; then
	echo "driver_sha=abcdef012345 ref=2222222222222222222222222222222222222222"
	exit 0
fi
/usr/bin/grep -c . "$1" >> "$SMOKE_REPLAYS" || true
cat "$1" > "$2"
STUB

# oraclediff stub: gen and forge-export emit three canned request lines;
# forge-diff mirrors the real cache Put for the rows it is handed (keyed by the
# line's sha256, as gate.Hash is); adjudicate writes a ledger shard.
cat > "$BIN/oraclediff" <<'STUB'
#!/usr/bin/env bash
cmd=$1; shift
declare -A o
while [ $# -gt 0 ]; do
	case "$1" in
	-*) k=${1#-}; o[$k]=${2:-}; shift 2 ;;
	*) shift ;;
	esac
done
emit() { printf '{"id":"i0"}\n{"id":"i1"}\n{"id":"i2"}\n' > "$1"; }
case "$cmd" in
gen) emit "${o[out]}" ;;
forge-export) emit "${o[out]}" ;;
forge-diff)
	while IFS= read -r line || [ -n "$line" ]; do
		[ -n "$line" ] || continue
		sha=$(printf '%s' "$line" | sha256sum | cut -d' ' -f1)
		mkdir -p "${o[cache]}/${sha:0:2}"
		printf '{}\n' > "${o[cache]}/${sha:0:2}/$sha.json"
	done < "${o[forge]}"
	: > "${o[out]}"
	;;
adjudicate)
	mkdir -p "${o[ledger]}"
	printf 'ledger\n' > "${o[ledger]}/smoke.jsonl"
	echo "patterns 3 AGREE3"
	;;
esac
STUB
chmod +x "$BIN"/*

run_pass() {
	FORGE_PASS_LOCK=none FORGE_PASS_ORACLEDIFF=$BIN/oraclediff FORGE_PASS_RUNNER=$BIN/runner \
		FORGE_PASS_OUT=$TMP/out FORGE_PASS_CACHE=$TMP/cache FORGE_PASS_XMAGE_CACHE=$TMP/xcache \
		FORGE_PASS_LEDGER=$TMP/ledger FORGE_ORACLE_DIR=$TMP/root \
		FORGE_ORACLE_REF=2222222222222222222222222222222222222222 \
		SMOKE_REPLAYS=$REPLAYS "$PASS" TEST "$@" 2>&1
}

# 1. Cold cache: the precondition is an empty cache; every request replays.
check "precondition: cold cache is empty" \
	$([ ! -d "$TMP/cache" ] || [ -z "$(find "$TMP/cache" -name '*.json' 2>/dev/null)" ] && echo 0 || echo 1)
out=$(run_pass); rc=$?
check "cold pass exits 0" $([ "$rc" = 0 ] && echo 0 || echo 1) "rc=$rc $out"
check "cold pass replays all 3" $(has "$out" "replayed 3/3" && echo 0 || echo 1) "$out"
check "cold pass started the runner (feature handler ran)" $([ -s "$REPLAYS" ] && echo 0 || echo 1) "replays: $(cat "$REPLAYS" 2>/dev/null)"
check "cold pass populated the cache (3 entries)" $([ "$(cached_count)" = 3 ] && echo 0 || echo 1) "cached: $(cached_count)"
check "cold pass reached adjudicate (ledger written)" $([ -s "$TMP/ledger/smoke.jsonl" ] && echo 0 || echo 1)

# 2. Warm cache: the precondition is a full cache; nothing replays.
check "precondition: warm cache holds all 3 shas" $([ "$(cached_count)" = 3 ] && echo 0 || echo 1)
: > "$REPLAYS"
rm -f "$TMP/ledger/smoke.jsonl"
out=$(run_pass); rc=$?
check "warm pass exits 0" $([ "$rc" = 0 ] && echo 0 || echo 1) "rc=$rc $out"
check "warm pass replays 0" $(has "$out" "replayed 0/3" && echo 0 || echo 1) "$out"
check "warm pass did not start the runner" $([ ! -s "$REPLAYS" ] && echo 0 || echo 1) "replays: $(cat "$REPLAYS" 2>/dev/null)"
check "warm pass still reached adjudicate" $([ -s "$TMP/ledger/smoke.jsonl" ] && echo 0 || echo 1)

# 3. Forced replay: every request replays even though the cache is warm.
: > "$REPLAYS"
out=$(run_pass --force); rc=$?
check "forced pass exits 0" $([ "$rc" = 0 ] && echo 0 || echo 1) "rc=$rc $out"
check "forced pass replays all 3 despite a warm cache" $(has "$out" "replayed 3/3" && echo 0 || echo 1) "$out"
check "forced pass started the runner with 3 lines" $([ "$(cat "$REPLAYS" 2>/dev/null)" = 3 ] && echo 0 || echo 1) "replays: $(cat "$REPLAYS" 2>/dev/null)"

[ "$fails" = 0 ] && echo "forge_pass_smoke: all passed" || echo "forge_pass_smoke: $fails FAILED"
exit $((fails != 0))
