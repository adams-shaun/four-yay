#!/usr/bin/env bash
# Host fixture: the Forge driver's setup placement contract. A permanent placed
# in setup must not draw on ETB (Elvish Visionary), a planeswalker must arrive
# with its printed loyalty (Ajani), the library is 40 minus the named cards, and
# the setup snapshot has an empty stack. Needs the out-of-tree build that
# make forge-oracle-setup makes; loads the card DB lazily (~2 s, ~200 MB).
# The request is hand-written here; nothing from Forge is committed.
#
#   scripts/forge-oracle-test-setup.sh [outdir]
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
root=${FORGE_ORACLE_DIR:-$(sed -n 's/^FORGE_ORACLE_DIR *?= *//p' "$here/Makefile")}
out=${1:-$root/runs/test-setup}
mkdir -p "$out"
cat > "$out/req.jsonl" <<'JSON'
{"id":"FIX/setup-etb-pw/v1","scenario_sha":"fixture","abilities":{},"gorge_decisions":[{"step":0,"seat":0,"kind":"target","picks":["Grizzly Bears (b)"],"pick_idx":[4],"pick_refs":["p1:Grizzly Bears"],"object_picks":["p1:Grizzly Bears"],"pick_kinds":["permanent"],"gorge_kind":"target","min":1,"max":1}],"item":{"id":"FIX/setup-etb-pw/v1","card":"Elvish Visionary","template":"setup","name":"fix-setup-etb-pw","setup":{"p0":{"battlefield":["Elvish Visionary","Ajani, Caller of the Pride"],"hand":["Shock"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[{"op":"cast","seat":0,"card":"p0:Shock","mana":"R","targets":["p1:Grizzly Bears"]},{"op":"resolve","seat":0}]}}
JSON
"$here/scripts/forge-oracle-run.sh" "$out/req.jsonl" "$out/forge.jsonl"
row=$(head -1 "$out/forge.jsonl")
fail=0
check() { # check DESCRIPTION JQ-EXPRESSION (must print true)
  if [ "$(jq -r "$2" <<<"$row")" = true ]; then echo "ok   $1"; else echo "FAIL $1"; fail=1; fi
}
check "no harness row" '.harness == null'
check "row is a forge row" '.engine == "forge"'
check "exactly three snapshots, setup first" '(.snapshots | length) == 3 and .snapshots[0].checkpoint == "setup"'
check "no ETB draw: p0 hand is [Shock]" '.snapshots[0].players[0].hand == ["Shock"]'
check "library is 40 minus the 3 named p0 cards" '.snapshots[0].players[0].library_count == 37'
check "setup stack is empty" '.snapshots[0].stack == []'
check "Ajani has printed loyalty 4" '[.snapshots[0].permanents[] | select(.name | startswith("Ajani")) | .counters.Loyalty] == [4]'
check "Elvish Visionary on the battlefield" '[.snapshots[0].permanents[].name] | index("Elvish Visionary") != null'
exit $fail
