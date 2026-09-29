#!/usr/bin/env bash
# reward-probe.sh — measure the reward axes and append them to the scoreboard.
#
# Design: docs/superpowers/specs/2026-09-29-reward-loop-and-seed-agent-design.md §2.
#
#   scripts/reward-probe.sh free    the free axes (under a second, no lease)
#   scripts/reward-probe.sh eff     engine throughput (needs a probe lease)
#   scripts/reward-probe.sh all     free, then eff if the broker allows it
#
# The split is the adaptive cadence: `free` runs on every seed cycle because it
# costs nothing, and `eff` only runs in headroom the broker grants, so a probe
# can never be the reason a gate could not land.
set -uo pipefail

ROOT=${GORGE_ROOT:-$(git rev-parse --show-toplevel)}
STATE=${GORGE_REWARD_DIR:-$ROOT/.ds4/reward}
SCORE=$STATE/scoreboard.jsonl
# Measure the CHECKOUT the loop actually lands on, not the probe's own
# worktree: the scoreboard's git_head must be a head that exists on main.
TARGET=${GORGE_TARGET_REPO:-$ROOT}
GRIND_DECK=${GRIND_DECK:-mono-red-prowess}
GRIND_SECONDS=${GRIND_SECONDS:-20}

mkdir -p "$STATE"
say() { printf 'reward-probe: %s\n' "$*"; }

append() { # append rows from stdin, skipping blanks
	while IFS= read -r line; do
		[ -n "$line" ] && printf '%s\n' "$line" >>"$SCORE"
	done
}

probe_free() {
	local t0 t1
	t0=$(date +%s)
	python3 "$ROOT/scripts/reward_collect.py" all --repo "$TARGET" --state-dir "$STATE" | append
	t1=$(date +%s)
	say "free axes measured in $((t1 - t0))s -> $SCORE"
}

# probe_eff measures engine throughput with botbench's grind mode: one deck
# pinned to one goroutine, played against itself for a fixed wall budget. It is
# the cheapest honest read of "how much game does a core buy", which is the
# denominator of the 10x efficiency axis. It plays no policy matchup, so it is
# insensitive to bot strength and only moves when the ENGINE gets faster or
# slower -- exactly what a hotspot fix should show up in.
probe_eff() {
	local out rate intents head t0 t1
	out=$(mktemp -d "${TMPDIR:-/tmp}/reward-eff.XXXXXX")
	trap 'rm -rf "$out"' RETURN
	t0=$(date +%s)
	if ! go build -o "$out/botbench" ./cmd/botbench 2>"$out/build.err"; then
		say "botbench build failed; eff not measured"
		sed -n 1,5p "$out/build.err" >&2
		return 1
	fi
	# The grind itself is the heavy part, so it runs under a probe lease: the
	# broker refuses it when a gate is in flight or memory is tight, and pauses
	# it if a gate starts while it runs.
	local rc=0
	"$ROOT/scripts/heavy.sh" probe --name reward-eff --mem 4G -- \
		"$out/botbench" -grind "$GRIND_DECK" -grind-seconds "$GRIND_SECONDS" \
		-dir "$TARGET/.cards" >"$out/grind.log" 2>&1 || rc=$?
	if [ "$rc" != 0 ]; then
		# 3 is the broker's refusal, which is "not now", not a failure.
		if [ "$rc" = 3 ]; then
			say "no headroom for the eff probe right now (broker refused); skipped"
			return 0
		fi
		say "grind failed rc=$rc"
		tail -n 5 "$out/grind.log" >&2
		return 1
	fi
	t1=$(date +%s)
	rate=$(awk '$1=="combined" {print $3}' "$out/grind.log" | tail -1)
	intents=$(awk -v d="$GRIND_DECK" 'tolower($1)==tolower(d) {print $4}' "$out/grind.log" | tail -1)
	if [ -z "$rate" ]; then
		say "could not parse a rate out of the grind log"
		tail -n 5 "$out/grind.log" >&2
		return 1
	fi
	head=$(git -C "$TARGET" rev-parse --short HEAD)
	local ms
	ms=$(awk -v r="$rate" 'BEGIN{ if (r>0) printf "%.2f", 1000/r; else print 0 }')
	{
		printf '{"ts":"%s","git_head":"%s","axis":"eff","metric":"sim_games_per_s","value":%s,"cost_s":%s,"cmd":"reward-probe.sh eff","note":"grind %s %ss"}\n' \
			"$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$head" "$rate" "$((t1 - t0))" "$GRIND_DECK" "$GRIND_SECONDS"
		printf '{"ts":"%s","git_head":"%s","axis":"eff","metric":"ms_per_game","value":%s,"cost_s":0,"cmd":"reward-probe.sh eff","note":"1000/%s"}\n' \
			"$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$head" "$ms" "$rate"
		[ -n "$intents" ] && printf '{"ts":"%s","git_head":"%s","axis":"eff","metric":"mean_intents_per_game","value":%s,"cost_s":0,"cmd":"reward-probe.sh eff","note":"%s"}\n' \
			"$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$head" "$intents" "$GRIND_DECK"
	} | append
	say "eff: $rate games/s ($ms ms/game) on $GRIND_DECK in $((t1 - t0))s"
}

case ${1:-all} in
free) probe_free ;;
eff) probe_eff ;;
all)
	probe_free
	if "$ROOT/scripts/broker.sh" may-i probe >/dev/null 2>&1; then
		probe_eff
	else
		say "skipping eff: $("$ROOT/scripts/broker.sh" may-i probe 2>&1 || true)"
	fi
	;;
*)
	printf 'usage: reward-probe.sh {free|eff|all}\n' >&2
	exit 2
	;;
esac
