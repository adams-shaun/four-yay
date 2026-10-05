"""gorge's agentctl hooks: what `.agentctl/config.toml` cannot say as a value.

Loaded by agentctl from this FILE (agentctl.pipeline.hooks.resolve), not
imported as `orchestrator.hooks`, so it must stay self-contained: standard
library only, no `from orchestrator import ...`. Canonical copy lives in
agentctl `consumers/gorge/hooks.py`; the controller copies it here and
agentctl's tests/test_consumer_parity.py pins it against the 2026-09-19
snapshot of orchestrator/gates.py, git_ops.py and daemon.py.
"""
from __future__ import annotations

import os
import re
import subprocess
from pathlib import Path

# config.py:121-128 (confirmed with the user 2026-09-11): a moved chain head is
# auto-accepted -- the golden rewritten and committed -- iff the CR
# conformance lane is 0 FAIL and `make sim` is 20/20 replay OK.
AUTO_ACCEPT_HEAD_MOVES = True

HEADS_CMD = ["go", "test", "./rules", "-run", "TestHeads", "-v"]   # gates.py:64-65
HEADS_TIMEOUT = 120
_HEAD_RE = re.compile(r"(\d+) seats: chain head ([0-9a-f]{16}), golden [0-9a-f]{16}")  # daemon.py:788


def cr_no_fail_lines(output: str) -> bool:
    """gates.py:56-61: the CR lane passes only with no `--- FAIL` / `FAIL` line."""
    fails = "\n".join(l for l in output.splitlines() if l.startswith("--- FAIL") or l == "FAIL")
    return "FAIL" not in fails


def sim_all_replayed(output: str) -> bool:
    """gates.py:68-73: every seeded game replayed OK, and at least one ran."""
    total = output.count("seed ")
    return total > 0 and total == output.count("replay OK")


def apply_head_move(wt: Path, seat_count: int, new_hash: str, reason: str) -> bool:
    """Re-pin one seat count's golden: rules/testdata/heads/<seats>.txt holds
    the hash and a newline, nothing else. The cause is NOT written into the
    tree (that prose, prepended to rules/heads_test.go on every move, was a
    merge conflict by construction; its history is frozen in
    docs/agents/heads-history.md) -- testheads_policy puts it in the commit
    message instead; `reason` stays in the signature for its callers."""
    path = Path(wt) / "rules" / "testdata" / "heads" / f"{seat_count}.txt"
    if not path.exists():
        return False
    path.write_text(f"{new_hash}\n")
    return True


def _heads_pass(wt: Path) -> bool:
    """gates.test_heads, re-run after the goldens are rewritten."""
    try:
        r = subprocess.run(HEADS_CMD, cwd=str(wt), capture_output=True, text=True, timeout=HEADS_TIMEOUT)
    except subprocess.TimeoutExpired:
        return False
    return r.returncode == 0


def _commit_all(wt: Path, message: str) -> bool:
    """git_ops.py:166-169."""
    subprocess.run(["git", "add", "-A"], cwd=str(wt), capture_output=True, text=True)
    return subprocess.run(["git", "commit", "-m", message], cwd=str(wt),
                          capture_output=True, text=True).returncode == 0


def _broker(repo: Path, verb: str) -> None:
    """Tell the resource broker a gate run has ended.

    The matching gate-begin follows only the cheap "ratchets only fall" gate
    in `[[gates]]`, so the whole suite runs with heavy work paused (a gate-end
    after a ratchet-gate failure, with no gate-begin, is a no-op resume). This is the release half. It is
    best-effort on purpose: a broker that is missing or failing must never be
    the reason a passing gate run does not land, and the broker's own stale-flag
    TTL resumes heavy work if this call never happens at all.
    """
    script = Path(repo) / "scripts" / "broker.sh"
    if not script.exists():
        return
    try:
        # The leases live under the MAIN checkout's .ds4/reward, the same path
        # the gate-begin gate pins, not under whichever worktree is landing.
        env = {"GORGE_REWARD_DIR": str(Path(repo) / ".ds4" / "reward"), "PATH": "/usr/bin:/bin"}
        subprocess.run([str(script), verb], cwd=str(repo), capture_output=True, text=True,
                       timeout=60, env={**os.environ, **env})
    except (OSError, subprocess.SubprocessError):
        pass


def testheads_policy(repo: Path, wt: Path, issue, passed: bool, results) -> bool:
    """daemon.py:756-773 as a post_gates hook: returns the gates' final verdict.

    A failed TestHeads (a non-blocking gate) is accepted only when the policy
    is on and CR conformance and `make sim` both passed: the moved heads are
    rewritten, TestHeads re-run, and the goldens committed. Anything else
    fails the gates."""
    _broker(repo, "gate-end")
    heads = next((r for r in results if r.name == "TestHeads"), None)
    if heads is None or heads.ok:
        return passed
    if not AUTO_ACCEPT_HEAD_MOVES:
        return False
    cr_ok = next((r.ok for r in results if r.name == "CR conformance"), False)
    sim_ok = next((r.ok for r in results if r.name == "make sim"), False)
    if not (cr_ok and sim_ok):
        return False
    reason = f"resolving {issue.id} ({issue.title[:80]})"
    moved = []
    for seat_count, new_hash in _HEAD_RE.findall(heads.output):
        if apply_head_move(wt, int(seat_count), new_hash, reason=reason):
            moved.append(f"{seat_count} seats moved to {new_hash}")
        issue.log(f"auto-accepted {seat_count}-seat head move to {new_hash}")
    if not _heads_pass(wt):
        return False
    body = "\n".join(moved) + (
        f"\n\nAutonomous orchestrator, {reason}.\n"
        "Auto-accepted: CR conformance lane 0 FAIL and `make sim` 20/20 replay OK,\n"
        "the same proxy this repo has used by hand for every head move -- neither\n"
        "check is sensitive to bot-choice quality, only engine correctness.\n"
        "The first diverging event was not named; find it with cmd/headdiff.")
    _commit_all(wt, f"test(rules): pin the head(s) moved by {issue.id} (autonomous orchestrator)\n\n{body}")
    return passed
