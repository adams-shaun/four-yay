#!/usr/bin/env bash
# deploy-demo.sh — stop the running demo gorged, then serve the demo on
# 127.0.0.1:8080 only: bot-only tables with an omniscient (clairvoyant) spectator view;
# set HUMANS=0 (slot index) for a person at t1. On-demand play-vs-bot tables stay public so a
# spectator never sees a person's hand.
#
# Invoked by `make deploy-demo`, which builds the client and the binary
# first. Run by hand by the operator only: stopping the servers aborts every
# in-flight vs-bot game, so since 2026-09-22 no hook or daemon runs it.
set -euo pipefail

BIN=${BIN:-bin/gorged}
DECKS=${DECKS:-internal/testutil/decks}
# 2026-09-29 (later): 1, down from 4. az-redeal's 100-sim MCTS search runs on
# EVERY table concurrently, and all of them restart fresh trees the instant
# their match ends. One table keeps that work bounded. Override with TABLES=4
# to get the old density back once search's per-decision cost is tuned down.
TABLES=${TABLES:-1}
# 2026-09-29: 2, tied to BOT_POLICY below. Both az-redeal and sb-search-lite-
# atk have Info.MaxSeats 2 (bots/azredeal, bots/sbsearch) -- measured and
# safe at 2 seats only. Override both together (SEATS=4
# BOT_POLICY=lethal-pressure) to get the old 4-seat shape back with a
# commander/4-seat-capable policy.
SEATS=${SEATS:-2}
# Wall-clock delay gorged inserts per decision. This has walked 1.5s -> 250ms
# -> 500ms -> 250ms; the user asked for 250ms back on 2026-09-07, so 250ms is
# the ruling and the earlier "tighter than a person can follow" judgement was
# the orchestrator's, not theirs. Kept the default in both places -- here and
# `gorged -pace` -- so "the default pace" means one number. Override for a
# single deploy with `PACE=1.5s make deploy-demo`.
#
# 2026-09-28: 50ms, the operator's call for the single omniscient demo.
PACE=${PACE:-50ms}
# Spectator visibility of the startup (bot-only) tables, and of on-demand
# play-vs-bot tables (-vsbot-spectator). Omniscient never exposes library
# order; it must never be used for a table with a person in it.
SPECTATOR=${SPECTATOR:-omniscient}
VSBOT_SPECTATOR=${VSBOT_SPECTATOR:-public}
# -humans takes comma-separated SLOT INDICES of table t1 (e.g. 0 or 0,2), so
# "0" means slot zero is a real person and the table waits for them. Empty
# (the default) passes no slot: every startup table is bot-only.
HUMANS=${HUMANS:-}
# Hosted bot policy (host/bot_policy.go's closed vocabulary) for every
# startup table and for a play-vs-bot game that names none.
#
# 2026-09-29 (later): sb-search-lite-atk (bots/sbsearch), down from az-redeal.
# az-redeal's honest MCTS froze the box when several tables' searches piled
# up at once right at match-end -- its own Cost note says why: "contention
# raises it, the host never cuts the search" (bots/azredeal/azredeal.go).
# There is no bail-out yet for EITHER search-based policy (tracked:
# cli-20260929T214831Z-6b1aaa71, a wall-clock deadline for the search loop);
# until that lands, sb-search-lite-atk is not meaningfully safer on cost --
# 219ms mean/searched decision vs az-redeal's 247ms, same ballpark
# (docs/superpowers/specs/2026-09-28-spellbench-agent-design.md §12.6) -- it
# is the operator's pick on strength/architecture, not a load fix by itself.
# It beats sb-tactical 328-184 (64.1%) at that cost; sb-tactical alone
# (~1ms/decision, no search) is the only registered policy that is actually
# cheap, if the bail-out ticket doesn't land before the next incident.
#
# 2026-09-29 (earlier): az-redeal (bots/azredeal), +20.5pp vs the production
# bot (§12.5), 100-simulation honest-redeal MCTS. Smoke-tested standalone
# (2 seats, constructed, port 8095): three matches, no panics or stalls --
# the freeze took several concurrent tables, not one.
#
# Both are Experimental tier and constructed-only/2-seat-only (bots.Info.
# Formats/MaxSeats); those fields are metadata, not enforced by the host, so
# FORMATS/SEATS below must stay inside that envelope by convention, not by a
# runtime check. Before that (2026-09-28): lethal-pressure (AR7).
BOT_POLICY=${BOT_POLICY:-sb-search-lite-atk}
# 2026-09-29: all four constructed, tied to BOT_POLICY above (commander and
# 4+ seats are outside az-redeal's measured envelope). This drops the
# commander half of the old "two commander, two constructed" split; restore
# it with FORMATS=commander,commander,constructed,constructed alongside a
# SEATS=4 BOT_POLICY=lethal-pressure override.
FORMATS=${FORMATS:-constructed}
# Deterministic across deploys: the same seed deals the same opening tables,
# so a UI change is the only thing that differs between two screenshots.
SEED=${SEED:-1}
# GOMEMLIMIT for the gorged processes only. The Makefile exports a
# test-oriented `GOMEMLIMIT ?= 5GiB` sized for `go test` package binaries;
# inherited by the server it drove the GC to run continuously with a live
# heap near that ceiling (~84% of the box in gcDrain, measured 2026-09-26).
# A distinct variable name here means the Makefile's export can never
# silently reach the server; a long-running demo wants a higher ceiling.
DEMO_GOMEMLIMIT=${DEMO_GOMEMLIMIT:-8GiB}

PUB_PORT=${PUB_PORT:-8080}
# Ports a previous demo layout served on. The sweep stops a gorged still
# listening there, so the first deploy after a layout change does not leave
# the old server running forever; nothing is started on them.
# (OMNI_PORT/MB_PORT were retired outright by 72d86d5ba — no second server —
# but the ports they bound stay swept: see demo-ports.sh below.)
#
# The set is COMPUTED, not listed: demo-ports.sh unions every port the demo has
# ever bound (an append-only history) with the ones this run binds, so
# emptying a *_PORT variable to retire a listener can never take a previous
# layout's server off the sweep. That bug left the :8081 -manabrew gorged
# standing after 2026-09-29 and tripped the standing_gorged_excess stability
# veto (2026-09-30). RETIRED_PORTS stays as a caller override for a one-off
# port a manual layout used; it is not the mechanism anymore.
RETIRED_PORTS=${RETIRED_PORTS-}
# shellcheck source=scripts/demo-ports.sh
. "$(dirname "${BASH_SOURCE[0]}")/demo-ports.sh"
PUB_DIR=${PUB_DIR:-/tmp/gorge-demo-pub}
PUB_LOG=${PUB_LOG:-/tmp/gorge-demo-pub.log}

# Durable card-art cache, OUTSIDE the wiped persistence dirs (task
# fb-20260914T113850Z-682e875e). The `rm -rf` inside start_one stays: a stale
# persistence config resuming with zero-valued fields is a correctness hazard
# (ledger finding cp). The art cache has no such hazard — every cache key is
# hashed under artKeyVersion (cmd/gorged/art.go), so a change in what a fetch
# writes rotates every key at once and stale bytes can never come back
# semantically wrong — which is why the wipe's rationale does NOT extend to
# the art cache, and why it must not be re-added: wiping it used to destroy
# the whole deck pool's art on every merge and leave browsers showing missing
# art for minutes while the cache refilled one paced Scryfall request at a
# time (gorged's startup prewarm refills it from the deck files instead).
# /mnt/sata is the reporter's suggested durable home; it is rw in the
# controller/deploy mount namespace (the server's /proc/<pid>/mounts shows it
# rw) even though an agent worktree jail binds it ro. Both servers share the
# one dir, so each name is fetched from Scryfall once per deploy, not once
# per server (single-flight is per-process, so a first-fetch race between the
# two servers can still fetch a cold name twice — harmless: image and facts
# writers use process-unique staging files and publish only complete artifacts
# with atomic renames). Scryfall pacing IS shared by every lock-aware gorged
# whose BINARY knows the stamp file (artCache.paceWait): the fill below and
# both new servers pace each other, and so do old servers a deploy replaces
# once they run a binary this new. The FIRST deploy after the stamp file's
# introduction overlaps old-binary servers, which pace only themselves; the
# fill's budget bounds that unshared window's duration, not its aggregate
# request rate. From the second deploy on, every lock-aware process sharing
# this dir stays within ~10 req/s.
ART_DIR=${ART_DIR:-/mnt/sata/gorge-data/art}

# Bounds on the pre-start art fill. Art is cosmetic: Scryfall being slow,
# rate-limiting or down must never block or indefinitely delay a deploy, and
# the post-merge hook waits only 600s for the deploy lock before it gives up
# on the NEXT merge's deploy. ART_FILL_BUDGET is gorged's own clean stop
# (it prints the summary); ART_FILL_MAX_FAILURES stops a pass against an
# erroring Scryfall after that many names in a row; ART_FILL_CEILING is the
# hard wall-clock kill in case the binary itself wedges. Build, fill ceiling
# and start together stay well inside the hook's 600s.
ART_FILL_BUDGET=${ART_FILL_BUDGET:-200s}
ART_FILL_MAX_FAILURES=${ART_FILL_MAX_FAILURES:-10}
ART_FILL_CEILING=${ART_FILL_CEILING:-230s}
# The ceiling prefers GNU timeout. uutils coreutils 0.2.2 (this box's
# /usr/bin/timeout) never escalates `-k` to SIGKILL and exits 125 rather than
# 124 on a ceiling (measured 2026-09-14: a child ignoring TERM ran its full 8s
# under uutils, and was killed at 1.3s by gnutimeout, Ubuntu's GNU build).
# That is the fallback's limit: under uutils the ceiling stops WAITING at
# 230s but cannot force-kill a fill wedged past gorged's own budget. Because
# -prewarm-art-only never listens, a later deploy's listener-only sweep cannot
# find it; a TERM-ignoring child can survive indefinitely until manual cleanup.
# With no timeout at all, gorged's own -prewarm-art-budget is the only bound.
TIMEOUT=$(command -v gnutimeout || command -v timeout || true)

say() { printf 'deploy-demo: %s\n' "$*"; }

# SWEEP=ports (default) stops only the gorged serving the two demo ports —
# on the ADDRESSES this deploy binds (127.0.0.1, plus a wildcard listener,
# which would keep those binds from succeeding). SWEEP=all stops every gorged
# on the box.
#
# The default is narrow on purpose. A wide sweep once killed a task agent's
# own measurement server mid-run: agents are told to serve on 8090-8099 to
# stay clear of the demo, and a deploy that kills everything makes that
# instruction worthless and silently corrupts their results. "Kill the thing
# occupying the ports I am about to bind" is the actual requirement; killing
# every gorged on the machine is a bigger hammer than the job needs.
SWEEP=${SWEEP:-ports}

# gorged_pids lists the pids of the LISTENING gorged this deploy should stop.
#
# Deliberately not `pkill -f gorged` or `pgrep -f`: a bare -f pattern is
# matched against every process's /proc/<pid>/cmdline INCLUDING this
# script's own, which here has killed the caller's shell and orphaned a
# day of work. Sockets are the safe index -- a server that is serving has
# a listening socket -- and /proc/<pid>/comm is an exact process name, not
# a substring of a command line, so nothing else can match it.
#
# The port sweep matches the LISTENER ADDRESS, not just the port number:
# "the thing occupying the ports I am about to bind" binds 127.0.0.1:P
# itself, or a wildcard (0.0.0.0/[*]/*:P) that would keep that bind from
# succeeding. A gorged bound to another SPECIFIC address of the same port
# — [::1]:P, say, a probe or a review server — keeps its own bind and its
# own port number to itself; sweeping it killed exactly such a process
# (found by a re-reviewer probing [::1]:8090 while the deploy test ran).
gorged_pids() {
	local filter='LISTEN'
	if [ "$SWEEP" != "all" ]; then
		local ports
		ports=$(demo_sweep_ports "$PUB_PORT" $RETIRED_PORTS | paste -sd'|')
		filter="[[:space:]](127\\.0\\.0\\.1|0\\.0\\.0\\.0|\\*|\\[::\\]):($ports)[[:space:]]"
	fi
	# `|| true` is load-bearing, not defensive noise. grep exits 1 when it
	# matches nothing, and with `set -o pipefail` that failure becomes the
	# pipeline's status, which `set -e` then turns into an exit -- from a
	# command substitution, so the script dies at `pids=$(gorged_pids)`
	# BEFORE its first line of output: exit 1, no server, no message.
	# "Nothing is listening" is the ordinary case for a free port, and it is
	# invisible on 8080 precisely because something is always bound
	# there. Found by the distill thread deploying a review server to 8082.
	ss -lptn 2>/dev/null |
		grep -E "$filter" |
		grep -oE 'pid=[0-9]+' | cut -d= -f2 | sort -u |
		while read -r pid; do
			[ -r "/proc/$pid/comm" ] || continue
			[ "$(cat "/proc/$pid/comm")" = "gorged" ] || continue
			echo "$pid"
		done || true
	return 0
}

stop_all() {
	local pids
	pids=$(gorged_pids)
	if [ -z "$pids" ]; then
		say "no gorged running"
		return 0
	fi
	say "stopping gorged: $(echo "$pids" | tr '\n' ' ')"
	# SIGTERM first: gorged flushes its persistence directory on the way
	# out, and a half-written match log is what makes the next start
	# resume something odd.
	for pid in $pids; do kill "$pid" 2>/dev/null || true; done
	for _ in $(seq 1 50); do
		[ -z "$(gorged_pids)" ] && return 0
		sleep 0.1
	done
	say "escalating to SIGKILL"
	for pid in $(gorged_pids); do kill -9 "$pid" 2>/dev/null || true; done
	sleep 0.3
}

start_one() {
	local port=$1 spectator=$2 dir=$3 log=$4 vsbot_spectator=$5
	shift 5
	# A FRESH directory every deploy, on purpose. gorged resumes a table
	# set from its persistence dir, and a config written by an older binary
	# comes back with the fields that binary did not have set to their zero
	# values -- Format's zero is "constructed", a real value, so a resumed
	# pre-format data dir silently serves four constructed tables and
	# ignores -format entirely, with nothing in the output to say so.
	# (Ledger finding cp.) The demo is disposable; determinism beats
	# history here.
	rm -rf "$dir"
	# 9>&- CLOSES THE DEPLOY LOCK'S FD IN THE SERVER. The post-merge hook
	# holds its flock on fd 9, and a child inherits every open descriptor --
	# so without this the servers themselves keep the lock file open for
	# their entire life, and the NEXT deploy blocks on a lock held by the
	# processes it is trying to replace. That is a deadlock the flock was
	# meant to prevent: observed as a merge whose deploy sat waiting behind
	# its own predecessor's servers. Harmless when fd 9 is not open.
	setsid nohup env GOMEMLIMIT="$DEMO_GOMEMLIMIT" "$BIN" \
		-addr "127.0.0.1:$port" \
		-spectator "$spectator" \
		-vsbot-spectator "$vsbot_spectator" \
		-humans "$HUMANS" \
		-bot-policy "$BOT_POLICY" \
		-dir "$dir" \
		-decks "$DECKS" \
		-tables "$TABLES" \
		-seats "$SEATS" \
		-pace "$PACE" \
		-format "$FORMATS" \
		-seed "$SEED" \
		-art-dir "$ART_DIR" \
		-vsbot \
		"$@" \
		>"$log" 2>&1 </dev/null 9>&- &
	say "started $spectator on 127.0.0.1:$port (log $log)"
}

wait_ready() {
	local port=$1
	for _ in $(seq 1 100); do
		if curl -fsS --max-time 2 "http://127.0.0.1:$port/api/tables" >/dev/null 2>&1; then
			return 0
		fi
		sleep 0.2
	done
	say "TIMED OUT waiting for 127.0.0.1:$port"
	return 1
}

if [ "${1:-}" = "--stop-only" ]; then
	stop_all
	exit 0
fi

[ -x "$BIN" ] || { say "no binary at $BIN (run make deploy-demo, not this script)"; exit 1; }

# Fill the card-art cache BEFORE any server is touched, so a deploy normally
# never serves a cold-cache missing-art window and the two servers' startup
# prewarms become no-ops. The cache lives in the durable ART_DIR (outside the
# wiped persistence dirs), and the fill is idempotent: a second run makes zero
# fetches. A genuine Scryfall 404 is a fact (a .miss marker), not a failure.
#
# The fill is BOUNDED (see ART_FILL_* above) and it NEVER aborts the deploy.
# A non-zero exit — failed names, a spent budget, a tripped failure streak or
# the hard ceiling — is reported loudly and the servers start anyway: their
# background prewarm (the same fill loop, unbounded) completes the cache.
# Aborting instead let one flaky name keep new code off the demo, and an
# unbounded 429 storm (about 60s per name) hold the deploy lock for hours
# while later merges' deploys timed out behind it. Rate limiting and 429
# backoff live inside gorged itself (artCache.paceWait / lookupNamed), so this
# can never burst the API.
say "filling card-art cache from $DECKS into $ART_DIR (budget $ART_FILL_BUDGET, ceiling $ART_FILL_CEILING)"
art_rc=0
${TIMEOUT:+"$TIMEOUT" -k 5s "$ART_FILL_CEILING"} "$BIN" -prewarm-art-only \
	-prewarm-art-budget "$ART_FILL_BUDGET" \
	-prewarm-art-max-consecutive-failures "$ART_FILL_MAX_FAILURES" \
	-decks "$DECKS" -art-dir "$ART_DIR" || art_rc=$?
if [ "$art_rc" -ne 0 ]; then
	say "!!! art fill INCOMPLETE (exit $art_rc; 124/137 = the $ART_FILL_CEILING hard ceiling under GNU timeout; 125 = uutils timeout gave up after TERM and cannot force-kill a wedged fill) — see the summary above; starting the servers anyway, their background prewarm finishes the cache"
fi

stop_all
start_one "$PUB_PORT" "$SPECTATOR" "$PUB_DIR" "$PUB_LOG" "$VSBOT_SPECTATOR"
wait_ready "$PUB_PORT"

# Report what each table actually IS, not what the flags asked for. The
# formats above are the request; this line is the server's own answer, and
# it is the thing that regressed silently once already.
say "$(curl -fsS "http://127.0.0.1:$PUB_PORT/api/tables" |
	tr ',' '\n' | grep '"format"' | cut -d'"' -f4 | sort | uniq -c |
	tr '\n' ' ')on :$PUB_PORT"
say "ready — omniscient spectator (bots $BOT_POLICY, pace $PACE) http://localhost:$PUB_PORT/"
