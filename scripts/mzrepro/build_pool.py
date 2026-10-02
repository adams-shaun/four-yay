#!/usr/bin/env python3
"""Build DraftZero experiment #2a's deck pool from a local 17lands file, with draft-zero's own code.

draft-zero builds its pool in two steps (README, tools/extract_decks.py, src/draftzero/pools.py):

    python tools/extract_decks.py --set FDN --format PremierDraft --min-winrate 0.60 --dck ...
    python -m draftzero.pools --decks data/deckgen/FDN_PremierDraft_wr60 --out data/pools/

This script runs the same functions (extract, write_dck, summarize; pools.is_eval) from an
unmodified draft-zero checkout, with two differences forced by this machine:

  * the input is a 17lands file already on disk (--data), not the one extract_decks.fetch
    would download into the checkout;
  * write_dck needs XMage's collector numbers, which extract_decks reads out of XMage's set
    jar with javap. There is no XMage here and gorge resolves cards by name, so every card
    is given the placeholder printing [<set>:0]. Upstream skips a deck holding a card XMage's
    Foundations / Special Guests sets lack; here nothing is skipped at this step, and the
    reference deck table (--reference, draft-zero's assets/decks.tsv) decides which decks
    are in the pool, so the pool is exactly upstream's.

Output, under --out (keep it OUTSIDE any repository: decks.jsonl carries raw 17lands draft ids):

    deckgen/<set>_<format>_wr<NN>/decks.jsonl, summary.txt, top_player_<set>_decks/*.dck
    pools/train.txt, pools/eval.txt   deck stems, the form draft-zero's loop reads
    pools/decks.tsv                   deck -> split, colours, player bucket (the loop's pools.meta)
    pools/REPORT.txt                  counts, and the comparison with the reference table
"""
import argparse
import importlib.util
import json
import os
import sys


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--draft-zero", required=True, help="an unmodified draft-zero checkout")
    ap.add_argument("--data", required=True, help="17lands game or replay data .csv.gz (it must carry the deck_* columns)")
    ap.add_argument("--out", required=True)
    ap.add_argument("--set", default="FDN")
    ap.add_argument("--format", default="PremierDraft")
    ap.add_argument("--min-winrate", type=float, default=0.60)
    ap.add_argument("--min-player-games", type=int, default=0)
    ap.add_argument("--eval-frac", type=float, default=0.10)
    ap.add_argument("--reference", help="draft-zero's assets/decks.tsv: keep exactly its decks and check the split against it")
    args = ap.parse_args()

    ed = load(os.path.join(args.draft_zero, "tools", "extract_decks.py"), "extract_decks")
    pools = load(os.path.join(args.draft_zero, "src", "draftzero", "pools.py"), "dz_pools")

    decks, rows_seen, rows_kept = ed.extract(args.data, args.min_winrate, args.min_player_games)
    decks.sort(key=lambda d: (d["draft_time"], d["draft_id"], d["build_index"]))      # extract_decks.main's order
    out = os.path.join(args.out, "deckgen", f"{args.set}_{args.format}_wr{int(args.min_winrate * 100)}")
    os.makedirs(out, exist_ok=True)
    numbers = {c: (args.set, "0") for d in decks for c in d["cards"]}
    dck_dir = os.path.join(out, f"top_player_{args.set}_decks")
    written, missing = ed.write_dck(decks, dck_dir, args.set, numbers)
    with open(os.path.join(out, "decks.jsonl"), "w") as f:
        for d in decks:
            f.write(json.dumps(d) + "\n")
    summary = ed.summarize(decks, rows_seen, rows_kept, args)
    with open(os.path.join(out, "summary.txt"), "w") as f:
        f.write(summary)

    reference = {}
    if args.reference:
        lines = open(args.reference).read().splitlines()
        header = lines[0].split("\t")
        for line in lines[1:]:
            if line.strip():
                v = dict(zip(header, line.split("\t")))
                reference[v["deck"]] = v

    report = [f"{args.set} {args.format}: {rows_seen:,} game rows in {args.data}",
              f"{rows_kept:,} rows from players with win-rate bucket >= {args.min_winrate:.2f}",
              f"{len(decks):,} unique decks (draft_id + build_index); {written:,} .dck files written to {dck_dir}"]
    split = {"train": [], "eval": []}
    rows, agree, disagree, outside = [], 0, 0, 0
    for d in decks:
        if "dck_file" not in d:
            continue
        stem = d["dck_file"][:-4]
        mine = "eval" if pools.is_eval(d["draft_id"], args.eval_frac) else "train"
        if reference:
            ref = reference.get(stem)
            if ref is None:
                outside += 1          # a deck upstream skipped: not in its pool
                continue
            agree += ref["split"] == mine
            disagree += ref["split"] != mine
        colors = d["main_colors"] + (f"+{d['splash_colors']}" if d["splash_colors"] else "")
        split[mine].append(stem)
        rows.append((stem, mine, colors, d["main_colors"], str(d["user_game_win_rate_bucket"])))
    pool_dir = os.path.join(args.out, "pools")
    os.makedirs(pool_dir, exist_ok=True)
    for name, stems in split.items():
        with open(os.path.join(pool_dir, f"{name}.txt"), "w") as f:
            f.write("\n".join(sorted(stems)) + "\n")
    with open(os.path.join(pool_dir, "decks.tsv"), "w") as f:
        f.write("deck\tsplit\tcolors\tmain_colors\tplayer_wr_bucket\n")
        f.writelines("\t".join(r) + "\n" for r in rows)
    report.append(f"pool: {len(split['train']):,} train decks, {len(split['eval']):,} eval decks "
                  f"(pools.is_eval, eval fraction {args.eval_frac})")
    status = 0
    if reference:
        produced = {d["dck_file"][:-4] for d in decks if "dck_file" in d}
        absent = sorted(set(reference) - produced)
        ref_split = {s: sum(v["split"] == s for v in reference.values()) for s in ("train", "eval")}
        report += [f"reference {args.reference}: {len(reference):,} decks ({ref_split['train']:,} train, {ref_split['eval']:,} eval)",
                   f"  {len(reference) - len(absent):,} of them produced here with the same stem; {len(absent):,} absent",
                   f"  split agrees for {agree:,}, disagrees for {disagree:,}",
                   f"  {outside:,} decks produced here are not in the reference (upstream skipped them: a card outside its XMage sets) and are left out of the pool"]
        if absent or disagree:
            status = 1
            report.append("  MISMATCH with the reference: " + ", ".join(absent[:10]))
    text = "\n".join(report) + "\n"
    with open(os.path.join(pool_dir, "REPORT.txt"), "w") as f:
        f.write(text)
    print(text, end="")
    return status


if __name__ == "__main__":
    sys.exit(main())
