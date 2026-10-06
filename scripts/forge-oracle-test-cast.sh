#!/usr/bin/env bash
# Host fixture: a real cast and resolve through the Forge engine. Shock is paid
# from the mana pool, targets a Grizzly Bears, sits on the stack after the cast
# step and kills the Bears on resolve. The same request with no mana in the step
# must produce a harness row, not a silent pass. Needs the out-of-tree build
# that make forge-oracle-setup makes. The request is hand-written here; nothing
# from Forge is committed.
#
#   scripts/forge-oracle-test-cast.sh [outdir]
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-$here/.ds4/scratch/forge-oracle-test-cast}
mkdir -p "$out"
decision='{"step":0,"seat":0,"kind":"target","picks":["Grizzly Bears (b)"],"pick_idx":[4],"pick_refs":["p1:Grizzly Bears"],"object_picks":["p1:Grizzly Bears"],"pick_kinds":["permanent"],"gorge_kind":"target","min":1,"max":1}'
req() { # req ID MANA
  printf '{"id":"%s","scenario_sha":"fixture","abilities":{},"gorge_decisions":[%s],"item":{"id":"%s","card":"Shock","template":"cast-resolve","name":"fix-cast","setup":{"p0":{"hand":["Shock"]},"p1":{"battlefield":["Grizzly Bears"]}},"steps":[{"op":"cast","seat":0,"card":"p0:Shock","mana":"%s","targets":["p1:Grizzly Bears"]},{"op":"resolve","seat":0}]}}\n' "$1" "$decision" "$1" "$2"
}
req FIX/cast/v1 R > "$out/req.jsonl"
req FIX/cast-nomana/v1 "" > "$out/req-nomana.jsonl"
"$here/scripts/forge-oracle-run.sh" "$out/req.jsonl" "$out/forge.jsonl"
"$here/scripts/forge-oracle-run.sh" "$out/req-nomana.jsonl" "$out/forge-nomana.jsonl"
fail=0
check() { # check ROWFILE DESCRIPTION JQ-EXPRESSION (must print true)
  if [ "$(head -1 "$1" | jq -r "$3")" = true ]; then echo "ok   $2"; else echo "FAIL $2"; fail=1; fi
}
f=$out/forge.jsonl
check "$f" "no harness row" '.harness == null'
check "$f" "cast step: Shock is the only stack item" '.snapshots[1].stack == [{"kind":"spell","source":"Shock","controller":0}]'
check "$f" "cast step: the pool was spent" '[.snapshots[1].players[].pool] == ["",""]'
check "$f" "resolve step: stack empty" '.snapshots[2].stack == []'
check "$f" "resolve step: the Bears died" '.snapshots[2].players[1].graveyard == ["Grizzly Bears"]'
check "$f" "resolve step: Shock in p0 graveyard" '.snapshots[2].players[0].graveyard == ["Shock"]'
# The negative: unpaid, the cast must be reported, never silently accepted.
check "$out/forge-nomana.jsonl" "no mana: harness row names the failed cast" '(.harness // "") | test("was not put on the stack")'
exit $fail
