#!/usr/bin/env python3
"""mzgen.py -- the MageZero-style generation loop on gorge (DraftZero experiment #2a).

    scripts/mzgen.py RUN_DIR [--generations 17] [--games 112] [--sims 300]
        [--eval-interval 8] [--eval-pairs 100] [--train-decks a,b,..]
        [--eval-decks a,b,..] [--workers 2] [--seed 20261002]

RUN_DIR is a name or a path under /mnt/sata/gorge-training/mzrepro/runs/ (a
bare name is placed there). Nothing is written into the repository: the two
binaries are built once into RUN_DIR/bin and reused, so one engine commit
plays the whole run.

Naming. "Checkpoint g" (gen<g>/ckpt/gen<g>.gpol) is the value network trained
on the self-play of generations 0..g. "Player j" is who plays generation j's
self-play: player 0 is search with the frozen heuristic leaf (no network);
player j >= 1 is search whose leaf is checkpoint j-1's value head. So in
generation g the current side is player g, the "older" opponents are players
0..g-1, and "generation 0" as an opponent is player 0, the heuristic leaf.
The evaluation of generation g plays checkpoint g.

Per generation g:

  self-play  --games games on decks drawn (seeded) from --train-decks, every
             game a mirror. Generation 0: player 0 against player 0, both
             sides recorded. Generation g >= 1: 20% current vs current (both
             recorded), 10% current vs player 0, the rest current vs an older
             player drawn uniformly (seeded) per pair from 0..g-1; against a
             non-current opponent only the current side is recorded, and the
             games are seat-swapped pairs on one seed. Current vs current is
             single games on distinct seeds (botbench
             -spellbench-first-game-only): two seats of one deterministic
             policy replay the same game when swapped. Every seat:
             clairvoyant world, uniform prior, no noise, discount 0.99 per
             ply, --sims simulations.
  train      cmd/policytrain visits mode on every generation's corpora,
             oldest first: TD-lambda 0.95 value target, policy weight 0
             (value head only), newest 150,000 records, batch 64, 1 epoch
             (2 at generation 0), continuing from checkpoint g-1 (fresh at
             generation 0).
  eval       when g % --eval-interval == 0: checkpoint g (network leaf)
             against the heuristic leaf at the same --sims, --eval-pairs
             seat-swapped pairs cycling through --eval-decks, on one seed
             reused at every evaluation. RUN_DIR/RESULTS.md gets one line per
             evaluation: generation, wins, games, win rate, Wilson 95%.

Resumable: every stage leaves a marker (or its checkpoint) when it finishes;
a rerun skips finished stages and redoes an unfinished one from scratch.
RESULTS.md is rewritten from the stored evaluations, so a rerun never
duplicates a line. --generations may grow between runs; every other
parameter must stay what RUN_DIR/config.json recorded.

Every heavy command runs as
    GOMAXPROCS=2 GOMEMLIMIT=3GiB systemd-run --user --scope -q \
        -p MemoryMax=4G -p CPUQuota=200% <command>
one at a time. Python 3 standard library only.
"""

import argparse
import json
import math
import os
import shutil
import subprocess
import sys
import time

RUNS_ROOT = "/mnt/sata/gorge-training/mzrepro/runs"
REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
FDN = ["FDN01-UBG", "FDN02-WG", "FDN03-WB", "FDN04-WB", "FDN05-UG", "FDN06-RG", "FDN07-UR", "FDN08-WBR",
       "FDN09-BG", "FDN10-BR", "FDN11-BWR", "FDN12-UGW", "FDN13-BG", "FDN14-BR", "FDN15-UB", "FDN16-UG"]
CAP = ["systemd-run", "--user", "--scope", "-q", "-p", "MemoryMax=4G", "-p", "CPUQuota=200%"]
CAP_ENV = {"GOMAXPROCS": "2", "GOMEMLIMIT": "3GiB"}
MASK64 = (1 << 64) - 1

TD_LAMBDA = "0.95"
WINDOW = "150000"
BATCH = "64"
DISCOUNT = "0.99"


def splitmix(x):
    """SplitMix64's finaliser: the loop's only source of pseudo-randomness."""
    x = (x + 0x9E3779B97F4A7C15) & MASK64
    x = ((x ^ (x >> 30)) * 0xBF58476D1CE4E5B9) & MASK64
    x = ((x ^ (x >> 27)) * 0x94D049BB133111EB) & MASK64
    return x ^ (x >> 31)


def derive(seed, *parts):
    """A seed for (seed, parts...), each part an int or a short string."""
    x = splitmix(seed & MASK64)
    for p in parts:
        if isinstance(p, str):
            for b in p.encode():
                x = splitmix(x ^ b)
        else:
            x = splitmix(x ^ (p & MASK64))
    return x


class Stream:
    """A seeded stream of integers below n (rejection-free modulo is fine at these sizes)."""

    def __init__(self, seed):
        self.x = seed

    def below(self, n):
        self.x = splitmix(self.x)
        return self.x % n


def wilson(wins, n, z=1.959964):
    if n == 0:
        return 0.0, 1.0
    p = wins / n
    den = 1 + z * z / n
    mid = (p + z * z / (2 * n)) / den
    half = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / den
    return max(0.0, mid - half), min(1.0, mid + half)


def log(msg):
    print("mzgen: " + msg, flush=True)


class Run:
    def __init__(self, args):
        self.a = args
        self.dir = args.run_dir
        self.botbench = os.path.join(self.dir, "bin", "botbench")
        self.policytrain = os.path.join(self.dir, "bin", "policytrain")
        self.cards = os.path.join(REPO, ".cards")

    # -- plumbing ---------------------------------------------------------

    def heavy(self, cmd, logfile, cwd=None, stage=""):
        """Runs one capped command, its output to logfile; raises on failure."""
        env = dict(os.environ)
        env.update(CAP_ENV)
        t0 = time.time()
        with open(logfile, "w") as lf:
            lf.write("$ " + " ".join(cmd) + "\n")
            lf.flush()
            rc = subprocess.call(CAP + cmd, stdout=lf, stderr=subprocess.STDOUT, env=env, cwd=cwd or self.dir)
        wall = time.time() - t0
        with open(os.path.join(self.dir, "timings.jsonl"), "a") as tf:
            tf.write(json.dumps({"stage": stage, "wall_seconds": round(wall, 1), "rc": rc}) + "\n")
        if rc != 0:
            sys.exit("mzgen: %s failed (rc %d); see %s" % (stage, rc, logfile))
        return wall

    def gen_dir(self, g):
        return os.path.join(self.dir, "gen%02d" % g)

    def ckpt(self, g):
        return os.path.join(self.gen_dir(g), "ckpt", "gen%02d.gpol" % g)

    def player(self, name, j, corpus=None):
        """The -az-variant definition of player j under a variant name."""
        spec = "heuristic-leaf" if j == 0 else "checkpoint:" + self.ckpt(j - 1)
        if corpus:
            spec += ",corpus:" + corpus
        return "%s=%s" % (name, spec)

    def search_flags(self):
        return ["-dir", self.cards, "-workers", str(self.a.workers), "-spellbench-catalog", "fdn",
                "-spellbench-pairs", "1", "-spellbench-engine-version", self.commit,
                "-az-world", "clairvoyant", "-az-uniform-prior", "-az-no-noise", "-az-discount", DISCOUNT,
                "-az-sims", str(self.a.sims)]

    # -- setup ------------------------------------------------------------

    def setup(self):
        os.makedirs(self.dir, exist_ok=True)
        a = self.a
        cfg = {"games": a.games, "sims": a.sims, "eval_interval": a.eval_interval, "eval_pairs": a.eval_pairs,
               "train_decks": a.train_decks, "eval_decks": a.eval_decks, "seed": a.seed, "lr": a.lr}
        path = os.path.join(self.dir, "config.json")
        if os.path.exists(path):
            with open(path) as f:
                old = json.load(f)
            for k, v in cfg.items():
                if old.get(k) != v:
                    sys.exit("mzgen: %s was started with %s=%r, this call says %r; use a new run directory" % (self.dir, k, old.get(k), v))
            self.commit = old["commit"]
        else:
            self.commit = subprocess.check_output(["git", "-C", REPO, "rev-parse", "HEAD"], text=True).strip()
            dirty = subprocess.check_output(["git", "-C", REPO, "status", "--porcelain", "--untracked-files=no"], text=True).strip()
            cfg.update(commit=self.commit, dirty=bool(dirty), repo=REPO)
            with open(path, "w") as f:
                json.dump(cfg, f, indent=2)
                f.write("\n")
        if not (os.path.exists(self.botbench) and os.path.exists(self.policytrain)):
            bindir = os.path.join(self.dir, "bin")
            tmp = bindir + ".tmp"
            shutil.rmtree(tmp, ignore_errors=True)
            os.makedirs(tmp)
            log("building botbench and policytrain at %s" % self.commit[:12])
            self.heavy(["go", "build", "-o", tmp + "/", "./cmd/botbench", "./cmd/policytrain"],
                       os.path.join(self.dir, "build.log"), cwd=REPO, stage="build")
            shutil.rmtree(bindir, ignore_errors=True)
            os.rename(tmp, bindir)

    # -- self-play --------------------------------------------------------

    def matchups(self, g):
        """Generation g's self-play as (tag, opponent player or None for current, decks).

        A current-vs-current matchup plays ONE game per deck; the others a
        seat-swapped pair per deck."""
        pairs = self.a.games // 2
        decks = Stream(derive(self.a.seed, "decks", g))
        draw = lambda n: [self.a.train_decks[decks.below(len(self.a.train_decks))] for _ in range(n)]
        if g == 0:
            return [("self", None, draw(2 * pairs))]
        n_cc = max(1, round(0.2 * pairs))
        n_g0 = max(1, round(0.1 * pairs)) if pairs >= 3 else 0
        n_old = max(0, pairs - n_cc - n_g0)
        vs = [0] * g  # pairs against each older player
        vs[0] += n_g0
        opp = Stream(derive(self.a.seed, "opponents", g))
        for _ in range(n_old):
            vs[opp.below(g)] += 1
        out = [("self", None, draw(2 * n_cc))]
        for j in range(g):
            if vs[j]:
                out.append(("vs%02d" % j, j, draw(vs[j])))
        return out

    def corpora(self, g):
        """Generation g's corpus files in training order."""
        sp = os.path.join(self.gen_dir(g), "selfplay")
        out = []
        for tag, opp, _ in self.matchups(g):
            if opp is None:
                out += [os.path.join(sp, tag + ".a.jsonl.gz"), os.path.join(sp, tag + ".b.jsonl.gz")]
            else:
                out.append(os.path.join(sp, tag + ".cur.jsonl.gz"))
        return out

    def selfplay(self, g):
        sp = os.path.join(self.gen_dir(g), "selfplay")
        os.makedirs(sp, exist_ok=True)
        for tag, opp, decks in self.matchups(g):
            done = os.path.join(sp, tag + ".done")
            if os.path.exists(done):
                continue
            out = os.path.join(sp, tag)
            shutil.rmtree(out, ignore_errors=True)
            if opp is None:
                files = [os.path.join(sp, tag + ".a.jsonl.gz"), os.path.join(sp, tag + ".b.jsonl.gz")]
                variants = [self.player("a", g, files[0]), self.player("b", g, files[1])]
                bots = "az:a,az:b"
                games = len(decks)
            else:
                files = [os.path.join(sp, tag + ".cur.jsonl.gz")]
                variants = [self.player("cur", g, files[0]), self.player("old", opp)]
                bots = "az:cur,az:old"
                games = 2 * len(decks)
            for f in files:
                if os.path.exists(f):
                    os.remove(f)
            cmd = [self.botbench, "-spellbench", bots, "-spellbench-out", out, "-spellbench-decks", ",".join(decks),
                   "-spellbench-base-seed", str(derive(self.a.seed, "selfplay", g, tag) & ((1 << 53) - 1))]
            if opp is None:
                cmd.append("-spellbench-first-game-only")
            for v in variants:
                cmd += ["-az-variant", v]
            cmd += self.search_flags()
            log("gen %d self-play %s: %d games" % (g, tag, games))
            wall = self.heavy(cmd, os.path.join(sp, tag + ".log"), stage="gen%02d/selfplay/%s" % (g, tag))
            missing = [f for f in files if not os.path.exists(f)]
            if missing:
                sys.exit("mzgen: gen %d self-play %s wrote no corpus %s" % (g, tag, missing))
            with open(done, "w") as f:
                f.write("%d games, %.1f s\n" % (games, wall))

    # -- train ------------------------------------------------------------

    def train(self, g):
        ck = self.ckpt(g)
        if os.path.exists(ck):
            return
        os.makedirs(os.path.dirname(ck), exist_ok=True)
        files = [f for k in range(g + 1) for f in self.corpora(k)]
        tmp = ck + ".tmp"
        cmd = [self.policytrain, "-visits-corpus", ",".join(files), "-out", tmp,
               "-epochs", "2" if g == 0 else "1", "-batch", BATCH, "-lr", str(self.a.lr), "-seed", str(self.a.seed + g),
               "-holdout", "0", "-value-weight", "1", "-visits-td-lambda", TD_LAMBDA, "-visits-policy-weight", "0",
               "-visits-window", WINDOW]
        if g > 0:
            cmd += ["-init", self.ckpt(g - 1)]
        log("gen %d train on %d corpus files" % (g, len(files)))
        self.heavy(cmd, os.path.join(os.path.dirname(ck), "train.log"), stage="gen%02d/train" % g)
        os.rename(tmp, ck)

    # -- eval -------------------------------------------------------------

    def evaluate(self, g):
        ev = os.path.join(self.gen_dir(g), "eval")
        result = os.path.join(ev, "result.json")
        if os.path.exists(result):
            return
        shutil.rmtree(ev, ignore_errors=True)
        os.makedirs(ev)
        decks = [self.a.eval_decks[i % len(self.a.eval_decks)] for i in range(self.a.eval_pairs)]
        out = os.path.join(ev, "out")
        cmd = [self.botbench, "-spellbench", "az:net,az:heur", "-spellbench-out", out,
               "-spellbench-decks", ",".join(decks),
               "-spellbench-base-seed", str(derive(self.a.seed, "eval") & ((1 << 53) - 1)),
               "-az-variant", "net=checkpoint:" + self.ckpt(g), "-az-variant", "heur=heuristic-leaf"] + self.search_flags()
        log("gen %d eval: %d games" % (g, 2 * len(decks)))
        self.heavy(cmd, os.path.join(ev, "eval.log"), stage="gen%02d/eval" % g)
        wins = losses = draws = excluded = 0
        with open(os.path.join(out, "games.jsonl")) as f:
            for line in f:
                row = json.loads(line)
                res = row["result"]
                if res == "draw":
                    draws += 1
                elif res in ("p0 wins", "p1 wins"):
                    if row["p" + res[1]] == "az:net":
                        wins += 1
                    else:
                        losses += 1
                else:
                    excluded += 1  # truncated or halted: not a game result
        with open(result + ".tmp", "w") as f:
            json.dump({"generation": g, "wins": wins, "losses": losses, "draws": draws, "excluded": excluded}, f)
            f.write("\n")
        os.rename(result + ".tmp", result)

    def results(self, upto):
        lines = ["# mzgen results: checkpoint g (value-head leaf) vs heuristic-leaf search, %d sims, clairvoyant, uniform prior" % self.a.sims,
                 "", "games = wins + losses + draws (truncated or halted games are excluded and counted last); win rate = wins / games.", "",
                 "| generation | wins | games | win rate | Wilson 95% | draws | excluded |", "|---|---|---|---|---|---|---|"]
        for g in range(upto + 1):
            path = os.path.join(self.gen_dir(g), "eval", "result.json")
            if not os.path.exists(path):
                continue
            with open(path) as f:
                r = json.load(f)
            n = r["wins"] + r["losses"] + r["draws"]
            lo, hi = wilson(r["wins"], n)
            rate = r["wins"] / n if n else 0.0
            lines.append("| %d | %d | %d | %.1f%% | %.1f%% - %.1f%% | %d | %d |" % (g, r["wins"], n, 100 * rate, 100 * lo, 100 * hi, r["draws"], r["excluded"]))
        tmp = os.path.join(self.dir, "RESULTS.md.tmp")
        with open(tmp, "w") as f:
            f.write("\n".join(lines) + "\n")
        os.rename(tmp, os.path.join(self.dir, "RESULTS.md"))

    def main(self):
        self.setup()
        for g in range(self.a.generations):
            self.selfplay(g)
            self.train(g)
            if g % self.a.eval_interval == 0:
                self.evaluate(g)
                self.results(g)
        self.results(self.a.generations - 1)
        log("done: %s" % os.path.join(self.dir, "RESULTS.md"))


def decks(s):
    out = [d.strip() for d in s.split(",") if d.strip()]
    if not out:
        raise argparse.ArgumentTypeError("an empty deck list")
    return out


def main():
    p = argparse.ArgumentParser(description="MageZero-style generation loop on gorge (see the file header).")
    p.add_argument("run_dir", help="run directory, a name or a path under " + RUNS_ROOT)
    p.add_argument("--generations", type=int, default=17, help="generations to run, 0..N-1 (default 17)")
    p.add_argument("--games", type=int, default=112, help="self-play games per generation, even (default 112)")
    p.add_argument("--sims", type=int, default=300, help="simulations per searched decision (default 300)")
    p.add_argument("--eval-interval", type=int, default=8, help="evaluate every this many generations, generation 0 included (default 8)")
    p.add_argument("--eval-pairs", type=int, default=100, help="seat-swapped pairs per evaluation (default 100)")
    p.add_argument("--train-decks", type=decks, default=FDN[:12], help="comma list of fdn catalog deck ids for self-play (default FDN01..FDN12)")
    p.add_argument("--eval-decks", type=decks, default=FDN[12:], help="comma list of fdn catalog deck ids for evaluation (default FDN13..FDN16)")
    p.add_argument("--workers", type=int, default=2, help="botbench game workers (default 2)")
    p.add_argument("--seed", type=int, default=20261002, help="run seed: decks, opponents, game seeds, training (default 20261002)")
    p.add_argument("--lr", type=float, default=0.1, help="policytrain learning rate (default 0.1, the trainer's)")
    a = p.parse_args()
    if os.sep not in a.run_dir:
        a.run_dir = os.path.join(RUNS_ROOT, a.run_dir)
    a.run_dir = os.path.abspath(a.run_dir)
    if not (a.run_dir + os.sep).startswith(RUNS_ROOT + os.sep):
        sys.exit("mzgen: the run directory must be under " + RUNS_ROOT)
    if a.games < 2 or a.games % 2 or a.generations < 1 or a.sims < 1 or a.eval_interval < 1 or a.eval_pairs < 1 or a.workers < 1:
        sys.exit("mzgen: --games must be even and >= 2; the other counts >= 1")
    Run(a).main()


if __name__ == "__main__":
    main()
