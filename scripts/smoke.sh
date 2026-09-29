#!/usr/bin/env bash
#
# scripts/smoke.sh — the browser smoke gate (Task SG1, extended by ui19).
#
# Builds the REAL client and the REAL binary, starts FIVE `gorged` servers on
# smoke ports (8090-8099 by default) — public and omniscient spectators, a SEATED 1v1,
# the shared ui24/wheel1 board fixture, and the fb-e079def5 Talisman two-stage
# continuation fixture — then drives
# the headless-browser smoke test in web/e2e against all five, and tears
# every server down (and removes its temp dir) whether the gate passes or
# fails.
#
# The public spectator server is non-negotiable: it is the mode that was
# broken (a literal JSON-null hand spread into the board), and the
# omniscient-only path is exactly what hid the regression from every unit
# test. The SEATED server is the ui19 extension: the earlier public/omni
# gates drive only the two SPECTATOR modes, so two real regressions — a
# hand fan that sized itself from its own output (running a big hand off the
# board) and a seated player's identity bar drawn on top of their own first
# card — sailed through the whole green unit suite to a human screenshot.
# Only a real seated 1v1 client can see them, so the gate now drives one.
# "GET the real ids from /api/tables", "POST /api/games for the join" and
# "fail on any browser error, any stuck loading state, a blank page that never
# mounts" are all in the Playwright spec, not here — this script only builds,
# serves and cleans up.
#
# The servers are started here rather than via Playwright's `webServer`
# because the gate's teardown is a SET-level concern: every server must die
# and every temp dir must go even when the test fails, and a leaked gorged on
# a smoke port poisons the next run.
#
# Run through `make smoke`, or directly: scripts/smoke.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

if [[ -v SMOKE_PORT_LO && ! -v SMOKE_PORT_HI ]] || [[ ! -v SMOKE_PORT_LO && -v SMOKE_PORT_HI ]]; then
  echo "smoke: set both SMOKE_PORT_LO and SMOKE_PORT_HI, or neither" >&2
  exit 1
fi
SMOKE_PORT_LO="${SMOKE_PORT_LO-8090}"
SMOKE_PORT_HI="${SMOKE_PORT_HI-8099}"
if [[ ! "$SMOKE_PORT_LO" =~ ^[0-9]{1,5}$ || ! "$SMOKE_PORT_HI" =~ ^[0-9]{1,5}$ ]]; then
  echo "smoke: invalid port range '$SMOKE_PORT_LO'-'$SMOKE_PORT_HI' (need integer ports 1-65535, LO < HI, at least five ports)" >&2
  exit 1
fi
# Canonical decimal avoids bash interpreting leading zeroes as octal.
SMOKE_PORT_LO=$((10#$SMOKE_PORT_LO))
SMOKE_PORT_HI=$((10#$SMOKE_PORT_HI))
if (( SMOKE_PORT_LO < 1 || SMOKE_PORT_HI > 65535 || SMOKE_PORT_LO >= SMOKE_PORT_HI || SMOKE_PORT_HI - SMOKE_PORT_LO + 1 < 5 )); then
  echo "smoke: invalid port range '$SMOKE_PORT_LO'-'$SMOKE_PORT_HI' (need integer ports 1-65535, LO < HI, at least five ports)" >&2
  exit 1
fi
if (( SMOKE_PORT_LO <= 8081 && SMOKE_PORT_HI >= 8080 )); then
  echo "smoke: port range $SMOKE_PORT_LO-$SMOKE_PORT_HI intersects reserved demo ports 8080-8081" >&2
  exit 1
fi

allocate_ports() {
  local taken p
  taken=$(ss -lptn 2>/dev/null | grep -oE ':[0-9]{1,5}[[:space:]]' | tr -d ': ' | sort -u || true)
  PORTS=()
  for p in $(seq "$SMOKE_PORT_LO" "$SMOKE_PORT_HI"); do
    if ! grep -qx "$p" <<<"$taken"; then PORTS+=("$p"); fi
    if [ "${#PORTS[@]}" -ge 5 ]; then break; fi
  done
  [ "${#PORTS[@]}" -ge 5 ]
}

# Both probe and farm call this for each attempt in the SAME five-try budget.
# A scan failure waits for a holder to leave; a bind race instead tears down
# the farm and waits two seconds in the farm loop below.
allocate_attempt() {
  local attempt="$1"
  if allocate_ports; then return 0; fi
  if (( attempt < 5 )); then
    echo "smoke: need five free ports in $SMOKE_PORT_LO-$SMOKE_PORT_HI (attempt $attempt/5); rescanning in 5s" >&2
    sleep 5
  fi
  return 1
}

allocation_failed() {
  echo "smoke: $SMOKE_PORT_LO-$SMOKE_PORT_HI stayed contended after 5 attempts — set SMOKE_PORT_LO/HI to another free range" >&2
}

if [[ "${SMOKE_ALLOC_PROBE:-}" == 1 ]]; then
  for attempt in 1 2 3 4 5; do
    if allocate_attempt "$attempt"; then
      printf 'smoke: ports'
      printf ' %s' "${PORTS[@]}"
      printf '\n'
      exit 0
    fi
  done
  allocation_failed
  exit 1
fi

# vitest/playwright need Node >=24 (the system node is v20). Prefix the v24
# toolchain so the target works from a plain shell with no nvm shim loaded.
export PATH="$HOME/.nvm/versions/node/v24.15.0/bin:$PATH"
if ! node --version 2>/dev/null | grep -q '^v24'; then
  echo "smoke: need Node 24 at $HOME/.nvm/versions/node/v24.15.0/bin (not found on PATH)" >&2
  exit 1
fi

# Vite writes a mutable cache; point it at our own scratch dir rather than
# through web/node_modules, so the build never touches the shared install.
export VITE_CACHE_DIR="${VITE_CACHE_DIR:-/tmp/gorge-smoke-vite-$$}"

echo "== smoke: building client =="
( cd web && npm run build )
echo "== smoke: building gorged =="
mkdir -p bin
CGO_ENABLED=0 go build -o bin/gorged ./cmd/gorged

# ---- allocate five free smoke ports (8090-8099 by default); NEVER 8080/8081 (demo) ----
# The scan-then-bind window races every OTHER gate running in the same range
# (two agent worktrees legitimately share 8090-8099, measured live twice on
# 2026-09-17: the loser's gorged fails to bind and the gate then drives the
# WINNER's servers — wrong fixture decks, wrong seat tokens, intents from two
# playwrights on one seeded game — which reads exactly like a product
# failure). So allocation is a function and the whole farm is retried below.

PUBDIR="$(mktemp -d /tmp/gorge-smoke-public-XXXXXX)"
OMNDIR="$(mktemp -d /tmp/gorge-smoke-omni-XXXXXX)"
SEATDIR="$(mktemp -d /tmp/gorge-smoke-seat-XXXXXX)"
FIXTUREDIR="$(mktemp -d /tmp/gorge-smoke-ui24-XXXXXX)"
TALISDIR="$(mktemp -d /tmp/gorge-smoke-talisman-XXXXXX)"
SERVER_PIDS=()

cleanup() {
  # Kill by recorded PID (never pkill -f / bare pgrep -f: the pattern matches
  # our own cmdline and has killed a session here). Find by socket if needed.
  for pid in "${SERVER_PIDS[@]:-}"; do
    kill "$pid" 2>/dev/null || true
  done
  # gorged flushes its persistence directory to disk on the way out, so a
  # server that is still dying re-creates files racing `rm -rf` below and the
  # temp dir survives as a half-flushed tables.json. Wait for every recorded
  # pid to actually exit (bounded) before removing the dirs.
  for _ in $(seq 1 50); do
    alive=0
    for pid in "${SERVER_PIDS[@]:-}"; do
      if kill -0 "$pid" 2>/dev/null; then alive=1; fi
    done
    [ "$alive" -eq 0 ] && break
    sleep 0.1
  done
  rm -rf "$PUBDIR" "$OMNDIR" "$SEATDIR" "$FIXTUREDIR" "$TALISDIR" "$VITE_CACHE_DIR"
}
trap cleanup EXIT

wait_ready() {
  local port="$1"
  for _ in $(seq 1 120); do
    if curl -sf -o /dev/null "http://127.0.0.1:$port/api/tables"; then
      return 0
    fi
    sleep 0.5
  done
  return 1
}

start_server() {
  local port="$1" dir="$2" spec="$3" log="$4"
  ./bin/gorged -addr "127.0.0.1:$port" -dir "$dir" -spectator "$spec" \
    -decks internal/testutil/decks -tables 4 -seats 4 -pace 1.5s >"$log" 2>&1 &
  SERVER_PIDS+=("$!")
}

# The SEATED server (ui19) is a 1v1 play-vs-bot server: `-vsbot` arms
# POST /api/games (which returns a join URL seating the client at seat 0 of
# a fresh 2-seat table), and `-humans 1` additionally seats a real human at
# seat 1 of startup table t1 (the -vsbot flow always seats at seat 0, so the
# two together let the gate assert the 1v1 top/bottom mapping from EACH seat).
# `-seat-token` fixes the seat-1 token so the spec can build the join path
# without scraping stderr. `-tables 2 -seats 2` keeps it a real 1v1 table.
start_seated_server() {
  local port="$1" dir="$2" log="$3"
  ./bin/gorged -addr "127.0.0.1:$port" -dir "$dir" -spectator omniscient \
    -decks internal/testutil/decks -tables 2 -seats 2 -pace 1.5s -seed 1 \
    -format constructed -vsbot -humans 1 -seat-token ui19seat1 >"$log" 2>&1 &
  SERVER_PIDS+=("$!")
}

# start_farm starts all five servers on the current PORTS allocation and
# records every pid. Kept as one function so the bind-race retry below can
# rerun it wholesale.
start_farm() {
  SERVER_PIDS=()
  PUBPORT="${PORTS[0]}"
  OMNPORT="${PORTS[1]}"
  SEATPORT="${PORTS[2]}"
  FIXTUREPORT="${PORTS[3]}"
  TALISPORT="${PORTS[4]}"

  start_server "$PUBPORT" "$PUBDIR" public  "$PUBDIR/server.log"
  start_server "$OMNPORT" "$OMNDIR" omniscient "$OMNDIR/server.log"
  start_seated_server "$SEATPORT" "$SEATDIR" "$SEATDIR/server.log"

  # ui24/wheel1: deterministic human hands with zero-cost Memnites plus the
  # Underground Sea that exercises the two-stage mana wheel. The tracked fixture
  # is a deck list (names/counts), never Forge card text.
  ./bin/gorged -addr "127.0.0.1:$FIXTUREPORT" -dir "$FIXTUREDIR" -spectator omniscient \
    -decks web/e2e/fixtures/decks -tables 1 -seats 2 -pace 0 -seed 24 \
    -mulligans 0 -perpetual=false -humans 0,1 -seat-token ui24fixture >"$FIXTUREDIR/server.log" 2>&1 &
  SERVER_PIDS+=("$!")

  # fb-e079def5: the two-stage Talisman continuation. The fixture gets a
  # Talisman of Indulgence castable by turn 2 (two Mountains for its {2} cost),
  # then stops at the priority window whose Talisman activation poses the
  # stage-1 ability wheel the browser test answers THROUGH the picker — the
  # path whose stage-2 colour ask must re-open the wheel at the card.
  ./bin/gorged -addr "127.0.0.1:$TALISPORT" -dir "$TALISDIR" -spectator omniscient \
    -decks web/e2e/fixtures/decks-talisman -tables 1 -seats 2 -pace 0 -seed 7 \
    -mulligans 0 -perpetual=false -humans 0,1 -seat-token talismanwheel >"$TALISDIR/server.log" 2>&1 &
  SERVER_PIDS+=("$!")
}

# The bind race: a gorged that lost the port to a concurrent gate exits within
# milliseconds, so a short settle and a liveness check on every pid decides
# whether this attempt owns all five ports. On a loss, kill what did start,
# wait out the winner's own scan (so the rescan sees its ports taken), throw
# away the persistence dirs (a retried farm must never resume a
# half-written one — the seeded t1 resume path would leak into the run), and
# try fresh ports. Five attempts is generous: the contention window is the
# other gate's own scan-to-bind, not its whole run.
farm=false
for attempt in 1 2 3 4 5; do
  if ! allocate_attempt "$attempt"; then
    continue
  fi
  start_farm
  sleep 1
  lost=""
  for pid in "${SERVER_PIDS[@]}"; do
    kill -0 "$pid" 2>/dev/null || lost="$lost $pid"
  done
  if [ -z "$lost" ]; then farm=true; break; fi
  echo "smoke: bind race on $SMOKE_PORT_LO-$SMOKE_PORT_HI (attempt $attempt, dead pids:$lost) — rescanning" >&2
  for pid in "${SERVER_PIDS[@]}"; do kill "$pid" 2>/dev/null || true; done
  sleep 2
  rm -rf "$PUBDIR" "$OMNDIR" "$SEATDIR" "$FIXTUREDIR" "$TALISDIR"
  PUBDIR="$(mktemp -d /tmp/gorge-smoke-public-XXXXXX)"
  OMNDIR="$(mktemp -d /tmp/gorge-smoke-omni-XXXXXX)"
  SEATDIR="$(mktemp -d /tmp/gorge-smoke-seat-XXXXXX)"
  FIXTUREDIR="$(mktemp -d /tmp/gorge-smoke-ui24-XXXXXX)"
  TALISDIR="$(mktemp -d /tmp/gorge-smoke-talisman-XXXXXX)"
done
if [ "$farm" != true ]; then
  allocation_failed
  exit 1
fi

echo "== smoke: gorged farm up (public :$PUBPORT, omniscient :$OMNPORT, seated :$SEATPORT, fixture :$FIXTUREPORT, talisman :$TALISPORT) =="
if ! wait_ready "$PUBPORT"; then
  echo "smoke: public gorged on :$PUBPORT never became ready:" >&2
  sed -n '1,60p' "$PUBDIR/server.log" >&2 || true
  exit 1
fi
if ! wait_ready "$OMNPORT"; then
  echo "smoke: omniscient gorged on :$OMNPORT never became ready:" >&2
  sed -n '1,60p' "$OMNDIR/server.log" >&2 || true
  exit 1
fi
if ! wait_ready "$SEATPORT"; then
  echo "smoke: seated gorged on :$SEATPORT never became ready:" >&2
  sed -n '1,60p' "$SEATDIR/server.log" >&2 || true
  exit 1
fi
if ! wait_ready "$FIXTUREPORT"; then
  echo "smoke: ui24 fixture gorged on :$FIXTUREPORT never became ready:" >&2
  sed -n '1,60p' "$FIXTUREDIR/server.log" >&2 || true
  exit 1
fi
if ! wait_ready "$TALISPORT"; then
  echo "smoke: talisman fixture gorged on :$TALISPORT never became ready:" >&2
  sed -n '1,60p' "$TALISDIR/server.log" >&2 || true
  exit 1
fi
echo "== smoke: driving the browser gate =="
set +e
( cd web && SMOKE_PUBLIC="http://127.0.0.1:$PUBPORT" SMOKE_OMNI="http://127.0.0.1:$OMNPORT" SMOKE_SEATED="http://127.0.0.1:$SEATPORT" SMOKE_FIXTURE="http://127.0.0.1:$FIXTUREPORT" SMOKE_WHEEL="http://127.0.0.1:$FIXTUREPORT" SMOKE_TALISMAN="http://127.0.0.1:$TALISPORT" npx playwright test )
status=$?
set -e

if [ "$status" -eq 0 ]; then
  echo "== smoke: PASS (public :$PUBPORT, omniscient :$OMNPORT, seated :$SEATPORT, fixture :$FIXTUREPORT, talisman :$TALISPORT) =="
else
  echo "== smoke: FAIL (public :$PUBPORT, omniscient :$OMNPORT, seated :$SEATPORT, fixture :$FIXTUREPORT, talisman :$TALISPORT) =="
fi
exit "$status"
