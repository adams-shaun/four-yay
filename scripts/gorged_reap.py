#!/usr/bin/env python3
"""gorged_reap.py — find (and, with --apply, stop) standing gorged servers.

The stability veto (reward.py's standing_gorged_excess) fires when more than
one agent-unowned gorged table server stands on the box, and nothing else in
the pipeline removes one: the detector (reward_collect.py) only measures, and
the instance behind the 2026-10-01 veto -- the ui24 fixture server a finished
seat left on :8095, `-dir /tmp/gorge-ui24-fda041b7` -- stood there until it
exited on its own. This is the removal tool for the class: run it (dry run by
default), read the list, then `--apply` when the list is what you expect.

Ownership is NOT re-decided here. The same `gorged_owned` predicate that feeds
the veto decides what may be reaped, so the reaper can never disagree with the
metric -- anything it would kill is exactly what the metric is counting.

The demo is protected by the documented port contract, not by a name list: a
gorged bound to a DEMO_PORT_HISTORY port (scripts/demo-ports.sh; ports
8080-8081, which AGENTS.md forbids every other user from binding) belongs to
the demo's own deploy sweep and is never touched, however unowned it looks.

    scripts/gorged_reap.py             # dry run: print the box's gorged state
    scripts/gorged_reap.py --apply     # SIGTERM, then SIGKILL after a grace
    scripts/cleanup.sh gorged          # the same thing from the janitor
    APPLY=1 scripts/cleanup.sh gorged

Exit status: 0 when nothing reapable survived, 1 when --apply left a survivor
(so a gate or a script can assert the reaping worked), 2 on usage error.

Scoping for tests: honour $GORGE_PROC_DIR exactly like reward_collect's
process walkers, so a smoke test can point this at a fake /proc tree.
"""

from __future__ import annotations

import argparse
import os
import signal
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import reward_collect as rc  # noqa: E402  (the one home for the ownership rule)

# Mirror of scripts/demo-ports.sh's DEMO_PORT_HISTORY default; cleanup.sh
# passes the live value through the environment when it sources that file.
DEFAULT_DEMO_PORTS = frozenset({"8080", "8081"})

TERM_GRACE_S = 5.0


def demo_ports() -> frozenset[str]:
    raw = os.environ.get("DEMO_PORT_HISTORY", "")
    ports = {p for p in raw.split() if p}
    return frozenset(ports) if ports else DEFAULT_DEMO_PORTS


def classify(proc: Path | None = None) -> dict[int, str]:
    """pid -> verdict, for every live gorged-shaped process.

    Verdicts: "owned" (a live agent owns it), "demo" (bound to a demo port;
    the deploy sweep owns it), "reapable" (the veto is counting it). A gorged
    process that matches no verdict cannot exist: unowned and off the demo
    ports is exactly "reapable".
    """
    ports = demo_ports()
    agent_pids = rc._live_agent_pids(proc)
    seat_ids = rc._agent_seat_ids(agent_pids)
    owners = rc._owner_dirs(agent_pids)
    out: dict[int, str] = {}
    for p in rc.gorged_processes(proc):
        if _addr_port(p["addr"]) in ports:
            out[p["pid"]] = "demo"
        elif rc.gorged_owned(p, proc, agent_pids, seat_ids, owners):
            out[p["pid"]] = "owned"
        else:
            out[p["pid"]] = "reapable"
    return out


def _addr_port(addr: str) -> str:
    return addr.rsplit(":", 1)[-1]


def reap(pids: list[int]) -> int:
    """SIGTERM every pid, then SIGKILL the survivors after the grace.

    Liveness is read from the real process table (`kill 0`), never from the
    possibly-scoped $GORGE_PROC_DIR tree, so a test that scopes the listing
    still drives a REAL kill and does not wait out the grace on a stale
    fake-tree entry. Returns the number of pids still alive at the end. A pid
    is only ever signalled if classify() called it reapable, and the signal
    goes to the pid that was read, so a process that exits between the
    listing and the kill can only make the signal hit a recycled pid after
    15 bits of pid space wrapped twice inside one grace window -- not a shape
    this box produces.
    """
    survivors = 0
    live = []
    for pid in pids:
        try:
            os.kill(pid, signal.SIGTERM)
            live.append(pid)
        except ProcessLookupError:
            continue  # already gone: reaped
    deadline = time.monotonic() + TERM_GRACE_S
    while live and time.monotonic() < deadline:
        live = [pid for pid in live if _alive(pid)]
        if not live:
            break
        time.sleep(0.1)
    for pid in live:
        survivors += 1
        try:
            os.kill(pid, signal.SIGKILL)
        except ProcessLookupError:
            survivors -= 1
    return survivors


def _alive(pid: int) -> bool:
    try:
        os.kill(pid, 0)
        return True
    except ProcessLookupError:
        return False
    except PermissionError:
        return True


def main(argv: list[str]) -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--apply", action="store_true",
                    help="SIGTERM (then SIGKILL) every reapable process")
    a = ap.parse_args(argv)

    proc = Path(os.environ.get("GORGE_PROC_DIR", "/proc"))
    verdicts = classify(proc)
    procs = {p["pid"]: p for p in rc.gorged_processes(proc)}
    reapable: list[int] = []

    if not procs:
        print("no gorged process running")
        return 0
    for pid in sorted(procs):
        p = procs[pid]
        v = verdicts[pid]
        tag = {"owned": "owned by a live agent",
               "demo": "demo port, protected",
               "reapable": "STANDING (agent-unowned)"}[v]
        print(f"pid={pid} addr={p['addr']} dir={p['dir']} ppid={p['ppid']}: {tag}")
        if v == "reapable":
            reapable.append(pid)
    if not reapable:
        print(f"nothing to reap ({len(procs)} process(es), none standing)")
        return 0
    if not a.apply:
        print(f"dry run: {len(reapable)} standing instance(s); "
              "run with --apply (or APPLY=1 scripts/cleanup.sh gorged) to stop them")
        return 0
    left = reap(reapable)
    if left:
        print(f"FAILED: {left} standing instance(s) survived SIGTERM+SIGKILL")
        return 1
    print(f"reaped {len(reapable)} standing instance(s)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
