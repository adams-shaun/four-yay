#!/usr/bin/env python3
"""SpellBench protocol v2 harness for cmd/sbagent: tournaments on the fake v2 engine, twin parity, Go fixtures.

Runs against a spellbench checkout of ``origin/protocol-v2`` with the (as of 2026-09-27 unmerged) game loop
``origin/protocol-v2-p23`` (``spellbench.host.game.play_game``) and tours ``origin/protocol-v2-p26``
(``fake_v2_scenario_kinds``/``_board``) merged in, because ``spellbench run`` on protocol-v2 is still the v1
runner. Point ``SPELLBENCH_V2_ROOT`` at that checkout (default /mnt/sata/gorge-training/spellbench-v2-harness);
the script puts its ``python`` and ``python/tests`` first on ``sys.path`` and on ``PYTHONPATH`` so the fake
engine subprocess imports the same code.

Commands::

    sbv2_harness.py tournament [--sbagent BIN] --out DIR [--bots a,b,...] [--pairs N] [--decks Burn,Elves,Faeries] [--workers N]
    sbv2_harness.py tours [--sbagent BIN] --out DIR [--bots a,b,...] [--repeats N]
    sbv2_harness.py fixtures --out DIR

``tournament`` plays a round robin (mirrors included, seat-swapped pairs) of the python builtins ``uniform``,
``heuristic``, ``first`` and the Go agents ``sbagent-random``, ``sbagent-heuristic``, ``sbagent-first`` (subprocess
bots) on the fake engine's scoring decks, then replays every game that seated a Go agent with each Go agent
replaced by its python twin (same game index, so same secrets and agent seeds) and compares game digests: the
digest chains every selection (spec 11.8), so equal digests mean the twins made the same choices.

``tours`` plays the scenario decks (``Scenario:kinds``, ``Scenario:board``, ``Scenario:smoke``) with each bot in seat p0 and ``first`` in p1, with the same twin comparison.

``fixtures`` (``--out internal/spellbench/v2agent/testdata`` regenerates the committed file) records, per seat, the exact agent-role request lines of the scoring game and the scenario tours,
and the python builtins' answer to each (``BotSession.handle_line``), for cmd/sbagent's Go tests.
"""

from __future__ import annotations

import argparse
import gzip
import json
import os
import sys
import threading
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass
from itertools import combinations_with_replacement
from pathlib import Path

ROOT = Path(os.environ.get("SPELLBENCH_V2_ROOT", "/mnt/sata/gorge-training/spellbench-v2-harness"))
_PATHS = [str(ROOT / "python"), str(ROOT / "python" / "tests")]
sys.path[:0] = _PATHS
os.environ["PYTHONPATH"] = os.pathsep.join(_PATHS + [p for p in os.environ.get("PYTHONPATH", "").split(os.pathsep) if p])

from spellbench import wire  # noqa: E402
from spellbench.agent_messages import Choice, OwnDeck, request  # noqa: E402
from spellbench.arena import ratings  # noqa: E402
from spellbench.arena.config import BotSpec  # noqa: E402
from spellbench.arena.drivers import BuiltinDriver, SubprocessDriver  # noqa: E402
from spellbench.bot import BotSession  # noqa: E402
from spellbench.builtins import create_builtin_bot  # noqa: E402
from spellbench.digests import card_name_domain, deck_id  # noqa: E402
from spellbench.host.engine_process import EngineProcess  # noqa: E402
from spellbench.host.game import GameSetup, play_game  # noqa: E402
from spellbench.messages import DeckRow, Limits, Resources, Rules, TimeControl, WireDeck  # noqa: E402
from spellbench.run_secret import RunSecret  # noqa: E402

import fake_v2_engine  # noqa: E402

ENGINE = ROOT / "python" / "tests" / "fake_v2_engine.py"
SECRET = RunSecret(bytes(range(32)))  # the spec 16 test-vector run secret
TIME_CONTROL = TimeControl(120000, 60000, 600000, 2000, 60000, 120000)
LIMITS = Limits(10000, 100000, 500, 4999, 49999)
RESOURCES = Resources(1, 4096, False, 1)
PY_VERSION = "2.0.0"
GO_VERSION = "0.1.0"
TWIN = {"sbagent-random": "uniform", "sbagent-heuristic": "heuristic", "sbagent-first": "first"}
GO_POLICY = {"sbagent-random": "random", "sbagent-heuristic": "heuristic", "sbagent-first": "first"}
# Scenario:pregame (Task 21) is left out: the Task 22 live validator halts its first decision (V1, R2-25:
# a mulligan candidate must match the viewer's hand_count, and the scenario deals no hand).
TOURS = ("kinds", "board", "smoke")


@dataclass(frozen=True)
class DeckSpec:
    catalog_id: str
    decklist: list
    engine_args: tuple

    @property
    def rows(self):
        return tuple(DeckRow(r["name"], r["count"]) for r in self.decklist)


def scoring_deck(name: str) -> DeckSpec:
    return DeckSpec(name, list(fake_v2_engine.SCORING_DECKLIST), ())


def scenario_deck(name: str) -> DeckSpec:
    module = __import__(f"fake_v2_scenario_{name}")
    args = tuple(module.SCENARIO.engine_args)
    if name == "board":
        args = args + ("--all-flags",) if "--all-flags" not in args else args
    return DeckSpec(f"Scenario:{name}", list(module.SCENARIO.decklist), args)


def engine_for(deck: DeckSpec) -> EngineProcess:
    engine = EngineProcess([sys.executable, str(ENGINE), *deck.engine_args], timeout_s=60)
    engine.hello()
    return engine


def game_setup(index: int, deck: DeckSpec, hello) -> GameSetup:
    london = "london" in hello.profile.rules_supported["mulligan"]
    toss = "toss_winner_chooses" in hello.profile.rules_supported["starting_player"]
    rules = Rules.from_json({
        "opponent_decklist": "visible", "mulligan": "london" if london else "none",
        "starting_player": "toss_winner_chooses" if toss else "host_assigned",
        "starting_seat": None if toss else "p0",
        "card_name_domain": card_name_domain(row["name"] for row in deck.decklist),
        "extensions": [], "probe": False})
    wire_deck = WireDeck(deck_id=deck_id(deck.decklist), catalog_id=deck.catalog_id)
    own = OwnDeck(deck_id=wire_deck.deck_id, name=deck.catalog_id, decklist=deck.rows)
    return GameSetup(game_index=index, game_id=SECRET.game_id(index), game_secret_hex=SECRET.game_secret(index).hex(),
                     format="pauper-bo1", wire_decks=(wire_deck, wire_deck), own_decks=(own, own), rules=rules,
                     time_control=TIME_CONTROL, limits=LIMITS, resources=RESOURCES,
                     agent_seeds=(SECRET.agent_seed(index, "p0"), SECRET.agent_seed(index, "p1")))


class CapturingSubprocessDriver(SubprocessDriver):
    """A SubprocessDriver that keeps the bot's stderr (diagnostics) before closing its process."""

    stderr_log: list

    def close(self) -> None:
        agent = self._agent
        if agent is not None:
            text = agent.stderr_text()
            if text.strip():
                self.stderr_log.append(text)
        super().close()


def make_driver(name: str, sbagent: str | None, stderr_log: list):
    if name in GO_POLICY:
        spec = BotSpec(name=name, version=GO_VERSION, type="subprocess",
                       command=(sbagent, "-policy", GO_POLICY[name], "-name", name, "-version", GO_VERSION))
        driver = CapturingSubprocessDriver(spec, startup_ms=TIME_CONTROL.startup_ms)
        driver.stderr_log = stderr_log
        return driver
    return BuiltinDriver(BotSpec(name=name, version=PY_VERSION, type="builtin", seed=0))


def play(index: int, deck: DeckSpec, p0: str, p1: str, sbagent: str | None, stderr_log: list) -> dict:
    engine = engine_for(deck)
    seats = {"p0": make_driver(p0, sbagent, stderr_log), "p1": make_driver(p1, sbagent, stderr_log)}
    try:
        result = play_game(game_setup(index, deck, engine.hello_result), engine=engine, seats=seats)
    finally:
        for driver in seats.values():
            driver.close()
        engine.close()
    return {"index": index, "deck": deck.catalog_id, "p0": p0, "p1": p1, "outcome": result.outcome,
            "classification": result.classification, "winner": result.winner, "reason": result.reason,
            "adjudication": result.adjudication, "step_count": result.step_count,
            "decision_count": result.decision_count, "game_digest": result.game_digest,
            "violation": result.violation}


def twin_of(name: str) -> str:
    return TWIN.get(name, name)


def run_games(jobs, sbagent, workers, stderr_log):
    """jobs: list of (index, deck, p0, p1); returns rows in job order."""
    with ThreadPoolExecutor(max_workers=workers) as pool:
        return list(pool.map(lambda job: play(*job, sbagent, stderr_log), jobs))


def parity(rows, sbagent, workers, stderr_log, deck_of):
    """Replay every row that seats a Go agent with python twins; compare digests and outcomes."""
    jobs, originals = [], []
    for row in rows:
        if row["p0"] in TWIN or row["p1"] in TWIN:
            jobs.append((row["index"], deck_of(row["deck"]), twin_of(row["p0"]), twin_of(row["p1"])))
            originals.append(row)
    replays = run_games(jobs, sbagent, workers, stderr_log)
    mismatches = []
    for original, replay in zip(originals, replays):
        same = (original["game_digest"], original["outcome"], original["reason"]) == (
            replay["game_digest"], replay["outcome"], replay["reason"])
        if not same:
            mismatches.append({"original": original, "twin_replay": replay})
    return {"compared": len(jobs), "mismatches": mismatches}


def leaderboard(rows, bots):
    table = {bot: {"games": 0, "wins": 0, "draws": 0, "losses": 0, "forfeits": 0, "halted": 0, "truncated": 0}
             for bot in bots}
    pairs: dict = {}
    for row in rows:
        p0, p1 = row["p0"], row["p1"]
        if p0 == p1:
            continue  # mirrors are played and reported but not scored
        for seat, bot in (("p0", p0), ("p1", p1)):
            entry = table[bot]
            if row["classification"] in ("halted", "truncated"):
                entry[row["classification"]] += 1
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
    try:
        fit = ratings.fit_bt_ratings([ratings.PairRecord(a, b, w, l, d) for (a, b), (w, l, d) in pairs.items()],
                                     reference_id="uniform")
        elo = {bot: round(ratings.elo_display(value), 1) for bot, value in fit.ratings_log_units}
    except ratings.BtRatingError as exc:
        elo = {"error": str(exc)}
    return {"table": table, "bt_elo_anchor_uniform_1000": elo,
            "pairs": {f"{a} vs {b}": {"a_wins": w, "b_wins": l, "draws": d} for (a, b), (w, l, d) in sorted(pairs.items())}}


def print_table(board, bots):
    elo = board["bt_elo_anchor_uniform_1000"]
    print(f"{'bot':20} {'games':>5} {'W':>4} {'D':>4} {'L':>4} {'score%':>7} {'BT-Elo':>8}  halted/trunc/forfeit")
    order = sorted(bots, key=lambda b: -(board["table"][b]["score_pct"] or 0))
    for bot in order:
        e = board["table"][bot]
        shown = elo.get(bot) if isinstance(elo, dict) and "error" not in elo else "-"
        print(f"{bot:20} {e['games']:>5} {e['wins']:>4} {e['draws']:>4} {e['losses']:>4} {e['score_pct']!s:>7} "
              f"{shown!s:>8}  {e['halted']}/{e['truncated']}/{e['forfeits']}")
    if isinstance(elo, dict) and "error" in elo:
        print(f"BT fit: {elo['error']}")


ALL_BOTS = "uniform,heuristic,first,sbagent-random,sbagent-heuristic,sbagent-first"


def cmd_tournament(args) -> int:
    bots = args.bots.split(",")
    decks = [scoring_deck(name) for name in args.decks.split(",")]
    by_id = {d.catalog_id: d for d in decks}
    jobs, index = [], 0
    for a, b in combinations_with_replacement(bots, 2):
        for pair in range(args.pairs):
            deck = decks[pair % len(decks)]
            for p0, p1 in ((a, b), (b, a)):
                jobs.append((index, deck, p0, p1))
                index += 1
    stderr_log: list = []
    rows = run_games(jobs, args.sbagent, args.workers, stderr_log)
    check = parity(rows, args.sbagent, args.workers, stderr_log, by_id.__getitem__)
    board = leaderboard(rows, bots)
    return report(args.out, "tournament", rows, board, check, stderr_log, bots)


def cmd_tours(args) -> int:
    bots = args.bots.split(",")
    decks = {name: scenario_deck(name) for name in TOURS}
    by_id = {d.catalog_id: d for d in decks.values()}
    jobs, index = [], 0
    for name in TOURS:
        for bot in bots:
            for _ in range(args.repeats):
                jobs.append((index, decks[name], bot, "first"))
                index += 1
    stderr_log: list = []
    rows = run_games(jobs, args.sbagent, args.workers, stderr_log)
    check = parity(rows, args.sbagent, args.workers, stderr_log, by_id.__getitem__)
    summary: dict = {}
    for row in rows:
        key = f"{row['deck']} p0={row['p0']}"
        s = summary.setdefault(key, {})
        label = f"{row['classification']}:{row['reason']}"
        s[label] = s.get(label, 0) + 1
    return report(args.out, "tours", rows, summary, check, stderr_log, bots)


def report(out: str, label: str, rows, board, check, stderr_log, bots) -> int:
    out_dir = Path(out)
    out_dir.mkdir(parents=True, exist_ok=True)
    (out_dir / f"{label}_games.jsonl").write_text("".join(json.dumps(r, sort_keys=True) + "\n" for r in rows))
    (out_dir / f"{label}_summary.json").write_text(json.dumps({"board": board, "parity": check}, indent=1, sort_keys=True))
    (out_dir / f"{label}_sbagent_stderr.txt").write_text("\n----\n".join(stderr_log))
    classes: dict = {}
    for row in rows:
        classes[row["classification"]] = classes.get(row["classification"], 0) + 1
    print(f"{label}: {len(rows)} games, classifications {classes}")
    if label == "tournament":
        print_table(board, bots)
    else:
        for key, value in sorted(board.items()):
            print(f"  {key:40} {value}")
    print(f"twin parity: {check['compared']} games replayed with python twins, {len(check['mismatches'])} mismatches")
    for m in check["mismatches"][:5]:
        print("  MISMATCH", json.dumps(m, sort_keys=True)[:600])
    print(f"sbagent stderr: {len(stderr_log)} non-empty process logs")
    for text in stderr_log[:3]:
        print("  " + text.strip().replace("\n", "\n  ")[:800])
    return 0 if not check["mismatches"] else 1


# ---------------------------------------------------------------------------
# fixtures
# ---------------------------------------------------------------------------


class RecordingSeat:
    """A SeatDriver that records each agent-role request line exactly as a subprocess bot reads it."""

    def __init__(self, pick) -> None:
        self.pick = pick
        self.lines = [wire.canonical_json_dumps(request("hello", "r-0", {"protocol_minor": 0})).decode()]
        self.count = 1

    def _record(self, request_type, payload) -> None:
        self.lines.append(wire.canonical_json_dumps(request(request_type, f"r-{self.count}", payload)).decode())
        self.count += 1

    def start(self, game_start, *, timeout_s):
        self._record("game_start", game_start)

    def choose(self, choose, *, timeout_s):
        self._record("choose", choose)
        return Choice(request_id="rec", candidate_id=self.pick(choose["decision"]), echoes={})

    def game_over(self, game_over, *, timeout_s):
        self._record("game_over", game_over)

    def close(self) -> None:
        pass


def heuristic_pick(seat):
    bot = create_builtin_bot("heuristic")
    bot._seat = seat

    def pick(sd):
        from spellbench.bot import Decision
        decision = Decision.from_request({"game_id": "g", "decision": sd})
        return bot.choose(decision)
    return pick


def answers(lines, bot_name):
    bot = create_builtin_bot(bot_name, seed=0)
    session = BotSession(choose=bot.choose, on_game_start=bot.on_game_start, on_game_over=bot.on_game_over,
                         name=bot_name, version=PY_VERSION)
    return [session.handle_line(line.encode()).decode().rstrip("\n") for line in lines]


ERROR_LINES = [
    "not json",
    "[1,2]",
    "null",
    '{"request_type":"hello","protocol":"spellbench/v2"}',
    '{"request_type":"hello","protocol":"spellbench/v2","request_id":""}',
    '{"request_type":"hello","protocol":"spellbench/v2","request_id":7}',
    '{"request_type":"hello","request_id":"r-0"}',
    '{"request_type":"hello","protocol":"spellbench/v1","request_id":"r-0"}',
    '{"request_type":"hello","protocol":2,"request_id":"r-0"}',
    '{"protocol":"spellbench/v2","request_id":"r-0"}',
    '{"request_type":"dance","protocol":"spellbench/v2","request_id":"r-0"}',
    '{"request_type":"choose","protocol":"spellbench/v2","request_id":"r-1","game_id":"g-none","decision":{},"clock":{}}',
    '{"request_type":"choose","protocol":"spellbench/v2","request_id":"r-1","decision":{}}',
    '{"request_type":"game_over","protocol":"spellbench/v2","request_id":"r-1","game_id":"g-none","terminal":{}}',
    '{"request_type":"game_start","protocol":"spellbench/v2","request_id":"r-1","game_id":5}',
    '{"request_type":"game_start","protocol":"spellbench/v2","request_id":"r-2","game_id":"g-1","seat":"p0","agent_seed":3}',
    '{"request_type":"game_start","protocol":"spellbench/v2","request_id":"r-3","game_id":"g-2","seat":"p0"}',
    '{"request_type":"choose","protocol":"spellbench/v2","request_id":"r-4","game_id":"g-2","decision":{"candidates":[]}}',
    '{"request_type":"choose","protocol":"spellbench/v2","request_id":"r-5","game_id":"g-1","decision":{"candidates":[]}}',
    '{"request_type":"choose","protocol":"spellbench/v2","request_id":"r-6","game_id":"g-1","decision":{"candidates":[{"semantic":{"kind":"pass"}}]}}',
    '{"request_type":"choose","protocol":"spellbench/v2","request_id":"r-7","game_id":"g-1","decision":{"candidates":[3]}}',
    '{"request_type":"choose","protocol":"spellbench/v2","request_id":"r-8","game_id":"g-1","decision":{"candidates":[{"candidate_id":"0","semantic":{"kind":"pass"}}]}}',
    '{"request_type":"choose","protocol":"spellbench/v2","request_id":"r-9","game_id":"g-1","decision":{"seat_step":0,"candidates":[{"candidate_id":0,"semantic":{"kind":"pass"}},{"candidate_id":1,"semantic":{"kind":"play_land","source":null,"face":0}}]},"x_future":1}',
    '{"request_type":"choose","protocol":"spellbench/v2","request_id":"r-10","game_id":"g-1","decision":{"candidates":[{"candidate_id":0,"semantic":7},{"candidate_id":1,"semantic":{"kind":"declare_attack","attacker":null,"defender":{"player":"p1"}}},{"candidate_id":2,"semantic":{"kind":"mulligan","keep":1}}]}}',
    '{"request_type":"game_over","protocol":"spellbench/v2","request_id":"r-11","game_id":"g-1","terminal":{"outcome":"draw"}}',
    '{"request_type":"game_over","protocol":"spellbench/v2","request_id":"r-12","game_id":"g-1","terminal":{}}',
    '{"request_type":"hello","protocol":"spellbench/v2","request_id":"r-13","protocol_minor":0}',
]


def cmd_fixtures(args) -> int:
    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    fixtures = []

    def record(name, deck, picks, index):
        engine = engine_for(deck)
        seats = {seat: RecordingSeat(picks[seat]) for seat in ("p0", "p1")}
        try:
            result = play_game(game_setup(index, deck, engine.hello_result), engine=engine, seats=seats)
        finally:
            engine.close()
        assert result.classification in ("natural",), (name, result)
        for seat, recorder in seats.items():
            if len(recorder.lines) <= 3:   # hello, game_start, game_over only: no decision for this seat
                continue
            fixtures.append({"name": f"{name}_{seat}", "seat": seat, "requests": recorder.lines,
                             "expected": {bot: answers(recorder.lines, bot) for bot in ("first", "heuristic", "uniform")}})

    record("scoring_burn", scoring_deck("Burn"), {"p0": heuristic_pick("p0"), "p1": lambda sd: 0}, 0)
    record("scoring_faeries", scoring_deck("Faeries"), {"p0": lambda sd: 0, "p1": heuristic_pick("p1")}, 1)
    kinds = __import__("fake_v2_scenario_kinds")
    record("tour_kinds", scenario_deck("kinds"), {"p0": kinds.pick, "p1": lambda sd: 0}, 2)
    record("tour_board", scenario_deck("board"), {"p0": lambda sd: 0, "p1": lambda sd: 0}, 3)
    record("tour_smoke", scenario_deck("smoke"), {"p0": kinds.pick, "p1": lambda sd: 0}, 4)
    fixtures.append({"name": "agent_errors", "seat": None, "requests": ERROR_LINES,
                     "expected": {bot: answers(ERROR_LINES, bot) for bot in ("first", "heuristic", "uniform")}})
    path = out / "agent_transcripts.jsonl.gz"
    text = "".join(json.dumps(f, sort_keys=True, ensure_ascii=False) + "\n" for f in fixtures)
    with open(path, "wb") as raw, gzip.GzipFile(filename="", mode="wb", fileobj=raw, compresslevel=9, mtime=0) as gz:
        gz.write(text.encode("utf-8"))
    for f in fixtures:
        print(f"{f['name']}: {len(f['requests'])} requests")
    print(f"wrote {path} ({path.stat().st_size} bytes)")
    return 0


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    sub = parser.add_subparsers(dest="command", required=True)
    t = sub.add_parser("tournament")
    t.add_argument("--sbagent")
    t.add_argument("--out", required=True)
    t.add_argument("--bots", default=ALL_BOTS)
    t.add_argument("--pairs", type=int, default=6)
    t.add_argument("--decks", default="Burn,Elves,Faeries")
    t.add_argument("--workers", type=int, default=3)
    r = sub.add_parser("tours")
    r.add_argument("--sbagent")
    r.add_argument("--out", required=True)
    r.add_argument("--bots", default=ALL_BOTS)
    r.add_argument("--repeats", type=int, default=3)
    r.add_argument("--workers", type=int, default=3)
    f = sub.add_parser("fixtures")
    f.add_argument("--out", required=True)
    args = parser.parse_args(argv)
    return {"tournament": cmd_tournament, "tours": cmd_tours, "fixtures": cmd_fixtures}[args.command](args)


if __name__ == "__main__":
    sys.exit(main())
