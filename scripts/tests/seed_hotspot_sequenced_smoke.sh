#!/usr/bin/env bash
# seed_hotspot_sequenced_smoke.sh -- the seed files no contention ticket for a
# hot group whose tickets are already chained by Depends-On.
#
# Why (2026-10-06): a contention ticket depends on every contending branch, so
# it dispatches only after the last one lands. When those tickets were already
# sequenced, nothing was left to un-contend: 119 of 172 contention tickets
# ended superseded. seed_candidates.already_sequenced is the filter.
#
#   scripts/tests/seed_hotspot_sequenced_smoke.sh
set -uo pipefail
ROOT=$(git rev-parse --show-toplevel)
TMP=$(mktemp -d ${SEED_SMOKE_TMP:-/tmp}/seed-seq.XXXXXX)
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/.ds4/issues"
mk() { # id [deps]
	{ printf -- '---\nid: %s\nstatus: briefed\n---\n\n## Report\n\n## Brief\n# t\n' "$1"
	  [ -n "${2:-}" ] && printf 'Depends-On: %s\n' "$2"
	  printf '\n## History\n'; } >"$TMP/.ds4/issues/$1.md"
}
mk cli-a
mk cli-b cli-a
mk cli-c "cli-b, cli-x"
mk cli-d
python3 - "$ROOT" "$TMP" <<'PY'
import importlib.util, sys
from pathlib import Path
root, tmp = Path(sys.argv[1]), Path(sys.argv[2])
sys.path.insert(0, str(root / "scripts"))
spec = importlib.util.spec_from_file_location("seed_candidates", root / "scripts" / "seed_candidates.py")
m = importlib.util.module_from_spec(spec); spec.loader.exec_module(m)
cases = [
    (("cli-a", "cli-b"), True, "direct chain"),
    (("cli-a", "cli-b", "cli-c"), True, "transitive chain"),
    (("cli-a", "cli-d"), False, "unrelated tickets"),
    (("cli-c", "cli-d"), False, "one unchained pair"),
    (("cli-a", "loop-design"), False, "a branch with no ticket file"),
]
fails = 0
for branches, want, name in cases:
    got = m.already_sequenced(tmp, branches)
    ok = got == want
    print(("ok   " if ok else "FAIL ") + f"{name}: already_sequenced{branches} = {got}")
    fails += not ok
sys.exit(1 if fails else 0)
PY
