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

# Declared up front, BEFORE any early exit: the EXIT trap below reads it, and
# under `set -u` an undeclared array on a probe-mode/build-failure exit turns
# the trap into `SERVER_PIDS: unbound variable`, whose failure status then
# overrides the script's own exit status.
SERVER_PIDS=()

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

# range_listeners prints the ss -lptn lines whose LOCAL port lies in the
# smoke range (nothing when the range is free). Shared by allocation_failed
# (name the holders that starved the allocation) and teardown_farm (name the
# holders that survived teardown).
range_listeners() {
  ss -lptn 2>/dev/null | awk -v lo="$SMOKE_PORT_LO" -v hi="$SMOKE_PORT_HI" '
    NR > 1 {
      n = split($4, a, ":"); p = a[n]
      if (p ~ /^[0-9]+$/ && p + 0 >= lo && p + 0 <= hi) print
    }'
}

allocation_failed() {
  echo "smoke: $SMOKE_PORT_LO-$SMOKE_PORT_HI stayed contended after 5 attempts — set SMOKE_PORT_LO/HI to another free range" >&2
  local holders
  holders=$(range_listeners)
  if [ -n "$holders" ]; then
    echo "smoke: current holders in $SMOKE_PORT_LO-$SMOKE_PORT_HI:" >&2
    printf '%s\n' "$holders" | sed 's/^/smoke:   /' >&2
  else
    echo "smoke: no listener found in $SMOKE_PORT_LO-$SMOKE_PORT_HI now — the contention was transient (a holder left after the last scan)" >&2
  fi
}

# teardown_farm is the ONE teardown path for every recorded smoke pid: the
# EXIT trap (cleanup) and the bind-race retry both call it, so no second
# kill/rm block can drift from this one. Bounded-but-strict:
#   1. SIGTERM every recorded pid (never pkill -f / bare pgrep -f: the
#      pattern matches our own cmdline and has killed a session here).
#   2. Bounded wait (~5 s of 0.1 s polls). gorged's graceful path is
#      srv.Shutdown under a 5 s deadline then Registry.Close; measured
#      mid-match exit is ≤15 ms, so TERM must stay the FIRST signal — its
#      flush-on-exit writes the persistence dir.
#   3. SIGKILL every pid still alive after that (a sleeper with `trap '' TERM`
#      is the proof this step exists); bounded wait (~2 s) again.
#   4. LOUD stderr for any pid that survived SIGKILL, and for any smoke-range
#      port still LISTENING, so a residual leak names its culprit by pid.
#   5. Only after all of that, rm -rf the persistence dirs: a dir removed
#      under a live server is the half-flushed tables.json race.
teardown_farm() {
  local context="${1:-the smoke run}" pid alive ports
  for pid in "${SERVER_PIDS[@]:-}"; do
    kill "$pid" 2>/dev/null || true
  done
  for _ in $(seq 1 50); do
    alive=0
    for pid in "${SERVER_PIDS[@]:-}"; do
      if kill -0 "$pid" 2>/dev/null; then alive=1; fi
    done
    [ "$alive" -eq 0 ] && break
    sleep 0.1
  done
  for pid in "${SERVER_PIDS[@]:-}"; do
    if kill -0 "$pid" 2>/dev/null; then
      echo "smoke: pid $pid still alive after SIGTERM; escalating to SIGKILL" >&2
      kill -9 "$pid" 2>/dev/null || true
    fi
  done
  for _ in $(seq 1 20); do
    alive=0
    for pid in "${SERVER_PIDS[@]:-}"; do
      if kill -0 "$pid" 2>/dev/null; then alive=1; fi
    done
    [ "$alive" -eq 0 ] && break
    sleep 0.1
  done
  for pid in "${SERVER_PIDS[@]:-}"; do
    if kill -0 "$pid" 2>/dev/null; then
      echo "smoke: LEAK: smoke pid $pid SURVIVED SIGKILL and is still running — kill it by pid; it holds its smoke port until it dies" >&2
    fi
  done
  ports=$(range_listeners)
  if [ -n "$ports" ]; then
    echo "smoke: ports $SMOKE_PORT_LO-$SMOKE_PORT_HI still LISTENING after $context:" >&2
    printf '%s\n' "$ports" | sed 's/^/smoke:   /' >&2
    echo "smoke:   (a listener with no users:(...) detail belongs to another user; a listener pid that is NOT one of the farm pids above is a peer gate or another agent's server, not a leak from this run)" >&2
  fi
  rm -rf "${PUBDIR:-}" "${OMNDIR:-}" "${SEATDIR:-}" "${FIXTUREDIR:-}" "${TALISDIR:-}" "${VITE_CACHE_DIR:-}"
}

cleanup() {
  if [ "${#SERVER_PIDS[@]}" -gt 0 ]; then
    teardown_farm "the smoke run"
  else
    # No servers were recorded (a probe mode, a build failure, an
    # allocation-failure exit): nothing to kill and nothing to re-report —
    # but the temp dirs the run created must STILL go. A teardown that
    # leaks on a failure path is exactly the class this ticket closes.
    # The selftest sets every dir to "" so this is a silent no-op there.
    rm -rf "${PUBDIR:-}" "${OMNDIR:-}" "${SEATDIR:-}" "${FIXTUREDIR:-}" "${TALISDIR:-}" "${VITE_CACHE_DIR:-}"
  fi
}
trap cleanup EXIT

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

if [[ "${SMOKE_TEARDOWN_SELFTEST:-}" == 1 ]]; then
  # Regression gate for the teardown itself (no build, no allocation, no
  # browser): record a sleeper that IGNORES SIGTERM in SERVER_PIDS, run the
  # real teardown helper, and fail loudly unless the escalation killed it.
  # Before the bounded-strict teardown this mode is the "fails without the
  # fix" witness: the old TERM-and-wait-only cleanup left the sleeper alive
  # after PASS/FAIL was already reported, which is exactly how a leaked farm
  # blocked every later run on this box.
  echo "== smoke: teardown selftest =="
  PUBDIR="" OMNDIR="" SEATDIR="" FIXTUREDIR="" TALISDIR="" VITE_CACHE_DIR=""
  SERVER_PIDS=()
  ( trap '' TERM; exec sleep 30 ) &
  sleeper=$!
  SERVER_PIDS+=("$sleeper")
  if ! kill -0 "$sleeper" 2>/dev/null; then
    echo "smoke: SELFTEST FAIL: sleeper pid $sleeper was not alive before teardown (bad precondition)" >&2
    SERVER_PIDS=()
    exit 1
  fi
  echo "smoke: selftest sleeper pid $sleeper recorded (ignores SIGTERM, dies to SIGKILL)"
  teardown_farm "the teardown selftest"
  if kill -0 "$sleeper" 2>/dev/null; then
    echo "smoke: SELFTEST FAIL: sleeper pid $sleeper SURVIVED teardown — SIGKILL escalation missing or ineffective" >&2
    kill -9 "$sleeper" 2>/dev/null || true
    SERVER_PIDS=()
    exit 1
  fi
  echo "smoke: SELFTEST PASS: sleeper pid $sleeper survived SIGTERM and died to the SIGKILL escalation"
  SERVER_PIDS=()
  exit 0
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
  # Same strict helper as the EXIT trap: the lost attempt's survivors (if any)
  # get TERM, a bounded wait, SIGKILL escalation and a port check before the
  # dirs go, instead of a bare `sleep 2` + `rm -rf` that could remove a dir
  # under a still-live process. The extra settle afterwards waits out the
  # winner's scan-to-bind window so the rescan sees its ports taken.
  teardown_farm "the lost bind-race attempt (attempt $attempt)"
  sleep 2
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
echo "smoke: farm pids: public:$PUBPORT=${SERVER_PIDS[0]} omniscient:$OMNPORT=${SERVER_PIDS[1]} seated:$SEATPORT=${SERVER_PIDS[2]} fixture:$FIXTUREPORT=${SERVER_PIDS[3]} talisman:$TALISPORT=${SERVER_PIDS[4]}" >&2
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
