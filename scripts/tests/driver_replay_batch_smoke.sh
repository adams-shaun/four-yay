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
[ -z "${STUB_REPLAY_RC:-}" ] || exit "$STUB_REPLAY_RC"
if [ -n "${STUB_MOVE_MAIN:-}" ]; then
	echo moved >"$STUB_MOVE_MAIN/tools/xmageoracle/shared.txt"
	git -C "$STUB_MOVE_MAIN" add tools/xmageoracle/shared.txt
	git -C "$STUB_MOVE_MAIN" commit -q -m "main moved during the replay"
fi
python3 - "$rdir/S/scen.jsonl" <<'PY'
import glob, json, os, sys
bad = set()
for pat in ("tools/xmageoracle/*.regress", "tools/xmageoracle/*.flaky"):
    for f in glob.glob(pat):
        bad |= set(open(f).read().split("\n")) - {""}
if os.environ.get("STUB_ALWAYS") and os.path.exists(os.environ["STUB_ALWAYS"]):
    bad |= set(open(os.environ["STUB_ALWAYS"]).read().split("\n")) - {""}
with open(sys.argv[1], "w") as sc:
    for f in glob.glob("compliance/verdicts/*.jsonl"):
        rows = [json.loads(l) for l in open(f) if l.strip()]
        for r in rows:
            sc.write(json.dumps({"id": r["id"], "card": r["card"]}) + "\n")
            if r["id"] in bad:
                r["status"] = "diverge"
            elif r["card"] == "Gamma":
                r["status"] = "agree"   # an improvement
        open(f, "w").write("\n".join(json.dumps(r) for r in rows) + "\n")
PY
echo "S ok"; echo done
EOF
cat >"$S/scen.sh" <<'EOF'
#!/usr/bin/env bash
# scen.sh IN OUT, in the tree whose driver is being tried.
id=$(python3 -c 'import json,sys; print(json.loads(open(sys.argv[1]).readline())["id"])' "$1")
state=good
d=tools/xmageoracle
for f in $d/*.regress; do [ -e "$f" ] && /usr/bin/grep -qxF -- "$id" "$f" && state=bad; done
[ -n "${STUB_ALWAYS:-}" ] && [ -e "$STUB_ALWAYS" ] && /usr/bin/grep -qxF -- "$id" "$STUB_ALWAYS" && state=bad
for f in $d/*.flaky; do [ -e "$f" ] && /usr/bin/grep -qxF -- "$id" "$f" && state="r$(date +%N)$RANDOM"; done
printf '{"id":"%s","state":"%s","ms":%s}\n' "$id" "$state" "$((RANDOM + 1))" >"$2"
EOF
cat >"$S/diff.sh" <<'EOF'
#!/usr/bin/env bash
# diff.sh SCEN XMAGE OUT: the oraclediff verdict for one snapshot.
id=$(python3 -c 'import json,sys; print(json.loads(open(sys.argv[1]).readline())["id"])' "$1")
st=DIVERGE; /usr/bin/grep -q '"state":"good"' "$2" && st=agree
printf '{"id":"%s","card":"x","verdict":{"status":"%s"}}\n' "$id" "$st" >"$3"
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
chmod +x "$S"/*.sh

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
	{
		echo '{"id":"Alpha/cast-resolve/v1","card":"Alpha","status":"agree"}'
		echo '{"id":"Beta/cast-resolve/v1","card":"Beta","status":"agree"}'
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
		export DRB_REPO=$R DRB_ONCE=1 DRB_RUNS=$TMP/runs-$(basename "$R") DRB_LOCKRUN=env \
			DRB_WORKTREE_CMD=$S/worktree.sh DRB_REPLAY_CMD=$S/replay.sh \
			DRB_COMPARE_CMD="python3 $ROOT/validation/oracle/verdict-compare.py" \
			DRB_SCENARIO_CMD=$S/scen.sh DRB_DIFF_CMD=$S/diff.sh DRB_CHECK_CMD=$S/check.sh \
			DRB_RATCHET_CMD=true DRB_ISSUE_TOOL=$S/issue.sh DRB_ISSUES=$R/.ds4/issues \
			STUB_ALWAYS=$R/always.txt
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

# ---- D: no single branch clears it: land nothing -------------------------------
mkrepo D
printf 'Beta/cast-resolve/v1\n' >"$R/always.txt"
mkticket t1 2026-10-06T03:00:00Z tools/xmageoracle/t1.txt one
mkticket t3 2026-10-06T04:00:00Z tools/xmageoracle/t3.txt three
main0=$(git -C "$R" rev-parse main)
runpass
has "$L" 'UNATTRIBUTED row Beta/cast-resolve/v1'
check "D an unattributable row is reported" $?
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

echo
[ "$fails" = 0 ] && echo "driver_replay_batch smoke: all passed" || echo "driver_replay_batch smoke: $fails FAILED"
[ "$fails" = 0 ]
