#!/usr/bin/env bash
# Batch-land tickets parked for an XMage driver host replay (operator, 2026-10-06).
#
# agentctl's gate "xmage driver needs host replay" (.agentctl/config.toml)
# parks every ticket whose branch touches tools/xmageoracle/ at human_needed:
# only a host XMage replay can judge a driver change. Doing that by hand was 7
# tickets in 4 replays on 2026-10-06. This loop does it unattended, ONE replay
# per pass instead of one per ticket:
#
#   1. select every ticket whose History carries the park line of that gate
#      (status human_needed, oldest park first);
#   2. merge their branches onto ONE integration branch wt/driver-batch-<stamp>
#      (a branch that conflicts is skipped and stays parked; never resolved here);
#   3. run the capped build/tests, then ONE forced full replay
#      (validation/oracle/full-replay.sh) under the heavy lock;
#   4. verdict-compare against main: "regressed 0" lands everything;
#   5. otherwise each regressed row is replayed 4x on the integration driver --
#      not all equal = FLAKY (main's verdict row is kept, .ds4/driver-flakes.log
#      gets a line; a row id already in that log is FLAKY without re-testing,
#      so a known flake is never named CULPRIT); stable = attributed by
#      replaying that row on the batch WITHOUT each branch in turn (newest
#      first). A branch that changes compliance/oraclegen/ or cmd/oraclediff/
#      changes the scenario itself, so for it the row is REGENERATED on the
#      batch without that branch (oraclediff gen, that set) before the replay.
#      The branch whose removal clears the row is the CULPRIT: it is re-parked with a History line and the
#      rest land after a fresh full replay. A row no single branch clears lands
#      NOTHING and every ticket stays parked with the findings.
#
# DRIFT pass: a change to the scenario generator (compliance/oraclegen,
# cmd/oraclediff, compliance/manifests) changes what XMage is asked, but is never
# replayed on its own, so its breakage would surface as unattributable
# "regressions" in an unrelated driver batch. When NO ticket is parked, the gates
# hold (no pause, GREEN) and those paths differ between the main sha the last full
# replay ran on (.ds4/driver-replay-last-main; else the last LANDED sha in the
# log; else main is recorded and nothing runs) and main, the script replays MAIN
# ITSELF on a fresh integration worktree (no branches merged), classifies FLAKY as
# for a batch, logs each stable regressed row as
#   DRIFT <row> <detail> commits=<h1,h2,..>
# (also appended to .ds4/driver-drift.log, keyed by the driver+generator trees),
# lands the refreshed verdicts, records the new last-main, and files one agentctl
# ticket per regressed template class. In a normal batch a stable regressed row
# that the drift log holds for the same driver+generator trees is DRIFT-KNOWN:
# main's row is kept and no branch is blamed.
#
# Landing = `git merge --no-ff` into the main checkout's main (NO push: the
# gorge-postmerge-batch unit pushes), then each included ticket is marked
# merged through agentctl's IssueStore lock and its worktree removed.
#
# It acts only while at least one parked ticket exists, the pipeline pause file
# (.ds4/orchestrator/pause) is absent and the batcher's last verdict line in
# .ds4/postmerge-batch.log is GREEN. Never lands onto a red main.
#
# Start it (the operator does; a seat never does):
#
#   systemd-run --user --unit gorge-driver-replay-batch \
#     -p WorkingDirectory=/home/sadams/projects/gorge --setenv=PATH=$PATH --setenv=HOME=$HOME \
#     bash scripts/driver_replay_batch.sh
#
# Log (timestamped; START DRIFT-START DRIFT DRIFT-KNOWN MERGED-INTO-BATCH SKIP-CONFLICT REPLAY FLAKY CULPRIT
# LANDED ...): .ds4/driver-replay-batch.log. Flakes: .ds4/driver-flakes.log.
# Run dirs: $DRB_RUNS/driver-batch-<stamp> (the newest 3 are kept).
#
# A HELD ticket carries a History line
#   driver_replay_batch: HELD <why> main=<sha12|-> branch=<sha12>
# and is not selected again until its branch head changes (or main moves, when
# main=<sha>; main=- ignores main) or the gate re-parks it (a newer park line).
#
# Test seams (scripts/tests/driver_replay_batch_smoke.sh): DRB_REPO DRB_MAIN
# DRB_LOG DRB_FLAKE_LOG DRB_POSTMERGE_LOG DRB_ISSUES DRB_RUNS DRB_POLL DRB_ONCE
# DRB_COOLDOWN DRB_LOCKRUN DRB_WORKTREE_CMD DRB_REPLAY_CMD DRB_COMPARE_CMD
# DRB_SCENARIO_CMD DRB_GEN_CMD DRB_DIFF_CMD DRB_CHECK_CMD DRB_RATCHET_CMD DRB_ISSUE_TOOL
# DRB_LASTMAIN DRB_DRIFT_LOG DRB_TICKET_TOOL (called: <title> <brief>).
set -uo pipefail
repo=${DRB_REPO:-$(git rev-parse --show-toplevel)}
cd "$repo" || exit 1
MAIN=${DRB_MAIN:-main}
LOG=${DRB_LOG:-$repo/.ds4/driver-replay-batch.log}
FLAKES=${DRB_FLAKE_LOG:-$repo/.ds4/driver-flakes.log}
PAUSE=${PAUSE:-$repo/.ds4/orchestrator/pause}
PMLOG=${DRB_POSTMERGE_LOG:-$repo/.ds4/postmerge-batch.log}
ISSUES=${DRB_ISSUES:-$repo/.ds4/issues}
RUNS=${DRB_RUNS:-/mnt/sata/gorge-training/xmageoracle/runs}
POLL=${DRB_POLL:-120}
COOLDOWN=${DRB_COOLDOWN:-1800}
LASTMAIN=${DRB_LASTMAIN:-$repo/.ds4/driver-replay-last-main}
DRIFTLOG=${DRB_DRIFT_LOG:-$repo/.ds4/driver-drift.log}
GEN_PATHS=(compliance/oraclegen cmd/oraclediff compliance/manifests)
FLAKE_RUNS=4
KEEP_RUNS=3

read -ra LOCKRUN <<<"${DRB_LOCKRUN:-$repo/scripts/heavy_lock.sh run --}"
read -ra WORKTREE_CMD <<<"${DRB_WORKTREE_CMD:-scripts/agent-worktree.sh}"
read -ra REPLAY_CMD <<<"${DRB_REPLAY_CMD:-validation/oracle/full-replay.sh}"
read -ra COMPARE_CMD <<<"${DRB_COMPARE_CMD:-python3 validation/oracle/verdict-compare.py}"
read -ra SCEN_CMD <<<"${DRB_SCENARIO_CMD:-scripts/xmage-oracle-run.sh}"
read -ra GEN_CMD <<<"${DRB_GEN_CMD:-}"
read -ra RATCHET_CMD <<<"${DRB_RATCHET_CMD:-go run ./cmd/oraclediff status -all -write-ratchet}"

mkdir -p "$(dirname "$LOG")" "$(dirname "$FLAKES")"
say() { echo "$(date '+%F %T') $*" | tee -a "$LOG"; }

# One instance only (a second unit would race the same worktrees).
exec 9>"$(dirname "$LOG")/driver-replay-batch.lock" || exit 1
flock -n 9 || { echo "driver_replay_batch: already running" >&2; exit 1; }

# capped <cmd...>: the seat test budget (2 GB / 2 vCPU), run in the cwd.
capped() {
  systemd-run --user --scope -q -p MemoryMax=2G -p CPUQuota=200% \
    env GOMAXPROCS=2 GOMEMLIMIT=1536MiB GOFLAGS="-p=2 -trimpath" "$@"
}

# checks <pre|post> <logfile>, run in the integration worktree. pre is the
# build and the tests that do not read the verdict records; post adds the
# compliance gates that DO read them (stale until the replay has written them).
checks() {
  (
    cd "$wt" || exit 1
    if [ -n "${DRB_CHECK_CMD:-}" ]; then $DRB_CHECK_CMD "$1"; exit; fi
    if [ "$1" = pre ]; then capped go build ./... || exit 1; fi
    capped go test -timeout 2m ./compliance/oraclegen ./compliance/oraclegen/templates ./cmd/oraclediff || exit 1
    if [ "$1" = post ]; then capped go test -timeout 2m ./compliance/adopt ./compliance/gate ./compliance || exit 1; fi
    capped go test -timeout 2m -run Oracle ./rules || exit 1
  ) >"$2" 2>&1
}

# ---- python helpers (stdlib only; no agentctl) -----------------------------
PYHELP=$(
  cat <<'PY'
import json, re, sys, glob, os

def normalize_ids(x):
    if isinstance(x, dict):
        return {k: normalize_ids(v) for k, v in x.items()}
    if isinstance(x, list):
        return [normalize_ids(v) for v in x]
    if isinstance(x, str):
        x = re.sub(r"\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b", "<uuid>", x)
        return re.sub(r"\[[0-9a-f]{3}\]", "[<id>]", x)
    return x

def canon(path):
    """One xmage.jsonl snapshot with per-run timings and object IDs dropped."""
    out = []
    for line in open(path):
        if line.strip():
            r = json.loads(line)
            for k in ("ms", "xmage_ms"):
                r.pop(k, None)
            out.append(normalize_ids(r))
    return out

def leaves(x, p=""):
    if isinstance(x, dict):
        for k in sorted(x):
            yield from leaves(x[k], p + "/" + k)
    elif isinstance(x, list):
        for i, v in enumerate(x):
            yield from leaves(v, p + "[%d]" % i)
    else:
        yield p, x

cmd, args = sys.argv[1], sys.argv[2:]
if cmd == "parked":
    # parked ISSUES_DIR -> TSV: parkstamp id branch heldmain heldbranch
    rows = []
    for f in glob.glob(os.path.join(args[0], "*.md")):
        txt = open(f).read()
        head, _, hist = txt.rpartition("\n## History\n")
        if not hist:
            continue
        m = re.match(r"---\n(.*?)\n---", txt, re.S)
        fm = dict(x.groups() for x in re.finditer(r"^(\w+):[ \t]*(.*)$", m.group(1), re.M)) if m else {}
        if fm.get("status") != "human_needed":
            continue
        park = None
        held = ("", "")
        for line in hist.split("\n"):
            m = re.match(r"^- (\S+) gate .*xmage driver needs host replay.* failed and is marked on_fail", line)
            if m:
                park, held = m.group(1), ("", "")
                continue
            m = re.match(r"^- \S+ driver_replay_batch: HELD .* main=(\S+) branch=(\S+)", line)
            if m and park:
                held = (m.group(1), m.group(2))
        if park:
            rows.append((park, fm["id"], fm.get("branch", ""), held[0], held[1]))
    for r in sorted(rows):
        print("\t".join(r))
elif cmd == "regressed":
    # regressed COMPARE_OUT -> "N" then TSV id status detail
    n, rows = None, []
    for line in open(args[0]):
        line = line.rstrip("\n")
        m = re.match(r"^regressed (\d+)", line)
        if m:
            n = int(m.group(1))
            continue
        m = re.match(r"^  R (.+?/v\d+) (\S+)(?: (.*))?$", line)
        if m:
            rows.append((m.group(1), m.group(2), m.group(3) or ""))
        elif line.startswith("  R "):
            sys.exit("unparseable regressed row: " + line)
    if n is None or n != len(rows):
        sys.exit("regressed count %r does not match %d rows" % (n, len(rows)))
    print(n)
    for r in rows:
        print("\t".join(r))
elif cmd == "row":
    # row ID scen.jsonl... -> the scenario row for that id
    for p in args[1:]:
        for line in open(p):
            if line.strip() and json.loads(line).get("id") == args[0]:
                print(line.rstrip("\n"))
                sys.exit(0)
    sys.exit(1)
elif cmd == "rowfile":
    # rowfile ID scen.jsonl... -> the first file holding a scenario row for that id
    for p in args[1:]:
        for line in open(p):
            if line.strip() and json.loads(line).get("id") == args[0]:
                print(p)
                sys.exit(0)
    sys.exit(1)
elif cmd == "same":
    a, b = (canon(p) for p in args[:2])
    sys.exit(0 if a == b else 1)
elif cmd == "vary":
    # vary A B... -> first differing leaf paths between the first and any other
    base = dict(leaves(canon(args[0])))
    seen = []
    for p in args[1:]:
        o = dict(leaves(canon(p)))
        for k in sorted(set(base) | set(o)):
            if base.get(k) != o.get(k) and k not in seen:
                seen.append(k)
    print(" ".join(seen[:4]) or "(none)")
elif cmd == "agrees":
    # agrees VERDICTS.jsonl ID -> exit 0 iff that row's verdict status is agree
    for line in open(args[0]):
        if line.strip():
            r = json.loads(line)
            if r.get("id") == args[1]:
                st = r.get("status") or (r.get("verdict") or {}).get("status", "")
                sys.exit(0 if str(st).lower() == "agree" else 1)
    sys.exit(1)
elif cmd == "scenariochanged":
    # scenariochanged WT MAINSHA ID: true iff both verdict rows have different scenario hashes.
    wt, ref, rid = args
    import subprocess
    current = None
    for f in sorted(glob.glob(os.path.join(wt, "compliance/verdicts/*.jsonl"))):
        for line in open(f):
            if line.strip() and json.loads(line).get("id") == rid:
                current = json.loads(line).get("scenario_sha")
                break
        if current is not None:
            break
    if current is None:
        sys.exit(1)
    for f in sorted(glob.glob(os.path.join(wt, "compliance/verdicts/*.jsonl"))):
        rel = "compliance/verdicts/" + os.path.basename(f)
        show = subprocess.run(["git", "-C", wt, "show", ref + ":" + rel], capture_output=True, text=True, check=True).stdout
        for line in show.split("\n"):
            if line.strip() and json.loads(line).get("id") == rid:
                original = json.loads(line).get("scenario_sha")
                sys.exit(0 if current and original and current != original else 1)
    sys.exit(1)
elif cmd == "keepmain":
    # keepmain WT MAINSHA ID: put main's verdict row for ID back in WT's file
    wt, ref, rid = args
    import subprocess
    for f in sorted(glob.glob(os.path.join(wt, "compliance/verdicts/*.jsonl"))):
        lines = open(f).read().split("\n")
        idx = [i for i, l in enumerate(lines) if l.strip() and json.loads(l).get("id") == rid]
        if not idx:
            continue
        rel = "compliance/verdicts/" + os.path.basename(f)
        show = subprocess.run(["git", "-C", wt, "show", ref + ":" + rel], capture_output=True, text=True, check=True).stdout
        old = [l for l in show.split("\n") if l.strip() and json.loads(l).get("id") == rid]
        if not old:
            sys.exit("main has no row " + rid)
        lines[idx[0]] = old[0]
        open(f, "w").write("\n".join(lines))
        sys.exit(0)
    sys.exit("no verdict row " + rid + " in " + wt)
elif cmd == "mainrow":
    wt, ref, rid = args
    import subprocess
    for f in sorted(glob.glob(os.path.join(wt, "compliance/verdicts/*.jsonl"))):
        rel = "compliance/verdicts/" + os.path.basename(f)
        show = subprocess.run(["git", "-C", wt, "show", ref + ":" + rel], capture_output=True, text=True).stdout
        for l in show.split("\n"):
            if l.strip() and json.loads(l).get("id") == rid:
                r = json.loads(l)
                print((r.get("status", "") + " " + str(r.get("detail", ""))).strip()[:200])
                sys.exit(0)
PY
)
py() { python3 -c "$PYHELP" "$@"; }

# issue_tool <log|merged> <id> <message>: History line (and status) under the
# IssueStore lock. Never edit .ds4/issues/*.md without that lock.
issue_tool() {
  if [ -n "${DRB_ISSUE_TOOL:-}" ]; then "$DRB_ISSUE_TOOL" "$@"; return; fi
  PYTHONPATH=$HOME/.agentctl/pins/current python3 - "$repo" "$@" <<'PY'
import sys
from pathlib import Path
from agentctl.ledger.issues import IssueStore
repo, op, iid, msg = sys.argv[1:5]
s = IssueStore(Path(repo) / ".ds4" / "issues")
with s.locked():
    i = s.load(iid)
    if op == "merged":
        i.status = "merged"
    i.log(msg)
    s.save(i)
PY
}

# file_ticket <title> <brief>: queue one new agentctl ticket.
file_ticket() {
  if [ -n "${DRB_TICKET_TOOL:-}" ]; then "$DRB_TICKET_TOOL" "$@"; return; fi
  (cd "$repo" && PYTHONPATH=$HOME/.agentctl/pins/current python3 -m agentctl issue add "$repo" --title "$1" --brief "$2" --priority 2)
}

# ---- gate conditions ---------------------------------------------------------
main_green() {
  [ -e "$PMLOG" ] || return 1
  [ "$(/usr/bin/grep -E '^[0-9-]+ [0-9:]+ (GREEN|RED|STILL) ' "$PMLOG" | tail -n1 | awk '{print $3}')" = GREEN ]
}
sha12() { git rev-parse --verify -q "$1" | cut -c1-12; }

# last_main: the main sha the last full replay ran on ("" if unknown).
last_main() {
  local s=""
  [ -s "$LASTMAIN" ] && s=$(head -n1 "$LASTMAIN")
  if [ -z "$s" ] && [ -e "$LOG" ]; then s=$(/usr/bin/grep -oE ' LANDED [0-9a-f]{7,}' "$LOG" | tail -n1 | awk '{print $2}'); fi
  [ -n "$s" ] && git rev-parse --verify -q "$s^{commit}"
}
record_last_main() { git rev-parse --verify -q "$1^{commit}" >"$LASTMAIN.tmp" && mv -f "$LASTMAIN.tmp" "$LASTMAIN"; }
# gen_key <ref>: identifies the driver + generator at <ref> (their tree ids).
gen_key() { git ls-tree -d "$1" -- tools/xmageoracle compliance/oraclegen cmd/oraclediff compliance/manifests | sha1sum | cut -c1-12; }
# drift_known <row>: 0 iff a DRIFT replay of this driver+generator regressed the row.
drift_known() {
  [ -e "$DRIFTLOG" ] || return 1
  /usr/bin/grep -qF -- " $(gen_key "$MAIN") $1 | " "$DRIFTLOG"
}

# ---- per-pass state ----------------------------------------------------------
declare -A BR       # ticket id -> branch
declare -A WT_OF    # ticket id -> worktree dir
MERGED=()           # ticket ids on the integration branch, merge order
SCEN_ROOT=""        # the replay dir whose <SET>/scen.jsonl rows one_scenario reads
stamp=""; bid=""; bbranch=""; wt=""; probe=""; run=""; M0=""

# hold <id> <why> <main|-> : History line; the ticket stays parked.
hold() {
  issue_tool log "$1" "driver_replay_batch: HELD $2 main=$3 branch=$(sha12 "${BR[$1]}")"
}

# build_batch <ids...>: reset the integration branch to main and merge each
# branch oldest first. A conflicting branch is skipped (aborted, held), never
# resolved. Sets MERGED.
build_batch() {
  MERGED=()
  # The replay leaves verdict/triage edits in the tree: drop them, or a rebuilt
  # batch would carry the dropped branch's results.
  git -C "$wt" switch -q --discard-changes -C "$bbranch" "$MAIN" || return 1
  git -C "$wt" clean -fdq -- compliance
  local id
  for id in "$@"; do
    if git -C "$wt" merge -q --no-ff --no-edit "${BR[$id]}" >/dev/null 2>&1; then
      MERGED+=("$id")
      say "MERGED-INTO-BATCH $id (${BR[$id]})"
    else
      git -C "$wt" merge --abort >/dev/null 2>&1
      say "SKIP-CONFLICT $id (${BR[$id]}) conflicts on the batch; left parked"
      hold "$id" "merge conflict on the batch" "$M0"
    fi
  done
}

cleanup_pass() { # cleanup_pass <landed 0|1>
  [ -n "$probe" ] && git worktree remove --force "$probe" >/dev/null 2>&1
  [ -n "$wt" ] && git worktree remove --force "$wt" >/dev/null 2>&1
  if [ "$1" = 1 ]; then git branch -q -d "$bbranch" >/dev/null 2>&1; else git branch -q -D "$bbranch" >/dev/null 2>&1; fi
  git worktree prune >/dev/null 2>&1
  # keep only the newest $KEEP_RUNS run dirs of ours
  # shellcheck disable=SC2012
  ls -1dt "$RUNS"/driver-batch-* 2>/dev/null | tail -n +$((KEEP_RUNS + 1)) | while read -r d; do
    case $d in "$RUNS"/driver-batch-*) rm -rf -- "$d" ;; esac
  done
}

# one_scenario <wtdir> <id> <outfile> [scen.jsonl]: replay ONE scenario row on
# that tree's driver (xmage-oracle-run.sh compiles the driver of the tree it
# runs from). The row comes from the batch's replay unless a scen.jsonl is given.
one_scenario() {
  local dir=$1 id=$2 out=$3 src=${4:-} in
  in="$out.in.jsonl"
  rm -f -- "$out"
  if [ -n "$src" ]; then py row "$id" "$src" >"$in" || return 1
  else py row "$id" "$SCEN_ROOT"/*/scen.jsonl >"$in" || return 1; fi
  (cd "$dir" && GOFLAGS="-p=2 -trimpath" XMAGE_ORACLE_MEM=6G "${LOCKRUN[@]}" "${SCEN_CMD[@]}" "$in" "$out") >/dev/null 2>&1
  [ -s "$out" ]
}

# known_flaky <id>: 0 iff .ds4/driver-flakes.log already lists that row id. A row
# that varied once is not re-tested: four equal replays prove nothing about it
# (92a06b05 was HELD for a row flaked in the pass before).
known_flaky() {
  local line
  [ -e "$FLAKES" ] || return 1
  while IFS= read -r line; do
    case $line in *" $1 "* | *" $1") return 0 ;; esac
  done <"$FLAKES"
  return 1
}

# touches_generator <ticket>: 0 iff that branch changes the scenario generator.
touches_generator() {
  git diff --name-only "$MAIN"..."${BR[$1]}" -- compliance/oraclegen cmd/oraclediff 2>/dev/null | /usr/bin/grep -q .
}

# regen_row <probe> <id> <outfile>: regenerate the scenario row <id> on <probe>'s
# generator (level B, that row's set only) into <outfile> (a one-row scen.jsonl).
regen_row() {
  local dir=$1 id=$2 out=$3 f set gen
  f=$(py rowfile "$id" "$SCEN_ROOT"/*/scen.jsonl) || return 1
  set=$(basename "$(dirname "$f")")
  gen="$out.gen.jsonl"
  rm -f -- "$gen" "$out"
  if [ "${#GEN_CMD[@]}" -gt 0 ]; then
    (cd "$dir" && "${GEN_CMD[@]}" "$set" "$gen") >/dev/null 2>&1 || return 1
  else
    (cd "$dir" && capped go run ./cmd/oraclediff gen -level "${ORACLE_LEVEL:-B}" -manifest "compliance/manifests/$set.json" -out "$gen") >/dev/null 2>&1 || return 1
  fi
  py row "$id" "$gen" >"$out" || return 1
  [ -s "$out" ]
}

# flaky_check <id>: 0 if FLAKE_RUNS replays on the integration driver differ
# (timing fields ignored), else 1; 2 if the scenario could not be replayed.
FLAKE_FIELD=""
flaky_check() {
  local id=$1 d i outs=()
  d="$run/flake-$(echo "$id" | tr -c 'A-Za-z0-9\n' _)"; mkdir -p "$d"
  for ((i = 1; i <= FLAKE_RUNS; i++)); do
    one_scenario "$wt" "$id" "$d/x$i.jsonl" || return 2
    outs+=("$d/x$i.jsonl")
  done
  for ((i = 1; i < FLAKE_RUNS; i++)); do
    if ! py same "${outs[0]}" "${outs[$i]}"; then FLAKE_FIELD=$(py vary "${outs[@]}"); return 0; fi
  done
  return 1
}

# cleared <id> <xmage.jsonl> <scen row>: the row is cleared iff the oraclediff
# verdict on that snapshot is agree (a row agree on main regressed to non-agree,
# so agree again is exactly "no longer regressed"; a snapshot merely equal to
# main's driver would also pass when the regression is not the driver's at all).
cleared() {
  local id=$1 xm=$2 scen=$3 v="$2.verdict.jsonl"
  if [ -n "${DRB_DIFF_CMD:-}" ]; then
    $DRB_DIFF_CMD "$scen" "$xm" "$v" >/dev/null 2>&1
  else
    "$SCEN_ROOT/oraclediff" diff -scenarios "$scen" -xmage "$xm" -out "$v" -xmage-ref "$(sed -n 's/^XMAGE_REF *?= *//p' "$wt/Makefile")" >/dev/null 2>&1
  fi
  [ -s "$v" ] && py agrees "$v" "$id"
}

# attribute <stable ids...>: sets CULPRIT[id]=ticket for the newest-first
# branch whose removal clears the row. Rows it cannot attribute stay unset.
declare -A CULPRIT
attribute() {
  local -a todo=("$@")
  local cand k r other d
  CULPRIT=()
  [ -d "$probe" ] || git worktree add -q --detach "$probe" "$MAIN" || return 1
  d="$run/attr"; mkdir -p "$d"
  for ((k = ${#MERGED[@]} - 1; k >= 0 && ${#todo[@]} > 0; k--)); do
    cand=${MERGED[$k]}
    git -C "$probe" switch -q --detach "$MAIN" || continue
    local ok=1
    for other in "${MERGED[@]}"; do
      [ "$other" = "$cand" ] && continue
      git -C "$probe" merge -q --no-ff --no-edit "${BR[$other]}" >/dev/null 2>&1 || { git -C "$probe" merge --abort >/dev/null 2>&1; ok=0; break; }
    done
    [ "$ok" = 1 ] || { say "ATTRIBUTE cannot build the batch without $cand (conflict); skipping it as a candidate"; continue; }
    local -a left=()
    local gen=0
    touches_generator "$cand" && gen=1
    for r in "${todo[@]}"; do
      local tag src=""
      tag=$(echo "$r" | tr -c 'A-Za-z0-9\n' _)
      if [ "$gen" = 1 ]; then
        # The branch changes the scenario itself: replaying the batch's row
        # without it can never clear, so regenerate the row without it.
        src="$d/regen-$cand-$tag.jsonl"
        regen_row "$probe" "$r" "$src" || { say "ATTRIBUTE cannot regenerate row $r without $cand; skipping it as a candidate"; left+=("$r"); continue; }
      fi
      if one_scenario "$probe" "$r" "$d/without-$cand-$tag.jsonl" "$src" &&
        cleared "$r" "$d/without-$cand-$tag.jsonl" "$d/without-$cand-$tag.jsonl.in.jsonl"; then
        CULPRIT[$r]=$cand
      else
        left+=("$r")
      fi
    done
    todo=("${left[@]}")
  done
}

# ---- the pass ----------------------------------------------------------------
INFRA_FAIL_AT=0
infra_fail() { say "ABORT $1 (no ticket changed; retry after ${COOLDOWN}s)"; INFRA_FAIL_AT=$(date +%s); }

# select_parked: fills PIDS (oldest park first) from eligible parked tickets.
PIDS=()
select_parked() {
  PIDS=()
  local park id branch hm hb bhead
  while IFS=$'\t' read -r park id branch hm hb; do
    [ -n "$id" ] || continue
    bhead=$(sha12 "refs/heads/$branch")
    if [ -z "$bhead" ]; then say "SKIP-NOBRANCH $id: branch $branch does not exist"; continue; fi
    if [ -n "$hb" ] && [ "$hb" = "$bhead" ] && { [ "$hm" = "-" ] || [ "$hm" = "$(sha12 "$MAIN")" ]; }; then continue; fi
    BR[$id]=$branch
    WT_OF[$id]=$repo/.worktrees/$id
    PIDS+=("$id")
  done < <(py parked "$ISSUES")
}

# classify <rdir> <use drift log 0|1>: sort the regressed rows of <rdir>.regressed
# into FLAKY (varies on the same driver, or listed in driver-flakes.log), DRIFTED
# (the drift log holds it for this driver+generator) and STABLE; main's row is put
# back for the first two. Returns 1 with CLASSIFY_ERR set on an infra failure.
STABLE=(); FLAKY=(); DRIFTED=(); CLASSIFY_ERR=""
declare -A DET=()
classify() {
  local id status detail rc
  STABLE=(); FLAKY=(); DRIFTED=(); DET=(); CLASSIFY_ERR=""
  while IFS=$'\t' read -r id status detail; do
    DET[$id]="$status $detail"
    if known_flaky "$id"; then
      say "FLAKY $id (listed in $(basename "$FLAKES")): known flake, not re-tested; keeping main's row"
      FLAKY+=("$id")
      continue
    fi
    if [ "$2" = 1 ] && drift_known "$id"; then
      say "DRIFT-KNOWN $id: ${DET[$id]} -- regressed on the DRIFT replay of this driver+generator; not blamed on the batch, keeping main's row"
      DRIFTED+=("$id")
      continue
    fi
    flaky_check "$id"; rc=$?
    if [ "$rc" -eq 2 ]; then CLASSIFY_ERR="could not replay single scenario $id"; return 1; fi
    if [ "$rc" -eq 0 ] && py scenariochanged "$wt" "$MAIN" "$id"; then
      say "STABLE $id ($FLAKE_FIELD): scenario differs from main; attributing instead of keeping main's stale row"
      STABLE+=("$id")
    elif [ "$rc" -eq 0 ]; then
      say "FLAKY $id ($FLAKE_FIELD): $FLAKE_RUNS replays on the batch driver differ; keeping main's row"
      echo "$(date '+%F %T') $bid ${id%%/*} $id varies: $FLAKE_FIELD" >>"$FLAKES"
      FLAKY+=("$id")
    else
      STABLE+=("$id")
    fi
  done < <(tail -n +2 "$1.regressed")
  for id in "${FLAKY[@]}" "${DRIFTED[@]}"; do
    py keepmain "$wt" "$MAIN" "$id" || { CLASSIFY_ERR="could not restore main's row for $id"; return 1; }
  done
}

land() { # land <regressed count> <flaky count>; returns 0 landed, 1 aborted/held, 2 redo
  local id sha lbl=${MERGED[*]:-drift-refresh}
  (cd "$wt" && "${RATCHET_CMD[@]}") >"$run.ratchet.log" 2>&1 || { infra_fail "ratchet command failed (see $run.ratchet.log)"; return 1; }
  git -C "$wt" add -A -- compliance/verdicts compliance/triage compliance/ratchet.json
  if ! git -C "$wt" diff --cached --quiet; then
    git -C "$wt" commit -q -m "compliance: driver batch replay $bid ($(echo "$lbl" | tr ' ' ','))" || { infra_fail "commit failed"; return 1; }
  fi
  if ! git -C "$wt" merge -q --no-edit "$MAIN" >/dev/null 2>&1; then
    git -C "$wt" merge --abort >/dev/null 2>&1
    say "REDO main moved and conflicts with the batch; the replay is stale, redoing next loop"
    return 2
  fi
  if [ "${#MERGED[@]}" -eq 0 ] && [ "$(git -C "$wt" rev-list --count "$MAIN..HEAD")" = 0 ]; then
    say "DRIFT-REFRESH $bid: the replay changed no verdict; nothing to land"
    record_last_main "$M0"
    return 0
  fi
  if ! checks post "$run.checks-post.log"; then
    infra_fail "post-replay compliance gates red (see $run.checks-post.log)"
    return 1
  fi
  if [ -e "$PAUSE" ] || ! main_green; then
    say "REDO main is not green / pipeline paused at landing time; nothing landed"
    return 2
  fi
  if [ "$(git symbolic-ref --short HEAD 2>/dev/null)" != "$MAIN" ]; then
    infra_fail "main checkout is not on $MAIN"
    return 1
  fi
  if ! git merge -q --no-ff -m "merge($bid): $lbl" "$bbranch" >/dev/null 2>&1; then
    git merge --abort >/dev/null 2>&1
    infra_fail "merge into $MAIN failed"
    return 1
  fi
  sha=$(git rev-parse --short=9 HEAD)
  say "LANDED $sha $lbl"
  # A generator change that reached main after M0 was not replayed: keep M0.
  if git diff --quiet "$M0" HEAD^1 -- "${GEN_PATHS[@]}" 2>/dev/null; then record_last_main HEAD; else record_last_main "$M0"; fi
  for id in "${MERGED[@]}"; do
    issue_tool merged "$id" "driver_replay_batch: landed in $sha (replay $run, $1 regressed, $2 flaky kept as main)"
    git worktree remove --force "${WT_OF[$id]}" >/dev/null 2>&1
    git branch -q -d "${BR[$id]}" >/dev/null 2>&1
  done
  return 0
}

pass() {
  local ids rc n r id status detail i attempt landed=0
  select_parked
  [ "${#PIDS[@]}" -ge 1 ] || { drift_pass; return 0; }
  if [ -e "$PAUSE" ]; then say "IDLE pipeline paused (${#PIDS[@]} parked)"; return 0; fi
  if ! main_green; then say "IDLE main is not GREEN in $PMLOG (${#PIDS[@]} parked)"; return 0; fi
  if [ "$INFRA_FAIL_AT" -gt 0 ] && [ $(($(date +%s) - INFRA_FAIL_AT)) -lt "$COOLDOWN" ]; then return 0; fi

  stamp=$(date -u +%Y%m%dT%H%M%SZ); bid=driver-batch-$stamp; bbranch=wt/$bid
  wt=$repo/.worktrees/$bid; probe=$repo/.worktrees/$bid-probe; run=$RUNS/$bid
  M0=$(sha12 "$MAIN")
  mkdir -p "$run"
  say "START $bid main=$M0 parked: ${PIDS[*]}"
  if ! "${WORKTREE_CMD[@]}" "$bid" "$MAIN" >"$run.worktree.log" 2>&1; then
    infra_fail "could not create the integration worktree (see $run.worktree.log)"
    cleanup_pass 0; return 0
  fi
  build_batch "${PIDS[@]}" || { infra_fail "could not reset the batch to $MAIN"; cleanup_pass 0; return 0; }
  if [ "${#MERGED[@]}" -eq 0 ]; then say "NOTHING mergeable this pass"; cleanup_pass 0; return 0; fi

  # Pre-checks: attribute a red batch to branches that fail alone before
  # blaming an otherwise-green branch for a combination regression.
  until checks pre "$run.checks.log"; do
    local -a batch=() red_alone=() remaining=()
    local n i j later earlier found_pair=0
    batch=("${MERGED[@]}")
    n=${#batch[@]}
    if [ "$n" -eq 1 ]; then
      id=${batch[0]}
      say "CHECKS-RED with $id alone (see $run.checks.log); dropping it"
      hold "$id" "build/tests red alone" "$M0"
      build_batch || { infra_fail "rebuild failed"; cleanup_pass 0; return 0; }
    else
      # Probe one branch at a time, newest first, using the same capped checks.
      for ((i = n - 1; i >= 0; i--)); do
        id=${batch[$i]}
        build_batch "$id" || { infra_fail "rebuild failed during pre-check attribution"; cleanup_pass 0; return 0; }
        if ! checks pre "$run.checks-alone-$id.log"; then red_alone+=("$id"); fi
      done
      if [ "${#red_alone[@]}" -gt 0 ]; then
        for id in "${red_alone[@]}"; do
          say "CHECKS-RED-ALONE $id; holding it"
          hold "$id" "build/tests red alone" "$M0"
        done
        for id in "${batch[@]}"; do
          case " ${red_alone[*]} " in *" $id "*) ;; *) remaining+=("$id") ;; esac
        done
      else
        # All singles passed. Find an interacting pair, newest member first.
        for ((i = n - 1; i >= 1 && found_pair == 0; i--)); do
          later=${batch[$i]}
          for ((j = i - 1; j >= 0; j--)); do
            earlier=${batch[$j]}
            build_batch "$earlier" "$later" || { infra_fail "rebuild failed during combination attribution"; cleanup_pass 0; return 0; }
            if ! checks pre "$run.checks-pair-$earlier-$later.log"; then
              say "CHECKS-RED-COMBINATION $earlier $later; holding later branch $later"
              hold "$later" "build/tests red in combination with $earlier" "$M0"
              found_pair=1
              break
            fi
          done
        done
        if [ "$found_pair" -eq 0 ]; then
          # A higher-order interaction remains unattributed; conservatively
          # remove the newest member, with the pair we last tested recorded.
          later=${batch[$((n - 1))]}; earlier=${batch[$((n - 2))]}
          say "CHECKS-RED-COMBINATION $earlier $later; holding later branch $later"
          hold "$later" "build/tests red in combination with $earlier" "$M0"
        fi
        for id in "${batch[@]}"; do [ "$id" = "$later" ] || remaining+=("$id"); done
      fi
      build_batch "${remaining[@]}" || { infra_fail "rebuild failed"; cleanup_pass 0; return 0; }
    fi
    if [ "${#MERGED[@]}" -eq 0 ]; then say "NOTHING left after pre-checks"; cleanup_pass 0; return 0; fi
  done

  for ((attempt = 0; attempt <= ${#PIDS[@]}; attempt++)); do
    rm -rf -- "$run/replay$attempt" && mkdir -p "$run/replay$attempt"
    local rdir=$run/replay$attempt
    (cd "$wt" && GOFLAGS="-p=2 -trimpath" XMAGE_ORACLE_MEM=6G "${LOCKRUN[@]}" "${REPLAY_CMD[@]}" "$rdir" >"$rdir.log" 2>&1)
    rc=$?
    say "REPLAY rc=$rc run=$rdir ids=${MERGED[*]}"
    if [ "$rc" -ne 0 ] || /usr/bin/grep -qE ' (gen|xmage|diff) FAILED' "$rdir.log"; then
      infra_fail "full replay failed (see $rdir.log)"; cleanup_pass 0; return 0
    fi
    SCEN_ROOT=$rdir
    (cd "$wt" && "${COMPARE_CMD[@]}" "$repo" "$wt") >"$rdir.compare" 2>&1
    if ! py regressed "$rdir.compare" >"$rdir.regressed"; then
      infra_fail "unparseable verdict-compare output (see $rdir.compare)"; cleanup_pass 0; return 0
    fi
    n=$(head -n1 "$rdir.regressed")
    say "COMPARE regressed=$n ($rdir.compare)"
    if [ "$n" -eq 0 ]; then
      land 0 0; rc=$?
      [ "$rc" -eq 0 ] && landed=1
      cleanup_pass "$landed"; return 0
    fi

    classify "$rdir" 1 || { infra_fail "$CLASSIFY_ERR"; cleanup_pass 0; return 0; }

    if [ "${#STABLE[@]}" -eq 0 ]; then
      land 0 $((${#FLAKY[@]} + ${#DRIFTED[@]})); rc=$?
      [ "$rc" -eq 0 ] && landed=1
      cleanup_pass "$landed"; return 0
    fi

    attribute "${STABLE[@]}" || { infra_fail "could not set up the attribution worktree"; cleanup_pass 0; return 0; }
    local unattributed=0 drop=()
    for r in "${STABLE[@]}"; do
      if [ -n "${CULPRIT[$r]:-}" ]; then
        say "CULPRIT ${CULPRIT[$r]} row $r: ${DET[$r]} (clears when that branch is removed)"
        local mrow
        mrow=$(py mainrow "$wt" "$MAIN" "$r")
        hold "${CULPRIT[$r]}" "CULPRIT regresses row '$r': batch says '${DET[$r]:0:200}'; main's row says '$mrow'" "-"
        case " ${drop[*]:-} " in *" ${CULPRIT[$r]} "*) ;; *) drop+=("${CULPRIT[$r]}") ;; esac
      else
        unattributed=1
        say "UNATTRIBUTED row $r: ${DET[$r]} -- no single branch clears it"
      fi
    done
    if [ "$unattributed" = 1 ]; then
      for id in "${MERGED[@]}"; do
        case " ${drop[*]:-} " in *" $id "*) continue ;; esac
        hold "$id" "batch regresses rows no single branch clears (see $rdir.compare); nothing landed" "-"
      done
      say "HOLD nothing landed; all tickets left parked with the findings ($rdir.compare)"
      cleanup_pass 0; return 0
    fi
    local -a rest=()
    for id in "${MERGED[@]}"; do case " ${drop[*]} " in *" $id "*) ;; *) rest+=("$id") ;; esac; done
    if [ "${#rest[@]}" -eq 0 ]; then say "NOTHING left after dropping culprits"; cleanup_pass 0; return 0; fi
    build_batch "${rest[@]}" || { infra_fail "rebuild failed"; cleanup_pass 0; return 0; }
    [ "${#MERGED[@]}" -gt 0 ] || { cleanup_pass 0; return 0; }
    say "REPLAY-AGAIN without ${drop[*]}"
  done
  infra_fail "gave up after $attempt replays"; cleanup_pass 0
}

# drift_pass: no ticket is parked. When the scenario generator changed on main
# since the last replayed main, replay main itself (see the header), land the
# refreshed verdicts and file one ticket per regressed template class.
drift_pass() {
  local last cur rdir rc n id r commits cls key landed=0
  [ -e "$PAUSE" ] && return 0
  main_green || return 0
  if [ "$INFRA_FAIL_AT" -gt 0 ] && [ $(($(date +%s) - INFRA_FAIL_AT)) -lt "$COOLDOWN" ]; then return 0; fi
  cur=$(git rev-parse --verify -q "$MAIN^{commit}") || return 0
  last=$(last_main)
  if [ -z "$last" ]; then
    record_last_main "$cur"
    say "DRIFT-INIT no replayed main on record; recorded ${cur:0:12}, no pass"
    return 0
  fi
  git diff --quiet "$last" "$cur" -- "${GEN_PATHS[@]}" && return 0

  stamp=$(date -u +%Y%m%dT%H%M%SZ); bid=driver-batch-$stamp; bbranch=wt/$bid
  wt=$repo/.worktrees/$bid; probe=$repo/.worktrees/$bid-probe; run=$RUNS/$bid
  M0=$(sha12 "$MAIN"); MERGED=()
  commits=$(git log --format=%h "$last..$cur" -- "${GEN_PATHS[@]}" | tr '\n' ',' | sed 's/,$//')
  mkdir -p "$run"
  say "DRIFT-START $bid main=$M0 last=${last:0:12} generator commits=$commits"
  if ! "${WORKTREE_CMD[@]}" "$bid" "$MAIN" >"$run.worktree.log" 2>&1; then
    infra_fail "could not create the integration worktree (see $run.worktree.log)"
    cleanup_pass 0; return 0
  fi
  rdir=$run/replay0
  rm -rf -- "$rdir" && mkdir -p "$rdir"
  (cd "$wt" && GOFLAGS="-p=2 -trimpath" XMAGE_ORACLE_MEM=6G "${LOCKRUN[@]}" "${REPLAY_CMD[@]}" "$rdir" >"$rdir.log" 2>&1)
  rc=$?
  say "REPLAY rc=$rc run=$rdir ids=drift-main"
  if [ "$rc" -ne 0 ] || /usr/bin/grep -qE ' (gen|xmage|diff) FAILED' "$rdir.log"; then
    infra_fail "full replay failed (see $rdir.log)"; cleanup_pass 0; return 0
  fi
  SCEN_ROOT=$rdir
  (cd "$wt" && "${COMPARE_CMD[@]}" "$repo" "$wt") >"$rdir.compare" 2>&1
  if ! py regressed "$rdir.compare" >"$rdir.regressed"; then
    infra_fail "unparseable verdict-compare output (see $rdir.compare)"; cleanup_pass 0; return 0
  fi
  n=$(head -n1 "$rdir.regressed")
  say "COMPARE regressed=$n ($rdir.compare)"
  if [ "$n" -gt 0 ]; then
    classify "$rdir" 0 || { infra_fail "$CLASSIFY_ERR"; cleanup_pass 0; return 0; }
  else
    STABLE=(); FLAKY=()
  fi
  for r in "${STABLE[@]}"; do
    say "DRIFT $r ${DET[$r]} commits=$commits"
  done
  land "${#STABLE[@]}" "${#FLAKY[@]}"; rc=$?
  if [ "$rc" -ne 0 ]; then cleanup_pass 0; return 0; fi
  landed=1
  # Recorded only after the landing: a failed landing replays again next loop.
  key=$(gen_key "$MAIN")
  for r in "${STABLE[@]}"; do
    echo "$(date '+%F %T') $key $r | ${DET[$r]}" >>"$DRIFTLOG"
  done
  local -a classes=()
  for r in "${STABLE[@]}"; do
    cls=${r%/*}; cls=${cls##*/}; cls=${cls%%#*}
    case " ${classes[*]:-} " in *" $cls "*) ;; *) classes+=("$cls") ;; esac
  done
  for cls in "${classes[@]}"; do
    local rows=""
    for r in "${STABLE[@]}"; do
      id=${r%/*}; id=${id##*/}; id=${id%%#*}
      [ "$id" = "$cls" ] && rows+="- \`$r\`: ${DET[$r]}"$'\n'
    done
    file_ticket "XMage drift: $cls scenarios regressed after a generator change" "A DRIFT replay of main (driver_replay_batch, $bid) regressed these \`$cls\` rows against main's recorded verdicts. The XMage driver was not changed; the scenario generator was.

$rows
Candidate commits (compliance/oraclegen, cmd/oraclediff since the last replayed main ${last:0:12}): $commits

The refreshed verdicts are landed on main; this ticket fixes the generator or the driver so the rows agree again. Log: .ds4/driver-replay-batch.log (DRIFT lines), run $rdir." >/dev/null 2>&1 &&
      say "DRIFT-TICKET filed for class $cls" ||
      say "DRIFT-TICKET could not file the ticket for class $cls"
  done
  cleanup_pass "$landed"
}

sweep_stale() {
  local d
  for d in "$repo"/.worktrees/driver-batch-*; do
    [ -e "$d" ] || continue
    git worktree remove --force "$d" >/dev/null 2>&1
    git branch -q -D "wt/$(basename "$d")" >/dev/null 2>&1
  done
  git worktree prune >/dev/null 2>&1
}

while true; do
  sweep_stale
  pass
  [ "${DRB_ONCE:-0}" = 1 ] && break
  sleep "$POLL"
done
