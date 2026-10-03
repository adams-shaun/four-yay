#!/usr/bin/env bash
# fleet.sh — coordination between the two orchestrator threads sharing this
# repo (engine and distill). The protocol it enforces is documented in
# .superpowers/fleet/PROTOCOL.md; this script is the part that is mechanical.
#
# Design rule: every command here is either instant or fails fast. Nothing in
# this script ever blocks one thread waiting on the other, with the single
# deliberate exception of `merge`, whose lock is held for seconds.
set -euo pipefail

ROOT=$(git rev-parse --show-toplevel 2>/dev/null || echo "$PWD")
FLEET="$ROOT/.superpowers/fleet"
SEATS="$FLEET/seats"
TOKENS="$FLEET/tokens"
# Two POOLS, not one cap (user ruling 2026-09-07). The four-seat limit was
# only ever about this box saturating: past four local agents, load-sensitive
# tests fail for reasons unrelated to any diff. A paid seat (codex/Claude) runs
# on someone else's hardware and does not load the box at all, so it cannot be
# what that limit is protecting. Paid seats get their own ceiling for a
# different reason -- they share one ChatGPT plan's rate limit and throttle
# each other and the user's own sessions.
# Lowered to 3/1 by user ruling 2026-09-07, after a CUDA OOM in the vLLM
# engine killed three concurrent local seats within 2.1 seconds of each other.
# The engine runs at gpu_memory_utilization 0.975 with ~2 GiB of headroom, so
# concurrent streams are the amplifier even though KV cache was only at 15%.
MAX_SEATS=${MAX_SEATS:-3}          # local seats: what the BOX and the GPU carry
MAX_PAID=${MAX_PAID:-1}            # paid seats: what the PLAN can carry

# Which thread is running this. Set FLEET_THREAD in the session's environment;
# the file is the fallback so a session that forgets still identifies itself.
THREAD=${FLEET_THREAD:-$(cat "$FLEET/thread" 2>/dev/null || echo engine)}

mkdir -p "$SEATS" "$TOKENS"

die() { echo "fleet: $*" >&2; exit 1; }

# kind_of prints the pool a claimed seat belongs to. A seat directory written
# before pools existed carries no `kind` file; it is local, which is the
# conservative reading (it counts against the scarcer, box-bound pool).
kind_of() { cat "$1/kind" 2>/dev/null || echo local; }

# --- liveness ---------------------------------------------------------------
# A seat is a CLAIM. A claim is not a running agent: an agent that finished or
# crashed used to hold its seat forever and cap the pool for nothing. So a seat
# now records WHICH PROCESS it is for, and liveness is read from the process
# table -- never from a file's mtime. A transcript's mtime says when it was
# last written, not whether the writer still exists.
#
# Identity is (pid, start time), not pid alone: pids are recycled, and after a
# recycle a bare `kill -0` on the old number says "alive" about a process that
# has nothing to do with the agent. /proc/<pid>/stat field 22 is the process's
# start time in clock ticks since boot -- fixed for the process's whole life,
# unforgeable by it, and different for whatever later inherits the number. It
# is preferred over cmdline, which a process can rewrite and which is identical
# across sibling agents anyway.
#
# NOTHING here signals a process. Existence is a read of /proc/<pid>/stat;
# `kill -0` on a specific numeric pid is the only other permitted probe. A
# cmdline PATTERN (`pkill -f`, bare `pgrep -f`) must never appear in this file:
# the pattern matches the searching process's own cmdline and has already
# killed a session on this box.
proc_starttime() {
	local pid=$1 stat rest
	case "$pid" in ''|*[!0-9]*) return 1 ;; esac
	stat=$(cat "/proc/$pid/stat" 2>/dev/null) || return 1
	[ -n "$stat" ] || return 1
	# Field 2 (comm) is parenthesised and may contain spaces and ')', so the
	# only safe split is at the LAST ')'. Fields resume at 3 (state), which
	# makes starttime -- field 22 -- the 20th of what is left.
	rest=${stat##*)}
	awk '{print $20}' <<<"$rest"
}

# seat_state prints exactly one of:
#   LIVE     recorded pid is running AND is the process that was claimed
#   DEAD     recorded pid is gone, or the number was reused by something else
#   REMOTE   runner is not a local process (a Claude subagent) -- never dead
#   PENDING  claimed by this protocol but no pid attached yet (fleet.sh attach)
#   UNKNOWN  no runner record at all: a seat that predates pid tracking, or one
#            whose pid was written without a start time. Unknown is NOT alive
#            and NOT dead -- it is unproven, and it is resolved by a human.
seat_state() {
	local seat=$1 runner pid want got
	runner=$(cat "$seat/runner" 2>/dev/null || echo '')
	case "$runner" in
		remote)  echo REMOTE;  return 0 ;;
		pending) echo PENDING; return 0 ;;
	esac
	pid=$(cat "$seat/pid" 2>/dev/null || echo '')
	[ -n "$pid" ] || { echo UNKNOWN; return 0; }
	want=$(cat "$seat/pidstart" 2>/dev/null || echo '')
	# A pid with no recorded start time cannot be told apart from a recycled
	# one, so it stays unproven rather than being called alive.
	[ -n "$want" ] || { echo UNKNOWN; return 0; }
	got=$(proc_starttime "$pid" 2>/dev/null || echo '')
	[ -n "$got" ] || { echo DEAD; return 0; }
	[ "$want" = "$got" ] && echo LIVE || echo DEAD
}

# seat_holds says whether a seat still occupies its slot in a pool. Only a seat
# with a running (or deliberately remote) runner does. A DEAD seat holds
# nothing -- that is the whole point of tracking the process. An UNKNOWN seat
# holds nothing either: it is unproven, and pretending otherwise is what let
# six exited agents cap the pool. Status prints unknowns loudly so they get
# resolved instead of quietly accumulating.
seat_holds() {
	case "$(seat_state "$1")" in
		LIVE|REMOTE|PENDING) return 0 ;;
		*) return 1 ;;
	esac
}

# count_pool counts OCCUPIED seats in one pool. Never use `ls | wc -l` for
# this: the two pools share one directory, and a directory's existence is a
# claim, not a running process.
count_pool() {
	local want=$1 n=0 s
	for s in "$SEATS"/*; do
		[ -d "$s" ] || continue
		[ "$(kind_of "$s")" = "$want" ] || continue
		seat_holds "$s" && n=$((n + 1))
	done
	echo "$n"
}

# count_state counts seats in one state, across both pools.
count_state() {
	local want=$1 n=0 s
	for s in "$SEATS"/*; do
		[ -d "$s" ] || continue
		[ "$(seat_state "$s")" = "$want" ] && n=$((n + 1))
	done
	echo "$n"
}

# --- ports -----------------------------------------------------------------
# Resolved from listening sockets, never from a process-name pattern: a bare
# `pgrep -f` matches this script's own cmdline. comm is the BINARY name, so an
# agent's `gorged-after` build does not read as "gorged" -- which is why the
# port, not the name, is the index.
# port_listening answers "is anything LISTENing on this port at all", which
# is a different question from "which process owns it". Inside the agent
# sandbox `ss -lptn` emits the LISTEN rows with no `pid=` field, so deciding
# occupancy from pid visibility reported occupied ports as free and handed
# the same port to two seats. The socket row itself is the source of truth;
# the pid is only ever a display detail.
port_listening() {
	# The `:$1<space>` tail anchors on the port number: the colon must sit
	# immediately before the digits, so it cannot match a longer port like
	# 18090, and a LISTEN row's peer address is always `*`/`0.0.0.0:*`, so it
	# can never carry a numeric port. Only the address column carries a
	# `host:port` shape. Local shapes covered: 127.0.0.1:<p>, *:<p>,
	# 0.0.0.0:<p>, [::]:<p>, [::ffff:...]:<p>.
	if ss -lptn 2>/dev/null | grep -qE "^LISTEN.*:$1[[:space:]]"; then
		return 0
	fi
	return 1
}

port_owner() {
	# `|| true`: grep exits 1 on no match, and under `set -o pipefail` +
	# `set -e` that kills the caller from inside a command substitution. A
	# port with no visible owner is the ordinary case in the sandbox.
	ss -lptn 2>/dev/null | grep -E "127.0.0.1:$1[[:space:]]|\*:$1[[:space:]]" |
		grep -oE 'pid=[0-9]+' | cut -d= -f2 | sort -u || true
	return 0
}

lane_for() {
	# The SHARED arm MUST come first: rules/heads_test.go and the goldens in
	# rules/testdata/heads/ are owned by both threads and would otherwise be
	# swallowed by the rules/* pattern in the engine arm. Order in a case
	# statement is the whole classification.
	case "$1" in
		Makefile|AGENTS.md|go.mod|go.sum|rules/heads_test.go|rules/testdata/heads/*|rules/acceptance_test.go) echo SHARED ;;
		rules/*|effects/*|state/*|events/*|cards/*|decision/*|view/*|replay/*|protocol/*|host/*|web/*|cmd/gorged/*|cmd/gentypes/*|scripts/*|.githooks/*) echo engine ;;
		botpolicy/*|seat/*|cmd/botbench/*|internal/testutil/*|distill/*) echo distill ;;
		*) echo unclaimed ;;
	esac
}

# seat_kind says which harness is driving a seat and, for the ones the
# dashboard cannot see, when it last did anything. Read the "wrote Nm ago"
# it prints as ORIENTATION ONLY, never as liveness: a file's mtime is when
# the writer last wrote, which says nothing about whether the writer still
# exists. Liveness lives in seat_state, which reads the process table.
#
# The monitor at :8765 globs <repo>/.worktrees/*/.ds4/{transcripts,pi-sessions}
# for its jsonl, so it shows pi and ds4 seats only. A CLAUDE subagent -- which
# is what visual tasks get, deliberately -- writes none of those files and is
# therefore invisible there, permanently and by construction, not because
# anything went wrong. That is worth stating in the one place someone goes to
# ask "where is my agent", rather than leaving them to conclude it died.
seat_kind() {
	local wt="$ROOT/.worktrees/$1"
	[ -d "$wt" ] || { echo "no worktree"; return 0; }
	if compgen -G "$wt/.ds4/pi-sessions/*.jsonl" >/dev/null 2>&1 ||
		compgen -G "$wt/.ds4/transcripts/*.jsonl" >/dev/null 2>&1; then
		echo "pi/ds4 — on the dash :8765"
		return 0
	fi
	# Newest file it has written, excluding the two directories that churn
	# for reasons unrelated to the agent (node_modules is copied wholesale;
	# .git moves on every command run against the worktree).
	local newest age
	newest=$(find "$wt" \( -name node_modules -o -name .git \) -prune -o -type f -printf '%T@\n' 2>/dev/null |
		sort -rn | head -1 | cut -d. -f1)
	if [ -n "$newest" ]; then
		age=$(( ( $(date +%s) - newest ) / 60 ))
		echo "Claude subagent — NOT on the dash; wrote ${age}m ago"
	else
		echo "Claude subagent — NOT on the dash; no writes yet"
	fi
}

cmd_status() {
	echo "fleet status — you are the '$THREAD' thread"
	echo
	echo "main: $(git -C "$ROOT" log --oneline -1 2>/dev/null)"
	local remote
	remote=$(git -C "$ROOT" log --oneline origin/main -1 2>/dev/null || echo '(no origin)')
	echo "origin/main: $remote"
	echo
	local dead unknown
	dead=$(count_state DEAD)
	unknown=$(count_state UNKNOWN)
	echo "== seats (LIVE only: local $(count_pool local)/$MAX_SEATS · paid $(count_pool paid)/$MAX_PAID; $dead dead, $unknown unknown)"
	if [ -n "$(ls -A "$SEATS" 2>/dev/null)" ]; then
		for s in "$SEATS"/*; do
			[ -d "$s" ] || continue
			local name id state pidcol
			name=$(basename "$s")
			id=${name#*-}
			state=$(seat_state "$s")
			case "$state" in
				REMOTE)  pidcol="no local pid" ;;
				PENDING) pidcol="awaiting attach" ;;
				UNKNOWN) pidcol="pid $(cat "$s/pid" 2>/dev/null || echo 'not recorded')" ;;
				*)       pidcol="pid $(cat "$s/pid" 2>/dev/null || echo '?')" ;;
			esac
			printf '  %-7s %-20s [%-5s] %-17s %-34s %s\n' \
				"$state" "$name" "$(kind_of "$s")" "$pidcol" \
				"$(seat_kind "$id")" "$(cat "$s/why" 2>/dev/null || echo '')"
		done
		echo "  LIVE=recorded pid is running and is the process claimed · DEAD=that process is gone"
		echo "  REMOTE=Claude subagent, no local pid (never reaped) · PENDING=claimed, pid not attached yet"
		echo "  UNKNOWN=no pid on record (predates pid tracking) — not counted as alive, and never reaped"
		[ "$dead" -gt 0 ] && echo "  -> $dead dead seat(s): 'fleet.sh reap' releases them." || true
		[ "$unknown" -gt 0 ] && echo "  -> $unknown unknown seat(s): prove or retire each by hand — 'fleet.sh attach <id> <pid>', 'fleet.sh attach <id> --remote', or 'fleet.sh release <id>'." || true
	else
		echo "  (none)"
	fi
	echo
	echo "== tokens"
	local any=0
	for t in heads quiet-box merge; do
		if [ -d "$TOKENS/$t" ]; then
			any=1
			printf '  %-10s HELD by %s — %s\n' "$t" \
				"$(cat "$TOKENS/$t/thread" 2>/dev/null)" "$(cat "$TOKENS/$t/why" 2>/dev/null)"
		fi
	done
	[ "$any" = 0 ] && echo "  (all free)"
	echo
	echo "== worktrees"
	git -C "$ROOT" worktree list | tail -n +2 | while read -r path sha branch; do
		printf '  %-14s %s %s\n' "$(basename "$path")" "$sha" "$branch"
	done
	echo
	cmd_ports
	echo
	echo "== the other thread's state file"
	local other="$FLEET/distill.md"
	[ "$THREAD" = distill ] && other="$FLEET/engine.md"
	if [ -f "$other" ]; then
		echo "  $other (updated $(date -r "$other" '+%Y-%m-%d %H:%M'))"
		sed -n '1,12p' "$other" | sed 's/^/  | /'
	else
		echo "  $other does not exist yet"
	fi
}

cmd_ports() {
	echo "== ports (8080-8099)"
	local p pid pids
	for p in $(seq 8080 8099); do
		# One row per LISTENING port, decided by the socket (see
		# port_listening): an occupied port must never be silently omitted.
		port_listening "$p" || continue
		pids=$(port_owner "$p")
		if [ -z "$pids" ]; then
			# The sandbox hides the pid; show the port with a `?` owner.
			printf '  %-6s pid %-8s %-16s %s\n' "$p" '?' '?' '?'
			continue
		fi
		for pid in $pids; do
			printf '  %-6s pid %-8s %-16s %s\n' "$p" "$pid" \
				"$(cat "/proc/$pid/comm" 2>/dev/null || echo '?')" \
				"$(basename "$(readlink "/proc/$pid/cwd" 2>/dev/null || echo '?')")"
		done
	done
	echo "  ranges: 8080-8081 demo (engine, never take) · 8082-8089 distill · 8090-8099 engine agents"
}

# free_port prints the first unused port in this thread's agent range, so a
# brief can name a concrete port instead of "pick one".
cmd_port() {
	local lo=8090 hi=8099
	[ "$THREAD" = distill ] && { lo=8082; hi=8089; }
	local p
	for p in $(seq "$lo" "$hi"); do
		port_listening "$p" || { echo "$p"; return 0; }
	done
	die "no free port in $lo-$hi for the $THREAD thread"
}

cmd_claim() {
	local id=${1:?usage: fleet.sh claim <task-id> [--paid|--local] [--pid N|--remote] [why]}
	shift || true
	# The pool is a flag, not a guess: a dispatch knows which seat it is about
	# to spend, and inferring it later from a transcript is how a paid run gets
	# miscounted as free.
	#
	# The runner is a flag for the same reason. It is optional because the usual
	# order is claim-then-launch: the pid does not exist yet at claim time. Such
	# a seat is PENDING -- it holds its slot, and `fleet.sh attach` turns it into
	# a tracked LIVE one. Claiming this process's own $$ would be a lie: fleet.sh
	# exits the moment this function returns.
	local kind=local runner=pending pid=''
	while [ $# -gt 0 ]; do
		case "$1" in
			--paid)   kind=paid;    shift ;;
			--local)  kind=local;   shift ;;
			--remote) runner=remote; shift ;;
			--pid)    runner=pid; pid=${2:?--pid needs a process id}; shift 2 ;;
			--pid=*)  runner=pid; pid=${1#--pid=}; shift ;;
			*) break ;;
		esac
	done
	local pidstart=''
	if [ "$runner" = pid ]; then
		pidstart=$(proc_starttime "$pid") ||
			die "no process $pid to claim a seat for (nothing at /proc/$pid)"
	fi
	local cap=$MAX_SEATS
	[ "$kind" = paid ] && cap=$MAX_PAID
	local n
	n=$(count_pool "$kind")
	local seat="$SEATS/$THREAD-$id"
	[ -d "$seat" ] && die "seat $THREAD-$id already claimed"
	# mkdir is the atomic primitive: two threads racing for the last seat
	# cannot both succeed. The count is checked first and re-checked after,
	# because the check itself is not atomic with the mkdir.
	if [ "$n" -ge "$cap" ]; then
		if [ "$kind" = paid ]; then
			die "all $cap PAID seats are claimed — wait, or use a local seat ($(count_pool local)/$MAX_SEATS in use)"
		fi
		die "all $cap LOCAL seats are claimed — use a paid seat ($(count_pool paid)/$MAX_PAID in use) or a Claude subagent rather than waiting"
	fi
	mkdir "$seat" || die "seat $THREAD-$id already claimed"
	# Write the kind AND the runner BEFORE re-counting. The kind, or the
	# re-count cannot see this seat's own pool and every racing claim reads as
	# local. The runner, or the re-count cannot see this seat as OCCUPIED at
	# all -- count_pool now counts running seats, and a seat with no runner
	# record is UNKNOWN, which holds nothing. Without this the re-check can
	# never fire and two threads can both take the last seat.
	echo "$kind" > "$seat/kind"
	echo "$runner" > "$seat/runner"
	if [ "$runner" = pid ]; then
		echo "$pid" > "$seat/pid"
		echo "$pidstart" > "$seat/pidstart"
	fi
	n=$(count_pool "$kind")
	if [ "$n" -gt "$cap" ]; then
		rm -rf "$seat"
		die "lost the race for the last $kind seat — use the other pool or a Claude subagent"
	fi
	{ echo "${*:-}"; } > "$seat/why"
	date -Iseconds > "$seat/since"
	echo "fleet: claimed $kind seat $THREAD-$id ($n/$cap $kind; local $(count_pool local)/$MAX_SEATS, paid $(count_pool paid)/$MAX_PAID)"
	case "$runner" in
		pid)     echo "fleet: tracking pid $pid (start $pidstart)" ;;
		remote)  echo "fleet: runner is REMOTE (no local pid) — reap will never touch it" ;;
		pending) echo "fleet: no runner yet — run 'fleet.sh attach $id <pid>' once the agent is up, or 'fleet.sh attach $id --remote' for a Claude subagent, or this seat holds its slot untracked" ;;
	esac
}

# attach binds a seat to the process that is actually doing its work, after the
# fact. This is the half of claim that a claim-then-launch dispatch cannot do.
cmd_attach() {
	local id=${1:?usage: fleet.sh attach <task-id> <pid>|--remote}
	local what=${2:?usage: fleet.sh attach <task-id> <pid>|--remote}
	local seat="$SEATS/$THREAD-$id"
	[ -d "$seat" ] || die "no seat $THREAD-$id — claim it first"
	if [ "$what" = --remote ]; then
		rm -f "$seat/pid" "$seat/pidstart"
		echo remote > "$seat/runner"
		echo "fleet: seat $THREAD-$id is REMOTE (Claude subagent, no local pid) — it holds its slot and reap will never release it"
		return 0
	fi
	case "$what" in ''|*[!0-9]*) die "attach takes a numeric pid or --remote, got '$what'" ;; esac
	local pidstart
	pidstart=$(proc_starttime "$what") ||
		die "no process $what to attach (nothing at /proc/$what) — do not attach a pid you have not seen"
	echo "$what" > "$seat/pid"
	echo "$pidstart" > "$seat/pidstart"
	echo pid > "$seat/runner"
	echo "fleet: seat $THREAD-$id now tracks pid $what (start $pidstart) — state $(seat_state "$seat")"
}

# reap releases every seat whose recorded process is provably gone. It is a
# separate verb on purpose: `status` must never mutate state, or the one
# command you run to find out what is happening becomes the command that
# changes it.
#
# reap RELEASES DIRECTORIES. It never signals, kills, or touches a process. It
# skips UNKNOWN (unproven, a human decides) and REMOTE (a Claude subagent has
# no local pid and its absence from the process table proves nothing).
cmd_reap() {
	local n=0 s name state
	for s in "$SEATS"/*; do
		[ -d "$s" ] || continue
		state=$(seat_state "$s")
		[ "$state" = DEAD ] || continue
		name=$(basename "$s")
		echo "fleet: reaped $(kind_of "$s") seat $name — pid $(cat "$s/pid" 2>/dev/null || echo '?') is gone (claimed $(cat "$s/since" 2>/dev/null || echo '?')) — $(cat "$s/why" 2>/dev/null || echo '')"
		rm -rf "$s"
		n=$((n + 1))
	done
	if [ "$n" = 0 ]; then
		echo "fleet: nothing to reap — no seat has a dead recorded process."
	else
		echo "fleet: released $n dead seat(s) (local $(count_pool local)/$MAX_SEATS, paid $(count_pool paid)/$MAX_PAID)"
	fi
	local unknown
	unknown=$(count_state UNKNOWN)
	[ "$unknown" -gt 0 ] && echo "fleet: $unknown seat(s) are UNKNOWN (no pid on record) and were left alone — resolve each with 'fleet.sh attach <id> <pid>|--remote' or 'fleet.sh release <id>'." || true
	return 0
}

cmd_release() {
	local id=${1:?usage: fleet.sh release <task-id>}
	local seat="$SEATS/$THREAD-$id"
	[ -d "$seat" ] || die "no seat $THREAD-$id to release"
	local kind
	kind=$(kind_of "$seat")
	rm -rf "$seat"
	echo "fleet: released $kind seat $THREAD-$id (local $(count_pool local)/$MAX_SEATS, paid $(count_pool paid)/$MAX_PAID)"
}

cmd_token() {
	local name=${1:?usage: fleet.sh token <heads|quiet-box|merge> <acquire|release|status> [why]}
	local action=${2:-status}
	shift 2 || true
	local dir="$TOKENS/$name"
	case "$action" in
		acquire)
			if ! mkdir "$dir" 2>/dev/null; then
				echo "fleet: '$name' is HELD by $(cat "$dir/thread" 2>/dev/null) — $(cat "$dir/why" 2>/dev/null)" >&2
				echo "fleet: do NOT wait. Ship everything the token does not guard and report BLOCKED on the rest." >&2
				exit 1
			fi
			echo "$THREAD" > "$dir/thread"
			{ echo "${*:-}"; } > "$dir/why"
			date -Iseconds > "$dir/since"
			echo "fleet: acquired '$name'"
			;;
		release)
			[ -d "$dir" ] || die "'$name' is not held"
			local holder
			holder=$(cat "$dir/thread" 2>/dev/null || echo '?')
			[ "$holder" = "$THREAD" ] || die "'$name' is held by $holder, not you — say so in your inbox instead of taking it"
			rm -rf "$dir"
			echo "fleet: released '$name'"
			;;
		status)
			if [ -d "$dir" ]; then
				echo "'$name' HELD by $(cat "$dir/thread" 2>/dev/null) since $(cat "$dir/since" 2>/dev/null) — $(cat "$dir/why" 2>/dev/null)"
			else
				echo "'$name' is free"
			fi
			;;
		*) die "unknown token action '$action'" ;;
	esac
}

cmd_lane() {
	[ $# -gt 0 ] || die "usage: fleet.sh lane <path>..."
	local mine=0 theirs=0 shared=0
	local p l
	for p in "$@"; do
		l=$(lane_for "$p")
		printf '  %-10s %s\n' "$l" "$p"
		case "$l" in
			SHARED) shared=1 ;;
			"$THREAD") mine=1 ;;
			unclaimed) ;;
			*) theirs=1 ;;
		esac
	done
	echo
	[ "$theirs" = 1 ] && echo "CROSSES THE OTHER THREAD'S LANE: land those files as their own small commit on main FIRST, then note it in your inbox (PROTOCOL.md §2)."
	[ "$shared" = 1 ] && echo "TOUCHES SHARED FILES: heads/ratchet goldens need the 'heads' token."
	[ "$theirs$shared" = "00" ] && echo "Entirely within the $THREAD lane — no coordination needed."
	return 0
}

# merge runs a command under the merge lock. The lock is held for the command's
# duration only, so it must be the merge itself, never a whole task.
cmd_merge() {
	[ "${1:-}" = "--" ] && shift
	[ $# -gt 0 ] || die "usage: fleet.sh merge -- <command>"
	# 9>&- in any server the command starts: a child inherits the lock fd and
	# would hold it for its lifetime. That deadlock has happened here.
	flock -w 300 9 || die "timed out waiting for the merge lock — check 'fleet.sh token merge status'"
	echo "fleet: merge lock held by $THREAD"
	"$@"
} 9>"$FLEET/merge.lock"

case "${1:-status}" in
	status)  cmd_status ;;
	ports)   cmd_ports ;;
	port)    cmd_port ;;
	claim)   shift; cmd_claim "$@" ;;
	attach)  shift; cmd_attach "$@" ;;
	reap)    cmd_reap ;;
	release) shift; cmd_release "$@" ;;
	token)   shift; cmd_token "$@" ;;
	lane)    shift; cmd_lane "$@" ;;
	merge)   shift; cmd_merge "$@" ;;
	*) die "unknown command '${1}' (status|ports|port|claim|attach|reap|release|token|lane|merge)" ;;
esac
