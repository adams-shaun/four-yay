#!/usr/bin/env python3
"""SpellBench v2 "in reverse" on gorge: the reference v2 host drives cmd/sbv2engine (real gorge rules).

Needs a spellbench checkout with the v2 game loop (``spellbench.host.game.play_game``, unmerged
``origin/protocol-v2-p23`` merged onto ``origin/protocol-v2`` as of 2026-09-28): point ``SPELLBENCH_V2_ROOT`` at it
(default /mnt/sata/gorge-training/spellbench-v2-harness). Its live validator (V1-V10) checks every decision the
gorge engine poses, exactly as in a rated run.

Commands::

    sbv2_reverse.py play --engine BIN --dir CARDS --out DIR [--sbagent BIN] [--bots a,b,...] [--pairs N]
                         [--decks D1,D2] [--workers N] [--truth] [--mana manual|autopay] [--from I] [--to J] [--mirrors]
    sbv2_reverse.py report --out DIR [--bots a,b,...]

``play`` runs the benchmark-shaped schedule (every unordered bot pair; per pair ``--pairs x len(decks)`` seat-swapped
game pairs; pair p plays deck ``decks[p % len(decks)]`` in both seats, as SpellBench's rotating pool does) and appends
one row per game to ``DIR/games.jsonl``. ``--from/--to`` select a slice of the schedule's game indices so long runs can
be chunked; rows already in games.jsonl are skipped. Each worker keeps one engine process (restarted after an engine
fault or a validator halt, as the host must). ``--truth`` turns on the engine's test-mode truth side channel
(``DIR/truth-w<k>.jsonl.gz``) and every Go agent's belief log (``DIR/belief/g<index>-<seat>.jsonl``) for the
shadow-state check (cmd/sbv2shadow).

Bots: ``uniform``, ``heuristic``, ``first`` (the python v2 builtins, in-process drivers) and ``sbagent-random``,
``sbagent-heuristic``, ``sbagent-first`` (cmd/sbagent subprocesses).

``report`` aggregates games.jsonl: classifications, halts by reason, validator violations, forfeits by cause, every
protocol error the engine and the Go agents counted, and a Bradley-Terry leaderboard (SpellBench's own ``ratings``
module, anchored ``uniform`` = 1000).
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import threading
import time
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

ROOT = Path(os.environ.get("SPELLBENCH_V2_ROOT", "/mnt/sata/gorge-training/spellbench-v2-harness"))
sys.path[:0] = [str(ROOT / "python")]

from spellbench.agent_messages import OwnDeck  # noqa: E402
from spellbench.arena import ratings  # noqa: E402
from spellbench.arena.config import BotSpec  # noqa: E402
from spellbench.arena.drivers import BuiltinDriver, SubprocessDriver  # noqa: E402
from spellbench.digests import card_name_domain, deck_id  # noqa: E402
from spellbench.host.engine_process import EngineProcess  # noqa: E402
from spellbench.host.game import GameSetup, play_game  # noqa: E402
from spellbench.messages import DeckRow, Limits, Resources, Rules, TimeControl, WireDeck  # noqa: E402
from spellbench.run_secret import RunSecret  # noqa: E402

SECRET = RunSecret(bytes(range(32)))  # the spec 16 test-vector run secret
TIME_CONTROL = TimeControl(120000, 60000, 600000, 2000, 60000, 120000)
LIMITS = Limits(10000, 100000, 500, 4999, 49999)
RESOURCES = Resources(1, 4096, False, 1)
PY_VERSION = "2.0.0"
GO_VERSION = "0.1.0"
GO_POLICY = {"sbagent-random": "random", "sbagent-heuristic": "heuristic", "sbagent-first": "first"}
POOL = ("Wildfire", "Rally", "Affinity", "Elves", "Spy", "Burn", "CawGates", "Faeries")


def schedule(bots, decks, pairs, mirrors):
    jobs, index = [], 0
    matchups = [(a, b) for i, a in enumerate(bots) for j, b in enumerate(bots) if i < j or (mirrors and i == j)]
    for a, b in matchups:
        for pair in range(pairs * len(decks)):
            deck = decks[pair % len(decks)]
            for p0, p1 in ((a, b), (b, a)):
                jobs.append({"index": index, "deck": deck, "p0": p0, "p1": p1, "pair": pair})
                index += 1
    return jobs


class Worker:
    """One engine process, reused across games."""

    def __init__(self, k, args):
        self.k, self.args = k, args
        self.engine = None

    def argv(self):
        a = self.args
        argv = [a.engine, "-dir", a.dir, "-mana", a.mana, "-stats", str(Path(a.out) / f"engine-stats-w{self.k}-{os.getpid()}-{time.time_ns()}.json")]
        if a.truth:
            argv += ["-truth", str(Path(a.out) / f"truth-w{self.k}.jsonl.gz")]
        return argv

    def ensure(self):
        if self.engine is None:
            self.engine = EngineProcess(self.argv(), timeout_s=120)
            self.engine.hello()
        return self.engine

    def restart(self):
        self.close()

    def close(self):
        if self.engine is not None:
            stderr = self.engine.stderr_text()
            try:
                self.engine.close()
            finally:
                self.engine = None
            if stderr.strip():
                with open(Path(self.args.out) / f"engine-stderr-w{self.k}.txt", "a") as fh:
                    fh.write(stderr)


class CapturingSubprocessDriver(SubprocessDriver):
    """A SubprocessDriver that keeps the bot's stderr (diagnostics and its sbagent-stats line)."""

    captured: list

    def close(self) -> None:
        agent = self._agent
        if agent is not None:
            try:
                super().close()
            finally:
                self.captured.append(agent.stderr_text())
            return
        super().close()


def make_driver(name, args, index, seat, captured):
    if name in GO_POLICY:
        command = [args.sbagent, "-policy", GO_POLICY[name], "-name", name, "-version", GO_VERSION]
        if args.truth:
            bdir = Path(args.out) / "belief"
            bdir.mkdir(parents=True, exist_ok=True)
            command += ["-belief-out", str(bdir / f"g{index}-{seat}.jsonl")]
        driver = CapturingSubprocessDriver(BotSpec(name=name, version=GO_VERSION, type="subprocess", command=tuple(command)),
                                           startup_ms=TIME_CONTROL.startup_ms)
        driver.captured = captured
        return driver
    return BuiltinDriver(BotSpec(name=name, version=PY_VERSION, type="builtin", seed=0))


def game_setup(index, deck, catalog, domain):
    rows = catalog[deck]
    rules = Rules.from_json({"opponent_decklist": "visible", "mulligan": "none", "starting_player": "host_assigned",
                             "starting_seat": "p0", "card_name_domain": domain, "extensions": [], "probe": False})
    did = deck_id(rows)
    wire = WireDeck(deck_id=did, catalog_id=deck)
    own = OwnDeck(deck_id=did, name=deck, decklist=tuple(DeckRow(r["name"], r["count"]) for r in rows))
    return GameSetup(game_index=index, game_id=SECRET.game_id(index), game_secret_hex=SECRET.game_secret(index).hex(),
                     format="pauper-bo1", wire_decks=(wire, wire), own_decks=(own, own), rules=rules,
                     time_control=TIME_CONTROL, limits=LIMITS, resources=RESOURCES,
                     agent_seeds=(SECRET.agent_seed(index, "p0"), SECRET.agent_seed(index, "p1")))


def play_one(worker, job, catalog, domain):
    args = worker.args
    captured = []
    seats = {s: make_driver(job[s], args, job["index"], s, captured) for s in ("p0", "p1")}
    t0 = time.time()
    try:
        engine = worker.ensure()
        result = play_game(game_setup(job["index"], job["deck"], catalog, domain), engine=engine, seats=seats)
    finally:
        for d in seats.values():
            try:
                d.close()
            except Exception:  # noqa: BLE001 - a close failure must not lose the row
                pass
    wall = time.time() - t0
    if result.classification == "halted":
        worker.restart()   # spec 11.3: the host restarts the engine before any further game
    agent_stats = []
    for text in captured:
        for line in text.splitlines():
            if line.startswith("sbagent-stats "):
                agent_stats.append(json.loads(line[len("sbagent-stats "):]))
    return dict(job, outcome=result.outcome, classification=result.classification, winner=result.winner,
                reason=result.reason, adjudication=result.adjudication, step_count=result.step_count,
                decision_count=result.decision_count, game_digest=result.game_digest, violation=result.violation,
                diagnostics=[d[:400] for d in result.diagnostics][:4], wall_s=round(wall, 2), agent_stats=agent_stats,
                agent_stderr=[t[:600] for t in captured if t.strip() and not t.strip().startswith("sbagent-stats")][:2])


def cmd_play(args):
    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    bots = args.bots.split(",")
    decks = args.decks.split(",")
    jobs = schedule(bots, decks, args.pairs, args.mirrors)
    lo, hi = args.from_index, (args.to_index if args.to_index is not None else len(jobs))
    games_path = out / "games.jsonl"
    done = set()
    if games_path.exists():
        for line in games_path.read_text().splitlines():
            if line.strip():
                done.add(json.loads(line)["index"])
    todo = [j for j in jobs if lo <= j["index"] < hi and j["index"] not in done]
    print(f"schedule {len(jobs)} games; this chunk {len(todo)} (indices [{lo},{hi}), {len(done)} already done)", flush=True)
    probe = EngineProcess([args.engine, "-dir", args.dir, "-quiet"], timeout_s=120)
    hello = probe.hello()
    probe.close()
    catalog = {d.catalog_id: [{"name": r.name, "count": r.count} for r in d.decklist] for d in hello.catalog}
    names = sorted({r["name"] for d in POOL for r in catalog[d]})
    domain = card_name_domain(names)
    lock = threading.Lock()
    workers = [Worker(k, args) for k in range(args.workers)]
    free = list(workers)
    t0 = time.time()
    count = [0]

    def run(job):
        with lock:
            w = free.pop()
        try:
            row = play_one(w, job, catalog, domain)
        except Exception as exc:  # noqa: BLE001 - record and keep going
            w.restart()
            row = dict(job, outcome="error", classification="harness_error", winner=None, reason=repr(exc)[:300])
        finally:
            with lock:
                free.append(w)
        with lock:
            with open(games_path, "a") as fh:
                fh.write(json.dumps(row, sort_keys=True) + "\n")
            count[0] += 1
            print(f"[{count[0]}/{len(todo)} {time.time() - t0:.0f}s] g{job['index']} {job['deck']:9} {job['p0']} vs {job['p1']}: "
                  f"{row['classification']} {row.get('winner')} {row['reason']} steps={row.get('step_count')} {row.get('wall_s')}s",
                  flush=True)
        return row

    try:
        with ThreadPoolExecutor(max_workers=args.workers) as pool:
            list(pool.map(run, todo))
    finally:
        for w in workers:
            w.close()
    return 0


def cmd_report(args):
    out = Path(args.out)
    rows = [json.loads(l) for l in (out / "games.jsonl").read_text().splitlines() if l.strip()]
    bots = args.bots.split(",") if args.bots else sorted({r["p0"] for r in rows} | {r["p1"] for r in rows})
    classes, halts, violations, forfeits, errors = {}, {}, {}, {}, {}
    agent = {"requests": 0, "chooses": 0, "policy_fallbacks": 0, "errors": {}}
    steps = []
    walls = []
    for r in rows:
        classes[r["classification"]] = classes.get(r["classification"], 0) + 1
        if r["classification"] == "halted":
            halts[r["reason"]] = halts.get(r["reason"], 0) + 1
        if r.get("violation"):
            key = r["violation"]["rule"] + ": " + r["violation"]["detail"][:160]
            violations[key] = violations.get(key, 0) + 1
        if r["classification"] == "forfeit":
            forfeits[r["reason"]] = forfeits.get(r["reason"], 0) + 1
        if r["classification"] == "harness_error":
            errors[r["reason"][:120]] = errors.get(r["reason"][:120], 0) + 1
        for s in r.get("agent_stats") or []:
            agent["requests"] += s.get("Requests", 0)
            agent["chooses"] += s.get("Chooses", 0)
            agent["policy_fallbacks"] += s.get("PolicyFallbacks", 0)
            for k, v in (s.get("Errors") or {}).items():
                agent["errors"][k] = agent["errors"].get(k, 0) + v
        if r.get("step_count"):
            steps.append(r["step_count"])
        if r.get("wall_s"):
            walls.append(r["wall_s"])
    engine = {}
    for p in out.glob("engine-stats-*.json"):
        try:
            for k, v in json.loads(p.read_text()).items():
                engine[k] = engine.get(k, 0) + v
        except (OSError, ValueError):
            pass
    board = leaderboard(rows, bots)
    summary = {"games": len(rows), "classifications": classes, "halts": halts, "violations": violations,
               "forfeits": forfeits, "harness_errors": errors, "agent": agent, "engine": engine, "board": board,
               "mean_steps": round(sum(steps) / len(steps), 1) if steps else None,
               "mean_wall_s": round(sum(walls) / len(walls), 2) if walls else None}
    (out / "summary.json").write_text(json.dumps(summary, indent=1, sort_keys=True))
    print(f"{len(rows)} games: {classes}; mean steps/game {summary['mean_steps']}, mean wall {summary['mean_wall_s']}s")
    for title, table in (("halts", halts), ("validator violations", violations), ("forfeits", forfeits), ("harness errors", errors)):
        print(f"{title}: {sum(table.values())}")
        for k, v in sorted(table.items(), key=lambda kv: -kv[1])[:12]:
            print(f"  {v:5} {k}")
    print(f"go agents: {agent}")
    keys = ["error:", "gorge_refused", "dropped_option", "enumerated", "degraded", "value_fallback", "combat_unfiltered",
            "engine_panic", "halt:", "retransmission", "terminal:", "games", "posed"]
    print("engine counters:")
    for k in sorted(engine):
        if any(k.startswith(p) for p in keys):
            print(f"  {k:48} {engine[k]}")
    print_table(board, bots)
    return 0


def leaderboard(rows, bots):
    table = {b: {"games": 0, "wins": 0, "draws": 0, "losses": 0, "forfeits": 0, "halted": 0, "truncated": 0} for b in bots}
    pairs = {}
    for row in rows:
        p0, p1 = row["p0"], row["p1"]
        if p0 == p1 or p0 not in table or p1 not in table:
            continue
        for seat, bot in (("p0", p0), ("p1", p1)):
            entry = table[bot]
            if row["classification"] in ("halted", "truncated", "harness_error"):
                entry[row["classification"] if row["classification"] != "harness_error" else "halted"] += 1
                continue
            entry["games"] += 1
            if row["winner"] is None:
                entry["draws"] += 1
            elif row["winner"] == seat:
                entry["wins"] += 1
            else:
                entry["losses"] += 1
                if row["classification"] == "forfeit":
                    entry["forfeits"] += 1
        if row["classification"] in ("natural", "forfeit"):
            a, b = sorted((p0, p1))
            rec = pairs.setdefault((a, b), [0, 0, 0])
            winner = None if row["winner"] is None else (p0 if row["winner"] == "p0" else p1)
            if winner is None:
                rec[2] += 1
            elif winner == a:
                rec[0] += 1
            else:
                rec[1] += 1
    for entry in table.values():
        entry["score"] = entry["wins"] + entry["draws"] / 2
        entry["score_pct"] = round(100 * entry["score"] / entry["games"], 1) if entry["games"] else None
    elo = None
    anchor = "uniform" if "uniform" in bots else ("sbagent-random" if "sbagent-random" in bots else bots[0])
    try:
        fit = ratings.fit_bt_ratings([ratings.PairRecord(a, b, w, l, d) for (a, b), (w, l, d) in pairs.items()],
                                     reference_id=anchor)
        elo = {bot: round(ratings.elo_display(value), 1) for bot, value in fit.ratings_log_units}
    except Exception as exc:  # noqa: BLE001 - report the fit failure
        elo = {"error": str(exc)}
    return {"table": table, "anchor": anchor, "bt_elo": elo,
            "pairs": {f"{a} vs {b}": {"a_wins": w, "b_wins": l, "draws": d} for (a, b), (w, l, d) in sorted(pairs.items())}}


def print_table(board, bots):
    elo = board["bt_elo"]
    print(f"BT-Elo anchored {board['anchor']} = 1000")
    print(f"{'bot':20} {'games':>5} {'W':>4} {'D':>4} {'L':>4} {'score%':>7} {'BT-Elo':>8}  halted/trunc/forfeit")
    for bot in sorted(bots, key=lambda b: -(board["table"][b]["score_pct"] or 0)):
        e = board["table"][bot]
        shown = elo.get(bot) if isinstance(elo, dict) and "error" not in elo else "-"
        print(f"{bot:20} {e['games']:>5} {e['wins']:>4} {e['draws']:>4} {e['losses']:>4} {e['score_pct']!s:>7} {shown!s:>8}  "
              f"{e['halted']}/{e['truncated']}/{e['forfeits']}")
    for k, v in board["pairs"].items():
        print(f"  {k:44} {v}")


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    sub = p.add_subparsers(dest="command", required=True)
    a = sub.add_parser("play")
    a.add_argument("--engine", required=True)
    a.add_argument("--dir", required=True)
    a.add_argument("--sbagent")
    a.add_argument("--out", required=True)
    a.add_argument("--bots", default="uniform,heuristic,sbagent-random,sbagent-heuristic")
    a.add_argument("--pairs", type=int, default=1)
    a.add_argument("--decks", default=",".join(POOL))
    a.add_argument("--workers", type=int, default=2)
    a.add_argument("--truth", action="store_true")
    a.add_argument("--mana", default="manual")
    a.add_argument("--mirrors", action="store_true")
    a.add_argument("--from", dest="from_index", type=int, default=0)
    a.add_argument("--to", dest="to_index", type=int)
    r = sub.add_parser("report")
    r.add_argument("--out", required=True)
    r.add_argument("--bots")
    args = p.parse_args(argv)
    return {"play": cmd_play, "report": cmd_report}[args.command](args)


if __name__ == "__main__":
    sys.exit(main())
