#!/usr/bin/env python3
# Copyright 2026 The gorge Authors. SPDX-License-Identifier: Apache-2.0
"""prep.py — the Python half of DraftZero's sb-v1 item builder, as a data-preparation tool.

gorge replicates DraftZero experiment #3 (docs/012, docs/016; danieljbrooks/draft-zero, MIT, pinned
at b1e0ba6863182a612a1a06fbac9754cbc979e018). Upstream builds the item set with
`tools/search_bench/items.py build`: a game-selection loop (`cmd_build`) that asks `make_item` for
one decision of a kind in a user turn of a game. `make_item` is two halves:

  Python half   reconstruct the StateSpec from the 17lands row (`reconstruct`), check fidelity and
                the turn's labels, and draw the item's `real` world and 8 search `worlds` from the
                belief model (`coach.plan_determinization`).
  bridge half   `bridge.request("build", real, ...)` on XMage, then legality checks on the decision
                XMage reached (turn, step, type, >= 2 options, label matching, defender open).

This script runs upstream's pinned Python (imported from --upstream, never modified or copied) for
the first half ONLY. A Go builder runs the selection loop and does gorge's legality checks where
the bridge's were. Nothing here talks to XMage.

Commands:

  prep.py games --out G.jsonl [--rows R.jsonl.gz]
      The exact selection order of cmd_build: every 36th row from 17, top players, held out,
      shuffled with Random(SEED), mirrored pairs deduped, the dev/test split drawn in the same RNG
      order, and each game's ordered candidate list [(turn, kind)] as `work()` orders it. Line 1 is
      a header (counts, input sha256s); then one game per line, in order. R holds the raw replay
      lines of the selected games and their mirrored partners (17lands data: never commit it), so
      `candidate`/`serve`/`batch` never rescan the 437 MB replay file.

  prep.py candidate --games G.jsonl ROW TURN KIND
      One candidate's Python-side result: {"reject": <make_item's Rejected text>} or the payload.

  prep.py serve --games G.jsonl
      JSON lines on stdin/stdout: {"row":..,"turn":..,"kind":..} -> one response line. Games load
      lazily (an LRU of parsed games, so `rc.analyze` is computed once per game).

  prep.py batch --games G.jsonl --first N --out C.jsonl.gz [--workers 4]
      Every candidate of the first N games, in game order then candidate order, into a
      deterministic gzip (mtime 0, no name).

  prep.py verify --games G.jsonl --first N
      Runs upstream's own `items.make_item` with a capturing stand-in for the bridge on every
      candidate of the first N games and checks that this port returns exactly what make_item
      computed before it called the bridge (rejection text, or real/worlds/opts/seed/labels).

  prep.py verify-games --games G.jsonl
      Runs cmd_build's selection literally (iter_games, shuffle, dedupe, read_games, split) and
      checks the games file and the partner rows against it.

  prep.py pairs
      Build upstream's mirrored-pairs file (data/gameplay/pairs_FDN_PremierDraft.jsonl in the
      clone, git-ignored there) with upstream's own `pairs.main` when it is missing.

Response of `candidate` (all JSON):

  {"row", "turn", "kind", "reject": "<text>", "ledger": "<kind>: <text[:60]>"}   a Rejected
  {"row", "turn", "kind", "error": "<ExcType>", "ledger": "<kind>: error <ExcType>", "message"}
                                                                                  another exception
  {"row", "turn", "kind", "split", "spec", "fidelity", "real", "worlds", "opts", "build_seed",
   "human", "attacked", "win_rate_bucket", "rank", "on_play", "mirrored", "tier", "belief"}
                                                                                  a payload

17lands public data is CC BY 4.0 (https://www.17lands.com/public_datasets).
"""
from __future__ import annotations

import os

# One core per process: numpy's BLAS/OpenMP pools default to every core (32 threads per worker on
# the build box). Set before upstream imports numpy; `serve` workers and `batch`'s spawned workers
# inherit it through the environment.
for _v in ("OMP_NUM_THREADS", "OPENBLAS_NUM_THREADS", "MKL_NUM_THREADS", "NUMEXPR_NUM_THREADS"):
    os.environ.setdefault(_v, "1")

import argparse
import collections
import csv
import dataclasses
import gzip
import hashlib
import json
import random
import subprocess
import sys
import time
from collections import Counter
from pathlib import Path

PINNED = "b1e0ba6863182a612a1a06fbac9754cbc979e018"
DEFAULT_UPSTREAM = "/mnt/sata/gorge-training/searchbench/draft-zero"
KINDS = ("spell", "hold", "attack", "block")
RANK = {"attack": 0, "hold": 1, "block": 2, "spell": 3}      # items.py work(): rarer kinds first
FORMAT = "sbrep-prep/v1"


# --------------------------------------------------------------------------------------------------
# upstream
# --------------------------------------------------------------------------------------------------

class Upstream:
    """The pinned draft-zero clone's modules: `items` (constants, held_out_rows, is_top, Rejected,
    make_item), coach, pairs, reconstruct, replay, Ids."""

    def __init__(self, path: str, allow_unpinned: bool = False):
        self.path = Path(path).resolve()
        head = _git_head(self.path)
        if head != PINNED and not allow_unpinned:
            raise SystemExit(f"upstream {self.path} is at {head}, not the pinned {PINNED} "
                             "(pass --allow-unpinned to use it anyway)")
        self.head = head
        for p in (self.path / "tools" / "search_bench", self.path / "src"):
            if str(p) not in sys.path:
                sys.path.insert(0, str(p))
        import items  # noqa: PLC0415  upstream tools/search_bench/items.py
        from draftzero.gameplay import coach, pairs, reconstruct, replay  # noqa: PLC0415
        from draftzero.gameplay.ids import Ids  # noqa: PLC0415
        self.items, self.co, self.pm, self.rc, self.replay, self.Ids = items, coach, pairs, reconstruct, replay, Ids
        self._ids = None

    @property
    def ids(self):
        if self._ids is None:
            self._ids = self.Ids.load("FDN")         # cmd_build: Ids.load("FDN")
        return self._ids

    def inputs(self) -> dict[str, Path]:
        rp = self.replay.replay_path()
        d = rp.parent
        gp = self.path / "data" / "gameplay"
        return {"replay": rp, "cards": d / "cards.csv", "abilities": d / "abilities.csv",
                "pairs": self.pm.pairs_path(), "deckpool": gp / "deckpool_FDN_PremierDraft.npz",
                "hand_retention": gp / "hand_retention_FDN_PremierDraft.json",
                "items_py": self.path / "tools" / "search_bench" / "items.py"}


def _git_head(path: Path) -> str | None:
    try:
        return subprocess.run(["git", "-C", str(path), "rev-parse", "HEAD"], capture_output=True, text=True,
                              check=True).stdout.strip()
    except (OSError, subprocess.CalledProcessError):
        return None


def sha256(path: Path) -> str | None:
    if not Path(path).exists():
        return None
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for b in iter(lambda: f.read(1 << 22), b""):
            h.update(b)
    return h.hexdigest()


def ensure_pairs(up: Upstream) -> dict:
    """The pairs file as upstream builds it (`python -m draftzero.gameplay.pairs`), if missing."""
    p = up.pm.pairs_path()
    if p.exists():
        return {"built": False, "path": str(p)}
    t0 = time.time()
    rc = up.pm.main([])
    return {"built": True, "path": str(p), "exit": rc, "seconds": round(time.time() - t0, 1)}


# --------------------------------------------------------------------------------------------------
# games: cmd_build's selection order
# --------------------------------------------------------------------------------------------------

def candidates_of(up: Upstream, g) -> list[list]:
    """items.py work(): the game's ordered (turn, kind) candidates."""
    it = up.items
    turns = [n for n in g.decision_turns() if n in it.TURNS]
    grng = random.Random(it.SEED ^ g.row_index)
    cands = [(n, k) for n in turns for k in ("spell", "hold", "attack", "block")]
    grng.shuffle(cands)
    cands.sort(key=lambda c: RANK[c[1]])
    return [[n, k] for n, k in cands]


def select_games(up: Upstream, every: int = 36, start: int = 17) -> tuple[list[dict], dict, list[str], str]:
    """(games in cmd_build's order, counts, raw lines {row: line} of games + partners, header line).

    Equivalent to cmd_build's `list(replay.iter_games(every, start, predicate=keep))` then
    `read_games(partners)`, done in one pass over the file: iter_games' row rule and parse, the
    same predicate, and the partner rows' raw lines (read_games parses them the same way)."""
    it, replay, pm = up.items, up.replay, up.pm
    rows_out: dict[int, str] = {}
    pmap = pm.partner_map(pm.load_pairs())
    held = it.held_out_rows(pmap)

    def keep(g) -> bool:            # cmd_build's predicate, verbatim
        return it.is_top(g) and g.row_index not in held and pmap.get(g.row_index) not in held

    path = replay.replay_path()
    with gzip.open(path, "rt", newline="", encoding="utf-8") as f:     # replay.open_lines
        header_line = f.readline()
    H, lines = replay.open_lines(path)
    games: list[tuple[int, list[int], str]] = []   # (row, decision_turns, line)
    maybe_partner: dict[int, str] = {}
    sampled = 0
    try:
        for i, line in enumerate(lines):
            if i < start or (i - start) % every:
                # a partner row of a sampled row: kept in case its game is selected
                if i in pmap and pmap[i] >= start and (pmap[i] - start) % every == 0:
                    maybe_partner[i] = line
                continue
            sampled += 1
            g = replay.parse_game(next(csv.reader([line])), H, i)
            if i in pmap and pmap[i] >= start and (pmap[i] - start) % every == 0:
                maybe_partner[i] = line
            if not keep(g):
                continue
            games.append((i, g.decision_turns(), line))
    finally:
        lines.close()
    n_top = len(games)
    rng = random.Random(it.SEED)
    rng.shuffle(games)
    used, uniq = set(), []
    for row, dturns, line in games:
        if row in used:
            continue
        used.add(row)
        if row in pmap:
            used.add(pmap[row])
        uniq.append((row, dturns, line))
    want = sorted({pmap[r] for r, _, _ in uniq if r in pmap})
    partners = {r: maybe_partner[r] for r in want if r in maybe_partner}
    missing = [r for r in want if r not in partners]
    if missing:   # a partner outside the sampled grid: read it like read_games does
        H2, lines = replay.open_lines(path)
        need = set(missing)
        try:
            for i, line in enumerate(lines):
                if i in need:
                    partners[i] = line
                    need.discard(i)
                    if not need:
                        break
        finally:
            lines.close()
    split_of = {r: ("dev" if rng.random() < it.DEV_SHARE else "test") for r, _, _ in uniq}
    out = []
    kinds_total = Counter()
    per_game = Counter()
    for idx, (row, dturns, line) in enumerate(uniq):
        rows_out[row] = line
        g = replay.parse_game(next(csv.reader([line])), H, row)
        cands = candidates_of(up, g)
        per_game[len(cands)] += 1
        kinds_total.update(k for _, k in cands)
        prow = pmap.get(row)
        if prow is not None and prow in partners:
            rows_out[prow] = partners[prow]
        out.append({"index": idx, "row": row, "split": split_of[row],
                    "partner": prow if row in pmap else None, "candidates": cands})
    split_counts = Counter(g["split"] for g in out)
    counts = {
        "rows_sampled": sampled, "top_held_out_rows": n_top, "games": len(uniq),
        "mirrored_rows_in_pool": sum(1 for r, _, _ in games if r in pmap),
        "games_with_partner": sum(1 for g in out if g["partner"] is not None),
        "partners_read": len(partners), "partners_off_grid": len(missing),
        "split": {"test": split_counts["test"], "dev": split_counts["dev"]},
        "candidates": sum(per_game[k] * k for k in per_game),
        "candidates_by_kind": {k: kinds_total[k] for k in KINDS},
        "candidates_per_game": {str(k): per_game[k] for k in sorted(per_game)},
        "games_without_candidates": per_game[0],
        "pairs": len(pmap) // 2, "held_out_rows": len(held),
    }
    return out, counts, rows_out, header_line


def _gzip_bytes_writer(path: Path):
    raw = open(path, "wb")
    return raw, gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=0, compresslevel=6)


def cmd_games(a) -> int:
    up = Upstream(a.upstream, a.allow_unpinned)
    t0 = time.time()
    pinfo = ensure_pairs(up)
    games, counts, rows, header_line = select_games(up, a.every, a.start)
    out = Path(a.out)
    out.parent.mkdir(parents=True, exist_ok=True)
    rows_path = Path(a.rows) if a.rows else out.with_name(out.name.removesuffix(".jsonl") + ".rows.jsonl.gz")
    raw, gz = _gzip_bytes_writer(rows_path)
    with raw, gz:
        gz.write((json.dumps({"header": header_line}) + "\n").encode())
        for r in sorted(rows):
            gz.write((json.dumps({"row": r, "line": rows[r]}) + "\n").encode())
    it = up.items
    inputs = {k: {"name": p.name, "sha256": sha256(p)} for k, p in up.inputs().items()}
    header = {
        "format": FORMAT, "version": it.VERSION, "upstream": {"repo": "danieljbrooks/draft-zero", "head": up.head},
        "every": a.every, "start": a.start, "seed": it.SEED, "dev_share": it.DEV_SHARE,
        "turns": [min(it.TURNS), max(it.TURNS)], "quota": it.QUOTA, "real_seed": it.REAL_SEED,
        "search_seed": it.SEARCH_SEED, "n_worlds": it.N_WORLDS, "rank": RANK,
        "counts": counts, "inputs": inputs,
        "rows_file": {"name": rows_path.name, "sha256": sha256(rows_path)},
        "python": sys.version.split()[0], "numpy": _numpy_version(),
    }
    with open(out, "w") as f:
        f.write(json.dumps({"header": header}) + "\n")
        for g in games:
            f.write(json.dumps(g) + "\n")
    print(json.dumps({"pairs": pinfo, "counts": counts, "seconds": round(time.time() - t0, 1)}), file=sys.stderr)
    return 0


def _numpy_version() -> str | None:
    try:
        import numpy  # noqa: PLC0415
        return numpy.__version__
    except ImportError:
        return None


# --------------------------------------------------------------------------------------------------
# candidate: make_item's Python half
# --------------------------------------------------------------------------------------------------

def human_form(up: Upstream, labels: dict) -> dict:
    """`co.human_17lands(labels)` in serialisable form. The resolver reads only labels: its
    PRIORITY answer needs no names (run it here, with upstream's own code); its attack and block
    answers need the engine's names of the aliases, so their inputs are passed through."""
    resolve = up.co.human_17lands(labels)
    pr = resolve({"type": "PRIORITY"}, {})
    return {"priority": dataclasses.asdict(pr),
            "attacks": labels.get("attacks", {}),
            "blocks": labels.get("blocks", []),
            "block_pairing": labels.get("block_pairing")}


def make_item_python(up: Upstream, g, partner, n: int, kind: str, split: str | None = None) -> dict:
    """items.make_item up to (not including) its bridge.request, step for step. Returns the payload,
    or {"reject": text} where make_item raises Rejected(text). Other exceptions propagate (cmd_build
    counts them as "<kind>: error <ExcType>")."""
    it, co, rc, ids = up.items, up.co, up.rc, up.ids
    Rejected = it.Rejected
    try:
        block = kind == "block"
        try:
            spec = (rc.state_after_user_turn(g, n, "declare_attackers", ids=ids, labels=True) if block
                    else rc.state_at_user_turn(g, n, "eot_rollover", ids=ids, labels=True))
        except ValueError as e:  # e.g. the opponent did not attack after user turn n
            raise Rejected("spec: " + str(e).split(": ", 1)[-1][:60])
        fid, low = co.fidelity(spec)
        if low:
            raise Rejected("fidelity")
        labels = spec.labels or {}
        acts = [x for x in (labels.get("casts") or []) + (labels.get("activations") or []) if x.get("key")]
        lands = [x for x in labels.get("lands") or [] if x.get("key")]
        attacked = any((labels.get("attacks") or {}).values())
        if kind == "spell" and not acts:
            raise Rejected("no cast")
        if kind in ("hold", "attack") and acts:
            raise Rejected("cast")
        if kind == "attack" and not (labels.get("attacks") or {}):
            raise Rejected("no attack question recorded")
        slot = g.user_slot(n) if block else g.prev_slot(n)
        if slot is None:
            raise Rejected("no slot")
        seen = Counter(rc.analyze(g, ids).states[slot.seq].revealed)
        exclude = rc.holdout_drafts(g, partner)
        real, info = co.plan_determinization(spec, k=1, seed=it.REAL_SEED + g.row_index, exclude_drafts=exclude,
                                             seen=seen)
        if real is None:
            raise Rejected("belief: " + str(info.get("fallback") or info.get("method")))
        worlds, winfo = co.plan_determinization(spec, k=it.N_WORLDS, seed=it.SEARCH_SEED, exclude_drafts=exclude,
                                                seen=seen)
        opts = dict(labels.get("bridge") or {})
        if kind == "attack":
            opts["decideFrom"] = {"turn": opts.get("decideFrom", {}).get("turn", n), "step": "DECLARE_ATTACKERS"}
        if kind != "block" and lands:
            opts["preLand"] = lands[0]["key"]
        real_d = real[0].to_dict()
    except Rejected as e:
        return {"reject": str(e)}
    return {
        "split": split,
        "spec": spec.to_dict(),
        "fidelity": fid,
        "real": real_d,
        "worlds": [w.to_dict() for w in worlds or []],
        "opts": opts,
        "build_seed": it.REAL_SEED + g.row_index,
        "human": human_form(up, labels),
        "attacked": attacked,
        "win_rate_bucket": g.meta.get("user_game_win_rate_bucket"),
        "rank": g.meta.get("rank"),
        "on_play": g.on_play,
        "mirrored": partner is not None,
        "tier": fid["tier"],
        "belief": {"real": info, "worlds": winfo},
    }


class Prep:
    """Games file + rows file -> candidates, with an LRU of parsed games (analyze is cached on the
    Game object by upstream, so a cached game is analysed once)."""

    def __init__(self, up: Upstream, games_path: str, rows_path: str | None = None, lru: int = 8):
        self.up = up
        gp = Path(games_path)
        self.games: dict[int, dict] = {}
        self.order: list[int] = []
        with open(gp) as f:
            self.header = json.loads(f.readline())["header"]
            for line in f:
                g = json.loads(line)
                self.games[g["row"]] = g
                self.order.append(g["row"])
        rp = Path(rows_path) if rows_path else gp.with_name(self.header["rows_file"]["name"])
        self.lines: dict[int, str] = {}
        with gzip.open(rp, "rt", encoding="utf-8") as f:
            H = next(csv.reader([json.loads(f.readline())["header"]]))
            for line in f:
                d = json.loads(line)
                self.lines[d["row"]] = d["line"]
        self.H = up.replay.Header(H)
        self._cache: collections.OrderedDict[int, object] = collections.OrderedDict()
        self._lru = lru

    def game(self, row: int):
        g = self._cache.get(row)
        if g is not None:
            self._cache.move_to_end(row)
            return g
        line = self.lines.get(row)
        if line is None:
            raise KeyError(f"row {row} is not in the rows file")
        g = self.up.replay.parse_game(next(csv.reader([line])), self.H, row)
        self._cache[row] = g
        while len(self._cache) > self._lru:
            self._cache.popitem(last=False)
        return g

    def candidate(self, row: int, turn: int, kind: str) -> dict:
        head = {"row": row, "turn": turn, "kind": kind}
        meta = self.games.get(row)
        if meta is None:
            return {**head, "error": "UnknownRow", "message": f"row {row} is not a selected game"}
        if kind not in KINDS:
            return {**head, "error": "UnknownKind", "message": f"kind {kind!r} not in {KINDS}"}
        try:
            g = self.game(row)
            partner = self.game(meta["partner"]) if meta["partner"] is not None else None
            r = make_item_python(self.up, g, partner, turn, kind, meta["split"])
        except Exception as e:  # noqa: BLE001  cmd_build: one bad game must not stop the build
            return {**head, "error": type(e).__name__, "ledger": f"{kind}: error {type(e).__name__}",
                    "message": str(e)[:200]}
        if "reject" in r:
            return {**head, "reject": r["reject"], "ledger": f"{kind}: {r['reject'][:60]}"}
        return {**head, **r}


def warm(up: Upstream) -> dict:
    """Load the belief model (pool cards + the full OpponentModel) and time it."""
    t0 = time.time()
    up.co._pool_cards()
    t1 = time.time()
    up.co.opponent_model(())
    t2 = time.time()
    return {"pool_cards_s": round(t1 - t0, 3), "opponent_model_s": round(t2 - t1, 3)}


def dumps(d: dict) -> str:
    return json.dumps(d, separators=(",", ":"))


def cmd_candidate(a) -> int:
    up = Upstream(a.upstream, a.allow_unpinned)
    p = Prep(up, a.games, a.rows)
    print(dumps(p.candidate(a.row, a.turn, a.kind)))
    return 0


def cmd_serve(a) -> int:
    up = Upstream(a.upstream, a.allow_unpinned)
    p = Prep(up, a.games, a.rows, lru=a.lru)
    if a.warm:
        print(json.dumps({"warm": warm(up)}), file=sys.stderr, flush=True)
    out = sys.stdout
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            q = json.loads(line)
            r = p.candidate(int(q["row"]), int(q["turn"]), str(q["kind"]))
        except (ValueError, KeyError, TypeError) as e:
            r = {"error": "BadRequest", "message": f"{type(e).__name__}: {e}"[:200]}
        out.write(dumps(r) + "\n")
        out.flush()
    return 0


# --------------------------------------------------------------------------------------------------
# batch
# --------------------------------------------------------------------------------------------------

_P: Prep | None = None


def _init_worker(upstream: str, allow: bool, games: str, rows: str | None) -> None:
    global _P
    _P = Prep(Upstream(upstream, allow), games, rows)


def _do_game(row: int) -> tuple[int, list[str], list[tuple[str, str, float]], int]:
    assert _P is not None
    lines, times = [], []
    for n, k in _P.games[row]["candidates"]:
        t0 = time.perf_counter()
        r = _P.candidate(row, n, k)
        dt = time.perf_counter() - t0
        lines.append(dumps(r))
        times.append((n, k, "reject" if "reject" in r else "error" if "error" in r else "payload", dt))
    return row, lines, times, _rss_kb()


def _rss_kb() -> int:
    try:
        with open("/proc/self/status") as f:
            for line in f:
                if line.startswith("VmHWM:"):
                    return int(line.split()[1])
    except OSError:
        pass
    return 0


def cmd_batch(a) -> int:
    import multiprocessing as mp  # noqa: PLC0415
    up = Upstream(a.upstream, a.allow_unpinned)
    p = Prep(up, a.games, a.rows)
    rows = p.order[:a.first]
    del p
    out = Path(a.out)
    out.parent.mkdir(parents=True, exist_ok=True)
    t0 = time.time()
    tally = {"payload": Counter(), "reject": Counter(), "error": Counter()}
    timing = collections.defaultdict(list)
    peak = 0
    raw, gz = _gzip_bytes_writer(out)
    tf = open(a.timing, "w") if a.timing else None
    ctx = mp.get_context("spawn")
    with raw, gz, ctx.Pool(a.workers, _init_worker, (a.upstream, a.allow_unpinned, a.games, a.rows),
                           maxtasksperchild=a.maxtasks) as pool:
        for gi, (row, lines, times, rss) in enumerate(pool.imap(_do_game, rows, chunksize=1)):
            peak = max(peak, rss)
            for line, (n, k, what, dt) in zip(lines, times):
                gz.write((line + "\n").encode())
                timing[what].append(dt)
                if tf is not None:
                    tf.write(dumps({"row": row, "turn": n, "kind": k, "outcome": what, "s": round(dt, 5)}) + "\n")
                if what == "payload":
                    tally["payload"][k] += 1
                else:
                    d = json.loads(line)
                    tally[what][d.get("ledger") or f"{k}: {d.get('error')}"] += 1
            if (gi + 1) % 25 == 0:
                print(f"  {gi + 1}/{len(rows)} games, {sum(len(v) for v in timing.values())} candidates "
                      f"({time.time() - t0:.0f}s)", file=sys.stderr, flush=True)
    if tf is not None:
        tf.close()
    summary = {
        "games": len(rows), "candidates": sum(len(v) for v in timing.values()),
        "payload_by_kind": {k: tally["payload"][k] for k in KINDS},
        "reject_by_reason": dict(sorted(tally["reject"].items(), key=lambda x: (-x[1], x[0]))),
        "error_by_reason": dict(sorted(tally["error"].items(), key=lambda x: (-x[1], x[0]))),
        "sha256": sha256(out),
    }
    tim = {w: {"n": len(v), "mean_s": round(sum(v) / len(v), 4), "p50_s": round(sorted(v)[len(v) // 2], 4),
               "p95_s": round(sorted(v)[int(len(v) * 0.95)], 4), "max_s": round(max(v), 3)}
           for w, v in timing.items() if v}
    Path(str(out) + ".summary.json").write_text(json.dumps(summary, indent=1) + "\n")
    print(json.dumps({"summary": summary, "timing": tim, "worker_peak_rss_kb": peak,
                      "wall_s": round(time.time() - t0, 1), "workers": a.workers}, indent=1), file=sys.stderr)
    return 0


# --------------------------------------------------------------------------------------------------
# verify: this port against upstream's own make_item
# --------------------------------------------------------------------------------------------------

class _Captured(Exception):
    pass


class _CaptureBridge:
    """Stands in for BridgePool: records make_item's locals at the bridge call and aborts it."""

    def __init__(self):
        self.frame_locals: dict | None = None
        self.call: tuple | None = None

    def request(self, op, spec, **kw):
        f = sys._getframe(1)
        self.frame_locals = dict(f.f_locals)
        self.call = (op, spec, kw)
        raise _Captured("captured")


def upstream_python_half(up: Upstream, g, partner, n: int, kind: str) -> dict:
    """Run items.make_item itself; return what it computed before the bridge call."""
    br = _CaptureBridge()
    try:
        up.items.make_item(g, partner, n, kind, br, up.ids)
    except up.items.Rejected as e:
        if br.call is None:
            return {"reject": str(e)}
    if br.call is None:
        raise RuntimeError("make_item returned without calling the bridge")
    L = br.frame_locals
    op, real_d, kw = br.call
    return {"op": op, "real": real_d, "seed": kw.pop("seed"), "dump": kw.pop("dumpDecisionState"), "opts": kw,
            "worlds": [w.to_dict() for w in L["worlds"] or []], "spec": L["spec"].to_dict(), "fid": L["fid"],
            "attacked": L["attacked"], "labels": L["labels"]}


def cmd_verify(a) -> int:
    up = Upstream(a.upstream, a.allow_unpinned)
    p = Prep(up, a.games, a.rows)
    n = bad = 0
    kinds = Counter()
    for row in p.order[:a.first]:
        meta = p.games[row]
        for t, k in meta["candidates"]:
            g = p.game(row)
            partner = p.game(meta["partner"]) if meta["partner"] is not None else None
            ours = p.candidate(row, t, k)
            # a fresh parse for upstream, so no cached analysis is shared between the two runs
            g2 = up.replay.parse_game(next(csv.reader([p.lines[row]])), p.H, row)
            p2 = (up.replay.parse_game(next(csv.reader([p.lines[meta["partner"]]])), p.H, meta["partner"])
                  if partner is not None else None)
            try:
                theirs = upstream_python_half(up, g2, p2, t, k)
            except Exception as e:  # noqa: BLE001
                theirs = {"error": type(e).__name__}
            n += 1
            if "reject" in theirs or "error" in theirs:
                ok = ours.get("reject") == theirs.get("reject") and ours.get("error") == theirs.get("error")
                kinds["reject" if "reject" in theirs else "error"] += 1
            else:
                kinds["payload"] += 1
                ok = ("reject" not in ours and "error" not in ours and theirs["op"] == "build" and theirs["dump"] is True
                      and ours["real"] == theirs["real"] and ours["worlds"] == theirs["worlds"]
                      and ours["opts"] == theirs["opts"] and ours["build_seed"] == theirs["seed"]
                      and ours["spec"] == theirs["spec"] and ours["fidelity"] == theirs["fid"]
                      and ours["attacked"] == theirs["attacked"]
                      and ours["human"] == human_form(up, theirs["labels"]))
            if not ok:
                bad += 1
                print(f"MISMATCH row {row} turn {t} {k}: ours {str(ours)[:200]} theirs {str(theirs)[:200]}",
                      file=sys.stderr)
    print(json.dumps({"checked": n, "mismatches": bad, "upstream_outcomes": dict(kinds)}))
    return 1 if bad else 0


def cmd_verify_games(a) -> int:
    """cmd_build's selection, run literally (iter_games, shuffle, dedupe, read_games, split, work()'s
    candidate order), against the games file."""
    up = Upstream(a.upstream, a.allow_unpinned)
    it, replay, pm = up.items, up.replay, up.pm
    pmap = pm.partner_map(pm.load_pairs())
    held = it.held_out_rows(pmap)

    def keep(g) -> bool:
        return it.is_top(g) and g.row_index not in held and pmap.get(g.row_index) not in held

    games = list(replay.iter_games(every=36, start=17, predicate=keep))
    rng = random.Random(it.SEED)
    rng.shuffle(games)
    used, uniq = set(), []
    for g in games:
        if g.row_index in used:
            continue
        used.add(g.row_index)
        if g.row_index in pmap:
            used.add(pmap[g.row_index])
        uniq.append(g)
    partners = replay.read_games(sorted({pmap[g.row_index] for g in uniq if g.row_index in pmap}))
    split_of = {g.row_index: ("dev" if rng.random() < it.DEV_SHARE else "test") for g in uniq}
    want = [{"index": i, "row": g.row_index, "split": split_of[g.row_index],
             "partner": pmap[g.row_index] if g.row_index in pmap else None, "candidates": candidates_of(up, g)}
            for i, g in enumerate(uniq)]
    with open(a.games) as f:
        f.readline()
        have = [json.loads(line) for line in f]
    bad = sum(x != y for x, y in zip(want, have)) + abs(len(want) - len(have))
    # the partner rows must parse to the same games as read_games gave
    p = Prep(up, a.games, a.rows)
    pbad = 0
    for r, g in partners.items():
        mine = up.replay.parse_game(next(csv.reader([p.lines[r]])), p.H, r)
        pbad += dataclasses.asdict(mine) != dataclasses.asdict(g)
    print(json.dumps({"games_upstream": len(want), "games_file": len(have), "mismatches": bad,
                      "partners_upstream": len(partners), "partner_mismatches": pbad}))
    return 1 if bad or pbad else 0


def cmd_pairs(a) -> int:
    up = Upstream(a.upstream, a.allow_unpinned)
    print(json.dumps(ensure_pairs(up)))
    return 0


def main(argv=None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--upstream", default=os.environ.get("SBREP_UPSTREAM", DEFAULT_UPSTREAM))
    ap.add_argument("--allow-unpinned", action="store_true")
    sub = ap.add_subparsers(dest="cmd", required=True)
    s = sub.add_parser("games")
    s.add_argument("--out", required=True)
    s.add_argument("--rows", default=None)
    s.add_argument("--every", type=int, default=36)
    s.add_argument("--start", type=int, default=17)
    for name in ("candidate", "serve", "batch", "verify", "verify-games"):
        c = sub.add_parser(name)
        c.add_argument("--games", required=True)
        c.add_argument("--rows", default=None)
        if name == "candidate":
            c.add_argument("row", type=int)
            c.add_argument("turn", type=int)
            c.add_argument("kind", choices=KINDS)
        if name == "serve":
            c.add_argument("--lru", type=int, default=8)
            c.add_argument("--warm", action="store_true", help="load the belief model before the first request")
        if name in ("batch", "verify"):
            c.add_argument("--first", type=int, required=True)
        if name == "batch":
            c.add_argument("--out", required=True)
            c.add_argument("--workers", type=int, default=4)
            c.add_argument("--maxtasks", type=int, default=None, help="games per worker before it is replaced")
            c.add_argument("--timing", default=None, help="per-candidate seconds (JSON lines; not deterministic)")
    sub.add_parser("pairs")
    a = ap.parse_args(argv)
    return {"games": cmd_games, "candidate": cmd_candidate, "serve": cmd_serve, "batch": cmd_batch,
            "verify": cmd_verify, "verify-games": cmd_verify_games, "pairs": cmd_pairs}[a.cmd](a)


if __name__ == "__main__":
    raise SystemExit(main())
