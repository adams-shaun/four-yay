#!/usr/bin/env bash
# dzrun.sh -- run draft-zero's own training loop with gorge as the game engine.
#
#   dzrun.sh setup <run_root> <loop_config.yml>   lay out a run root
#   dzrun.sh loop  <run_root> [loop args...]      run draftzero.loop in it (default: --fresh)
#   dzrun.sh env   <run_root>                     print the environment `loop` uses
#
# draft-zero's loop works on paths relative to its working directory (runs/,
# data/, models/, configs/, assets/, .mz_tmp/), so a run lives in a run root
# that holds what the loop reads:
#
#   configs/loop.yml           the loop config (a copy of <loop_config.yml>)
#   configs/game.yml           draft-zero's base game config, unchanged
#   configs/curriculum_*.yml   draft-zero's curricula, unchanged
#   assets/decks.tsv           the deck table (pools.meta)
#   assets/reference/          draft-zero's 17lands reference, unchanged
#   data/pools/{train,eval}.txt  deck stems
#   xmage/                     empty: the loop only uses it as the engine's cwd
#
# Nothing in draft-zero or MageZero is modified. The engine is swapped by
# MZ_JAVA alone (scripts/mzrepro/mzjava).
#
# Locations, each overridable from the environment:
#   DZ_DIR        the draft-zero checkout
#   MZ_VENV       the Python environment with magezero, draftzero, torch, h5py
#   MZ_POOL_DIR   the deck pool built by build_pool.py (deckgen/ and pools/)
#   MZ_SELFPLAY_BIN  the built cmd/mzselfplay (default <repo>/bin/mzselfplay)
#   MZ_GPU        CUDA_VISIBLE_DEVICES for train.py / server.py ("" = CPU)
set -euo pipefail

here=$(cd "$(dirname "$(readlink -f "${BASH_SOURCE[0]}")")" && pwd)
repo=$(cd "$here/../.." && pwd)
DZ_DIR=${DZ_DIR:-/mnt/sata/gorge-training/searchbench/draft-zero}
MZ_VENV=${MZ_VENV:-/mnt/sata/gorge-training/mzrepro/venv}
MZ_POOL_DIR=${MZ_POOL_DIR:-/mnt/sata/gorge-training/mzrepro/pool}
deck_dir=$(echo "$MZ_POOL_DIR"/deckgen/*/top_player_*_decks)

usage() { sed -n '2,8p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 2; }
[ "$#" -ge 2 ] || usage
cmd=$1; root=$2; shift 2

run_env() {
	export MZ_JAVA="$here/mzjava"
	export MZ_XMAGE_DIR="$root/xmage"
	export MZ_DECK_DIR="$deck_dir"
	export MZ_ACTION_VOCAB="$DZ_DIR/assets/vocab/FDN_SPG.tsv"
	export MZ_SELFPLAY_BIN="${MZ_SELFPLAY_BIN:-$repo/bin/mzselfplay}"
	export MZ_GORGE_CARDS="${MZ_GORGE_CARDS:-$repo/.cards}"
	export MZ_PYTHON="$MZ_VENV/bin/python"
	export MZ_NPY2H5="$here/npy2h5.py"
	export CUDA_VISIBLE_DEVICES="${MZ_GPU-1}"
	export PYTHONUNBUFFERED=1
}

case "$cmd" in
setup)
	[ "$#" -eq 1 ] || usage
	cfg=$1
	mkdir -p "$root"/{configs,assets,data/pools,xmage}
	cp "$cfg" "$root/configs/loop.yml"
	cp "$DZ_DIR"/configs/game.yml "$DZ_DIR"/configs/curriculum*.yml "$root/configs/"
	cp "$MZ_POOL_DIR/pools/decks.tsv" "$root/assets/decks.tsv"
	cp -r "$DZ_DIR/assets/reference" "$root/assets/"
	cp "$MZ_POOL_DIR/pools/train.txt" "$MZ_POOL_DIR/pools/eval.txt" "$root/data/pools/"
	echo "run root $root ready: $(wc -l < "$root/data/pools/train.txt") train decks, $(wc -l < "$root/data/pools/eval.txt") eval decks"
	;;
env)
	run_env
	env | grep -E '^(MZ_|CUDA_VISIBLE_DEVICES=)' | sort
	;;
loop)
	run_env
	[ -x "$MZ_SELFPLAY_BIN" ] || { echo "dzrun: build the engine first: go build -o $MZ_SELFPLAY_BIN $repo/cmd/mzselfplay" >&2; exit 2; }
	cd "$root"
	[ "$#" -gt 0 ] || set -- --fresh
	exec "$MZ_VENV/bin/python" -u -m draftzero.loop --config configs/loop.yml "$@"
	;;
*) usage ;;
esac
