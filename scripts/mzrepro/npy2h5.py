#!/usr/bin/env python3
"""Convert a gorge training shard (four .npy files) to the HDF5 file MageZero reads.

gorge's internal/mzbridge writes one shard as

    <stem>.indices.npy       int32   [nnz]
    <stem>.offsets.npy       int64   [N+1]
    <stem>.row.npy           float32 [N, A+4]
    <stem>.game_offsets.npy  int64   [G+1]

and this script stores them as /indices, /offsets, /row and /game_offsets in one HDF5 file,
uncompressed and chunked, which is the layout XMage's LabeledStateWriter.java produces and
MageZero's dataset.H5Indexed loads. Every invariant either side relies on is checked first, so
a bad shard is refused here instead of being skipped with a warning (or silently accepted) by
the trainer.

    npy2h5.py [--dim A] [--remove] <stem> <out.hdf5>

Needs numpy and h5py (the MageZero environment has both).
"""
import argparse
import os
import sys

import h5py
import numpy as np

FEATURE_BINS = 2**31 - 1          # Features.TABLE_SIZE == model.GLOBAL_MAX
ACTION_TYPES = (0, 3, 5)          # PRIORITY, CHOOSE_TARGET, CHOOSE_USE: the ones upstream records
PARTS = ("indices", "offsets", "row", "game_offsets")
DTYPES = {"indices": "<i4", "offsets": "<i8", "row": "<f4", "game_offsets": "<i8"}


class BadShard(ValueError):
    pass


def check(cond, msg):
    if not cond:
        raise BadShard(msg)


def load(stem):
    parts = {}
    for name in PARTS:
        a = np.load(f"{stem}.{name}.npy", allow_pickle=False)
        check(a.dtype == np.dtype(DTYPES[name]), f"{name}: dtype {a.dtype}, want {DTYPES[name]}")
        parts[name] = a
    return parts


def validate(parts, dim=None):
    idx, off, row, games = (parts[n] for n in PARTS)
    check(idx.ndim == 1 and off.ndim == 1 and games.ndim == 1, "indices, offsets and game_offsets must be 1-d")
    check(row.ndim == 2, f"row must be 2-d, got shape {row.shape}")
    n, width = row.shape
    check(width > 4, f"row width {width} leaves no policy columns")
    a = width - 4
    if dim is not None:
        check(a == dim, f"policy width {a}, want {dim}")

    # the checks H5Indexed makes (dataset.py:56), plus the ones it does not
    check(off.shape[0] == n + 1, f"{off.shape[0]} offsets for {n} rows (want N+1)")
    check(off[0] == 0, "offsets[0] != 0")
    check((np.diff(off) >= 0).all(), "offsets decrease")
    check(off[-1] == idx.shape[0], f"offsets end at {off[-1]}, indices has {idx.shape[0]}")
    if idx.size:
        check(idx.min() >= 0 and idx.max() < FEATURE_BINS, "feature id outside [0, 2^31-1)")
        # each state is a sorted set: strictly increasing except across a state boundary
        rising = np.ones(idx.shape[0], dtype=bool)
        rising[1:] = idx[1:] > idx[:-1]
        starts = off[:-1][np.diff(off) > 0]
        rising[starts] = True
        check(rising.all(), "a state's feature ids are not strictly increasing")

    check(np.isfinite(row).all(), "row holds NaN or inf")
    policy, value, is_player, atype = row[:, :a], row[:, a], row[:, a + 2], row[:, a + 3]
    check((policy >= 0).all(), "negative visit count")
    check((np.abs(value) <= 1).all(), "value target outside [-1, 1]")
    check(np.isin(is_player, (0.0, 1.0)).all(), "isPlayer is not 0/1")
    check(np.isin(atype, ACTION_TYPES).all(), f"action type outside {ACTION_TYPES}")
    check((policy[atype == 5][:, 2:] == 0).all(), "CHOOSE_USE row with visits past index 1")

    check(games.shape[0] >= 1 and games[0] == 0, "game_offsets[0] != 0")
    check((np.diff(games) >= 0).all(), "game_offsets decrease")
    check(games[-1] == n, f"game_offsets end at {games[-1]}, want {n} rows")
    return n, a


def write(parts, out):
    idx, off, row, games = (parts[n] for n in PARTS)
    tmp = out + ".tmp"
    with h5py.File(tmp, "w") as f:
        # extendable, chunked, uncompressed: the storage LabeledStateWriter.java asks for
        f.create_dataset("indices", data=idx, dtype="<i4", maxshape=(None,), chunks=(1_000_000,))
        f.create_dataset("offsets", data=off, dtype="<i8", maxshape=(None,), chunks=(2048,))
        f.create_dataset("game_offsets", data=games, dtype="<i8", maxshape=(None,), chunks=(512,))
        f.create_dataset("row", data=row, dtype="<f4", maxshape=(None, row.shape[1]), chunks=(2048, row.shape[1]))
    os.replace(tmp, out)     # the loop moves shards between directories; never expose a partial file


def main(argv=None):
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("stem", help="path prefix of the four .npy files")
    ap.add_argument("out", help="HDF5 file to write")
    ap.add_argument("--dim", type=int, help="required policy width A (1024 for FDN_SPG.tsv)")
    ap.add_argument("--remove", action="store_true", help="delete the .npy files once the HDF5 file is written")
    args = ap.parse_args(argv)
    try:
        parts = load(args.stem)
        n, a = validate(parts, args.dim)
    except (BadShard, OSError, ValueError) as e:
        print(f"npy2h5: {args.stem}: {e}", file=sys.stderr)
        return 1
    write(parts, args.out)
    if args.remove:
        for name in PARTS:
            os.remove(f"{args.stem}.{name}.npy")
    print(f"npy2h5: {args.out}: {n} states, {parts['indices'].shape[0]} feature ids, "
          f"{parts['game_offsets'].shape[0] - 1} games, A={a}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
