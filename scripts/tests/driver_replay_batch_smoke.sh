#!/usr/bin/env bash
# driver_replay_batch_smoke.sh -- scripts/driver_replay_batch.sh's selection and
# decision logic against fakes: a temp git repo, fake issue files carrying the
# park History line, and stub replay / scenario / diff / check commands. The real
# validation/oracle/verdict-compare.py judges the verdict rows. No XMage, no Go
# build, no network, no systemd.
#
#   scripts/tests/driver_replay_batch_smoke.sh
#
# Stub conventions (all inside the integration / probe tree the script runs in):
#   tools/xmageoracle/*.regress  ids a branch makes regress, STABLY (bad output)
#   tools/xmageoracle/*.flaky    ids a branch makes regress with a VARYING output
#   tools/xmageoracle/*.badtest  the pre-check command fails while it exists
#   compliance/oraclegen/*.genregress  ids whose GENERATED scenario is bad (the
#                                generator changed; the driver is fine)
#   $STUB_ALWAYS                 ids that regress whatever the branches are
set -uo pipefail
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE
ROOT=$(git rev-parse --show-toplevel)
SCRIPT=${DRB_SCRIPT_UNDER_TEST:-$ROOT/scripts/driver_replay_batch.sh}  # a mutant, when proving the smoke can fail
TMP=$(mktemp -d "${TMPDIR:-/tmp}/drb-smoke.XXXXXX")
trap '[ -n "${DRB_SMOKE_KEEP:-}" ] || rm -rf "$TMP"; [ -z "${DRB_SMOKE_KEEP:-}" ] || echo "kept $TMP"' EXIT
export GIT_AUTHOR_NAME=smoke GIT_AUTHOR_EMAIL=smoke@example.invalid GIT_COMMITTER_NAME=smoke GIT_COMMITTER_EMAIL=smoke@example.invalid
fails=0
check() { # check <name> <exit status of the assertion> [detail]
	if [ "$2" = 0 ]; then
		printf 'ok   %s\n' "$1"
	else
		printf 'FAIL %s %s\n' "$1" "${3:-}"
		fails=$((fails + 1))
	fi
}
has() { /usr/bin/grep -qF -- "$2" "$1"; }                  # has <file> <fixed string>
hasnt() { ! /usr/bin/grep -qF -- "$2" "$1" 2>/dev/null; }

# ---- stubs --------------------------------------------------------------------
S=$TMP/stubs
mkdir -p "$S"
cat >"$S/worktree.sh" <<'EOF'
#!/usr/bin/env bash
git worktree add -q -b "wt/$1" ".worktrees/$1" "$2"
EOF
cat >"$S/replay.sh" <<'EOF'
#!/usr/bin/env bash
# replay.sh OUTDIR, in the integration worktree: "replays" every verdict row.
rdir=$1; mkdir -p "$rdir/S"
# The retained oraclediff binary executes the batch's gorge code, regardless
# of the cwd from which cleared invokes it.
printf '%s\n' "$PWD" >"$DRB_REPO/.ds4/stub-batch-tree"
[ -z "${STUB_REPLAY_RC:-}" ] || exit "$STUB_REPLAY_RC"
if [ -n "${STUB_MOVE_MAIN:-}" ]; then
	mf=${STUB_MOVE_FILE:-tools/xmageoracle/shared.txt}
	echo moved >"$STUB_MOVE_MAIN/$mf"
	git -C "$STUB_MOVE_MAIN" add "$mf"
	git -C "$STUB_MOVE_MAIN" commit -q -m "main moved during the replay"
fi
python3 - "$rdir/S/scen.jsonl" <<'PY'
import glob, json, os, sys
rdir = os.path.dirname(os.path.dirname(sys.argv[1]))
bad = set()
for pat in ("tools/xmageoracle/*.regress", "tools/xmageoracle/*.flaky", "compliance/oraclegen/*.genregress"):
    for f in glob.glob(pat):
        bad |= set(open(f).read().split("\n")) - {""}
if os.path.exists("rules/oracle_run.go"):
    bad |= set(open("rules/oracle_run.go").read().split("\n")) - {""}
if os.environ.get("STUB_ALWAYS") and os.path.exists(os.environ["STUB_ALWAYS"]):
    bad |= set(open(os.environ["STUB_ALWAYS"]).read().split("\n")) - {""}
with open(sys.argv[1], "w") as sc:
    for f in glob.glob("compliance/verdicts/*.jsonl"):
        rows = [json.loads(l) for l in open(f) if l.strip()]
        for r in rows:
            gb = set()
            for g in glob.glob("compliance/oraclegen/*.genregress"):
                gb |= set(open(g).read().split("\n")) - {""}
            if os.environ.get("STUB_RUNNER_SCEN") and os.path.exists("rules/oracle_run.go"):
                gb |= set(open("rules/oracle_run.go").read().split("\n")) - {""}
            sc.write(json.dumps({"id": r["id"], "card": r["card"], "scen": "bad" if r["id"] in gb else "ok"}) + "\n")
            if r["id"] in bad:
                r["status"] = "diverge"
                # The compliance row's detail, formatted as oraclediff's runDiff
                # does; diff.sh's verdict for the same snapshot carries the parts.
                od = set()
                for g in glob.glob("tools/xmageoracle/*.otherdet"):
                    od |= set(open(g).read().split("\n")) - {""}
                r["detail"] = 'setup graveyard: gorge "[Wastes x5]", xmage "%s"' % ("[Island]" if r["id"] in od else "[]")
            elif r["card"] == "Gamma":
                r["status"] = "agree"   # an improvement
        open(f, "w").write("\n".join(json.dumps(r) for r in rows) + "\n")
    sc.flush()
    scenarios = {}
    for line in open(sys.argv[1]):
        if line.strip():
            row = json.loads(line)
            scenarios[row["id"]] = row.get("scen", "ok")
    os.makedirs(os.path.join(rdir, "S"), exist_ok=True)
    # The retained snapshot is the BATCH's driver output: a candidate that adds a
    # tools/xmageoracle/*.regress driver change (every selected ticket has one)
    # is present here, so its row's state is bad. The probe replay, without the
    # candidate, is what turns it good again.
    driver_bad = set()
    for f in glob.glob("tools/xmageoracle/*.regress"):
        driver_bad |= set(open(f).read().split("\n")) - {""}
    with open(os.path.join(rdir, "S", "xmage.jsonl"), "w") as xm:
        for f in glob.glob("compliance/verdicts/*.jsonl"):
            for line in open(f):
                if line.strip():
                    r = json.loads(line)
                    state = "bad" if r["id"] in driver_bad else "good"
                    xv = "[]"
                    for g in glob.glob("tools/xmageoracle/*.otherdet"):
                        if r["id"] in set(open(g).read().split("\n")):
                            xv = "[Island]"
                    xm.write(json.dumps({"id": r["id"], "scen": scenarios.get(r["id"], "ok"), "state": state, "xv": xv, "ms": 1}) + "\n")
PY
echo "S ok"; echo done
EOF
cat >"$S/scen.sh" <<'EOF'
#!/usr/bin/env bash
# scen.sh IN OUT, in the tree whose driver is being tried.
id=$(python3 -c 'import json,sys; print(json.loads(open(sys.argv[1]).readline())["id"])' "$1")
scenario=$(python3 -c 'import json,sys; print(json.loads(open(sys.argv[1]).readline()).get("scen", "ok"))' "$1")
[ -z "${DRB_SCENARIO_TRACE:-}" ] || printf '%s %s %s\n' "$PWD" "$id" "$scenario" >>"$DRB_SCENARIO_TRACE"
state=good
d=tools/xmageoracle
for f in $d/*.regress; do [ -e "$f" ] && /usr/bin/grep -qxF -- "$id" "$f" && state=bad; done
[ -n "${STUB_ALWAYS:-}" ] && [ -e "$STUB_ALWAYS" ] && /usr/bin/grep -qxF -- "$id" "$STUB_ALWAYS" && state=bad
# Gorge's runner cannot change XMage's result for an unchanged scenario.
for f in $d/*.flaky; do [ -e "$f" ] && /usr/bin/grep -qxF -- "$id" "$f" && state="r$(date +%N)$RANDOM"; done
/usr/bin/grep -q '"scen": "bad"' "$1" && state=bad
extra=""
[ "${STUB_VOLATILE_IDS:-}" != "$id" ] || extra=",\"error\":\"object_id='$(cat /proc/sys/kernel/random/uuid)' [$(printf '%03x' "$((RANDOM % 4096))")]\""
xv='[]'
for f in $d/*.otherdet; do [ -e "$f" ] && /usr/bin/grep -qxF -- "$id" "$f" && xv='[Island]'; done
printf '{"id":"%s","scen":"%s","state":"%s","xv":"%s","ms":%s%s}\n' "$id" "$scenario" "$state" "$xv" "$((RANDOM + 1))" "$extra" >"$2"
EOF
cat >"$S/gen.sh" <<'EOF'
#!/usr/bin/env bash
# gen.sh SET OUT CARDS, in the tree whose generator is being tried.
if [ -n "${STUB_GEN_FAIL:-}" ]; then
	echo 'generator progress on stdout'
	printf 'missing corpus fixture\nsecond diagnostic\n' >&2
	exit 1
fi
python3 - "$2" "${3:-}" <<'PY'
import glob, json, os, sys
if os.environ.get("STUB_REQUIRE_CARDS") and (len(sys.argv) < 3 or sys.argv[2] != os.environ["DRB_REPO"] + "/.cards"):
    print("explicit cards directory required", file=sys.stderr)
    sys.exit(1)
gb = set()
for g in glob.glob("compliance/oraclegen/*.genregress"):
    gb |= set(open(g).read().split("\n")) - {""}
if os.environ.get("STUB_RUNNER_SCEN") and os.path.exists("rules/oracle_run.go"):
    gb |= set(open("rules/oracle_run.go").read().split("\n")) - {""}
with open(sys.argv[1], "w") as sc:
    for f in glob.glob("compliance/verdicts/*.jsonl"):
        for l in open(f):
            if l.strip():
                r = json.loads(l)
                sc.write(json.dumps({"id": r["id"], "card": r["card"], "scen": "bad" if r["id"] in gb else "ok"}) + "\n")
PY
EOF
cat >"$S/diff.sh" <<'EOF'
#!/usr/bin/env bash
# diff.sh SCEN XMAGE OUT: the oraclediff verdict for one snapshot.
# A call from main models the retained batch binary; a probe call models
# go run in that probe. Select the code tree, not an artificial verdict.
if [ "$PWD" = "$DRB_REPO" ]; then
	cd "$(head -n1 "$DRB_REPO/.ds4/stub-batch-tree")" || exit 1
fi
id=$(python3 -c 'import json,sys; print(json.loads(open(sys.argv[1]).readline())["id"])' "$1")
scenario=$(python3 -c 'import json,sys; print(json.loads(open(sys.argv[1]).readline()).get("scen", "ok"))' "$1")
state=$(python3 -c 'import json,sys; print(json.loads(open(sys.argv[1]).readline()).get("state", "good"))' "$1")
xmage=$(python3 - "$2" "$id" <<'PY'
import json, sys
for line in open(sys.argv[1]):
    row = json.loads(line)
    if row["id"] == sys.argv[2]:
        print(row.get("scen", "ok"), row.get("state", "good"))
        break
PY
)
st=DIVERGE
if [ "$scenario" = "$(echo "$xmage" | cut -d' ' -f1)" ] && [ "$(echo "$xmage" | cut -d' ' -f2)" = "good" ] && [ "$state" = "good" ]; then
  st=agree
  for f in compliance/oraclegen/*.genregress rules/oracle_run.go; do
    [ -e "$f" ] && /usr/bin/grep -qxF -- "$id" "$f" && st=DIVERGE
  done
fi
[ -z "${DRB_DIFF_TRACE:-}" ] || printf '%s %s %s %s %s\n' "$PWD" "$id" "$st" "$scenario" "$xmage" >>"$DRB_DIFF_TRACE"
# A divergence carries oraclediff.Verdict's parts (the -out file has no detail);
# with a 4th argument (-write DIR) the compliance row, detail and all, lands in
# DIR as runDiff's MergeVerdicts writes it.
xv=$(python3 - "$2" "$id" <<'PY'
import json, sys
for line in open(sys.argv[1]):
    row = json.loads(line)
    if row["id"] == sys.argv[2]:
        print(row.get("xv", "[]"))
        break
PY
)
[ -n "$xv" ] || xv='[]'
if [ "$st" = DIVERGE ]; then
  printf '{"id":"%s","card":"x","verdict":{"status":"%s","checkpoint":"setup","field":"graveyard","gorge":"[Wastes x5]","xmage":"%s"}}\n' "$id" "$st" "$xv" >"$3"
  if [ -n "${4:-}" ]; then
    mkdir -p "$4"
    printf '{"card":"x","id":"%s","status":"diverge","detail":"setup graveyard: gorge \\"[Wastes x5]\\", xmage \\"%s\\""}\n' "$id" "$xv" >"$4/x.jsonl"
  fi
else
  printf '{"id":"%s","card":"x","verdict":{"status":"%s"}}\n' "$id" "$st" >"$3"
  if [ -n "${4:-}" ]; then mkdir -p "$4"; printf '{"card":"x","id":"%s","status":"agree"}\n' "$id" >"$4/x.jsonl"; fi
fi
[ -z "${DRB_WRITE_TRACE:-}" ] || printf '%s write=%s\n' "$id" "${4:-}" >>"$DRB_WRITE_TRACE"
EOF
cat >"$S/check.sh" <<'EOF'
#!/usr/bin/env bash
[ "$1" = pre ] || exit 0
ls tools/xmageoracle/*.badtest >/dev/null 2>&1 && { echo "stub test red"; exit 1; }
ls tools/xmageoracle/*.redalone >/dev/null 2>&1 && { echo "stub branch red alone"; exit 1; }
[ -e tools/xmageoracle/t1.combo ] && [ -e tools/xmageoracle/t3.combo ] && { echo "stub combination red"; exit 1; }
exit 0
EOF
cat >"$S/issue.sh" <<'EOF'
#!/usr/bin/env bash
# issue.sh <log|merged> <id> <msg>: History is the file's last section.
f=$DRB_ISSUES/$2.md
[ "$1" = merged ] && sed -i 's/^status: .*/status: merged/' "$f"
printf -- '- 2026-10-06T09:00:00Z %s\n' "$3" >>"$f"
EOF
cat >"$S/ticket.sh" <<'EOF'
#!/usr/bin/env bash
# ticket.sh TITLE BRIEF: records one filed agentctl ticket.
printf 'TICKET %s\n%s\n--END--\n' "$1" "$2" >>"$DRB_REPO/tickets.out"
EOF
# Fake `systemd-run` + `go` so the production `capped go run ./cmd/oraclediff gen|diff`
# branches run (DRB_GEN_CMD / DRB_DIFF_CMD unset): they forward to gen.sh / diff.sh.
mkdir -p "$S/bin"
cat >"$S/bin/systemd-run" <<'EOF'
#!/usr/bin/env bash
while [ $# -gt 0 ] && [ "$1" != env ]; do shift; done
exec "$@"
EOF
cat >"$S/bin/go" <<'EOF'
#!/usr/bin/env bash
# go run ./cmd/oraclediff gen|diff FLAGS...
[ "$1 $2" = "run ./cmd/oraclediff" ] || { echo "unexpected go $*" >&2; exit 1; }
sub=$3; shift 3
declare -A f
while [ $# -gt 1 ]; do f[${1#-}]=$2; shift 2; done
printf '%s %s cards=%s\n' "$PWD" "$sub" "${f[cards]:-}" >>"$DRB_GO_TRACE"
case $sub in
gen) "$STUB_DIR/gen.sh" "$(basename "${f[manifest]}" .json)" "${f[out]}" "${f[cards]:-}" ;;
diff) "$STUB_DIR/diff.sh" "${f[scenarios]}" "${f[xmage]}" "${f[out]}" "${f[write]:-}" ;;
*) exit 1 ;;
esac
EOF
chmod +x "$S"/*.sh "$S"/bin/*

# ---- fixtures ------------------------------------------------------------------
# mkrepo <name>: a main checkout with three verdict rows (Gamma diverges on main).
mkrepo() {
	R=$TMP/$1
	mkdir -p "$R/compliance/verdicts" "$R/compliance/triage" "$R/tools/xmageoracle" "$R/.ds4/issues"
	git -C "$R" init -q -b main
	printf '.ds4\n.worktrees\n' >"$R/.git/info/exclude"
	printf 'XMAGE_REF ?= abc123\n' >"$R/Makefile"
	: >"$R/compliance/triage/.keep"
	echo '{}' >"$R/compliance/ratchet.json"
	echo base >"$R/tools/xmageoracle/base.txt"
	# every path gen_key and GEN_PATHS name must exist, or a key test cannot see it
	mkdir -p "$R/compliance/oraclegen" "$R/cmd/oraclediff" "$R/compliance/manifests"
	echo base >"$R/compliance/oraclegen/base.txt"
	echo base >"$R/cmd/oraclediff/base.txt"
	echo base >"$R/compliance/manifests/base.txt"
	{
		echo '{"id":"Alpha/cast-resolve/v1","card":"Alpha","status":"agree"}'
		echo '{"id":"Beta/cast-resolve/v1","card":"Beta","scenario_sha":"original-scenario-sha","status":"agree"}'
		echo '{"id":"Gamma/cast-resolve/v1","card":"Gamma","status":"diverge"}'
	} >"$R/compliance/verdicts/a.jsonl"
	git -C "$R" add -A && git -C "$R" commit -q -m init
	echo "2026-10-06 00:00:00 GREEN abcdef012 pushed (3s)" >"$R/.ds4/postmerge-batch.log"
}
# mkticket <id> <parkstamp> <file> <content> [status] [extra history line]: a
# ticket worktree on wt/<id> with one commit, and its parked issue file.
mkticket() {
	local id=$1 stamp=$2 file=$3 content=$4 status=${5:-human_needed} extra=${6:-}
	git -C "$R" worktree add -q -b "wt/$id" "$R/.worktrees/$id" main
	mkdir -p "$R/.worktrees/$id/$(dirname "$file")"
	printf '%s\n' "$content" >"$R/.worktrees/$id/$file"
	git -C "$R/.worktrees/$id" add -A && git -C "$R/.worktrees/$id" commit -q -m "feat: $id"
	cat >"$R/.ds4/issues/$id.md" <<EOF
---
id: $id
title: ticket $id
status: $status
branch: wt/$id
worktree: .worktrees/$id
---

## Brief
mentions gate xmage driver needs host replay failed and is marked on_fail = "park" (not History)

## History
- 2026-10-06T00:00:00Z queued via agentctl issue add
- $stamp gate xmage driver needs host replay failed and is marked on_fail = "park": an operator must clear it by hand and land the approved work; no round charged.
### xmage driver needs host replay
XMAGE DRIVER CHANGED -- parked for scripts/driver_replay_batch.sh (gorge-driver-replay-batch):
$file
EOF
	[ -z "$extra" ] || printf -- '- 2026-10-06T08:00:00Z %s\n' "$extra" >>"$R/.ds4/issues/$id.md"
}
status_of() { sed -n 's/^status: //p' "$R/.ds4/issues/$1.md"; }
# runpass: one pass of the script against $R.
runpass() {
	(
		cd "$R" || exit 1
		export DRB_REPO=$R DRB_ONCE=${DRB_ONCE:-1} DRB_POLL=0 DRB_RUNS=$TMP/runs-$(basename "$R") DRB_LOCKRUN=env \
			DRB_WORKTREE_CMD=$S/worktree.sh DRB_REPLAY_CMD=$S/replay.sh \
			DRB_COMPARE_CMD="python3 $ROOT/validation/oracle/verdict-compare.py" \
			DRB_SCENARIO_CMD=$S/scen.sh DRB_GEN_CMD=$S/gen.sh DRB_DIFF_CMD=$S/diff.sh DRB_CHECK_CMD=$S/check.sh \
			DRB_RATCHET_CMD=true DRB_ISSUE_TOOL=$S/issue.sh DRB_ISSUES=$R/.ds4/issues DRB_TICKET_TOOL=$S/ticket.sh \
			STUB_ALWAYS=$R/always.txt
		if [ -n "${STUB_PROD_GO:-}" ]; then unset DRB_GEN_CMD DRB_DIFF_CMD; export PATH=$S/bin:$PATH STUB_DIR=$S; fi
		timeout 120 bash "$SCRIPT" >"$R/pass.out" 2>&1
	)
	rc=$?
	L=$R/.ds4/driver-replay-batch.log
	[ -e "$L" ] || : >"$L"
	check "$(basename "$R"): script exited 0" "$rc" "(rc=$rc: $(tail -5 "$R/pass.out"))"
}

# ---- A: selection, oldest-first order, skip-on-conflict, landing -------------
mkrepo A
echo base2 >"$R/tools/xmageoracle/shared.txt"
git -C "$R" add -A && git -C "$R" commit -q -m "shared file"
mkticket t3 2026-10-06T01:00:00Z tools/xmageoracle/t3.txt three
mkticket t7 2026-10-06T02:00:00Z tools/xmageoracle/t7.txt seven human_needed "driver_replay_batch: HELD old reason main=- branch=000000000000"
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/shared.txt one
mkticket t2 2026-10-06T04:00:00Z tools/xmageoracle/shared.txt two
mkticket t4 2026-10-06T05:00:00Z tools/xmageoracle/t4.txt four
sed -i 's/xmage driver needs host replay failed/some other gate failed/' "$R/.ds4/issues/t4.md"
mkticket t5 2026-10-06T05:30:00Z tools/xmageoracle/t5.txt five merged
mkticket t6 2026-10-06T06:00:00Z tools/xmageoracle/t6.txt six
t6head=$(git -C "$R" rev-parse --short=12 wt/t6)
printf -- '- 2026-10-06T07:00:00Z driver_replay_batch: HELD CULPRIT earlier main=- branch=%s\n' "$t6head" >>"$R/.ds4/issues/t6.md"
main0=$(git -C "$R" rev-parse main)
runpass
merges=$(/usr/bin/grep -o 'MERGED-INTO-BATCH [a-z0-9]*' "$L" | awk '{print $2}' | tr '\n' ' ')
[ "$merges" = "t3 t7 t1 " ]
check "A selection and oldest-first order (got: $merges)" $?
has "$L" 'SKIP-CONFLICT t2'
check "A conflicting ticket is skipped" $?
hasnt "$L" 'MERGED-INTO-BATCH t4'
check "A a ticket parked by another gate is not selected" $?
hasnt "$L" 'MERGED-INTO-BATCH t5'
check "A a merged ticket is not selected" $?
hasnt "$L" 'MERGED-INTO-BATCH t6'
check "A a HELD ticket with an unchanged branch is not selected" $?
/usr/bin/grep -qE 'LANDED [0-9a-f]{9} t3 t7 t1$' "$L"
check "A LANDED line names the tickets in order" $?
[ "$(git -C "$R" rev-parse --short=9 main)" = "$(/usr/bin/grep -o 'LANDED [0-9a-f]*' "$L" | awk '{print $2}')" ]
check "A LANDED sha is main's head" $?
git -C "$R" merge-base --is-ancestor "$main0" main && [ "$(git -C "$R" rev-parse main)" != "$main0" ]
check "A main advanced by a merge on top of its old head" $?
for t in t3 t7 t1; do
	[ "$(status_of $t)" = merged ]
	check "A $t marked merged" $?
	[ ! -e "$R/.worktrees/$t" ]
	check "A $t worktree removed" $?
done
git -C "$R" cat-file -e main:tools/xmageoracle/t3.txt && git -C "$R" cat-file -e main:tools/xmageoracle/t7.txt
check "A main has the landed branches' files" $?
git -C "$R" cat-file -e main:tools/xmageoracle/t6.txt 2>/dev/null
[ $? != 0 ]
check "A main lacks the held ticket's file" $?
[ "$(status_of t2)" = human_needed ] && has "$R/.ds4/issues/t2.md" 'driver_replay_batch: HELD merge conflict on the batch main='
check "A the conflicting ticket stays parked with a HELD line" $?
[ -d "$R/.worktrees/t2" ]
check "A the conflicting ticket's worktree is kept" $?
git -C "$R" show main:compliance/verdicts/a.jsonl | /usr/bin/grep -q '"card": "Gamma", "status": "agree"'
check "A the batch's improved verdict row landed (ratchet commit)" $?
git -C "$R" worktree list | /usr/bin/grep -q driver-batch && r=1 || r=0
check "A the integration worktree is removed" $r

# ---- B: a flaky regressed row keeps main's row --------------------------------
mkrepo B
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/x.flaky 'Alpha/cast-resolve/v1'
mkticket t3 2026-10-06T04:00:00Z tools/xmageoracle/t3.txt three
runpass
has "$L" 'FLAKY Alpha/cast-resolve/v1'
check "B the varying row is classified FLAKY" $?
hasnt "$L" 'CULPRIT'
check "B no CULPRIT for a flaky row" $?
has "$R/.ds4/driver-flakes.log" 'Alpha'
check "B driver-flakes.log names the card" $?
git -C "$R" show main:compliance/verdicts/a.jsonl | /usr/bin/grep -q '{"id":"Alpha/cast-resolve/v1","card":"Alpha","status":"agree"}'
check "B main's verdict row is kept for the flaky row" $?
/usr/bin/grep -qE 'LANDED [0-9a-f]{9} t1 t3$' "$L"
check "B both tickets landed" $?
[ "$(status_of t1)" = merged ] && [ "$(status_of t3)" = merged ]
check "B both tickets marked merged" $?

# ---- C: a stable regression re-parks only the culprit --------------------------
mkrepo C
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.txt one
mkticket t3 2026-10-06T04:00:00Z tools/xmageoracle/x.regress 'Beta/cast-resolve/v1'
mkticket t8 2026-10-06T05:00:00Z tools/xmageoracle/t8.txt eight
runpass
has "$L" 'CULPRIT t3 row Beta/cast-resolve/v1'
check "C the branch whose removal clears the row is the CULPRIT" $?
hasnt "$L" 'FLAKY'
check "C a stable row is not FLAKY" $?
[ "$(status_of t3)" = human_needed ] && has "$R/.ds4/issues/t3.md" 'driver_replay_batch: HELD CULPRIT regresses row '"'"'Beta/cast-resolve/v1'"'"
check "C the culprit stays parked with a History line naming the row" $?
has "$R/.ds4/issues/t3.md" ' main=- branch='
check "C the culprit's HELD line is main-independent" $?
[ "$(status_of t1)" = merged ] && [ "$(status_of t8)" = merged ] && hasnt "$R/.ds4/issues/t1.md" 'HELD'
check "C the others are marked merged" $?
git -C "$R" cat-file -e main:tools/xmageoracle/t1.txt && ! git -C "$R" cat-file -e main:tools/xmageoracle/x.regress 2>/dev/null
check "C main has the others and not the culprit" $?
[ "$(/usr/bin/grep -c 'REPLAY rc=' "$L")" = 2 ]
check "C a fresh full replay ran without the culprit" $?
/usr/bin/grep -qE 'LANDED [0-9a-f]{9} t1 t8$' "$L"
check "C only the non-culprits landed" $?

# ---- D: no single branch clears it and main alone agrees: land nothing -----------
# (brief case b) each branch alone breaks the row, so removing either leaves it
# broken; main alone is clean, so it is the batch's regression, not main drift.
mkrepo D
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.regress 'Beta/cast-resolve/v1'
mkticket t3 2026-10-06T04:00:00Z tools/xmageoracle/t3.regress 'Beta/cast-resolve/v1'
main0=$(git -C "$R" rev-parse main)
runpass
has "$L" 'UNATTRIBUTED row Beta/cast-resolve/v1'
check "D an unattributable row is reported" $?
has "$L" "MAIN-ALONE Beta/cast-resolve/v1: main alone says 'agree" && hasnt "$L" 'DRIFT-MAIN' && hasnt "$L" 'CULPRIT'
check "D (b) the row is replayed on main alone, agrees there, and is not main drift" $?
has "$L" 'HOLD nothing landed' && [ ! -e "$R/.ds4/driver-drift.log" ] && [ ! -e "$R/tickets.out" ]
check "D (b) the batch HOLDs and records no drift" $?
hasnt "$L" 'LANDED'
check "D nothing lands" $?
[ "$(git -C "$R" rev-parse main)" = "$main0" ]
check "D main is untouched" $?
[ "$(status_of t1)" = human_needed ] && [ "$(status_of t3)" = human_needed ] &&
	has "$R/.ds4/issues/t1.md" 'driver_replay_batch: HELD' && has "$R/.ds4/issues/t3.md" 'driver_replay_batch: HELD'
check "D every ticket stays parked with the findings" $?
runpass
[ "$(/usr/bin/grep -c ' START ' "$L")" = 1 ]
check "D a HELD set is not replayed again until a branch changes" $?

# ---- E: red pre-check drops the newest branch ------------------------------------
mkrepo E
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.txt one
mkticket t3 2026-10-06T04:00:00Z tools/xmageoracle/x.badtest broken
runpass
has "$L" 'CHECKS-RED-ALONE t3'
check "E the red branch is identified by an alone pre-check" $?
/usr/bin/grep -qE 'LANDED [0-9a-f]{9} t1$' "$L"
check "E the rest lands" $?
[ "$(status_of t3)" = human_needed ] && has "$R/.ds4/issues/t3.md" 'driver_replay_batch: HELD build/tests red alone'
check "E the dropped ticket stays parked, HELD" $?

# ---- E2: the first merged branch is red alone; the second lands -------------------
mkrepo E2
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.redalone red
mkticket t3 2026-10-06T04:00:00Z tools/xmageoracle/t3.txt green
runpass
has "$L" 'CHECKS-RED-ALONE t1'
check "E2 the first-merged red branch is isolated and held" $?
[ "$(status_of t1)" = human_needed ] && has "$R/.ds4/issues/t1.md" 'driver_replay_batch: HELD build/tests red alone'
check "E2 only the red branch is held" $?
[ "$(status_of t3)" = merged ] && git -C "$R" cat-file -e main:tools/xmageoracle/t3.txt
check "E2 the green branch lands" $?

# ---- E3: green singles with a red combination hold only the later branch ----------
mkrepo E3
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.combo first
mkticket t3 2026-10-06T04:00:00Z tools/xmageoracle/t3.combo second
runpass
has "$L" 'CHECKS-RED-COMBINATION t1 t3'
check "E3 a red pair is reported as a combination" $?
[ "$(status_of t1)" = merged ] && [ "$(status_of t3)" = human_needed ]
check "E3 only the later combination branch is held" $?

# ---- F: when not to act ------------------------------------------------------------
mkrepo F1
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.txt one
mkdir -p "$R/.ds4/orchestrator" && echo "operator pause" >"$R/.ds4/orchestrator/pause"
runpass
hasnt "$L" 'START' && has "$L" 'IDLE pipeline paused'
check "F1 a pause file stops the pass" $?
mkrepo F2
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.txt one
echo "2026-10-06 00:00:00 GREEN abcdef012 pushed (3s)" >"$R/.ds4/postmerge-batch.log"
echo "2026-10-06 01:00:00 RED fedcba987 (30s): --- FAIL" >>"$R/.ds4/postmerge-batch.log"
runpass
hasnt "$L" 'START' && has "$L" 'IDLE main is not GREEN'
check "F2 a red main stops the pass" $?
mkrepo F3
runpass
hasnt "$L" 'START'
check "F3 no parked ticket: nothing happens" $?
mkrepo F4
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.txt one
main0=$(git -C "$R" rev-parse main)
STUB_REPLAY_RC=3 runpass
has "$L" 'REPLAY rc=3' && has "$L" 'ABORT full replay failed' && hasnt "$L" 'LANDED'
check "F4 a failed replay lands nothing" $?
[ "$(git -C "$R" rev-parse main)" = "$main0" ] && [ "$(status_of t1)" = human_needed ] && hasnt "$R/.ds4/issues/t1.md" 'driver_replay_batch:'
check "F4 a failed replay leaves main and the ticket untouched" $?
[ ! -e "$R/.worktrees/driver-batch-"* ] 2>/dev/null
check "F4 the integration worktree is cleaned up" $?

# ---- G: main moves and conflicts during the replay: redo, land nothing -------------
mkrepo G
echo base2 >"$R/tools/xmageoracle/shared.txt"
git -C "$R" add -A && git -C "$R" commit -q -m "shared file"
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/shared.txt one
STUB_MOVE_MAIN=$R runpass
has "$L" 'REDO main moved' && hasnt "$L" 'LANDED'
check "G a conflicting main move redoes the pass without landing" $?
[ "$(status_of t1)" = human_needed ] && hasnt "$R/.ds4/issues/t1.md" 'driver_replay_batch:'
check "G the ticket is untouched, so the redo selects it again" $?

# ---- H0: changing XMage object identifiers do not turn a stable failure flaky -----
mkrepo H0
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.txt one
mkticket t3 2026-10-06T04:00:00Z tools/xmageoracle/t3.regress 'Beta/cast-resolve/v1'
STUB_VOLATILE_IDS=Beta/cast-resolve/v1 runpass
has "$L" 'CULPRIT t3 row Beta/cast-resolve/v1'
check "H0 UUID and three-hex XMage IDs normalize so stable failures are attributed" $?
hasnt "$L" 'FLAKY Beta/cast-resolve/v1'
check "H0 volatile object identifiers do not log a false flake" $?

# ---- H1: a genuinely varying replay with a changed scenario is attributed, not restored -----
mkrepo H1
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.txt one
mkticket t3 2026-10-06T04:00:00Z tools/xmageoracle/t3.regress 'Beta/cast-resolve/v1'
printf 'Beta/cast-resolve/v1\n' >"$R/.worktrees/t3/tools/xmageoracle/t3.flaky"
# The changed branch scenario hash must prevent restoring main's stale verdict row.
python3 - "$R/.worktrees/t3/compliance/verdicts/a.jsonl" <<'PY'
import json, sys
p=sys.argv[1]
rows=[json.loads(line) for line in open(p) if line.strip()]
for row in rows:
    if row["id"] == "Beta/cast-resolve/v1":
        row["scenario_sha"] = "new-scenario-sha"
with open(p,"w") as f:
    f.write("\n".join(json.dumps(row) for row in rows)+"\n")
PY
git -C "$R/.worktrees/t3" add compliance/verdicts/a.jsonl tools/xmageoracle/t3.flaky && git -C "$R/.worktrees/t3" commit -q -m "change scenario hash"
runpass
has "$L" 'STABLE Beta/cast-resolve/v1 (' && has "$L" 'CULPRIT t3 row Beta/cast-resolve/v1'
check "H1 variable output with a changed scenario is promoted to stable attribution" $?
hasnt "$L" 'FLAKY Beta/cast-resolve/v1'
check "H1 stale main verdict is not kept for the changed scenario" $?

# ---- H: a row already in driver-flakes.log is never a CULPRIT -----------------------
mkrepo H
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.txt one
mkticket t3 2026-10-06T04:00:00Z tools/xmageoracle/x.regress 'Beta/cast-resolve/v1'
driver_sha=$(git -C "$R" log -1 --format=%H main -- tools/xmageoracle | cut -c1-12)
echo "2026-10-06 10:05:03 driver-batch-20261006T100503Z Beta Beta/cast-resolve/v1 driver=$driver_sha varies: /state" >"$R/.ds4/driver-flakes.log"
runpass
has "$L" 'FLAKY Beta/cast-resolve/v1 (listed in driver-flakes.log)'
check "H a stable-looking row listed in the flakes log is FLAKY without re-testing" $?
hasnt "$L" 'CULPRIT'
check "H a known-flaky row is never named CULPRIT" $?
git -C "$R" show main:compliance/verdicts/a.jsonl | /usr/bin/grep -q '"id":"Beta/cast-resolve/v1".*"status":"agree"'
check "H main's verdict row is kept for the known flake" $?
/usr/bin/grep -qE 'LANDED [0-9a-f]{9} t1 t3$' "$L" && [ "$(status_of t3)" = merged ]
check "H the branch that only looked guilty lands with the rest" $?

# ---- H2: an expired flake is re-tested and a stable verdict is refreshed ----------
mkrepo H2
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.txt one
mkticket t3 2026-10-06T04:00:00Z tools/xmageoracle/x.regress 'Beta/cast-resolve/v1'
driver_sha=$(git -C "$R" log -1 --format=%H main -- tools/xmageoracle | cut -c1-12)
echo "2026-10-06 10:05:03 old-batch Beta Beta/cast-resolve/v1 driver=000000000000 varies: /state" >"$R/.ds4/driver-flakes.log"
runpass
has "$L" 'CULPRIT t3 row Beta/cast-resolve/v1'
check "H2 expired stable row is re-tested and classified normally" $?
hasnt "$L" 'known flake, not re-tested'
check "H2 expired row does not remain masked" $?
hasnt "$R/.ds4/driver-flakes.log" "Beta/cast-resolve/v1 driver=$driver_sha"
check "H2 stable row is not re-listed for the current driver" $?

# ---- H3: an expired flake that still varies is recorded against the new driver ------
mkrepo H3
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.txt one
mkticket t3 2026-10-06T04:00:00Z tools/xmageoracle/x.regress 'Beta/cast-resolve/v1'
printf 'Beta/cast-resolve/v1\n' >"$R/.worktrees/t3/tools/xmageoracle/x.flaky"
git -C "$R/.worktrees/t3" add tools/xmageoracle/x.flaky && git -C "$R/.worktrees/t3" commit -q -m 'make row vary'
driver_sha=$(git -C "$R" log -1 --format=%H main -- tools/xmageoracle | cut -c1-12)
echo "2026-10-06 10:05:03 old-batch Beta Beta/cast-resolve/v1 driver=000000000000 varies: /state" >"$R/.ds4/driver-flakes.log"
runpass
has "$L" 'FLAKY Beta/cast-resolve/v1 ('
check "H3 expired varying row is re-tested as FLAKY" $?
/usr/bin/grep -q "Beta/cast-resolve/v1 driver=$driver_sha varies:" "$R/.ds4/driver-flakes.log"
check "H3 varying row is re-listed with the current driver sha" $?
hasnt "$L" 'CULPRIT t3 row Beta/cast-resolve/v1'
check "H3 a varying row is never a CULPRIT" $?

# ---- H4: a long-lived process refreshes the driver identity between passes --------
mkrepo H4
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.txt one
mkticket t3 2026-10-06T04:00:00Z tools/xmageoracle/x.regress 'Beta/cast-resolve/v1'
driver_sha=$(git -C "$R" log -1 --format=%H main -- tools/xmageoracle | cut -c1-12)
echo "2026-10-06 10:05:03 old-batch Beta Beta/cast-resolve/v1 driver=$driver_sha varies: /state" >"$R/.ds4/driver-flakes.log"
mkdir -p "$TMP/loopbin"
cat >"$TMP/loopbin/sleep" <<'EOF'
#!/usr/bin/env bash
count_file=$DRB_REPO/.ds4/sleep-count
n=0; [ ! -e "$count_file" ] || n=$(cat "$count_file")
n=$((n + 1)); echo "$n" >"$count_file"
if [ "$n" = 1 ]; then
  echo driver-v2 >"$DRB_REPO/tools/xmageoracle/driver-v2"
  git -C "$DRB_REPO" add tools/xmageoracle/driver-v2
  git -C "$DRB_REPO" rm -q tools/xmageoracle/x.regress
  git -C "$DRB_REPO" commit -q -m 'change xmage driver between passes and fix row'
  git -C "$DRB_REPO" worktree add -q -b wt/t4 "$DRB_REPO/.worktrees/t4" main
  echo 'Beta/cast-resolve/v1' >"$DRB_REPO/.worktrees/t4/tools/xmageoracle/t4.regress"
  git -C "$DRB_REPO/.worktrees/t4" add tools/xmageoracle/t4.regress
  git -C "$DRB_REPO/.worktrees/t4" commit -q -m 'reintroduce regression'
  cat >"$DRB_REPO/.ds4/issues/t4.md" <<ISSUE
---
id: t4
title: ticket t4
status: human_needed
branch: wt/t4
worktree: .worktrees/t4
---
## Brief
XMAGE DRIVER CHANGED -- parked for scripts/driver_replay_batch.sh
## History
- 2026-10-06T00:00:00Z queued via agentctl issue add
- 2026-10-06T01:00:00Z gate xmage driver needs host replay failed and is marked on_fail = "park"
### xmage driver needs host replay
ISSUE
else
  kill -TERM "$PPID"
fi
EOF
chmod +x "$TMP/loopbin/sleep"
(
  cd "$R" || exit 1
  export DRB_REPO=$R DRB_ONCE=0 DRB_POLL=0 DRB_RUNS=$TMP/runs-H4 DRB_LOCKRUN=env \
    DRB_WORKTREE_CMD=$S/worktree.sh DRB_REPLAY_CMD=$S/replay.sh \
    DRB_COMPARE_CMD="python3 $ROOT/validation/oracle/verdict-compare.py" \
    DRB_SCENARIO_CMD=$S/scen.sh DRB_GEN_CMD=$S/gen.sh DRB_DIFF_CMD=$S/diff.sh DRB_CHECK_CMD=$S/check.sh \
    DRB_RATCHET_CMD=true DRB_ISSUE_TOOL=$S/issue.sh DRB_ISSUES=$R/.ds4/issues DRB_TICKET_TOOL=$S/ticket.sh \
    STUB_ALWAYS=$R/always.txt PATH=$TMP/loopbin:$PATH
  timeout 120 bash "$SCRIPT" >"$R/loop.out" 2>&1
)
loop_rc=$?
has "$R/loop.out" 'FLAKY Beta/cast-resolve/v1 (listed in driver-flakes.log)'
check "H4 first pass skips a flake on its unchanged driver" $?
has "$R/loop.out" 'CULPRIT t4 row Beta/cast-resolve/v1'
check "H4 second pass sees the landed driver change and re-tests the row" $?
[ "$(cat "$R/.ds4/sleep-count")" = 2 ] && [ "$loop_rc" != 124 ]
check "H4 loop completed two passes and stopped at the test sleep hook" $?

# ---- I: a generator-caused regression is attributed by regenerating the row ---------
mkrepo I
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.txt one
mkticket t3 2026-10-06T04:00:00Z compliance/oraclegen/x.genregress 'Beta/cast-resolve/v1'
mkticket t8 2026-10-06T05:00:00Z tools/xmageoracle/t8.txt eight
mkdir -p "$R/.cards"
export STUB_REQUIRE_CARDS=1 DRB_SCENARIO_TRACE=$R/scenario.trace
DRB_REPO=$R "$S/gen.sh" S "$R/without-cards.jsonl" >"$R/no-cards.log" 2>&1
[ $? != 0 ] && has "$R/no-cards.log" 'explicit cards directory required'
check "I precondition: regeneration without explicit cards fails" $?
runpass
unset STUB_REQUIRE_CARDS DRB_SCENARIO_TRACE
has "$L" 'CULPRIT t3 row Beta/cast-resolve/v1'
check "I a row whose scenario the branch's generator changed is attributed to that branch" $?
hasnt "$L" 'UNATTRIBUTED'
check "I a generator-caused row is not UNATTRIBUTED" $?
/usr/bin/grep -q '/\.worktrees/.*-probe Beta/cast-resolve/v1 ok$' "$R/scenario.trace"
check "I the regenerated scenario is replayed on the probe, not paired with a stale snapshot" $?
[ "$(status_of t3)" = human_needed ] && [ "$(status_of t1)" = merged ] && [ "$(status_of t8)" = merged ]
check "I only the generator branch stays parked" $?
/usr/bin/grep -qE 'LANDED [0-9a-f]{9} t1 t8$' "$L"
check "I the others land" $?

# ---- I2: a rules-side runner change with identical scenarios and XMage output
mkrepo I2
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.txt one
mkticket t3 2026-10-06T04:00:00Z rules/oracle_run.go 'Beta/cast-resolve/v1'
mkticket t8 2026-10-06T05:00:00Z tools/xmageoracle/t8.txt eight
export DRB_DIFF_TRACE=$R/diff.trace
runpass
unset DRB_DIFF_TRACE
has "$R/diff.trace" 'Beta/cast-resolve/v1 DIVERGE ok ok good'
check "I2 precondition: gorge diverges although scenario and XMage stay unchanged" $?
/usr/bin/grep -q '/\.worktrees/.*-probe Beta/cast-resolve/v1 agree ok ok good$' "$R/diff.trace"
check "I2 removing the runner changes only gorge's verdict on the probe" $?
has "$L" 'CULPRIT t3 row Beta/cast-resolve/v1'
check "I2 a rules/oracle_run.go-only cause is attributed to its branch" $?
hasnt "$L" 'UNATTRIBUTED'
check "I2 a runner regression is not UNATTRIBUTED" $?
[ "$(status_of t3)" = human_needed ] && [ "$(status_of t1)" = merged ] && [ "$(status_of t8)" = merged ]
check "I2 only the runner culprit stays parked; innocent branches land" $?
hasnt "$R/.ds4/issues/t1.md" 'driver_replay_batch: HELD' && hasnt "$R/.ds4/issues/t8.md" 'driver_replay_batch: HELD'
check "I2 innocent branches are not HELD" $?
/usr/bin/grep -qE 'LANDED [0-9a-f]{9} t1 t8$' "$L"
check "I2 the non-culprit branches land" $?

# ---- I3: a runner change that alters the GENERATED scenario (generation runs the engine) ----
mkrepo I3
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.txt one
mkticket t3 2026-10-06T04:00:00Z rules/oracle_run.go 'Beta/cast-resolve/v1'
mkticket t8 2026-10-06T05:00:00Z tools/xmageoracle/t8.txt eight
export STUB_RUNNER_SCEN=1 DRB_SCENARIO_TRACE=$R/scenario.trace
runpass
unset STUB_RUNNER_SCEN DRB_SCENARIO_TRACE
has "$L" 'CULPRIT t3 row Beta/cast-resolve/v1'
check "I3 a runner change that alters the scenario is attributed to its branch" $?
hasnt "$L" 'UNATTRIBUTED'
check "I3 it is not UNATTRIBUTED (no stale snapshot paired with the regenerated scenario)" $?
/usr/bin/grep -q '/\.worktrees/.*-probe Beta/cast-resolve/v1 ok$' "$R/scenario.trace"
check "I3 the regenerated scenario is replayed on the probe" $?
[ "$(status_of t3)" = human_needed ] && [ "$(status_of t1)" = merged ] && [ "$(status_of t8)" = merged ]
check "I3 only the runner culprit stays parked; innocent branches land" $?

# ---- I4: the production `go run ./cmd/oraclediff gen|diff` branches (no DRB_GEN_CMD/DRB_DIFF_CMD) ----
mkrepo I4
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.txt one
mkticket t3 2026-10-06T04:00:00Z rules/oracle_run.go 'Beta/cast-resolve/v1'
mkticket t8 2026-10-06T05:00:00Z tools/xmageoracle/t8.txt eight
mkdir -p "$R/.cards"
export STUB_PROD_GO=1 STUB_REQUIRE_CARDS=1 STUB_RUNNER_SCEN=1 DRB_GO_TRACE=$R/go.trace
: >"$DRB_GO_TRACE"
runpass
unset STUB_PROD_GO STUB_REQUIRE_CARDS STUB_RUNNER_SCEN
has "$L" 'CULPRIT t3 row Beta/cast-resolve/v1'
check "I4 the production go-run path attributes a runner change" $?
/usr/bin/grep -qE '/\.worktrees/.*-probe gen cards='"$R"'/\.cards$' "$DRB_GO_TRACE" &&
  /usr/bin/grep -qE '/\.worktrees/.*-probe diff cards='"$R"'/\.cards$' "$DRB_GO_TRACE"
check "I4 gen and diff run in the probe with the main checkout's .cards" $?
unset DRB_GO_TRACE

# ---- I5: generator failures report stderr, not progress written to stdout --------
mkrepo I5
mkticket t3 2026-10-06T04:00:00Z compliance/oraclegen/x.genregress 'Beta/cast-resolve/v1'
STUB_GEN_FAIL=1 runpass
has "$L" 'ATTRIBUTE generator failed for Beta/cast-resolve/v1: missing corpus fixture'
check "I5 regeneration failure logs the first stderr line" $?
hasnt "$L" 'generator progress on stdout' && hasnt "$L" 'second diagnostic'
check "I5 progress and later diagnostics do not replace the first stderr line" $?
has "$L" 'UNATTRIBUTED row Beta/cast-resolve/v1' && hasnt "$L" 'CULPRIT' && hasnt "$L" 'LANDED'
check "I5 failed regeneration cannot clear a row" $?

# ---- X: a candidate that changes BOTH the driver and gorge Go is attributed -------
# Every selected ticket changes tools/xmageoracle/ (that is the park gate), so a
# ticket that ALSO changes a non-driver .go file is the mixed case: removing it
# for attribution must remove its DRIVER half too, not just re-run the
# generator, or the row can never clear and every ticket is held.
mkrepo X
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.txt one
mkticket t3 2026-10-06T04:00:00Z tools/xmageoracle/x.regress 'Beta/cast-resolve/v1'
mkdir -p "$R/.worktrees/t3/rules"
echo harmless >"$R/.worktrees/t3/rules/harmless.go"
git -C "$R/.worktrees/t3" add -A && git -C "$R/.worktrees/t3" commit -q -m "feat: harmless gorge change"
mkticket t8 2026-10-06T05:00:00Z tools/xmageoracle/t8.txt eight
runpass
has "$L" 'CULPRIT t3 row Beta/cast-resolve/v1'
check "X a mixed driver+rules culprit is attributed" $?
hasnt "$L" 'UNATTRIBUTED'
check "X a mixed driver+rules regression is not UNATTRIBUTED" $?
[ "$(status_of t3)" = human_needed ] && [ "$(status_of t1)" = merged ] && [ "$(status_of t8)" = merged ]
check "X only the mixed culprit stays parked; the innocent branches land" $?
hasnt "$R/.ds4/issues/t1.md" 'driver_replay_batch: HELD' && hasnt "$R/.ds4/issues/t8.md" 'driver_replay_batch: HELD'
check "X the other branches are not HELD" $?
/usr/bin/grep -qE 'LANDED [0-9a-f]{9} t1 t8$' "$L"
check "X the non-culprit branches land" $?

# ---- J: DRIFT -- the generator moved and no ticket is parked: replay main, land ------
# mkdrift <name>: a repo whose last replayed main is recorded, then main's generator moves.
mkdrift() {
	mkrepo "$1"
	git -C "$R" rev-parse main >"$R/.ds4/driver-replay-last-main"
	drift_base=$(git -C "$R" rev-parse main)
}
gencommit() { # gencommit <file> <content>: a generator commit on main
	mkdir -p "$R/compliance/oraclegen"
	printf '%s\n' "$2" >"$R/compliance/oraclegen/$1"
	git -C "$R" add -A && git -C "$R" commit -q -m "feat(oraclegen): $1"
}
mkdrift J
gencommit gen.txt changed
runpass
has "$L" 'DRIFT-START' && /usr/bin/grep -q 'REPLAY rc=0 .*ids=drift-main' "$L"
check "J a generator change with no parked ticket runs a DRIFT replay of main" $?
hasnt "$L" 'MERGED-INTO-BATCH' && hasnt "$L" ' START '
check "J the DRIFT pass merges no branch" $?
/usr/bin/grep -qE 'LANDED [0-9a-f]{9} drift-refresh$' "$L" && [ "$(git -C "$R" rev-parse --short=9 main)" = "$(/usr/bin/grep -o 'LANDED [0-9a-f]*' "$L" | awk '{print $2}')" ]
check "J the refreshed verdicts land on main" $?
git -C "$R" show main:compliance/verdicts/a.jsonl | /usr/bin/grep -q '"card": "Gamma", "status": "agree"'
check "J main's verdict row is refreshed" $?
[ "$(cat "$R/.ds4/driver-replay-last-main")" = "$(git -C "$R" rev-parse main)" ] && [ "$(cat "$R/.ds4/driver-replay-last-main")" != "$drift_base" ]
check "J the new last-main is recorded (main's head after the landing)" $?
hasnt "$L" 'DRIFT Alpha' && [ ! -e "$R/tickets.out" ]
check "J a clean DRIFT replay files no ticket" $?
git -C "$R" worktree list | /usr/bin/grep -q driver-batch && r=1 || r=0
check "J the integration worktree is removed" $r
runpass
[ "$(/usr/bin/grep -c 'DRIFT-START' "$L")" = 1 ]
check "J a recorded last-main is not replayed again" $?

# ---- K: nothing the generator reads changed: no pass ---------------------------------
mkdrift K
echo driver >"$R/tools/xmageoracle/driver.txt"
echo '{}' >"$R/compliance/ratchet.json.new"
git -C "$R" add -A && git -C "$R" commit -q -m "feat(driver): a driver-only change"
main0=$(git -C "$R" rev-parse main)
runpass
hasnt "$L" 'DRIFT' && hasnt "$L" 'REPLAY'
check "K no generator change: no DRIFT pass" $?
[ "$(git -C "$R" rev-parse main)" = "$main0" ] && [ "$(cat "$R/.ds4/driver-replay-last-main")" = "$drift_base" ]
check "K main and last-main are untouched" $?
mkdrift K2
gencommit gen.txt changed
echo "2026-10-06 00:00:00 GREEN abcdef012 pushed (3s)" >"$R/.ds4/postmerge-batch.log"
echo "2026-10-06 01:00:00 RED fedcba987 (30s): --- FAIL" >>"$R/.ds4/postmerge-batch.log"
runpass
hasnt "$L" 'DRIFT-START'
check "K a red main does not run a DRIFT pass" $?
mkdrift K3
gencommit gen.txt changed
mkdir -p "$R/.ds4/orchestrator" && echo pause >"$R/.ds4/orchestrator/pause"
runpass
hasnt "$L" 'DRIFT-START'
check "K a paused pipeline does not run a DRIFT pass" $?
mkdrift K4
gencommit gen.txt changed
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.txt one
runpass
hasnt "$L" 'DRIFT-START' && has "$L" 'MERGED-INTO-BATCH t1'
check "K a parked ticket runs the normal batch, not a DRIFT pass" $?
[ "$(cat "$R/.ds4/driver-replay-last-main")" = "$(git -C "$R" rev-parse main)" ]
check "K a batch landing records last-main too" $?

# ---- L: a stable DRIFT regression is logged with its commits and ticketed per class ----
mkrepo L
echo '{"id":"Delta/trigger#0.0/v1","card":"Delta","status":"agree"}' >>"$R/compliance/verdicts/a.jsonl"
git -C "$R" add -A && git -C "$R" commit -q -m "a trigger row"
git -C "$R" rev-parse main >"$R/.ds4/driver-replay-last-main"
printf 'Alpha/cast-resolve/v1\nBeta/cast-resolve/v1\nDelta/trigger#0.0/v1\n' >"$R/x.list"
gencommit x.genregress "$(cat "$R/x.list")"
gc=$(git -C "$R" rev-parse --short HEAD)
runpass
has "$L" "DRIFT Beta/cast-resolve/v1 diverge" && has "$L" "DRIFT Delta/trigger#0.0/v1 diverge" && /usr/bin/grep -qE "DRIFT Alpha/cast-resolve/v1 diverge .*commits=$gc\$" "$L"
check "L each stable regressed row is logged as DRIFT <row> <detail> commits=<sha>" $?
hasnt "$L" 'FLAKY' && hasnt "$L" 'CULPRIT' && hasnt "$L" 'UNATTRIBUTED'
check "L a DRIFT row is neither flaky nor attributed to a branch" $?
/usr/bin/grep -q 'LANDED' "$L" && git -C "$R" show main:compliance/verdicts/a.jsonl | /usr/bin/grep -q '"card": "Beta".*"status": "diverge"'
check "L the refreshed (regressed) verdicts land so main matches its generator" $?
[ "$(/usr/bin/grep -c '^TICKET ' "$R/tickets.out")" = 2 ]
check "L one ticket per regressed template class (two rows of one class: one ticket)" $?
has "$R/tickets.out" 'cast-resolve' && has "$R/tickets.out" 'trigger' && has "$R/tickets.out" 'Beta/cast-resolve/v1' && has "$R/tickets.out" "$gc"
check "L the tickets name the rows and the candidate commits" $?
has "$R/.ds4/driver-drift.log" 'Beta/cast-resolve/v1 | diverge'
check "L the DRIFT rows are recorded in driver-drift.log" $?
runpass
[ "$(/usr/bin/grep -c 'DRIFT-START' "$L")" = 1 ] && [ "$(/usr/bin/grep -c '^TICKET ' "$R/tickets.out")" = 2 ]
check "L the same drift is neither replayed nor ticketed twice" $?

# ---- M: a batch does not blame a branch for a row the DRIFT replay already regressed --
mkrepo M
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.txt one
mkticket t3 2026-10-06T04:00:00Z tools/xmageoracle/t3.txt three
printf 'Beta/cast-resolve/v1\n' >"$R/always.txt"
key=$(git -C "$R" ls-tree -d main -- tools/xmageoracle compliance/oraclegen cmd/oraclediff compliance/manifests | sha1sum | cut -c1-12)
echo "2026-10-06 09:00:00 $key Beta/cast-resolve/v1 | diverge" >"$R/.ds4/driver-drift.log"
runpass
has "$L" 'DRIFT-KNOWN Beta/cast-resolve/v1'
check "M a row regressed on the DRIFT replay of this main is DRIFT-KNOWN in a batch" $?
hasnt "$L" 'CULPRIT' && hasnt "$L" 'UNATTRIBUTED'
check "M it is excluded from attribution" $?
/usr/bin/grep -qE 'LANDED [0-9a-f]{9} t1 t3$' "$L" && [ "$(status_of t1)" = merged ] && [ "$(status_of t3)" = merged ]
check "M the batch lands" $?
mkrepo M2
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.txt one
mkticket t3 2026-10-06T04:00:00Z tools/xmageoracle/t3.txt three
printf 'Beta/cast-resolve/v1\n' >"$R/always.txt"
echo "2026-10-06 09:00:00 0123456789ab Beta/cast-resolve/v1 | diverge" >"$R/.ds4/driver-drift.log"
runpass
hasnt "$L" 'DRIFT-KNOWN' && has "$L" 'DRIFT-MAIN Beta/cast-resolve/v1 diverge'
check "M2 a drift record for another driver+generator is not DRIFT-KNOWN; the row is re-proved on main alone" $?
/usr/bin/grep -qE 'LANDED [0-9a-f]{9} t1 t3$' "$L" && hasnt "$L" 'UNATTRIBUTED' && hasnt "$L" 'HOLD'
check "M2 two tickets with a main-drift row land (fresh main-alone replay)" $?

# ---- N: a manifests-only change drifts, and its commit is a candidate ------------------
mkdrift N
echo changed >"$R/compliance/manifests/m.txt"
git -C "$R" add -A && git -C "$R" commit -q -m "feat(manifests): m"
mc=$(git -C "$R" rev-parse --short HEAD)
printf 'Beta/cast-resolve/v1\n' >"$R/always.txt"
runpass
has "$L" 'DRIFT-START' && /usr/bin/grep -qE "DRIFT Beta/cast-resolve/v1 diverge .*commits=$mc\$" "$L"
check "N a manifests-only drift runs, and the DRIFT row names the manifests commit" $?
has "$R/tickets.out" "$mc"
check "N the ticket's candidate commits include the manifests commit" $?

# ---- P: (brief case a) a row that diverges on main alone is main drift, not the batch's ----
mkrepo P
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.txt one
printf 'Beta/cast-resolve/v1\n' >"$R/always.txt"
export DRB_SCENARIO_TRACE=$R/scen.trace
runpass
unset DRB_SCENARIO_TRACE
has "$L" 'DRIFT-MAIN Beta/cast-resolve/v1 diverge' && hasnt "$L" 'UNATTRIBUTED' && hasnt "$L" 'HOLD' && hasnt "$L" 'CULPRIT'
check "P a row diverging the same way on main alone is DRIFT-MAIN, not UNATTRIBUTED" $?
/usr/bin/grep -qE 'LANDED [0-9a-f]{9} t1$' "$L" && [ "$(status_of t1)" = merged ] && hasnt "$R/.ds4/issues/t1.md" 'HELD'
check "P the ticket lands" $?
git -C "$R" show main:compliance/verdicts/a.jsonl | /usr/bin/grep -q '"card": "Beta".*"status": "diverge"'
check "P the batch's refreshed row lands for the drift row" $?
key=$(git -C "$R" ls-tree -d "$(git -C "$R" rev-parse main^1)" -- tools/xmageoracle compliance/oraclegen cmd/oraclediff compliance/manifests | sha1sum | cut -c1-12)
/usr/bin/grep -qE " $key Beta/cast-resolve/v1 \| diverge" "$R/.ds4/driver-drift.log"
check "P a drift line is written under the replayed main's key" $?
[ "$(/usr/bin/grep -c '^TICKET XMage drift: cast-resolve' "$R/tickets.out" 2>/dev/null)" = 1 ] && has "$R/tickets.out" 'Beta/cast-resolve/v1'
check "P the per-class drift ticket is filed" $?
[ "$(/usr/bin/grep -c 'probe Beta/cast-resolve/v1 ' "$R/scen.trace")" = 1 ]
check "P one ticket: the without-candidate replay is reused as main alone (one probe replay)" $? "($(/usr/bin/grep -c 'probe Beta' "$R/scen.trace") probe replays)"
[ "$(cat "$R/.ds4/driver-replay-last-main")" = "$(git -C "$R" rev-parse main)" ]
check "P the batch landing records last-main" $?

# ---- P3: case P on the production `go run ./cmd/oraclediff diff` branch: the main-alone
# verdict's detail comes from the -write compliance row, not the detail-less -out file ----
mkrepo P3
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.txt one
printf 'Beta/cast-resolve/v1\n' >"$R/always.txt"
export DRB_GO_TRACE=$R/go.trace DRB_WRITE_TRACE=$R/write.trace
STUB_PROD_GO=1 runpass
unset DRB_GO_TRACE DRB_WRITE_TRACE
/usr/bin/grep -qE '^Beta/cast-resolve/v1 write=.+\.verdict\.jsonl\.rows$' "$R/write.trace"
check "P3 the production diff writes the compliance row (-write) beside the -out verdict" $? "($(cat "$R/write.trace" 2>/dev/null))"
has "$L" 'DRIFT-MAIN Beta/cast-resolve/v1 diverge setup graveyard: gorge "[Wastes x5]", xmage "[]"' && hasnt "$L" 'UNATTRIBUTED' && hasnt "$L" 'HOLD'
check "P3 a real-detail row diverging the same way on main alone is DRIFT-MAIN" $?
/usr/bin/grep -qE 'LANDED [0-9a-f]{9} t1$' "$L" && [ "$(status_of t1)" = merged ]
check "P3 the ticket lands" $?

# ---- M3: main alone diverges on the row too, but with a DIFFERENT detail (the
# ticket's driver changed XMage's result): not main drift; the batch HOLDs ----
mkrepo M3
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.otherdet 'Beta/cast-resolve/v1'
printf 'Beta/cast-resolve/v1\n' >"$R/always.txt"
runpass
has "$L" "MAIN-ALONE Beta/cast-resolve/v1: main alone says 'diverge setup graveyard: gorge \"[Wastes x5]\", xmage \"[]\"'" && hasnt "$L" 'DRIFT-MAIN'
check "M3 a different main-alone detail is not main drift" $?
has "$L" 'UNATTRIBUTED row Beta/cast-resolve/v1' && has "$L" 'HOLD nothing landed' && [ "$(status_of t1)" != merged ] && hasnt "$R/.ds4/driver-drift.log" 'Beta'
check "M3 the row is UNATTRIBUTED, the batch HOLDs and no drift line is written" $?

# ---- P2: a refused DRIFT landing (10:04) leaves the drift unrecorded; the next ticket
# batch still classifies the generator-drift row as main drift and HOLDs nothing ----
mkdrift P2
gencommit x.genregress 'Beta/cast-resolve/v1'
gc=$(git -C "$R" rev-parse --short HEAD)
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.txt one
runpass
hasnt "$L" 'DRIFT-START' && has "$L" 'DRIFT-MAIN Beta/cast-resolve/v1 diverge' && hasnt "$L" 'HOLD'
check "P2 no DRIFT pass first; the batch proves the row is main drift" $?
/usr/bin/grep -qE 'LANDED [0-9a-f]{9} t1$' "$L" && [ "$(status_of t1)" = merged ]
check "P2 the ticket lands" $?
has "$R/tickets.out" "$gc" && has "$R/.ds4/driver-drift.log" 'Beta/cast-resolve/v1 | diverge'
check "P2 the drift ticket names the generator commit and the drift log has the row" $?
runpass
hasnt "$L" 'DRIFT-START'
check "P2 the drift the batch covered is not replayed again by a DRIFT pass" $?

# ---- O: a generator change that reaches main during a batch replay is not marked replayed
mkrepo O
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.txt one
o_main0=$(git -C "$R" rev-parse main)
STUB_MOVE_MAIN=$R STUB_MOVE_FILE=compliance/oraclegen/late.txt runpass
/usr/bin/grep -qE 'LANDED [0-9a-f]{9} t1$' "$L" && [ "$(git -C "$R" rev-parse main~1 | head -c 9)" != "" ]
check "O the batch lands despite a non-conflicting generator move on main" $?
[ -e "$R/compliance/oraclegen/late.txt" ] && [ "$(cat "$R/.ds4/driver-replay-last-main")" != "$(git -C "$R" rev-parse main)" ]
check "O last-main is not advanced past the unreplayed generator commit" $?
[ "$(cat "$R/.ds4/driver-replay-last-main")" = "$o_main0" ]
check "O last-main stays at the replayed main M0" $?

echo
[ "$fails" = 0 ] && echo "driver_replay_batch smoke: all passed" || echo "driver_replay_batch smoke: $fails FAILED"
[ "$fails" = 0 ]
